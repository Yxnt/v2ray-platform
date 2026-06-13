package cloudfront

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type AWSClientConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Region          string
	Endpoint        string
	HTTPClient      *http.Client
	Now             func() time.Time
	Sleep           func(context.Context, time.Duration) error
}

type AWSClient struct {
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	region          string
	endpoint        string
	httpClient      *http.Client
	now             func() time.Time
	sleep           func(context.Context, time.Duration) error
}

// AWS managed cache policy "CachingDisabled"; websocket proxy traffic should
// not be cached, and CloudFront requires either CachePolicyId or ForwardedValues.
const managedCachingDisabledPolicyID = "4135ea2d-6df8-44a3-9df3-4b5a84be39ad"
const routeRewriteFunctionName = "v2ray-platform-route-rewrite"
const cloudFrontSigningRegion = "us-east-1"

func NewAWSClient(cfg AWSClientConfig) (*AWSClient, error) {
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("cloudfront: access key id and secret access key are required")
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = "https://cloudfront.amazonaws.com"
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	return &AWSClient{
		accessKeyID:     cfg.AccessKeyID,
		secretAccessKey: cfg.SecretAccessKey,
		sessionToken:    cfg.SessionToken,
		// CloudFront is a global service, but its public REST endpoint is signed
		// with us-east-1. Keep cfg.Region for stored UI compatibility; do not let
		// operator-entered node/AWS regions break CloudFront SigV4 requests.
		region:     cloudFrontSigningRegion,
		endpoint:   strings.TrimRight(endpoint, "/"),
		httpClient: httpClient,
		now:        now,
		sleep:      sleep,
	}, nil
}

func (c *AWSClient) ListDistributions(ctx context.Context) ([]DistributionSummary, error) {
	out := make([]DistributionSummary, 0)
	needsTenantLookup := false
	marker := ""
	for {
		path := "/2020-05-31/distribution"
		if marker != "" {
			path += "?Marker=" + url.QueryEscape(marker)
		}
		resp, err := c.do(ctx, http.MethodGet, path, nil, "")
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			err := readCloudFrontError(resp)
			resp.Body.Close()
			return nil, err
		}

		var payload distributionListResponse
		if err := xml.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("cloudfront: decode distribution list: %w", err)
		}
		resp.Body.Close()

		for _, item := range payload.Items.Items {
			aliases := append([]string(nil), item.Aliases.Items...)
			displayDomain := preferredDistributionDomain(item.DomainName, aliases)
			if displayDomain == "" {
				needsTenantLookup = true
			}
			out = append(out, DistributionSummary{
				DistributionID: item.ID,
				DomainName:     displayDomain,
				Status:         item.Status,
				Comment:        item.Comment,
				Aliases:        aliases,
			})
		}
		if !payload.IsTruncated {
			break
		}
		marker = strings.TrimSpace(payload.NextMarker)
		if marker == "" {
			return nil, fmt.Errorf("cloudfront: distribution list is truncated but NextMarker is missing")
		}
	}
	if needsTenantLookup {
		tenantDomainsByDistribution, err := c.listAllDistributionTenantDomains(ctx)
		if err == nil {
			for i := range out {
				if out[i].DomainName != "" {
					continue
				}
				out[i].Aliases = mergeDistributionDomains(out[i].Aliases, tenantDomainsByDistribution[out[i].DistributionID])
				out[i].DomainName = preferredDistributionDomain("", out[i].Aliases)
			}
		}
	}
	return out, nil
}

func (c *AWSClient) GetDistribution(ctx context.Context, distributionID string) (*DistributionState, error) {
	resp, err := c.do(ctx, http.MethodGet, "/2020-05-31/distribution/"+url.PathEscape(distributionID), nil, "")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, readCloudFrontError(resp)
	}

	var payload distributionResponse
	if err := xml.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("cloudfront: decode distribution: %w", err)
	}

	dist := distributionStateFromResponse(payload)
	if dist.DomainName == "" {
		tenantDomainsByDistribution, err := c.listAllDistributionTenantDomains(ctx)
		if err == nil {
			dist.Aliases = mergeDistributionDomains(dist.Aliases, tenantDomainsByDistribution[distributionID])
			dist.DomainName = preferredDistributionDomain("", dist.Aliases)
		} else if isCloudFrontAccessDenied(err) {
			return nil, fmt.Errorf("cloudfront: distribution tenant domains require cloudfront:ListDistributionTenants permission: %w", err)
		}
	}
	return dist, nil
}

func (c *AWSClient) CreateDistribution(ctx context.Context, input CreateDistributionInput) (*DistributionState, error) {
	cfg := newManagedDistributionConfig(input)
	cfg.XMLNS = "http://cloudfront.amazonaws.com/doc/2020-05-31/"
	rewrites := rewriteRoutesFromCreateInput(input)
	if len(rewrites) > 0 {
		functionARN, err := c.ensureRouteRewriteFunction(ctx, rewrites)
		if err != nil {
			return nil, err
		}
		cfg.attachRouteRewriteFunction(rewrites, functionARN)
	}
	body, err := xml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cloudfront: encode distribution create config: %w", err)
	}

	resp, err := c.do(ctx, http.MethodPost, "/2020-05-31/distribution", body, "application/xml")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, readCloudFrontError(resp)
	}

	var payload distributionResponse
	if err := xml.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("cloudfront: decode created distribution: %w", err)
	}
	return distributionStateFromResponse(payload), nil
}

func distributionStateFromResponse(payload distributionResponse) *DistributionState {
	aliases := aliasItems(payload.DistributionConfig.Aliases)
	displayDomain := preferredDistributionDomain(payload.DomainName, aliases)
	dist := &DistributionState{
		DistributionID: payload.ID,
		DomainName:     displayDomain,
		Status:         payload.Status,
		Comment:        payload.DistributionConfig.Comment,
		Aliases:        aliases,
		Origins:        make([]OriginState, 0, len(payload.DistributionConfig.Origins.Items)),
		Behaviors:      make([]BehaviorState, 0, len(payload.DistributionConfig.CacheBehaviors.Items)),
		Parameters:     nil,
	}
	for _, origin := range payload.DistributionConfig.Origins.Items {
		dist.Origins = append(dist.Origins, OriginState{
			OriginID:   origin.ID,
			DomainName: origin.DomainName,
		})
	}
	if strings.TrimSpace(payload.DistributionConfig.DefaultCacheBehavior.TargetOriginID) != "" {
		dist.Behaviors = append(dist.Behaviors, BehaviorState{
			PathPattern: "/",
			OriginID:    payload.DistributionConfig.DefaultCacheBehavior.TargetOriginID,
			IsDefault:   true,
		})
	}
	for _, behavior := range payload.DistributionConfig.CacheBehaviors.Items {
		dist.Behaviors = append(dist.Behaviors, BehaviorState{
			PathPattern: behavior.PathPattern,
			OriginID:    behavior.TargetOriginID,
			IsDefault:   false,
		})
	}
	if payload.DistributionConfig.TenantConfig != nil {
		dist.Parameters = make([]ParameterDefinitionState, 0, len(payload.DistributionConfig.TenantConfig.ParameterDefinitions.all()))
		for _, parameter := range payload.DistributionConfig.TenantConfig.ParameterDefinitions.all() {
			dist.Parameters = append(dist.Parameters, ParameterDefinitionState{
				Name:         strings.TrimSpace(parameter.Name),
				Required:     parameter.Definition.StringSchema.Required,
				DefaultValue: strings.TrimSpace(parameter.Definition.StringSchema.DefaultValue),
				Comment:      strings.TrimSpace(parameter.Definition.StringSchema.Comment),
			})
		}
	}
	return dist
}

