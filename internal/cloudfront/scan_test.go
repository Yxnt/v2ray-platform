package cloudfront

import (
	"context"
	"testing"

	"v2ray-platform/internal/store"
)

type mockScanClient struct {
	state *DistributionState
	err   error
}

func (m *mockScanClient) ListDistributions(ctx context.Context) ([]DistributionSummary, error) {
	return nil, m.err
}

func (m *mockScanClient) GetDistribution(ctx context.Context, id string) (*DistributionState, error) {
	return m.state, m.err
}

func (m *mockScanClient) ApplyDistributionRoutes(ctx context.Context, id string, actions []RouteAction, rewrites []RewriteRoute) error {
	return nil
}

func TestScanDistributionSuccess(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	client := &mockScanClient{
		state: &DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []OriginState{
				{OriginID: "origin-1", DomainName: "node1.example.com"},
			},
			Behaviors: []BehaviorState{{PathPattern: "/key1234", OriginID: "origin-1"}},
		},
	}

	svc := NewScanService(memStore, client)
	result, err := svc.ScanDistribution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.DistributionID != "E1234" {
		t.Fatalf("expected distributionID 'E1234', got '%s'", result.DistributionID)
	}
	if result.DomainName != "d1234.cloudfront.net" {
		t.Fatalf("expected domainName 'd1234.cloudfront.net', got '%s'", result.DomainName)
	}
	if len(result.Origins) != 1 {
		t.Fatalf("expected 1 origin, got %d", len(result.Origins))
	}
	if result.Origins[0].OriginID != "origin-1" {
		t.Fatalf("expected originID 'origin-1', got '%s'", result.Origins[0].OriginID)
	}
	if result.Origins[0].Host != "node1.example.com" {
		t.Fatalf("expected host 'node1.example.com', got '%s'", result.Origins[0].Host)
	}
	if result.Origins[0].RouteKey != "key1234" {
		t.Fatalf("expected routeKey 'key1234', got '%s'", result.Origins[0].RouteKey)
	}

	// Verify origins were persisted
	cfg, err := memStore.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OriginsJSON == "" || cfg.OriginsJSON == "[]" {
		t.Fatal("expected origins to be persisted in config")
	}
}

func TestScanDistributionNoConfig(t *testing.T) {
	memStore := store.NewMemoryStore()
	client := &mockScanClient{}
	svc := NewScanService(memStore, client)
	_, err := svc.ScanDistribution(context.Background())
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestScanDistributionEmptyDistributionID(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	// DistributionID is empty by default
	client := &mockScanClient{}
	svc := NewScanService(memStore, client)
	_, err := svc.ScanDistribution(context.Background())
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound for empty distributionID, got %v", err)
	}
}

func TestScanDistributionClientError(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	client := &mockScanClient{
		err: context.DeadlineExceeded,
	}

	svc := NewScanService(memStore, client)
	_, err := svc.ScanDistribution(context.Background())
	if err != context.DeadlineExceeded {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestScanDistributionMultipleOrigins(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E5678", "d5678.cloudfront.net", "managed")

	client := &mockScanClient{
		state: &DistributionState{
			DistributionID: "E5678",
			DomainName:     "d5678.cloudfront.net",
			Origins: []OriginState{
				{OriginID: "origin-1", DomainName: "node1.example.com"},
				{OriginID: "origin-2", DomainName: "node2.example.com"},
				{OriginID: "origin-3", DomainName: "node3.example.com"},
			},
			Behaviors: []BehaviorState{
				{PathPattern: "/aaa111", OriginID: "origin-1"},
				{PathPattern: "/bbb222", OriginID: "origin-2"},
				{PathPattern: "/ccc333", OriginID: "origin-3"},
			},
		},
	}

	svc := NewScanService(memStore, client)
	result, err := svc.ScanDistribution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Origins) != 3 {
		t.Fatalf("expected 3 origins, got %d", len(result.Origins))
	}
	expectedRouteKeys := map[string]bool{"aaa111": true, "bbb222": true, "ccc333": true}
	for _, o := range result.Origins {
		if !expectedRouteKeys[o.RouteKey] {
			t.Fatalf("unexpected routeKey '%s'", o.RouteKey)
		}
	}
}

func TestScanDistributionIgnoresRootBehavior(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	client := &mockScanClient{
		state: &DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []OriginState{
				{OriginID: "default-origin", DomainName: "default.example.com"},
			},
			Behaviors: []BehaviorState{{PathPattern: "/", OriginID: "default-origin"}},
		},
	}

	svc := NewScanService(memStore, client)
	result, err := svc.ScanDistribution(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Origins) != 0 {
		t.Fatalf("expected root behavior to be ignored, got %+v", result.Origins)
	}
}
