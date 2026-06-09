package cloudfront

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestAWSClientListDistributionsParsesXML(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got == "" {
			t.Fatal("expected signed request")
		}
		if r.URL.Path != "/2020-05-31/distribution" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionList>
  <Items>
    <DistributionSummary>
      <Id>DIST123</Id>
      <Status>Deployed</Status>
      <DomainName>d123.cloudfront.net</DomainName>
      <Comment>Main distribution</Comment>
      <Aliases>
        <Items>
          <CNAME>edge.example.com</CNAME>
        </Items>
      </Aliases>
    </DistributionSummary>
  </Items>
</DistributionList>`)),
			Request: r,
		}, nil
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := client.ListDistributions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].DistributionID != "DIST123" || items[0].DomainName != "d123.cloudfront.net" {
		t.Fatalf("unexpected distribution %+v", items[0])
	}
	if len(items[0].Aliases) != 1 || items[0].Aliases[0] != "edge.example.com" {
		t.Fatalf("unexpected aliases %+v", items[0].Aliases)
	}
}

func TestAWSClientListDistributionsFollowsPagination(t *testing.T) {
	paths := []string{}
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.RequestURI())
		switch r.URL.RequestURI() {
		case "/2020-05-31/distribution":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionList>
  <IsTruncated>true</IsTruncated>
  <NextMarker>page-2</NextMarker>
  <Items>
    <DistributionSummary>
      <Id>DIST1</Id>
      <Status>Deployed</Status>
      <DomainName>d1.cloudfront.net</DomainName>
    </DistributionSummary>
  </Items>
</DistributionList>`)),
				Request: r,
			}, nil
		case "/2020-05-31/distribution?Marker=page-2":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionList>
  <IsTruncated>false</IsTruncated>
  <Items>
    <DistributionSummary>
      <Id>DIST2</Id>
      <Status>Deployed</Status>
      <DomainName>d2.cloudfront.net</DomainName>
    </DistributionSummary>
  </Items>
</DistributionList>`)),
				Request: r,
			}, nil
		default:
			t.Fatalf("unexpected request %s", r.URL.RequestURI())
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := client.ListDistributions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].DistributionID != "DIST1" || items[1].DistributionID != "DIST2" {
		t.Fatalf("unexpected paginated items %+v", items)
	}
	if len(paths) != 2 {
		t.Fatalf("expected two page requests, got %v", paths)
	}
}

func TestAWSClientSignsCloudFrontRequestsWithGlobalEndpointRegion(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		auth := r.Header.Get("Authorization")
		if !strings.Contains(auth, "/"+cloudFrontSigningRegion+"/cloudfront/aws4_request") {
			t.Fatalf("expected CloudFront signing region %q in auth header, got %q", cloudFrontSigningRegion, auth)
		}
		if strings.Contains(auth, "/ap-southeast-1/cloudfront/aws4_request") {
			t.Fatalf("expected operator region not to be used for CloudFront signing, got %q", auth)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionList>
  <Items></Items>
</DistributionList>`)),
			Request: r,
		}, nil
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "ap-southeast-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := client.ListDistributions(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestAWSClientGetDistributionParsesOriginsAndBehaviors(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/2020-05-31/distribution/DIST123" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<Distribution>
  <Id>DIST123</Id>
  <Status>Deployed</Status>
  <DomainName>d123.cloudfront.net</DomainName>
  <DistributionConfig>
    <Comment>Main distribution</Comment>
    <Aliases>
      <Items>
        <CNAME>edge.example.com</CNAME>
      </Items>
    </Aliases>
    <Origins>
      <Items>
        <Origin>
          <Id>origin-1</Id>
          <DomainName>node-1.example.com</DomainName>
        </Origin>
      </Items>
    </Origins>
    <CacheBehaviors>
      <Items>
        <CacheBehavior>
          <PathPattern>/rk123</PathPattern>
          <TargetOriginId>origin-1</TargetOriginId>
        </CacheBehavior>
      </Items>
    </CacheBehaviors>
  </DistributionConfig>