func aliasItems(aliases *aliasesXML) []string {
	if aliases == nil {
		return nil
	}
	return aliases.Items
}

func preferredDistributionDomain(domainName string, aliases []string) string {
	domainName = normalizeDistributionDomain(domainName)
	if domainName != "" {
		return domainName
	}
	for _, alias := range aliases {
		alias = normalizeDistributionDomain(alias)
		if alias != "" {
			return alias
		}
	}
	return ""
}

func normalizeDistributionDomain(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return ""
	}
	return value
}

func isCloudFrontAccessDenied(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "AccessDenied")
}

func isCloudFrontPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "PreconditionFailed")
}

func mergeDistributionDomains(existing []string, extras []string) []string {
	if len(existing) == 0 && len(extras) == 0 {
		return nil
	}
	out := make([]string, 0, len(existing)+len(extras))
	seen := make(map[string]struct{}, len(existing)+len(extras))
	for _, value := range append(append([]string(nil), existing...), extras...) {
		normalized := normalizeDistributionDomain(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func (c *AWSClient) listAllDistributionTenantDomains(ctx context.Context) (map[string][]string, error) {
	marker := ""
	out := make(map[string][]string)
	for {
		reqBody, err := xml.Marshal(listDistributionTenantsRequestXML{
			XMLNS:  "http://cloudfront.amazonaws.com/doc/2020-05-31/",
			Marker: marker,
		})
		if err != nil {
			return nil, fmt.Errorf("cloudfront: encode distribution tenants request: %w", err)
		}
		resp, err := c.do(ctx, http.MethodPost, "/2020-05-31/distribution-tenants", reqBody, "application/xml")
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			err := readCloudFrontError(resp)
			resp.Body.Close()
			return nil, err
		}
		var payload listDistributionTenantsResponseXML
		if err := xml.NewDecoder(resp.Body).Decode(&payload); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("cloudfront: decode distribution tenants: %w", err)
		}
		resp.Body.Close()
		for _, tenant := range payload.DistributionTenantList.Items {
			domains := make([]string, 0, len(tenant.Domains.Items))
			for _, domain := range tenant.Domains.Items {
				domains = append(domains, domain.Domain)
			}
			out[tenant.DistributionID] = mergeDistributionDomains(out[tenant.DistributionID], domains)
		}
		next := strings.TrimSpace(payload.NextMarker)
		if next == "" {
			break
		}
		marker = next
	}
	return out, nil
}

func (c *AWSClient) ApplyDistributionRoutes(ctx context.Context, distributionID string, actions []RouteAction, rewrites []RewriteRoute) error {
	actionable := make([]RouteAction, 0, len(actions))
	for _, action := range actions {
		if strings.TrimSpace(action.Action) == "" || action.Action == "noop" {
			continue
		}
		actionable = append(actionable, action)
	}
	if len(actionable) == 0 && len(rewrites) == 0 {
		return nil
	}

	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		cfg, etag, err := c.getDistributionConfig(ctx, distributionID)
		if err != nil {
			return err
		}
		currentBody, err := canonicalDistributionConfigXML(cfg)
		if err != nil {
			return err
		}
		log.Printf("cloudfront distribution update attempt=%d distribution_id=%s etag=%q actionable_routes=%d rewrites=%d", attempt+1, distributionID, etag, len(actionable), len(rewrites))

		behaviorTemplate := cfg.defaultBehaviorTemplate()
		for _, action := range actionable {
			switch action.Action {
			case "add_route", "replace_route":
				if !isManagedOriginID(action.OriginID) {
					return fmt.Errorf("cloudfront: refusing to manage route %q with unmanaged origin %q", routePattern(action.RouteKey), action.OriginID)
				}
				if existing, ok := cfg.behaviorForRouteKey(action.RouteKey); ok && existing.TargetOriginID != action.OriginID && !isManagedOriginID(existing.TargetOriginID) {
					return fmt.Errorf("cloudfront: route %q is occupied by unmanaged origin %q", routePattern(action.RouteKey), existing.TargetOriginID)
				}
				cfg.upsertOrigin(action.OriginID, action.Host)
				cfg.upsertBehavior(action.RouteKey, action.OriginID, behaviorTemplate)
			case "remove_route":
				if existing, ok := cfg.behaviorForRouteKey(action.RouteKey); ok && !isManagedOriginID(existing.TargetOriginID) {
					return fmt.Errorf("cloudfront: refusing to remove unmanaged route %q with origin %q", routePattern(action.RouteKey), existing.TargetOriginID)
				}
				cfg.removeBehavior(action.RouteKey)
				if strings.TrimSpace(action.OriginID) != "" && !isManagedOriginID(action.OriginID) {
					return fmt.Errorf("cloudfront: refusing to remove unmanaged origin %q", action.OriginID)
				}
				if strings.TrimSpace(action.OriginID) != "" && !cfg.behaviorReferencesOrigin(action.OriginID) {
					cfg.removeOrigin(action.OriginID)
				}
			default:
				return fmt.Errorf("cloudfront: unsupported route action %q", action.Action)
			}
		}

		if len(rewrites) > 0 {
			functionARN, err := c.ensureRouteRewriteFunction(ctx, rewrites)
			if err != nil {
				return err
			}
			cfg.attachRouteRewriteFunction(rewrites, functionARN)
		}

		cfg.normalize()
		cfg.XMLNS = "http://cloudfront.amazonaws.com/doc/2020-05-31/"
		body, err := xml.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("cloudfront: encode distribution config: %w", err)
		}
		if bytes.Equal(currentBody, body) {
			log.Printf("cloudfront distribution update skipped attempt=%d distribution_id=%s reason=no_config_change", attempt+1, distributionID)
			return nil
		}

		resp, err := c.doWithHeaders(ctx, http.MethodPut, "/2020-05-31/distribution/"+url.PathEscape(distributionID)+"/config", body, "application/xml", map[string]string{
			"If-Match": etag,
		})
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusOK {
			resp.Body.Close()
			return nil
		}

		lastErr = readCloudFrontError(resp)
		resp.Body.Close()
		log.Printf("cloudfront distribution update failed attempt=%d distribution_id=%s etag=%q error=%v", attempt+1, distributionID, etag, lastErr)
		if !isCloudFrontPreconditionFailed(lastErr) || attempt == 4 {
			return lastErr
		}
		if err := c.sleep(ctx, time.Duration(attempt+1)*200*time.Millisecond); err != nil {
			return err
		}
	}
	return lastErr
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func canonicalDistributionConfigXML(cfg *distributionConfigXML) ([]byte, error) {
	clone, err := cloneDistributionConfigXML(cfg)
	if err != nil {
		return nil, err
	}
	clone.normalize()
	clone.XMLNS = "http://cloudfront.amazonaws.com/doc/2020-05-31/"
	body, err := xml.Marshal(clone)
	if err != nil {
		return nil, fmt.Errorf("cloudfront: encode canonical distribution config: %w", err)
	}
	return body, nil
}

