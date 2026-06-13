package cloudfront

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"reflect"
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

func TestAWSClientListDistributionsUsesAliasWhenDomainIsPlaceholder(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionList>
  <Items>
    <DistributionSummary>
      <Id>DIST123</Id>
      <Status>Deployed</Status>
      <DomainName>-</DomainName>
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	items, err := client.ListDistributions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := items[0].DomainName; got != "edge.example.com" {
		t.Fatalf("expected alias fallback domain, got %q", got)
	}
}

func TestAWSClientListDistributionsUsesTenantDomainWhenStandardDomainIsMissing(t *testing.T) {
	requests := []string{}
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		switch r.URL.RequestURI() {
		case "/2020-05-31/distribution":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionList>
  <Items>
    <DistributionSummary>
      <Id>DIST123</Id>
      <Status>Deployed</Status>
      <DomainName>-</DomainName>
    </DistributionSummary>
  </Items>
</DistributionList>`)),
				Request: r,
			}, nil
		case "/2020-05-31/distribution-tenants":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			var req listDistributionTenantsRequestXML
			if err := xml.NewDecoder(bytes.NewReader(body)).Decode(&req); err != nil {
				t.Fatal(err)
			}
			if req.AssociationFilter != nil {
				t.Fatalf("expected tenant list request without server-side distribution filter, got %+v", req)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<ListDistributionTenantsResult>
  <DistributionTenantList>
    <DistributionTenantSummary>
      <DistributionId>DIST123</DistributionId>
      <Domains>
        <member><Domain>tenant-a.example.com</Domain></member>
        <member><Domain>tenant-b.example.com</Domain></member>
      </Domains>
    </DistributionTenantSummary>
  </DistributionTenantList>
</ListDistributionTenantsResult>`)),
				Request: r,
			}, nil
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.RequestURI())
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

	items, err := client.ListDistributions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got := items[0].DomainName; got != "tenant-a.example.com" {
		t.Fatalf("expected tenant domain fallback, got %q", got)
	}
	if len(items[0].Aliases) != 2 || items[0].Aliases[1] != "tenant-b.example.com" {
		t.Fatalf("expected tenant domains in aliases, got %+v", items[0].Aliases)
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
    <DefaultCacheBehavior>
      <TargetOriginId>origin-1</TargetOriginId>
    </DefaultCacheBehavior>
    <CacheBehaviors>
      <Items>
        <CacheBehavior>
          <PathPattern>/rk123</PathPattern>
          <TargetOriginId>origin-1</TargetOriginId>
        </CacheBehavior>
      </Items>
    </CacheBehaviors>
    <TenantConfig>
      <ParameterDefinitions>
        <member>
          <Name>tenantHost</Name>
          <Definition>
            <StringSchema>
              <Required>true</Required>
              <Comment>Tenant hostname</Comment>
              <DefaultValue>edge.example.com</DefaultValue>
            </StringSchema>
          </Definition>
        </member>
      </ParameterDefinitions>
    </TenantConfig>
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
	if len(dist.Behaviors) != 2 {
		t.Fatalf("unexpected behaviors %+v", dist.Behaviors)
	}
	if dist.Behaviors[0].PathPattern != "/" || dist.Behaviors[0].OriginID != "origin-1" || !dist.Behaviors[0].IsDefault {
		t.Fatalf("expected default behavior first, got %+v", dist.Behaviors[0])
	}
	if dist.Behaviors[1].PathPattern != "/rk123" || dist.Behaviors[1].OriginID != "origin-1" || dist.Behaviors[1].IsDefault {
		t.Fatalf("expected custom behavior second, got %+v", dist.Behaviors[1])
	}
	if len(dist.Parameters) != 1 || dist.Parameters[0].Name != "tenantHost" || !dist.Parameters[0].Required {
		t.Fatalf("unexpected parameters %+v", dist.Parameters)
	}
}