</Distribution>`)),
			Request: r,
		}, nil
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	dist, err := client.GetDistribution(t.Context(), "DIST123")
	if err != nil {
		t.Fatal(err)
	}
	if dist.DistributionID != "DIST123" || dist.DomainName != "d123.cloudfront.net" {
		t.Fatalf("unexpected distribution %+v", dist)
	}
	if len(dist.Origins) != 1 || dist.Origins[0].OriginID != "origin-1" {
		t.Fatalf("unexpected origins %+v", dist.Origins)
	}
	if len(dist.Behaviors) != 1 || dist.Behaviors[0].PathPattern != "/rk123" {
		t.Fatalf("unexpected behaviors %+v", dist.Behaviors)
	}
}

func TestAWSClientCreateDistributionBuildsManagedConfig(t *testing.T) {
	var postedBody []byte
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/distribution":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			postedBody = append([]byte(nil), body...)
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<Distribution>
  <Id>ENEW123</Id>
  <Status>InProgress</Status>
  <DomainName>dnew123.cloudfront.net</DomainName>
  <DistributionConfig>
    <Comment>Managed by v2ray-platform</Comment>
    <Aliases><Quantity>0</Quantity></Aliases>
    <Origins>
      <Quantity>1</Quantity>
      <Items>
        <Origin>
          <Id>v2ray-platform-node-node-1</Id>
          <DomainName>node-1.example.com</DomainName>
        </Origin>
      </Items>
    </Origins>
    <CacheBehaviors>
      <Quantity>1</Quantity>
      <Items>
        <CacheBehavior>
          <PathPattern>/rk123</PathPattern>
          <TargetOriginId>v2ray-platform-node-node-1</TargetOriginId>
        </CacheBehavior>
      </Items>
    </CacheBehaviors>
  </DistributionConfig>
</Distribution>`)),
				Request: r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	dist, err := client.CreateDistribution(context.Background(), CreateDistributionInput{
		Comment: "Managed by v2ray-platform",
		Nodes: []CreateDistributionNode{
			{NodeID: "node-1", RouteKey: "rk123", Host: "node-1.example.com"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if dist.DistributionID != "ENEW123" || dist.DomainName != "dnew123.cloudfront.net" {
		t.Fatalf("unexpected distribution %+v", dist)
	}

	var cfg distributionConfigXML
	if err := xml.NewDecoder(bytes.NewReader(postedBody)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Origins.Quantity != 1 || len(cfg.Origins.Items) != 1 {
		t.Fatalf("expected one origin in create request, got %+v", cfg.Origins)
	}
	if cfg.CacheBehaviors.Quantity != 1 || len(cfg.CacheBehaviors.Items) != 1 {
		t.Fatalf("expected one cache behavior in create request, got %+v", cfg.CacheBehaviors)
	}
	if cfg.XMLNS != "http://cloudfront.amazonaws.com/doc/2020-05-31/" {
		t.Fatalf("expected cloudfront XML namespace, got %q", cfg.XMLNS)
	}
	if cfg.DefaultCacheBehavior.TargetOriginID != "v2ray-platform-node-node-1" {
		t.Fatalf("unexpected default cache behavior %+v", cfg.DefaultCacheBehavior)
	}
	if cfg.DefaultCacheBehavior.CachePolicyID != managedCachingDisabledPolicyID {
		t.Fatalf("expected default cache policy %q, got %q", managedCachingDisabledPolicyID, cfg.DefaultCacheBehavior.CachePolicyID)
	}
	if cfg.CacheBehaviors.Items[0].CachePolicyID != managedCachingDisabledPolicyID {
		t.Fatalf("expected route cache policy %q, got %q", managedCachingDisabledPolicyID, cfg.CacheBehaviors.Items[0].CachePolicyID)
	}
	if !strings.Contains(cfg.Origins.Items[0].CustomOriginConfig.OriginSslProtocols.Inner, "TLSv1.2") {
		t.Fatalf("expected TLSv1.2 origin ssl protocols, got %+v", cfg.Origins.Items[0].CustomOriginConfig.OriginSslProtocols)
	}
}

func TestAWSClientCreateDistributionAttachesRouteRewriteFunction(t *testing.T) {
	var postedFunction createFunctionRequestXML
	var postedDistribution []byte
	functionARN := "arn:aws:cloudfront::123456789012:function/v2ray-platform-route-rewrite"
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/function":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&postedFunction); err != nil {
				t.Fatal(err)
			}
			if postedFunction.Name != routeRewriteFunctionName {
				t.Fatalf("unexpected function name %q", postedFunction.Name)
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"FUNCETAG1"},
				},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>UNPUBLISHED</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>DEVELOPMENT</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/function/"+routeRewriteFunctionName+"/publish":
			if got := r.Header.Get("If-Match"); got != "FUNCETAG1" {
				t.Fatalf("expected function publish If-Match FUNCETAG1, got %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>LIVE</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>LIVE</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/distribution":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			postedDistribution = append([]byte(nil), body...)
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<Distribution>
  <Id>ENEW123</Id>
  <Status>InProgress</Status>
  <DomainName>dnew123.cloudfront.net</DomainName>
  <DistributionConfig>
    <Comment>Managed by v2ray-platform</Comment>
  </DistributionConfig>
</Distribution>`)),
				Request: r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.CreateDistribution(context.Background(), CreateDistributionInput{
		Comment: "Managed by v2ray-platform",
		Nodes: []CreateDistributionNode{
			{NodeID: "node-1", RouteKey: "rk123", Host: "node-1.example.com", RewritePath: "/node-1"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	decodedCode, err := base64.StdEncoding.DecodeString(postedFunction.FunctionCode)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decodedCode), `"/rk123":"/node-1"`) {
		t.Fatalf("expected route rewrite in function code, got %s", decodedCode)
	}

	var cfg distributionConfigXML
	if err := xml.NewDecoder(bytes.NewReader(postedDistribution)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.CacheBehaviors.Items) != 1 {
		t.Fatalf("expected one cache behavior, got %+v", cfg.CacheBehaviors.Items)
	}
	associations := cfg.CacheBehaviors.Items[0].FunctionAssociations
	if associations.Quantity != 1 || len(associations.Items) != 1 {
		t.Fatalf("expected one function association, got %+v", associations)
	}
	if associations.Items[0].EventType != "viewer-request" || associations.Items[0].FunctionARN != functionARN {
		t.Fatalf("unexpected function association %+v", associations.Items[0])
	}
}

func TestAWSClientApplyDistributionRoutesUpsertsOriginAndBehavior(t *testing.T) {
	var putBody []byte
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"ETAG123"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-1</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>v2ray-platform-node-node-1</Id>
        <DomainName>old.example.com</DomainName>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>default-origin</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
    <Compress>true</Compress>
    <CachePolicyId>cache-policy-1</CachePolicyId>
    <OriginRequestPolicyId>origin-policy-1</OriginRequestPolicyId>
    <ResponseHeadersPolicyId>headers-policy-1</ResponseHeadersPolicyId>
  </DefaultCacheBehavior>
  <CacheBehaviors>
    <Quantity>0</Quantity>
  </CacheBehaviors>
  <Comment>test distribution</Comment>
  <PriceClass>PriceClass_All</PriceClass>
  <Enabled>true</Enabled>
  <ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate>
  <Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
  <HttpVersion>http2</HttpVersion>
  <IsIPV6Enabled>true</IsIPV6Enabled>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			if got := r.Header.Get("If-Match"); got != "ETAG123" {
				t.Fatalf("expected If-Match ETAG123, got %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			putBody = append([]byte(nil), body...)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body:       io.NopCloser(strings.NewReader(`<DistributionConfig/>`)),
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "replace_route", RouteKey: "rk123", OriginID: managedOriginID("node-1"), Host: "new.example.com"},
		{Action: "add_route", RouteKey: "rk456", OriginID: managedOriginID("node-2"), Host: "node-2.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var cfg distributionConfigXML
	if err := xml.NewDecoder(bytes.NewReader(putBody)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Origins.Quantity != 2 {
		t.Fatalf("expected 2 origins, got %d", cfg.Origins.Quantity)
	}
	if cfg.CacheBehaviors.Quantity != 2 {
		t.Fatalf("expected 2 cache behaviors, got %d", cfg.CacheBehaviors.Quantity)
	}
	if cfg.XMLNS != "http://cloudfront.amazonaws.com/doc/2020-05-31/" {
		t.Fatalf("expected cloudfront XML namespace, got %q", cfg.XMLNS)
	}

	originHosts := map[string]string{}
	for _, origin := range cfg.Origins.Items {
		originHosts[origin.ID] = origin.DomainName
	}
	if originHosts[managedOriginID("node-1")] != "new.example.com" {
		t.Fatalf("expected origin-1 host replacement, got %+v", originHosts)
	}
	if originHosts[managedOriginID("node-2")] != "node-2.example.com" {
		t.Fatalf("expected origin-2 host creation, got %+v", originHosts)
	}

	behaviorTargets := map[string]string{}
	for _, behavior := range cfg.CacheBehaviors.Items {
		behaviorTargets[behavior.PathPattern] = behavior.TargetOriginID
		if behavior.PathPattern == "/rk456" && behavior.ViewerProtocolPolicy != "redirect-to-https" {
			t.Fatalf("expected new behavior to inherit viewer policy, got %+v", behavior)
		}
		if behavior.PathPattern == "/rk456" && behavior.CachePolicyID != "cache-policy-1" {
			t.Fatalf("expected new behavior to inherit cache policy, got %+v", behavior)
		}
	}
	if behaviorTargets["/rk123"] != managedOriginID("node-1") || behaviorTargets["/rk456"] != managedOriginID("node-2") {
		t.Fatalf("unexpected behavior targets %+v", behaviorTargets)
	}
	for _, origin := range cfg.Origins.Items {
		if origin.ID == managedOriginID("node-2") && !strings.Contains(origin.CustomOriginConfig.OriginSslProtocols.Inner, "TLSv1.2") {
			t.Fatalf("expected new origin to include TLSv1.2 ssl protocols, got %+v", origin.CustomOriginConfig.OriginSslProtocols)
		}
	}
}

func TestAWSClientApplyDistributionRoutesPreservesForwardedValuesTemplate(t *testing.T) {
	var putBody []byte
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"ETAG789"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-legacy</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins><Quantity>0</Quantity></Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>default-origin</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
    <ForwardedValues><QueryString>false</QueryString><Cookies><Forward>none</Forward></Cookies></ForwardedValues>
  </DefaultCacheBehavior>
  <CacheBehaviors><Quantity>0</Quantity></CacheBehaviors>
  <Comment>legacy distribution</Comment>
  <Enabled>true</Enabled>
  <ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate>
  <Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			putBody = append([]byte(nil), body...)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body:       io.NopCloser(strings.NewReader(`<DistributionConfig/>`)),
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "add_route", RouteKey: "rk789", OriginID: managedOriginID("node-789"), Host: "node-789.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var cfg distributionConfigXML
	if err := xml.NewDecoder(bytes.NewReader(putBody)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.CacheBehaviors.Items) != 1 {
		t.Fatalf("expected one cache behavior, got %+v", cfg.CacheBehaviors.Items)
	}
	behavior := cfg.CacheBehaviors.Items[0]
	if behavior.CachePolicyID != "" {
		t.Fatalf("expected legacy forwarded values instead of cache policy, got %+v", behavior)
	}
	if !strings.Contains(behavior.ForwardedValues.Inner, "<QueryString>false</QueryString>") {
		t.Fatalf("expected forwarded values to be preserved, got %+v", behavior.ForwardedValues)
	}
}

func TestAWSClientApplyDistributionRoutesAttachesRouteRewriteFunction(t *testing.T) {
	var postedFunction createFunctionRequestXML
	var putBody []byte
	functionARN := "arn:aws:cloudfront::123456789012:function/v2ray-platform-route-rewrite"
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"DISTETAG1"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-rewrite</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>v2ray-platform-node-node-1</Id>
        <DomainName>node-1.example.com</DomainName>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>v2ray-platform-node-node-1</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
    <CachePolicyId>cache-policy-1</CachePolicyId>
  </DefaultCacheBehavior>
  <CacheBehaviors>
    <Quantity>1</Quantity>
    <Items>
      <CacheBehavior>
        <PathPattern>/rk123</PathPattern>
        <TargetOriginId>v2ray-platform-node-node-1</TargetOriginId>
        <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
        <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
        <CachePolicyId>cache-policy-1</CachePolicyId>
      </CacheBehavior>
    </Items>
  </CacheBehaviors>
  <Comment>test distribution</Comment>
  <Enabled>true</Enabled>
  <ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate>
  <Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/function":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&postedFunction); err != nil {
				t.Fatal(err)
			}
			return &http.Response{
				StatusCode: http.StatusCreated,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"FUNCETAG2"},
				},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>UNPUBLISHED</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>DEVELOPMENT</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/function/"+routeRewriteFunctionName+"/publish":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>LIVE</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>LIVE</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			if got := r.Header.Get("If-Match"); got != "DISTETAG1" {
				t.Fatalf("expected distribution If-Match DISTETAG1, got %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			putBody = append([]byte(nil), body...)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body:       io.NopCloser(strings.NewReader(`<DistributionConfig/>`)),
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", nil, []RewriteRoute{
		{RouteKey: "rk123", Path: "/node-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	decodedCode, err := base64.StdEncoding.DecodeString(postedFunction.FunctionCode)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decodedCode), `"/rk123":"/node-1"`) {
		t.Fatalf("expected route rewrite in function code, got %s", decodedCode)
	}

	var cfg distributionConfigXML
	if err := xml.NewDecoder(bytes.NewReader(putBody)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.CacheBehaviors.Items) != 1 {
		t.Fatalf("expected one cache behavior, got %+v", cfg.CacheBehaviors.Items)
	}
	associations := cfg.CacheBehaviors.Items[0].FunctionAssociations
	if associations.Quantity != 1 || len(associations.Items) != 1 {
		t.Fatalf("expected one function association, got %+v", associations)
	}
	if associations.Items[0].EventType != "viewer-request" || associations.Items[0].FunctionARN != functionARN {
		t.Fatalf("unexpected function association %+v", associations.Items[0])
	}
}

func TestAWSClientApplyDistributionRoutesUpdatesExistingRouteRewriteFunction(t *testing.T) {
	var updatedFunction updateFunctionRequestXML
	var publishIfMatch string
	var putDistributionCalled bool
	functionARN := "arn:aws:cloudfront::123456789012:function/v2ray-platform-route-rewrite"
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"DISTETAG2"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-rewrite-update</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>v2ray-platform-node-node-1</Id>
        <DomainName>node-1.example.com</DomainName>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>v2ray-platform-node-node-1</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
    <CachePolicyId>cache-policy-1</CachePolicyId>
  </DefaultCacheBehavior>
  <CacheBehaviors>
    <Quantity>1</Quantity>
    <Items>
      <CacheBehavior>
        <PathPattern>/rk123</PathPattern>
        <TargetOriginId>v2ray-platform-node-node-1</TargetOriginId>
        <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
        <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
        <CachePolicyId>cache-policy-1</CachePolicyId>
      </CacheBehavior>
    </Items>
  </CacheBehaviors>
  <Comment>test distribution</Comment>
  <Enabled>true</Enabled>
  <ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate>
  <Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/function":
			return &http.Response{
				StatusCode: http.StatusConflict,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body:       io.NopCloser(strings.NewReader(`<Error><Code>FunctionAlreadyExists</Code></Error>`)),
				Request:    r,
			}, nil
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/function/"+routeRewriteFunctionName+"/describe":
			if got := r.URL.Query().Get("Stage"); got != "DEVELOPMENT" {
				t.Fatalf("expected describe Stage=DEVELOPMENT, got %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"FUNCETAG-OLD"},
				},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>UNPUBLISHED</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>DEVELOPMENT</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/function/"+routeRewriteFunctionName:
			if got := r.Header.Get("If-Match"); got != "FUNCETAG-OLD" {
				t.Fatalf("expected function update If-Match FUNCETAG-OLD, got %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&updatedFunction); err != nil {
				t.Fatal(err)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"FUNCETAG-NEW"},
				},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>UNPUBLISHED</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>DEVELOPMENT</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPost && r.URL.Path == "/2020-05-31/function/"+routeRewriteFunctionName+"/publish":
			publishIfMatch = r.Header.Get("If-Match")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>LIVE</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>LIVE</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			putDistributionCalled = true
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body:       io.NopCloser(strings.NewReader(`<DistributionConfig/>`)),
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", nil, []RewriteRoute{
		{RouteKey: "rk123", Path: "/node-renamed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if publishIfMatch != "FUNCETAG-NEW" {
		t.Fatalf("expected publish to use updated function etag, got %q", publishIfMatch)
	}
	decodedCode, err := base64.StdEncoding.DecodeString(updatedFunction.FunctionCode)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(decodedCode), `"/rk123":"/node-renamed"`) {
		t.Fatalf("expected updated route rewrite in function code, got %s", decodedCode)
	}
	if !putDistributionCalled {
		t.Fatal("expected distribution update to keep function association attached")
	}
}

func TestAWSClientApplyDistributionRoutesRemovesManagedBehaviorAndOrphanOrigin(t *testing.T) {
	var putBody []byte
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"ETAG456"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-2</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins>
    <Quantity>2</Quantity>
    <Items>
      <Origin>
        <Id>v2ray-platform-node-delete</Id>
        <DomainName>delete.example.com</DomainName>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
        </CustomOriginConfig>
      </Origin>
      <Origin>
        <Id>origin-keep</Id>
        <DomainName>keep.example.com</DomainName>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>default-origin</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
    <Compress>true</Compress>
    <CachePolicyId>cache-policy-1</CachePolicyId>
  </DefaultCacheBehavior>
  <CacheBehaviors>
    <Quantity>2</Quantity>
    <Items>
      <CacheBehavior>
        <PathPattern>/gone</PathPattern>
        <TargetOriginId>v2ray-platform-node-delete</TargetOriginId>
        <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
        <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
        <Compress>true</Compress>
        <CachePolicyId>cache-policy-1</CachePolicyId>
      </CacheBehavior>
      <CacheBehavior>
        <PathPattern>/keep</PathPattern>
        <TargetOriginId>origin-keep</TargetOriginId>
        <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
        <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
        <Compress>true</Compress>
        <CachePolicyId>cache-policy-1</CachePolicyId>
      </CacheBehavior>
    </Items>
  </CacheBehaviors>
  <Comment>test distribution</Comment>
  <PriceClass>PriceClass_All</PriceClass>
  <Enabled>true</Enabled>
  <ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate>
  <Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
  <HttpVersion>http2</HttpVersion>
  <IsIPV6Enabled>true</IsIPV6Enabled>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			putBody = append([]byte(nil), body...)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body:       io.NopCloser(strings.NewReader(`<DistributionConfig/>`)),
				Request:    r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "remove_route", RouteKey: "gone", OriginID: "v2ray-platform-node-delete"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}

	var cfg distributionConfigXML
	if err := xml.NewDecoder(bytes.NewReader(putBody)).Decode(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Origins.Quantity != 1 || len(cfg.Origins.Items) != 1 || cfg.Origins.Items[0].ID != "origin-keep" {
		t.Fatalf("expected orphan origin removal, got %+v", cfg.Origins.Items)
	}
	if cfg.CacheBehaviors.Quantity != 1 || len(cfg.CacheBehaviors.Items) != 1 || cfg.CacheBehaviors.Items[0].PathPattern != "/keep" {
		t.Fatalf("expected only keep behavior, got %+v", cfg.CacheBehaviors.Items)
	}
	if cfg.XMLNS != "http://cloudfront.amazonaws.com/doc/2020-05-31/" {
		t.Fatalf("expected cloudfront XML namespace, got %q", cfg.XMLNS)
	}
}

func TestAWSClientApplyDistributionRoutesRefusesUnmanagedRouteRemoval(t *testing.T) {
	putCalled := false
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"ETAG-REMOVE-CONFLICT"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-remove-conflict</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>custom-origin</Id>
        <DomainName>custom.example.com</DomainName>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>custom-origin</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
    <Compress>true</Compress>
    <CachePolicyId>cache-policy-1</CachePolicyId>
  </DefaultCacheBehavior>
  <CacheBehaviors>
    <Quantity>1</Quantity>
    <Items>
      <CacheBehavior>
        <PathPattern>/custom</PathPattern>
        <TargetOriginId>custom-origin</TargetOriginId>
        <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
        <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
        <Compress>true</Compress>
        <CachePolicyId>cache-policy-1</CachePolicyId>
      </CacheBehavior>
    </Items>
  </CacheBehaviors>
  <Comment>test distribution</Comment>
  <PriceClass>PriceClass_All</PriceClass>
  <Enabled>true</Enabled>
  <ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate>
  <Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
  <HttpVersion>http2</HttpVersion>
  <IsIPV6Enabled>true</IsIPV6Enabled>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			putCalled = true
			t.Fatalf("unexpected PUT for unmanaged route removal")
			return nil, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "remove_route", RouteKey: "custom", OriginID: "custom-origin"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unmanaged route") {
		t.Fatalf("expected unmanaged removal error, got %v", err)
	}
	if putCalled {
		t.Fatal("expected no PUT for unmanaged route removal")
	}
}

func TestAWSClientApplyDistributionRoutesRefusesUnmanagedRouteOverwrite(t *testing.T) {
	putCalled := false
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"ETAG-CONFLICT"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-conflict</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>custom-origin</Id>
        <DomainName>custom.example.com</DomainName>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
        </CustomOriginConfig>
      </Origin>
    </Items>
  </Origins>
  <DefaultCacheBehavior>
    <TargetOriginId>custom-origin</TargetOriginId>
    <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
    <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
    <CachePolicyId>cache-policy-1</CachePolicyId>
  </DefaultCacheBehavior>
  <CacheBehaviors>
    <Quantity>1</Quantity>
    <Items>
      <CacheBehavior>
        <PathPattern>/rk123</PathPattern>
        <TargetOriginId>custom-origin</TargetOriginId>
        <ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy>
        <AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods>
        <CachePolicyId>cache-policy-1</CachePolicyId>
      </CacheBehavior>
    </Items>
  </CacheBehaviors>
  <Comment>test distribution</Comment>
  <Enabled>true</Enabled>
  <ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate>
  <Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut:
			putCalled = true
			t.Fatalf("unexpected PUT for unmanaged conflict")
			return nil, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
			return nil, nil
		}
	})}

	client, err := NewAWSClient(AWSClientConfig{
		AccessKeyID:     "AKIATEST",
		SecretAccessKey: "secret",
		Region:          "us-east-1",
		Endpoint:        "https://cloudfront.amazonaws.com",
		HTTPClient:      httpClient,
		Now: func() time.Time {
			return time.Unix(0, 0).UTC()
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "add_route", RouteKey: "rk123", OriginID: managedOriginID("node-1"), Host: "node-1.example.com"},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "unmanaged origin") {
		t.Fatalf("expected unmanaged conflict error, got %v", err)
	}
	if putCalled {
		t.Fatal("expected no PUT for unmanaged conflict")
	}
}