func cloneDistributionConfigXML(cfg *distributionConfigXML) (*distributionConfigXML, error) {
	body, err := xml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("cloudfront: clone distribution config marshal: %w", err)
	}
	var cloned distributionConfigXML
	if err := xml.Unmarshal(body, &cloned); err != nil {
		return nil, fmt.Errorf("cloudfront: clone distribution config unmarshal: %w", err)
	}
	return &cloned, nil
}

func (c *AWSClient) do(ctx context.Context, method, path string, body []byte, contentType string) (*http.Response, error) {
	return c.doWithHeaders(ctx, method, path, body, contentType, nil)
}

func (c *AWSClient) doWithHeaders(ctx context.Context, method, path string, body []byte, contentType string, extraHeaders map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}
	if err := c.sign(req, body); err != nil {
		return nil, err
	}
	return c.httpClient.Do(req)
}

func (c *AWSClient) getDistributionConfig(ctx context.Context, distributionID string) (*distributionConfigXML, string, error) {
	resp, err := c.do(ctx, http.MethodGet, "/2020-05-31/distribution/"+url.PathEscape(distributionID)+"/config", nil, "")
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", readCloudFrontError(resp)
	}

	var cfg distributionConfigXML
	if err := xml.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, "", fmt.Errorf("cloudfront: decode distribution config: %w", err)
	}
	return &cfg, resp.Header.Get("ETag"), nil
}

func (c *AWSClient) ensureRouteRewriteFunction(ctx context.Context, rewrites []RewriteRoute) (string, error) {
	code, err := routeRewriteFunctionCode(rewrites)
	if err != nil {
		return "", err
	}
	createPayload := createFunctionRequestXML{
		XMLNS: "http://cloudfront.amazonaws.com/doc/2020-05-31/",
		Name:  routeRewriteFunctionName,
		FunctionConfig: functionConfigXML{
			Comment: "Managed by v2ray-platform: rewrite stable CloudFront route keys to node websocket paths",
			Runtime: "cloudfront-js-2.0",
		},
		FunctionCode: base64.StdEncoding.EncodeToString(code),
	}
	createBody, err := xml.Marshal(createPayload)
	if err != nil {
		return "", fmt.Errorf("cloudfront: encode function config: %w", err)
	}
	updatePayload := updateFunctionRequestXML{
		XMLNS:          "http://cloudfront.amazonaws.com/doc/2020-05-31/",
		FunctionConfig: createPayload.FunctionConfig,
		FunctionCode:   createPayload.FunctionCode,
	}
	updateBody, err := xml.Marshal(updatePayload)
	if err != nil {
		return "", fmt.Errorf("cloudfront: encode function update config: %w", err)
	}

	summary, etag, created, err := c.createFunction(ctx, createBody)
	if err != nil {
		return "", err
	}
	if !created {
		_, currentETag, exists, err := c.describeFunction(ctx)
		if err != nil {
			return "", err
		}
		if !exists {
			summary, etag, created, err = c.createFunction(ctx, createBody)
			if err != nil {
				return "", err
			}
			if !created {
				return "", fmt.Errorf("cloudfront: route rewrite function disappeared during create")
			}
		} else {
			summary, etag, err = c.updateFunction(ctx, updateBody, currentETag)
			if err != nil {
				return "", err
			}
		}
	}
	if strings.TrimSpace(etag) == "" {
		_, latestETag, exists, err := c.describeFunction(ctx)
		if err != nil {
			return "", err
		}
		if !exists || strings.TrimSpace(latestETag) == "" {
			return "", fmt.Errorf("cloudfront: route rewrite function etag missing after update")
		}
		etag = latestETag
	}

	published, _, err := c.publishFunction(ctx, etag)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(published.FunctionMetadata.FunctionARN) != "" {
		return published.FunctionMetadata.FunctionARN, nil
	}
	if strings.TrimSpace(summary.FunctionMetadata.FunctionARN) != "" {
		return summary.FunctionMetadata.FunctionARN, nil
	}
	return "", fmt.Errorf("cloudfront: route rewrite function arn missing")
}

func (c *AWSClient) createFunction(ctx context.Context, body []byte) (*functionSummaryXML, string, bool, error) {
	resp, err := c.do(ctx, http.MethodPost, "/2020-05-31/function", body, "application/xml")
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusConflict {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, "", false, nil
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, "", false, readCloudFrontError(resp)
	}
	summary, err := decodeFunctionSummary(resp.Body)
	if err != nil {
		return nil, "", false, err
	}
	return summary, resp.Header.Get("ETag"), true, nil
}

func (c *AWSClient) describeFunction(ctx context.Context) (*functionSummaryXML, string, bool, error) {
	resp, err := c.do(ctx, http.MethodGet, "/2020-05-31/function/"+url.PathEscape(routeRewriteFunctionName)+"/describe?Stage=DEVELOPMENT", nil, "")
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", false, readCloudFrontError(resp)
	}
	summary, err := decodeFunctionSummary(resp.Body)
	if err != nil {
		return nil, "", false, err
	}
	return summary, resp.Header.Get("ETag"), true, nil
}

func (c *AWSClient) updateFunction(ctx context.Context, body []byte, etag string) (*functionSummaryXML, string, error) {
	resp, err := c.doWithHeaders(ctx, http.MethodPut, "/2020-05-31/function/"+url.PathEscape(routeRewriteFunctionName), body, "application/xml", map[string]string{
		"If-Match": etag,
	})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", readCloudFrontError(resp)
	}
	summary, err := decodeFunctionSummary(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return summary, resp.Header.Get("ETag"), nil
}

func (c *AWSClient) publishFunction(ctx context.Context, etag string) (*functionSummaryXML, string, error) {
	resp, err := c.doWithHeaders(ctx, http.MethodPost, "/2020-05-31/function/"+url.PathEscape(routeRewriteFunctionName)+"/publish", nil, "", map[string]string{
		"If-Match": etag,
	})
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", readCloudFrontError(resp)
	}
	summary, err := decodeFunctionSummary(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return summary, resp.Header.Get("ETag"), nil
}