func TestAWSClientGetDistributionUsesAliasWhenDomainIsPlaceholder(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/xml"}},
			Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<Distribution>
  <Id>DIST123</Id>
  <Status>Deployed</Status>
  <DomainName>-</DomainName>
  <DistributionConfig>
    <Comment>Main distribution</Comment>
    <Aliases>
      <Items>
        <CNAME>edge.example.com</CNAME>
      </Items>
    </Aliases>
    <Origins><Items></Items></Origins>
    <CacheBehaviors><Items></Items></CacheBehaviors>
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	dist, err := client.GetDistribution(t.Context(), "DIST123")
	if err != nil {
		t.Fatal(err)
	}
	if got := dist.DomainName; got != "edge.example.com" {
		t.Fatalf("expected alias fallback domain, got %q", got)
	}
}

func TestAWSClientGetDistributionUsesTenantDomainWhenStandardDomainIsMissing(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.RequestURI() {
		case "/2020-05-31/distribution/DIST123":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<Distribution>
  <Id>DIST123</Id>
  <Status>Deployed</Status>
  <DomainName>-</DomainName>
  <DistributionConfig>
    <Comment>Main distribution</Comment>
    <Aliases><Quantity>0</Quantity></Aliases>
    <Origins><Items></Items></Origins>
    <CacheBehaviors><Items></Items></CacheBehaviors>
  </DistributionConfig>
</Distribution>`)),
				Request: r,
			}, nil
		case "/2020-05-31/distribution-tenants":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<ListDistributionTenantsResult>
  <DistributionTenantList>
    <DistributionTenantSummary>
      <DistributionId>DIST123</DistributionId>
      <Domains>
        <member><Domain>c.telaria.me</Domain></member>
      </Domains>
    </DistributionTenantSummary>
  </DistributionTenantList>
</ListDistributionTenantsResult>`)),
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	dist, err := client.GetDistribution(t.Context(), "DIST123")
	if err != nil {
		t.Fatal(err)
	}
	if got := dist.DomainName; got != "c.telaria.me" {
		t.Fatalf("expected tenant domain fallback, got %q", got)
	}
	if len(dist.Aliases) != 1 || dist.Aliases[0] != "c.telaria.me" {
		t.Fatalf("expected tenant domain aliases, got %+v", dist.Aliases)
	}
}