func decodeFunctionSummary(body io.Reader) (*functionSummaryXML, error) {
	var summary functionSummaryXML
	if err := xml.NewDecoder(body).Decode(&summary); err != nil {
		return nil, fmt.Errorf("cloudfront: decode function summary: %w", err)
	}
	return &summary, nil
}

func routeRewriteFunctionCode(rewrites []RewriteRoute) ([]byte, error) {
	mapping := make(map[string]string, len(rewrites))
	for _, rewrite := range rewrites {
		route := routePattern(rewrite.RouteKey)
		path := routePattern(rewrite.Path)
		if route == "/" || path == "/" {
			continue
		}
		mapping[route] = path
	}
	encoded, err := json.Marshal(mapping)
	if err != nil {
		return nil, fmt.Errorf("cloudfront: encode rewrite map: %w", err)
	}
	code := "function handler(event) {\n" +
		"  var request = event.request;\n" +
		"  var rewrites = " + string(encoded) + ";\n" +
		"  if (rewrites[request.uri]) {\n" +
		"    request.uri = rewrites[request.uri];\n" +
		"  }\n" +
		"  return request;\n" +
		"}\n"
	return []byte(code), nil
}

func rewriteRoutesFromCreateInput(input CreateDistributionInput) []RewriteRoute {
	rewrites := make([]RewriteRoute, 0, len(input.Nodes))
	for _, node := range input.Nodes {
		if strings.TrimSpace(node.RouteKey) == "" || strings.TrimSpace(node.RewritePath) == "" {
			continue
		}
		rewrites = append(rewrites, RewriteRoute{
			RouteKey: node.RouteKey,
			Path:     node.RewritePath,
		})
	}
	return rewrites
}

func (c *AWSClient) sign(req *http.Request, body []byte) error {
	now := c.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	shortDate := now.Format("20060102")
	payloadHash := sha256HexBytes(body)

	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if c.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", c.sessionToken)
	}

	canonicalHeaders, signedHeaders := canonicalHeaders(req.Header)
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		canonicalQuery(req.URL),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	credentialScope := shortDate + "/" + c.region + "/cloudfront/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256HexString(canonicalRequest),
	}, "\n")

	signingKey := c.signingKey(shortDate)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization",
		fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
			c.accessKeyID, credentialScope, signedHeaders, signature,
		),
	)
	return nil
}

func (c *AWSClient) signingKey(shortDate string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+c.secretAccessKey), shortDate)
	kRegion := hmacSHA256(kDate, c.region)
	kService := hmacSHA256(kRegion, "cloudfront")
	return hmacSHA256(kService, "aws4_request")
}

func canonicalHeaders(headers http.Header) (string, string) {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, strings.ToLower(key))
	}
	sort.Strings(keys)
	var b strings.Builder
	signed := make([]string, 0, len(keys))
	for _, key := range keys {
		values := headers.Values(key)
		if len(values) == 0 {
			continue
		}
		signed = append(signed, key)
		b.WriteString(key)
		b.WriteByte(':')
		b.WriteString(strings.Join(normalizeHeaderValues(values), ","))
		b.WriteByte('\n')
	}
	return b.String(), strings.Join(signed, ";")
}

func normalizeHeaderValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.Join(strings.Fields(value), " "))
	}
	return out
}

func canonicalQuery(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	parts := strings.Split(u.RawQuery, "&")
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256HexBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func sha256HexString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func readCloudFrontError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		msg = resp.Status
	}
	if requestID := extractCloudFrontRequestID(msg); requestID != "" {
		return fmt.Errorf("cloudfront: status=%d request_id=%s body=%s", resp.StatusCode, requestID, msg)
	}
	return fmt.Errorf("cloudfront: status=%d body=%s", resp.StatusCode, msg)
}

func extractCloudFrontRequestID(body string) string {
	const startTag = "<RequestId>"
	const endTag = "</RequestId>"
	start := strings.Index(body, startTag)
	if start < 0 {
		return ""
	}
	start += len(startTag)
	end := strings.Index(body[start:], endTag)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(body[start : start+end])
}

type distributionListResponse struct {
	XMLName     xml.Name `xml:"DistributionList"`
	IsTruncated bool     `xml:"IsTruncated"`
	NextMarker  string   `xml:"NextMarker"`
	Items       struct {
		Items []distributionSummaryXML `xml:"DistributionSummary"`
	} `xml:"Items"`
}

type distributionSummaryXML struct {
	ID         string `xml:"Id"`
	Status     string `xml:"Status"`
	DomainName string `xml:"DomainName"`
	Comment    string `xml:"Comment"`
	Aliases    struct {
		Items []string `xml:"Items>CNAME"`
	} `xml:"Aliases"`
}

type distributionResponse struct {
	XMLName            xml.Name              `xml:"Distribution"`
	ID                 string                `xml:"Id"`
	Status             string                `xml:"Status"`
	DomainName         string                `xml:"DomainName"`
	DistributionConfig distributionConfigXML `xml:"DistributionConfig"`
}

type listDistributionTenantsRequestXML struct {
	XMLName           xml.Name                                `xml:"ListDistributionTenantsRequest"`
	XMLNS             string                                  `xml:"xmlns,attr,omitempty"`
	AssociationFilter *distributionTenantAssociationFilterXML `xml:"AssociationFilter,omitempty"`
	Marker            string                                  `xml:"Marker,omitempty"`
}

type distributionTenantAssociationFilterXML struct {
	DistributionID string `xml:"DistributionId,omitempty"`
}

type listDistributionTenantsResponseXML struct {
	XMLName                xml.Name                  `xml:"ListDistributionTenantsResult"`
	NextMarker             string                    `xml:"NextMarker"`
	DistributionTenantList distributionTenantListXML `xml:"DistributionTenantList"`
}

type distributionTenantListXML struct {
	Items []distributionTenantSummaryXML `xml:"DistributionTenantSummary"`
}

type distributionTenantSummaryXML struct {
	DistributionID string                       `xml:"DistributionId"`
	Domains        distributionTenantDomainsXML `xml:"Domains"`
}

type distributionTenantDomainsXML struct {
	Items []distributionTenantDomainResultXML `xml:"member"`
}

type distributionTenantDomainResultXML struct {
	Domain string `xml:"Domain"`
}

type createFunctionRequestXML struct {
	XMLName        xml.Name          `xml:"CreateFunctionRequest"`
	XMLNS          string            `xml:"xmlns,attr,omitempty"`
	Name           string            `xml:"Name"`
	FunctionConfig functionConfigXML `xml:"FunctionConfig"`
	FunctionCode   string            `xml:"FunctionCode"`
}

type updateFunctionRequestXML struct {
	XMLName        xml.Name          `xml:"UpdateFunctionRequest"`
	XMLNS          string            `xml:"xmlns,attr,omitempty"`
	FunctionConfig functionConfigXML `xml:"FunctionConfig"`
	FunctionCode   string            `xml:"FunctionCode"`
}

type functionConfigXML struct {
	Comment string `xml:"Comment,omitempty"`
	Runtime string `xml:"Runtime"`
}

type functionSummaryXML struct {
	XMLName          xml.Name            `xml:"FunctionSummary"`
	Name             string              `xml:"Name"`
	Status           string              `xml:"Status"`
	FunctionConfig   functionConfigXML   `xml:"FunctionConfig"`
	FunctionMetadata functionMetadataXML `xml:"FunctionMetadata"`
}

type functionMetadataXML struct {
	FunctionARN string `xml:"FunctionARN"`
	Stage       string `xml:"Stage,omitempty"`
}

type distributionConfigXML struct {
	XMLName                       xml.Name                `xml:"DistributionConfig"`
	XMLNS                         string                  `xml:"xmlns,attr,omitempty"`
	CallerReference               string                  `xml:"CallerReference,omitempty"`
	Aliases                       *aliasesXML             `xml:"Aliases,omitempty"`
	DefaultRootObject             string                  `xml:"DefaultRootObject"`
	Origins                       originsXML              `xml:"Origins"`
	OriginGroups                  passthroughXML          `xml:"OriginGroups,omitempty"`
	DefaultCacheBehavior          defaultCacheBehaviorXML `xml:"DefaultCacheBehavior"`
	CacheBehaviors                cacheBehaviorsXML       `xml:"CacheBehaviors"`
	CacheTagConfig                passthroughXML          `xml:"CacheTagConfig,omitempty"`
	ConnectionFunctionAssociation passthroughXML          `xml:"ConnectionFunctionAssociation,omitempty"`
	ConnectionMode                string                  `xml:"ConnectionMode,omitempty"`
	CustomErrorResponses          passthroughXML          `xml:"CustomErrorResponses,omitempty"`
	Comment                       string                  `xml:"Comment"`
	Logging                       passthroughXML          `xml:"Logging,omitempty"`
	PriceClass                    string                  `xml:"PriceClass,omitempty"`
	Enabled                       bool                    `xml:"Enabled"`
	ViewerCertificate             passthroughXML          `xml:"ViewerCertificate,omitempty"`
	Restrictions                  passthroughXML          `xml:"Restrictions,omitempty"`
	WebACLID                      string                  `xml:"WebACLId"`
	HttpVersion                   string                  `xml:"HttpVersion"`
	IsIPV6Enabled                 bool                    `xml:"IsIPV6Enabled,omitempty"`
	ContinuousDeploymentPolicyID  string                  `xml:"ContinuousDeploymentPolicyId,omitempty"`
	Staging                       bool                    `xml:"Staging,omitempty"`
	AnycastIPListID               string                  `xml:"AnycastIpListId,omitempty"`
	TenantConfig                  *tenantConfigXML        `xml:"TenantConfig,omitempty"`
	ViewerMTlsConfig              passthroughXML          `xml:"ViewerMtlsConfig,omitempty"`
}

type aliasesXML struct {
	Quantity int      `xml:"Quantity"`
	Items    []string `xml:"Items>CNAME,omitempty"`
}

type originsXML struct {
	Quantity int         `xml:"Quantity"`
	Items    []originXML `xml:"Items>Origin,omitempty"`
}

type originXML struct {
	ID                    string                `xml:"Id"`
	DomainName            string                `xml:"DomainName"`
	OriginPath            string                `xml:"OriginPath"`
	CustomHeaders         passthroughXML        `xml:"CustomHeaders,omitempty"`
	S3OriginConfig        passthroughXML        `xml:"S3OriginConfig,omitempty"`
	CustomOriginConfig    customOriginConfigXML `xml:"CustomOriginConfig,omitempty"`
	ConnectionAttempts    int                   `xml:"ConnectionAttempts"`
	ConnectionTimeout     int                   `xml:"ConnectionTimeout"`
	OriginShield          passthroughXML        `xml:"OriginShield,omitempty"`
	OriginAccessControlID string                `xml:"OriginAccessControlId"`
}

type cacheBehaviorsXML struct {
	Quantity int                `xml:"Quantity"`
	Items    []cacheBehaviorXML `xml:"Items>CacheBehavior,omitempty"`
}

type cacheBehaviorXML struct {
	PathPattern                string                  `xml:"PathPattern,omitempty"`
	TargetOriginID             string                  `xml:"TargetOriginId"`
	TrustedSigners             passthroughXML          `xml:"TrustedSigners,omitempty"`
	TrustedKeyGroups           passthroughXML          `xml:"TrustedKeyGroups,omitempty"`
	ViewerProtocolPolicy       string                  `xml:"ViewerProtocolPolicy,omitempty"`
	AllowedMethods             passthroughXML          `xml:"AllowedMethods,omitempty"`
	SmoothStreaming            *bool                   `xml:"SmoothStreaming,omitempty"`
	Compress                   *bool                   `xml:"Compress,omitempty"`
	LambdaFunctionAssociations passthroughXML          `xml:"LambdaFunctionAssociations,omitempty"`
	FunctionAssociations       functionAssociationsXML `xml:"FunctionAssociations,omitempty"`
	FieldLevelEncryptionID     string                  `xml:"FieldLevelEncryptionId"`
	RealtimeLogConfigArn       string                  `xml:"RealtimeLogConfigArn,omitempty"`
	CachePolicyID              string                  `xml:"CachePolicyId,omitempty"`
	ForwardedValues            passthroughXML          `xml:"ForwardedValues,omitempty"`
	OriginRequestPolicyID      string                  `xml:"OriginRequestPolicyId,omitempty"`
	ResponseHeadersPolicyID    string                  `xml:"ResponseHeadersPolicyId,omitempty"`
	GrpcConfig                 passthroughXML          `xml:"GrpcConfig,omitempty"`
}

type defaultCacheBehaviorXML struct {
	TargetOriginID             string                  `xml:"TargetOriginId"`
	TrustedSigners             passthroughXML          `xml:"TrustedSigners,omitempty"`
	TrustedKeyGroups           passthroughXML          `xml:"TrustedKeyGroups,omitempty"`
	ViewerProtocolPolicy       string                  `xml:"ViewerProtocolPolicy,omitempty"`
	AllowedMethods             passthroughXML          `xml:"AllowedMethods,omitempty"`
	SmoothStreaming            *bool                   `xml:"SmoothStreaming,omitempty"`
	Compress                   *bool                   `xml:"Compress,omitempty"`
	LambdaFunctionAssociations passthroughXML          `xml:"LambdaFunctionAssociations,omitempty"`
	FunctionAssociations       functionAssociationsXML `xml:"FunctionAssociations,omitempty"`
	FieldLevelEncryptionID     string                  `xml:"FieldLevelEncryptionId"`
	RealtimeLogConfigArn       string                  `xml:"RealtimeLogConfigArn,omitempty"`
	CachePolicyID              string                  `xml:"CachePolicyId,omitempty"`
	ForwardedValues            passthroughXML          `xml:"ForwardedValues,omitempty"`
	OriginRequestPolicyID      string                  `xml:"OriginRequestPolicyId,omitempty"`
	ResponseHeadersPolicyID    string                  `xml:"ResponseHeadersPolicyId,omitempty"`
	GrpcConfig                 passthroughXML          `xml:"GrpcConfig,omitempty"`
}