func TestAWSClientGetDistributionReturnsTenantPermissionErrorWhenLookupDenied(t *testing.T) {
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.RequestURI() {
		case "/2020-05-31/distribution/DIST123":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<Distribution>
  <Id>DIST123</Id>
  <Status>Deployed</Status>
  <DomainName>-</DomainName>
  <DistributionConfig>
    <Comment>Main distribution</Comment>
    <Aliases><Quantity>0</Quantity></Aliases>
    <Origins><Items></Items></Origins>
    <CacheBehaviors><Items></Items></CacheBehaviors>
  </DistributionConfig>
</Distribution>`)),
				Request: r,
			}, nil
		case "/2020-05-31/distribution-tenants":
			return &http.Response{
				StatusCode: http.StatusForbidden,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?>
<ErrorResponse xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/"><Error><Type>Sender</Type><Code>AccessDenied</Code><Message>missing permission</Message></Error></ErrorResponse>`)),
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GetDistribution(t.Context(), "DIST123")
	if err == nil {
		t.Fatal("expected permission error")
	}
	if !strings.Contains(err.Error(), "cloudfront:ListDistributionTenants") {
		t.Fatalf("expected tenant permission guidance, got %v", err)
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
	if !strings.Contains(string(cfg.Origins.Items[0].CustomOriginConfig.OriginSslProtocols), "TLSv1.2") {
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
  <DefaultRootObject></DefaultRootObject>
  <Origins>
    <Quantity>1</Quantity>
    <Items>
      <Origin>
        <Id>v2ray-platform-node-node-1</Id>
        <DomainName>old.example.com</DomainName>
        <OriginPath></OriginPath>
        <CustomOriginConfig>
          <HTTPPort>80</HTTPPort>
          <HTTPSPort>443</HTTPSPort>
          <OriginProtocolPolicy>https-only</OriginProtocolPolicy>
          <OriginReadTimeout>30</OriginReadTimeout>
          <OriginKeepaliveTimeout>5</OriginKeepaliveTimeout>
        </CustomOriginConfig>
        <ConnectionAttempts>3</ConnectionAttempts>
        <ConnectionTimeout>10</ConnectionTimeout>
        <OriginAccessControlId></OriginAccessControlId>
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
  <ConnectionMode>direct</ConnectionMode>
  <WebACLId></WebACLId>
  <HttpVersion>http2</HttpVersion>
  <IsIPV6Enabled>true</IsIPV6Enabled>
  <ContinuousDeploymentPolicyId></ContinuousDeploymentPolicyId>
  <AnycastIpListId></AnycastIpListId>
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
	if !strings.Contains(string(putBody), "<Comment>test distribution</Comment>") {
		t.Fatalf("expected comment to be preserved in marshaled config, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<PriceClass>PriceClass_All</PriceClass>") {
		t.Fatalf("expected price class to be preserved in marshaled config, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<DefaultRootObject></DefaultRootObject>") {
		t.Fatalf("expected empty default root object to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<ConnectionMode>direct</ConnectionMode>") {
		t.Fatalf("expected connection mode to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<OriginPath></OriginPath>") {
		t.Fatalf("expected empty origin path to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<OriginAccessControlId></OriginAccessControlId>") {
		t.Fatalf("expected empty origin access control id to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<OriginReadTimeout>30</OriginReadTimeout>") {
		t.Fatalf("expected origin read timeout to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<OriginKeepaliveTimeout>5</OriginKeepaliveTimeout>") {
		t.Fatalf("expected origin keepalive timeout to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<ConnectionAttempts>3</ConnectionAttempts>") {
		t.Fatalf("expected connection attempts to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<ConnectionTimeout>10</ConnectionTimeout>") {
		t.Fatalf("expected connection timeout to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<CustomHeaders><Quantity>0</Quantity></CustomHeaders>") {
		t.Fatalf("expected empty custom headers to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<OriginShield><Enabled>false</Enabled></OriginShield>") {
		t.Fatalf("expected origin shield default to be preserved, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<DefaultCacheBehavior><TargetOriginId>default-origin</TargetOriginId>") {
		t.Fatalf("expected default cache behavior to remain present, got %s", putBody)
	}
	for _, expected := range []string{
		"<SmoothStreaming>false</SmoothStreaming>",
		"<TrustedSigners><Enabled>false</Enabled><Quantity>0</Quantity></TrustedSigners>",
		"<TrustedKeyGroups><Enabled>false</Enabled><Quantity>0</Quantity></TrustedKeyGroups>",
		"<LambdaFunctionAssociations><Quantity>0</Quantity></LambdaFunctionAssociations>",
		"<GrpcConfig><Enabled>false</Enabled></GrpcConfig>",
	} {
		if !strings.Contains(string(putBody), expected) {
			t.Fatalf("expected behavior defaults to include %s, got %s", expected, putBody)
		}
	}
	if strings.Contains(string(putBody), "<ForwardedValues></ForwardedValues>") {
		t.Fatalf("expected empty forwarded values to be omitted, got %s", putBody)
	}
	if strings.Contains(string(putBody), "<RealtimeLogConfigArn></RealtimeLogConfigArn>") {
		t.Fatalf("expected empty realtime log config arn to be omitted, got %s", putBody)
	}
	if !strings.Contains(string(putBody), "<FieldLevelEncryptionId></FieldLevelEncryptionId>") {
		t.Fatalf("expected empty field level encryption id to be preserved, got %s", putBody)
	}
	if strings.Contains(string(putBody), "<TenantConfig>") {
		t.Fatalf("expected empty tenant config to be omitted for direct distributions, got %s", putBody)
	}
	for _, origin := range cfg.Origins.Items {
		if origin.ID == managedOriginID("node-2") && !strings.Contains(string(origin.CustomOriginConfig.OriginSslProtocols), "TLSv1.2") {
			t.Fatalf("expected new origin to include TLSv1.2 ssl protocols, got %+v", origin.CustomOriginConfig.OriginSslProtocols)
		}
	}
}

func TestAWSClientApplyDistributionRoutesKeepsEmptyCommentElement(t *testing.T) {
	var putBody []byte
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"ETAGEMPTYCOMMENT"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-empty-comment</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins><Quantity>1</Quantity><Items><Origin><Id>v2ray-platform-node-node-1</Id><DomainName>node-1.example.com</DomainName><CustomOriginConfig><HTTPPort>80</HTTPPort><HTTPSPort>443</HTTPSPort><OriginProtocolPolicy>https-only</OriginProtocolPolicy></CustomOriginConfig></Origin></Items></Origins>
  <DefaultCacheBehavior><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></DefaultCacheBehavior>
  <CacheBehaviors><Quantity>0</Quantity></CacheBehaviors>
  <Comment></Comment>
  <Enabled>true</Enabled>
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
		{Action: "replace_route", RouteKey: "rk123", OriginID: managedOriginID("node-1"), Host: "node-1.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(putBody), "<Comment></Comment>") {
		t.Fatalf("expected empty comment element to be preserved, got %s", putBody)
	}
}

func TestAWSClientApplyDistributionRoutesDefaultsMissingPriceClass(t *testing.T) {
	var putBody []byte
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"ETAGNOPRICECLASS"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-no-price-class</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins><Quantity>1</Quantity><Items><Origin><Id>v2ray-platform-node-node-1</Id><DomainName>node-1.example.com</DomainName><CustomOriginConfig><HTTPPort>80</HTTPPort><HTTPSPort>443</HTTPSPort><OriginProtocolPolicy>https-only</OriginProtocolPolicy></CustomOriginConfig></Origin></Items></Origins>
  <DefaultCacheBehavior><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></DefaultCacheBehavior>
  <CacheBehaviors><Quantity>0</Quantity></CacheBehaviors>
  <Comment>test distribution</Comment>
  <Enabled>true</Enabled>
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
		{Action: "replace_route", RouteKey: "rk123", OriginID: managedOriginID("node-1"), Host: "node-1.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(putBody), "<PriceClass>PriceClass_All</PriceClass>") {
		t.Fatalf("expected missing price class to default to PriceClass_All, got %s", putBody)
	}
}

func TestDistributionConfigNormalizeDefaultsMissingBehaviorFields(t *testing.T) {
	cfg := distributionConfigXML{
		TenantConfig: &tenantConfigXML{},
		DefaultCacheBehavior: defaultCacheBehaviorXML{
			TargetOriginID: "default-origin",
		},
		CacheBehaviors: cacheBehaviorsXML{
			Items: []cacheBehaviorXML{
				{
					PathPattern:    "/rk123",
					TargetOriginID: "origin-1",
				},
			},
		},
	}

	cfg.normalize()

	if cfg.TenantConfig != nil {
		t.Fatalf("expected empty tenant config to be removed during normalize, got %+v", cfg.TenantConfig)
	}

	if cfg.DefaultCacheBehavior.SmoothStreaming == nil || *cfg.DefaultCacheBehavior.SmoothStreaming {
		t.Fatalf("expected default cache behavior smooth streaming to default false, got %+v", cfg.DefaultCacheBehavior.SmoothStreaming)
	}
	if cfg.DefaultCacheBehavior.Compress == nil || *cfg.DefaultCacheBehavior.Compress {
		t.Fatalf("expected default cache behavior compress to default false, got %+v", cfg.DefaultCacheBehavior.Compress)
	}
	if cfg.DefaultCacheBehavior.TrustedSigners != passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`) {
		t.Fatalf("unexpected default trusted signers %+v", cfg.DefaultCacheBehavior.TrustedSigners)
	}
	if cfg.DefaultCacheBehavior.TrustedKeyGroups != passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`) {
		t.Fatalf("unexpected default trusted key groups %+v", cfg.DefaultCacheBehavior.TrustedKeyGroups)
	}
	if cfg.DefaultCacheBehavior.LambdaFunctionAssociations != passthroughXML(`<Quantity>0</Quantity>`) {
		t.Fatalf("unexpected default lambda associations %+v", cfg.DefaultCacheBehavior.LambdaFunctionAssociations)
	}
	if cfg.DefaultCacheBehavior.GrpcConfig != passthroughXML(`<Enabled>false</Enabled>`) {
		t.Fatalf("unexpected default grpc config %+v", cfg.DefaultCacheBehavior.GrpcConfig)
	}

	behavior := cfg.CacheBehaviors.Items[0]
	if behavior.SmoothStreaming == nil || *behavior.SmoothStreaming {
		t.Fatalf("expected cache behavior smooth streaming to default false, got %+v", behavior.SmoothStreaming)
	}
	if behavior.Compress == nil || *behavior.Compress {
		t.Fatalf("expected cache behavior compress to default false, got %+v", behavior.Compress)
	}
	if behavior.TrustedSigners != passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`) {
		t.Fatalf("unexpected behavior trusted signers %+v", behavior.TrustedSigners)
	}
	if behavior.TrustedKeyGroups != passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`) {
		t.Fatalf("unexpected behavior trusted key groups %+v", behavior.TrustedKeyGroups)
	}
	if behavior.LambdaFunctionAssociations != passthroughXML(`<Quantity>0</Quantity>`) {
		t.Fatalf("unexpected behavior lambda associations %+v", behavior.LambdaFunctionAssociations)
	}
	if behavior.GrpcConfig != passthroughXML(`<Enabled>false</Enabled>`) {
		t.Fatalf("unexpected behavior grpc config %+v", behavior.GrpcConfig)
	}
}

func TestDistributionConfigNormalizeOmitsPriceClassForTenantOnlyMode(t *testing.T) {
	trueValue := true

	cfg := distributionConfigXML{
		ConnectionMode:               "tenant-only",
		ContinuousDeploymentPolicyID: "policy-123",
		AnycastIPListID:              "list-123",
		IsIPV6Enabled:                true,
		Aliases:                      &aliasesXML{Quantity: 1, Items: []string{"edge.example.com"}},
		DefaultCacheBehavior: defaultCacheBehaviorXML{
			TargetOriginID:   "default-origin",
			TrustedSigners:   passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`),
			SmoothStreaming:  &trueValue,
			TrustedKeyGroups: passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`),
			GrpcConfig:       passthroughXML(`<Enabled>false</Enabled>`),
			Compress:         &trueValue,
			CachePolicyID:    "cache-policy-1",
		},
		CacheBehaviors: cacheBehaviorsXML{
			Items: []cacheBehaviorXML{
				{
					PathPattern:      "/rk123",
					TargetOriginID:   "origin-1",
					TrustedSigners:   passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`),
					SmoothStreaming:  &trueValue,
					TrustedKeyGroups: passthroughXML(`<Enabled>false</Enabled><Quantity>0</Quantity>`),
					Compress:         &trueValue,
					CachePolicyID:    "cache-policy-1",
				},
			},
		},
	}

	cfg.normalize()

	if cfg.PriceClass != "" {
		t.Fatalf("expected tenant-only distributions to omit price class, got %q", cfg.PriceClass)
	}
	if cfg.ContinuousDeploymentPolicyID != "" {
		t.Fatalf("expected tenant-only distributions to omit continuous deployment policy id, got %q", cfg.ContinuousDeploymentPolicyID)
	}
	if cfg.AnycastIPListID != "" {
		t.Fatalf("expected tenant-only distributions to omit anycast ip list id, got %q", cfg.AnycastIPListID)
	}
	if cfg.IsIPV6Enabled {
		t.Fatal("expected tenant-only distributions to omit ipv6 toggle")
	}
	if cfg.Aliases != nil {
		t.Fatalf("expected tenant-only distributions to omit aliases, got %+v", cfg.Aliases)
	}
	if cfg.DefaultCacheBehavior.TrustedSigners != "" {
		t.Fatalf("expected tenant-only default behavior to omit trusted signers, got %+v", cfg.DefaultCacheBehavior.TrustedSigners)
	}
	if cfg.DefaultCacheBehavior.SmoothStreaming != nil {
		t.Fatalf("expected tenant-only default behavior to omit smooth streaming, got %+v", cfg.DefaultCacheBehavior.SmoothStreaming)
	}
	if cfg.CacheBehaviors.Items[0].TrustedSigners != "" {
		t.Fatalf("expected tenant-only cache behavior to omit trusted signers, got %+v", cfg.CacheBehaviors.Items[0].TrustedSigners)
	}
	if cfg.CacheBehaviors.Items[0].SmoothStreaming != nil {
		t.Fatalf("expected tenant-only cache behavior to omit smooth streaming, got %+v", cfg.CacheBehaviors.Items[0].SmoothStreaming)
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
	if !strings.Contains(string(behavior.ForwardedValues), "<QueryString>false</QueryString>") {
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

func TestAWSClientApplyDistributionRoutesRefetchesFunctionETagWhenUpdateResponseOmitsIt(t *testing.T) {
	var publishIfMatch string
	var describeCalls int
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
  <Origins><Quantity>1</Quantity><Items><Origin><Id>v2ray-platform-node-node-1</Id><DomainName>node-1.example.com</DomainName><CustomOriginConfig><HTTPPort>80</HTTPPort><HTTPSPort>443</HTTPSPort><OriginProtocolPolicy>https-only</OriginProtocolPolicy></CustomOriginConfig></Origin></Items></Origins>
  <DefaultCacheBehavior><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></DefaultCacheBehavior>
  <CacheBehaviors><Quantity>1</Quantity><Items><CacheBehavior><PathPattern>/rk123</PathPattern><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></CacheBehavior></Items></CacheBehaviors>
  <Comment>test distribution</Comment><Enabled>true</Enabled><ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate><Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
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
			describeCalls++
			etag := "FUNCETAG-OLD"
			if describeCalls > 1 {
				etag = "FUNCETAG-REFETCHED"
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{etag},
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
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", nil, []RewriteRoute{{RouteKey: "rk123", Path: "/node-renamed"}})
	if err != nil {
		t.Fatal(err)
	}
	if describeCalls < 2 {
		t.Fatalf("expected ETag refetch after update response omitted it, got %d describe calls", describeCalls)
	}
	if publishIfMatch != "FUNCETAG-REFETCHED" {
		t.Fatalf("expected publish to use refetched ETag, got %q", publishIfMatch)
	}
}

func TestAWSClientApplyDistributionRoutesRetriesOnDistributionPreconditionFailed(t *testing.T) {
	var ifMatches []string
	var getConfigCalls int
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			getConfigCalls++
			etag := "DISTETAG-1"
			if getConfigCalls > 1 {
				etag = "DISTETAG-2"
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{etag},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-precondition-retry</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins><Quantity>1</Quantity><Items><Origin><Id>v2ray-platform-node-node-1</Id><DomainName>node-1.example.com</DomainName><CustomOriginConfig><HTTPPort>80</HTTPPort><HTTPSPort>443</HTTPSPort><OriginProtocolPolicy>https-only</OriginProtocolPolicy></CustomOriginConfig></Origin></Items></Origins>
  <DefaultCacheBehavior><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></DefaultCacheBehavior>
  <CacheBehaviors><Quantity>0</Quantity></CacheBehaviors>
  <Comment>retry distribution</Comment>
  <PriceClass>PriceClass_All</PriceClass>
  <Enabled>true</Enabled>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			ifMatches = append(ifMatches, r.Header.Get("If-Match"))
			if len(ifMatches) == 1 {
				return &http.Response{
					StatusCode: http.StatusPreconditionFailed,
					Header:     http.Header{"Content-Type": []string{"application/xml"}},
					Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?>
<ErrorResponse xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/"><Error><Type>Sender</Type><Code>PreconditionFailed</Code><Message>etag changed</Message></Error></ErrorResponse>`)),
					Request: r,
				}, nil
			}
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "replace_route", RouteKey: "rk123", OriginID: managedOriginID("node-1"), Host: "node-1.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if getConfigCalls != 2 {
		t.Fatalf("expected config refetch after precondition failure, got %d get calls", getConfigCalls)
	}
	if len(ifMatches) != 2 || ifMatches[0] != "DISTETAG-1" || ifMatches[1] != "DISTETAG-2" {
		t.Fatalf("expected retry with refreshed etags, got %+v", ifMatches)
	}
}

func TestAWSClientApplyDistributionRoutesRetriesMultipleDistributionPreconditionFailures(t *testing.T) {
	var ifMatches []string
	var getConfigCalls int
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			getConfigCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{fmt.Sprintf("DISTETAG-%d", getConfigCalls)},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-precondition-retry-multi</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins><Quantity>1</Quantity><Items><Origin><Id>v2ray-platform-node-node-1</Id><DomainName>node-1.example.com</DomainName><CustomOriginConfig><HTTPPort>80</HTTPPort><HTTPSPort>443</HTTPSPort><OriginProtocolPolicy>https-only</OriginProtocolPolicy></CustomOriginConfig></Origin></Items></Origins>
  <DefaultCacheBehavior><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></DefaultCacheBehavior>
  <CacheBehaviors><Quantity>0</Quantity></CacheBehaviors>
  <Comment>retry distribution multiple</Comment>
  <PriceClass>PriceClass_All</PriceClass>
  <Enabled>true</Enabled>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			ifMatches = append(ifMatches, r.Header.Get("If-Match"))
			if len(ifMatches) < 3 {
				return &http.Response{
					StatusCode: http.StatusPreconditionFailed,
					Header:     http.Header{"Content-Type": []string{"application/xml"}},
					Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?>
<ErrorResponse xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/"><Error><Type>Sender</Type><Code>PreconditionFailed</Code><Message>etag changed</Message></Error></ErrorResponse>`)),
					Request: r,
				}, nil
			}
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "replace_route", RouteKey: "rk123", OriginID: managedOriginID("node-1"), Host: "node-1.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if getConfigCalls != 3 {
		t.Fatalf("expected config refetch after each precondition failure, got %d get calls", getConfigCalls)
	}
	expectedIfMatches := []string{"DISTETAG-1", "DISTETAG-2", "DISTETAG-3"}
	if !reflect.DeepEqual(ifMatches, expectedIfMatches) {
		t.Fatalf("expected retry with refreshed etags, got %+v", ifMatches)
	}
}

func TestAWSClientApplyDistributionRoutesWaitsBeforeRetryingDistributionPreconditionFailures(t *testing.T) {
	var ifMatches []string
	var getConfigCalls int
	var sleepCalls int
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			getConfigCalls++
			etag := "DISTETAG-1"
			if sleepCalls >= 2 {
				etag = "DISTETAG-3"
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{etag},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-precondition-retry-wait</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins><Quantity>1</Quantity><Items><Origin><Id>v2ray-platform-node-node-1</Id><DomainName>node-1.example.com</DomainName><CustomOriginConfig><HTTPPort>80</HTTPPort><HTTPSPort>443</HTTPSPort><OriginProtocolPolicy>https-only</OriginProtocolPolicy></CustomOriginConfig></Origin></Items></Origins>
  <DefaultCacheBehavior><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></DefaultCacheBehavior>
  <CacheBehaviors><Quantity>0</Quantity></CacheBehaviors>
  <Comment>retry distribution wait</Comment>
  <PriceClass>PriceClass_All</PriceClass>
  <Enabled>true</Enabled>
</DistributionConfig>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			ifMatches = append(ifMatches, r.Header.Get("If-Match"))
			if r.Header.Get("If-Match") != "DISTETAG-3" {
				return &http.Response{
					StatusCode: http.StatusPreconditionFailed,
					Header:     http.Header{"Content-Type": []string{"application/xml"}},
					Body: io.NopCloser(strings.NewReader(`<?xml version="1.0"?>
<ErrorResponse xmlns="http://cloudfront.amazonaws.com/doc/2020-05-31/"><Error><Type>Sender</Type><Code>PreconditionFailed</Code><Message>etag changed</Message></Error></ErrorResponse>`)),
					Request: r,
				}, nil
			}
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
		Sleep: func(_ context.Context, _ time.Duration) error {
			sleepCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", []RouteAction{
		{Action: "replace_route", RouteKey: "rk123", OriginID: managedOriginID("node-1"), Host: "node-1.example.com"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sleepCalls != 2 {
		t.Fatalf("expected backoff between precondition retries, got %d sleeps", sleepCalls)
	}
	if getConfigCalls != 3 {
		t.Fatalf("expected config refetch after each wait, got %d get calls", getConfigCalls)
	}
	expectedIfMatches := []string{"DISTETAG-1", "DISTETAG-1", "DISTETAG-3"}
	if !reflect.DeepEqual(ifMatches, expectedIfMatches) {
		t.Fatalf("expected retries to wait for a newer etag, got %+v", ifMatches)
	}
}

func TestAWSClientApplyDistributionRoutesSkipsDistributionUpdateWhenRewriteAssociationIsAlreadyCurrent(t *testing.T) {
	var putDistributionCalls int
	functionARN := "arn:aws:cloudfront::123456789012:function/v2ray-platform-route-rewrite"
	httpClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2020-05-31/distribution/DIST123/config":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"DISTETAG-STABLE"},
				},
				Body: io.NopCloser(strings.NewReader(`<?xml version="1.0" encoding="UTF-8"?>
<DistributionConfig>
  <CallerReference>ref-rewrite-noop</CallerReference>
  <Aliases><Quantity>0</Quantity></Aliases>
  <Origins><Quantity>1</Quantity><Items><Origin><Id>v2ray-platform-node-node-1</Id><DomainName>node-1.example.com</DomainName><CustomOriginConfig><HTTPPort>80</HTTPPort><HTTPSPort>443</HTTPSPort><OriginProtocolPolicy>https-only</OriginProtocolPolicy></CustomOriginConfig></Origin></Items></Origins>
  <DefaultCacheBehavior><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId></DefaultCacheBehavior>
  <CacheBehaviors><Quantity>1</Quantity><Items><CacheBehavior><PathPattern>/rk123</PathPattern><TargetOriginId>v2ray-platform-node-node-1</TargetOriginId><ViewerProtocolPolicy>redirect-to-https</ViewerProtocolPolicy><AllowedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items><CachedMethods><Quantity>2</Quantity><Items><Method>GET</Method><Method>HEAD</Method></Items></CachedMethods></AllowedMethods><CachePolicyId>cache-policy-1</CachePolicyId><FunctionAssociations><Quantity>1</Quantity><Items><FunctionAssociation><EventType>viewer-request</EventType><FunctionARN>` + functionARN + `</FunctionARN></FunctionAssociation></Items></FunctionAssociations></CacheBehavior></Items></CacheBehaviors>
  <Comment>rewrite already attached</Comment><Enabled>true</Enabled><ViewerCertificate><CloudFrontDefaultCertificate>true</CloudFrontDefaultCertificate></ViewerCertificate><Restrictions><GeoRestriction><RestrictionType>none</RestrictionType><Quantity>0</Quantity></GeoRestriction></Restrictions>
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
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{
					"Content-Type": []string{"application/xml"},
					"Etag":         []string{"FUNCETAG-STABLE"},
				},
				Body: io.NopCloser(strings.NewReader(`<FunctionSummary>
  <Name>v2ray-platform-route-rewrite</Name>
  <Status>LIVE</Status>
  <FunctionConfig><Runtime>cloudfront-js-2.0</Runtime></FunctionConfig>
  <FunctionMetadata><FunctionARN>` + functionARN + `</FunctionARN><Stage>LIVE</Stage></FunctionMetadata>
</FunctionSummary>`)),
				Request: r,
			}, nil
		case r.Method == http.MethodPut && r.URL.Path == "/2020-05-31/function/"+routeRewriteFunctionName:
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/xml"}},
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
			putDistributionCalls++
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
		Now:             func() time.Time { return time.Unix(0, 0).UTC() },
	})
	if err != nil {
		t.Fatal(err)
	}

	err = client.ApplyDistributionRoutes(context.Background(), "DIST123", nil, []RewriteRoute{{RouteKey: "rk123", Path: "/node-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if putDistributionCalls != 0 {
		t.Fatalf("expected no distribution update when rewrite association is already current, got %d puts", putDistributionCalls)
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