type customOriginConfigXML struct {
	HTTPPort               int            `xml:"HTTPPort"`
	HTTPSPort              int            `xml:"HTTPSPort"`
	OriginProtocolPolicy   string         `xml:"OriginProtocolPolicy"`
	OriginSslProtocols     passthroughXML `xml:"OriginSslProtocols,omitempty"`
	OriginReadTimeout      int            `xml:"OriginReadTimeout"`
	OriginKeepaliveTimeout int            `xml:"OriginKeepaliveTimeout"`
}

type functionAssociationsXML struct {
	Quantity int                      `xml:"Quantity"`
	Items    []functionAssociationXML `xml:"Items>FunctionAssociation,omitempty"`
}

type functionAssociationXML struct {
	EventType   string `xml:"EventType"`
	FunctionARN string `xml:"FunctionARN"`
}

type tenantConfigXML struct {
	ParameterDefinitions parameterDefinitionsXML `xml:"ParameterDefinitions"`
}

type parameterDefinitionsXML struct {
	Direct []parameterDefinitionXML `xml:"ParameterDefinition"`
	Member []parameterDefinitionXML `xml:"member"`
}

func (p parameterDefinitionsXML) all() []parameterDefinitionXML {
	if len(p.Direct) == 0 {
		return p.Member
	}
	if len(p.Member) == 0 {
		return p.Direct
	}
	out := make([]parameterDefinitionXML, 0, len(p.Direct)+len(p.Member))
	out = append(out, p.Direct...)
	out = append(out, p.Member...)
	return out
}

type parameterDefinitionXML struct {
	Name       string                       `xml:"Name"`
	Definition parameterDefinitionSchemaXML `xml:"Definition"`
}

type parameterDefinitionSchemaXML struct {
	StringSchema stringSchemaConfigXML `xml:"StringSchema"`
}

type stringSchemaConfigXML struct {
	Required     bool   `xml:"Required"`
	Comment      string `xml:"Comment"`
	DefaultValue string `xml:"DefaultValue"`
}

type passthroughXML string

func (p passthroughXML) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if p == "" {
		return nil
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	decoder := xml.NewDecoder(bytes.NewBufferString("<wrapper>" + string(p) + "</wrapper>"))
	depth := 0
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		switch typed := tok.(type) {
		case xml.StartElement:
			if depth > 0 {
				if err := e.EncodeToken(typed); err != nil {
					return err
				}
			}
			depth++
		case xml.EndElement:
			depth--
			if depth > 0 {
				if err := e.EncodeToken(typed); err != nil {
					return err
				}
			}
		default:
			if depth > 0 {
				if err := e.EncodeToken(tok); err != nil {
					return err
				}
			}
		}
	}
	if err := e.EncodeToken(start.End()); err != nil {
		return err
	}
	return e.Flush()
}

func (p *passthroughXML) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var raw struct {
		Inner string `xml:",innerxml"`
	}
	if err := d.DecodeElement(&raw, &start); err != nil {
		return err
	}
	*p = passthroughXML(raw.Inner)
	return nil
}

func (cfg *distributionConfigXML) defaultBehaviorTemplate() cacheBehaviorXML {
	if len(cfg.CacheBehaviors.Items) > 0 {
		template := cfg.CacheBehaviors.Items[0]
		template.PathPattern = ""
		template.TargetOriginID = ""
		return template
	}
	return cacheBehaviorXML{
		TrustedSigners:             cfg.DefaultCacheBehavior.TrustedSigners,
		TrustedKeyGroups:           cfg.DefaultCacheBehavior.TrustedKeyGroups,
		ViewerProtocolPolicy:       cfg.DefaultCacheBehavior.ViewerProtocolPolicy,
		AllowedMethods:             cfg.DefaultCacheBehavior.AllowedMethods,
		SmoothStreaming:            cfg.DefaultCacheBehavior.SmoothStreaming,
		Compress:                   cfg.DefaultCacheBehavior.Compress,
		LambdaFunctionAssociations: cfg.DefaultCacheBehavior.LambdaFunctionAssociations,
		FunctionAssociations:       cfg.DefaultCacheBehavior.FunctionAssociations,
		FieldLevelEncryptionID:     cfg.DefaultCacheBehavior.FieldLevelEncryptionID,
		RealtimeLogConfigArn:       cfg.DefaultCacheBehavior.RealtimeLogConfigArn,
		CachePolicyID:              cfg.DefaultCacheBehavior.CachePolicyID,
		ForwardedValues:            cfg.DefaultCacheBehavior.ForwardedValues,
		OriginRequestPolicyID:      cfg.DefaultCacheBehavior.OriginRequestPolicyID,
		ResponseHeadersPolicyID:    cfg.DefaultCacheBehavior.ResponseHeadersPolicyID,
		GrpcConfig:                 cfg.DefaultCacheBehavior.GrpcConfig,
	}
}

func (cfg *distributionConfigXML) upsertOrigin(originID, host string) {
	for i := range cfg.Origins.Items {
		if cfg.Origins.Items[i].ID == originID {
			cfg.Origins.Items[i].DomainName = host
			if cfg.Origins.Items[i].CustomOriginConfig.OriginProtocolPolicy == "" {
				cfg.Origins.Items[i].CustomOriginConfig = defaultCustomOriginConfig()
			}
			return
		}
	}
	cfg.Origins.Items = append(cfg.Origins.Items, originXML{
		ID:                 originID,
		DomainName:         host,
		CustomOriginConfig: defaultCustomOriginConfig(),
	})
}

func (cfg *distributionConfigXML) upsertBehavior(routeKey, originID string, template cacheBehaviorXML) {
	pathPattern := routePattern(routeKey)
	for i := range cfg.CacheBehaviors.Items {
		if cfg.CacheBehaviors.Items[i].PathPattern == pathPattern {
			cfg.CacheBehaviors.Items[i].TargetOriginID = originID
			cfg.copyBehaviorDefaults(&cfg.CacheBehaviors.Items[i], template)
			return
		}
	}
	behavior := template
	behavior.PathPattern = pathPattern
	behavior.TargetOriginID = originID
	cfg.copyBehaviorDefaults(&behavior, template)
	cfg.CacheBehaviors.Items = append(cfg.CacheBehaviors.Items, behavior)
}

func (cfg *distributionConfigXML) attachRouteRewriteFunction(rewrites []RewriteRoute, functionARN string) {
	managedRoutes := make(map[string]bool, len(rewrites))
	for _, rewrite := range rewrites {
		managedRoutes[routePattern(rewrite.RouteKey)] = true
	}
	for i := range cfg.CacheBehaviors.Items {
		behavior := &cfg.CacheBehaviors.Items[i]
		if !managedRoutes[behavior.PathPattern] || !isManagedOriginID(behavior.TargetOriginID) {
			continue
		}
		behavior.FunctionAssociations.setViewerRequestFunction(functionARN)
	}
}

func (cfg *distributionConfigXML) copyBehaviorDefaults(dst *cacheBehaviorXML, template cacheBehaviorXML) {
	if dst.ViewerProtocolPolicy == "" {
		dst.ViewerProtocolPolicy = template.ViewerProtocolPolicy
	}
	if dst.AllowedMethods == "" {
		dst.AllowedMethods = template.AllowedMethods
	}
	if dst.TrustedSigners == "" {
		dst.TrustedSigners = template.TrustedSigners
	}
	if dst.TrustedKeyGroups == "" {
		dst.TrustedKeyGroups = template.TrustedKeyGroups
	}
	if dst.SmoothStreaming == nil {
		dst.SmoothStreaming = template.SmoothStreaming
	}
	if dst.Compress == nil {
		dst.Compress = template.Compress
	}
	if dst.LambdaFunctionAssociations == "" {
		dst.LambdaFunctionAssociations = template.LambdaFunctionAssociations
	}
	if dst.FunctionAssociations.Quantity == 0 && len(dst.FunctionAssociations.Items) == 0 {
		dst.FunctionAssociations = template.FunctionAssociations
	}
	if dst.FieldLevelEncryptionID == "" {
		dst.FieldLevelEncryptionID = template.FieldLevelEncryptionID
	}
	if dst.RealtimeLogConfigArn == "" {
		dst.RealtimeLogConfigArn = template.RealtimeLogConfigArn
	}
	if dst.CachePolicyID == "" {
		dst.CachePolicyID = template.CachePolicyID
	}
	if dst.ForwardedValues == "" {
		dst.ForwardedValues = template.ForwardedValues
	}
	if dst.CachePolicyID == "" && dst.ForwardedValues == "" {
		dst.CachePolicyID = managedCachingDisabledPolicyID
	}
	if dst.OriginRequestPolicyID == "" {
		dst.OriginRequestPolicyID = template.OriginRequestPolicyID
	}
	if dst.ResponseHeadersPolicyID == "" {
		dst.ResponseHeadersPolicyID = template.ResponseHeadersPolicyID
	}
	if dst.GrpcConfig == "" {
		dst.GrpcConfig = template.GrpcConfig
	}
}

func (associations *functionAssociationsXML) setViewerRequestFunction(functionARN string) {
	filtered := associations.Items[:0]
	for _, item := range associations.Items {
		if item.EventType == "viewer-request" {
			continue
		}
		filtered = append(filtered, item)
	}
	filtered = append(filtered, functionAssociationXML{
		EventType:   "viewer-request",
		FunctionARN: functionARN,
	})
	associations.Items = filtered
	associations.Quantity = len(filtered)
}

func (cfg *distributionConfigXML) removeBehavior(routeKey string) {
	pathPattern := routePattern(routeKey)
	filtered := cfg.CacheBehaviors.Items[:0]
	for _, behavior := range cfg.CacheBehaviors.Items {
		if behavior.PathPattern == pathPattern {
			continue
		}
		filtered = append(filtered, behavior)
	}
	cfg.CacheBehaviors.Items = filtered
}

func (cfg *distributionConfigXML) behaviorForRouteKey(routeKey string) (cacheBehaviorXML, bool) {
	pathPattern := routePattern(routeKey)
	for _, behavior := range cfg.CacheBehaviors.Items {
		if behavior.PathPattern == pathPattern {
			return behavior, true
		}
	}
	return cacheBehaviorXML{}, false
}

func (cfg *distributionConfigXML) removeOrigin(originID string) {
	filtered := cfg.Origins.Items[:0]
	for _, origin := range cfg.Origins.Items {
		if origin.ID == originID {
			continue
		}
		filtered = append(filtered, origin)
	}
	cfg.Origins.Items = filtered
}

func (cfg *distributionConfigXML) behaviorReferencesOrigin(originID string) bool {
	for _, behavior := range cfg.CacheBehaviors.Items {
		if behavior.TargetOriginID == originID {
			return true
		}
	}
	return false
}

func (cfg *distributionConfigXML) normalize() {
	if cfg.TenantConfig != nil && len(cfg.TenantConfig.ParameterDefinitions.all()) == 0 {
		cfg.TenantConfig = nil
	}
	tenantOnly := strings.EqualFold(strings.TrimSpace(cfg.ConnectionMode), "tenant-only")
	if tenantOnly {
		cfg.Aliases = nil
		cfg.PriceClass = ""
		cfg.ContinuousDeploymentPolicyID = ""
		cfg.AnycastIPListID = ""
		cfg.IsIPV6Enabled = false
	} else if strings.TrimSpace(cfg.PriceClass) == "" {
		cfg.PriceClass = "PriceClass_All"
	}
	if !tenantOnly && cfg.Aliases == nil {
		cfg.Aliases = &aliasesXML{}
	}
	for i := range cfg.Origins.Items {
		if cfg.Origins.Items[i].CustomHeaders == "" {
			cfg.Origins.Items[i].CustomHeaders = passthroughXML(`<Quantity>0</Quantity>`)
		}
		if cfg.Origins.Items[i].OriginShield == "" {
			cfg.Origins.Items[i].OriginShield = passthroughXML(`<Enabled>false</Enabled>`)
		}
		if cfg.Origins.Items[i].ConnectionAttempts == 0 {
			cfg.Origins.Items[i].ConnectionAttempts = 3
		}
		if cfg.Origins.Items[i].ConnectionTimeout == 0 {
			cfg.Origins.Items[i].ConnectionTimeout = 10
		}
		if cfg.Origins.Items[i].CustomOriginConfig.HTTPPort == 0 {
			cfg.Origins.Items[i].CustomOriginConfig.HTTPPort = 80
		}
		if cfg.Origins.Items[i].CustomOriginConfig.HTTPSPort == 0 {
			cfg.Origins.Items[i].CustomOriginConfig.HTTPSPort = 443
		}
		if cfg.Origins.Items[i].CustomOriginConfig.OriginReadTimeout == 0 {
			cfg.Origins.Items[i].CustomOriginConfig.OriginReadTimeout = 30
		}
		if cfg.Origins.Items[i].CustomOriginConfig.OriginKeepaliveTimeout == 0 {
			cfg.Origins.Items[i].CustomOriginConfig.OriginKeepaliveTimeout = 5
		}
	}
	defaultFalse := false
	if tenantOnly {
		cfg.DefaultCacheBehavior.SmoothStreaming = nil
		cfg.DefaultCacheBehavior.TrustedSigners = ""
	} else if cfg.DefaultCacheBehavior.SmoothStreaming == nil {
		cfg.DefaultCacheBehavior.SmoothStreaming = &defaultFalse
	}
	if cfg.DefaultCacheBehavior.Compress == nil {
		cfg.DefaultCacheBehavior.Compress = &defaultFalse
	}
	if !tenantOnly && cfg.DefaultCacheBehavior.TrustedSigners == "" {
		cfg.DefaultCacheBehavior.TrustedSigners = passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`)
	}
	if cfg.DefaultCacheBehavior.TrustedKeyGroups == "" {
		cfg.DefaultCacheBehavior.TrustedKeyGroups = passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`)
	}
	if cfg.DefaultCacheBehavior.LambdaFunctionAssociations == "" {
		cfg.DefaultCacheBehavior.LambdaFunctionAssociations = passthroughXML(`<Quantity>0</Quantity>`)
	}
	if cfg.DefaultCacheBehavior.GrpcConfig == "" {
		cfg.DefaultCacheBehavior.GrpcConfig = passthroughXML(`<Enabled>false</Enabled>`)
	}
	for i := range cfg.CacheBehaviors.Items {
		if tenantOnly {
			cfg.CacheBehaviors.Items[i].SmoothStreaming = nil
			cfg.CacheBehaviors.Items[i].TrustedSigners = ""
		} else if cfg.CacheBehaviors.Items[i].SmoothStreaming == nil {
			cfg.CacheBehaviors.Items[i].SmoothStreaming = &defaultFalse
		}
		if cfg.CacheBehaviors.Items[i].Compress == nil {
			cfg.CacheBehaviors.Items[i].Compress = &defaultFalse
		}
		if !tenantOnly && cfg.CacheBehaviors.Items[i].TrustedSigners == "" {
			cfg.CacheBehaviors.Items[i].TrustedSigners = passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`)
		}
		if cfg.CacheBehaviors.Items[i].TrustedKeyGroups == "" {
			cfg.CacheBehaviors.Items[i].TrustedKeyGroups = passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`)
		}
		if cfg.CacheBehaviors.Items[i].LambdaFunctionAssociations == "" {
			cfg.CacheBehaviors.Items[i].LambdaFunctionAssociations = passthroughXML(`<Quantity>0</Quantity>`)
		}
		if cfg.CacheBehaviors.Items[i].GrpcConfig == "" {
			cfg.CacheBehaviors.Items[i].GrpcConfig = passthroughXML(`<Enabled>false</Enabled>`)
		}
	}
	sort.Slice(cfg.Origins.Items, func(i, j int) bool {
		return cfg.Origins.Items[i].ID < cfg.Origins.Items[j].ID
	})
	sort.Slice(cfg.CacheBehaviors.Items, func(i, j int) bool {
		return cfg.CacheBehaviors.Items[i].PathPattern < cfg.CacheBehaviors.Items[j].PathPattern
	})
	if cfg.Aliases != nil {
		cfg.Aliases.Quantity = len(cfg.Aliases.Items)
	}
	cfg.Origins.Quantity = len(cfg.Origins.Items)
	cfg.CacheBehaviors.Quantity = len(cfg.CacheBehaviors.Items)
	for i := range cfg.CacheBehaviors.Items {
		cfg.CacheBehaviors.Items[i].FunctionAssociations.Quantity = len(cfg.CacheBehaviors.Items[i].FunctionAssociations.Items)
	}
	cfg.DefaultCacheBehavior.FunctionAssociations.Quantity = len(cfg.DefaultCacheBehavior.FunctionAssociations.Items)
}

func defaultCustomOriginConfig() customOriginConfigXML {
	return customOriginConfigXML{
		HTTPPort:               80,
		HTTPSPort:              443,
		OriginProtocolPolicy:   "https-only",
		OriginSslProtocols:     passthroughXML(`<Quantity>1</Quantity><Items><SslProtocol>TLSv1.2</SslProtocol></Items>`),
		OriginReadTimeout:      30,
		OriginKeepaliveTimeout: 5,
	}
}

func routePattern(routeKey string) string {
	return "/" + strings.TrimPrefix(strings.TrimSpace(routeKey), "/")
}

func newManagedDistributionConfig(input CreateDistributionInput) *distributionConfigXML {
	filteredNodes := make([]CreateDistributionNode, 0, len(input.Nodes))
	for _, node := range input.Nodes {
		if strings.TrimSpace(node.NodeID) == "" || strings.TrimSpace(node.RouteKey) == "" || strings.TrimSpace(node.Host) == "" {
			continue
		}
		filteredNodes = append(filteredNodes, node)
	}
	if len(filteredNodes) == 0 {
		return &distributionConfigXML{}
	}

	allowedMethods := passthroughXML(`<Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods>`)
	compress := true
	comment := strings.TrimSpace(input.Comment)
	if comment == "" {
		comment = "Managed by v2ray-platform"
	}

	cfg := &distributionConfigXML{
		XMLNS:           "http://cloudfront.amazonaws.com/doc/2020-05-31/",
		CallerReference: fmt.Sprintf("v2ray-platform-%d", time.Now().UnixNano()),
		Aliases:         &aliasesXML{Quantity: 0},
		Origins:         originsXML{Items: make([]originXML, 0, len(filteredNodes))},
		DefaultCacheBehavior: defaultCacheBehaviorXML{
			TargetOriginID:       managedOriginID(filteredNodes[0].NodeID),
			ViewerProtocolPolicy: "redirect-to-https",
			AllowedMethods:       allowedMethods,
			Compress:             &compress,
			CachePolicyID:        managedCachingDisabledPolicyID,
		},
		CacheBehaviors:    cacheBehaviorsXML{Items: make([]cacheBehaviorXML, 0, len(filteredNodes))},
		Comment:           comment,
		PriceClass:        "PriceClass_All",
		Enabled:           true,
		ViewerCertificate: passthroughXML(`<CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate>`),
		Restrictions:      passthroughXML(`<GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction>`),
		HttpVersion:       "http2",
		IsIPV6Enabled:     true,
	}

	for _, node := range filteredNodes {
		originID := managedOriginID(node.NodeID)
		cfg.Origins.Items = append(cfg.Origins.Items, originXML{
			ID:                 originID,
			DomainName:         node.Host,
			CustomOriginConfig: defaultCustomOriginConfig(),
		})
		cfg.CacheBehaviors.Items = append(cfg.CacheBehaviors.Items, cacheBehaviorXML{
			PathPattern:          routePattern(node.RouteKey),
			TargetOriginID:       originID,
			ViewerProtocolPolicy: "redirect-to-https",
			AllowedMethods:       allowedMethods,
			Compress:             &compress,
			CachePolicyID:        managedCachingDisabledPolicyID,
		})
	}
	cfg.normalize()
	return cfg
}
