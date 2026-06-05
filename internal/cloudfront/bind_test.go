package cloudfront

import (
	"encoding/json"
	"testing"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

func TestBindNodesMatchesByRouteKey(t *testing.T) {
	memStore := store.NewMemoryStore()

	// Create bootstrap tokens for node registration
	_, tok1, err := memStore.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node1",
		TTLHours:    24,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, tok2, err := memStore.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node2",
		TTLHours:    24,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Register nodes (route keys are auto-generated)
	reg1, err := memStore.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: tok1,
		Name:           "Node 1",
		Region:         "us-east-1",
		PublicHost:     "node1.example.com",
		Provider:       "aws",
		RuntimeFlavor:  "vmess",
	})
	if err != nil {
		t.Fatal(err)
	}
	reg2, err := memStore.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: tok2,
		Name:           "Node 2",
		Region:         "us-east-1",
		PublicHost:     "node2.example.com",
		Provider:       "aws",
		RuntimeFlavor:  "vmess",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Read back nodes to get their generated route keys
	nodes := memStore.ListNodes()
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}

	// Build a route key lookup by node ID
	routeKeyByID := make(map[string]string)
	for _, n := range nodes {
		routeKeyByID[n.ID] = n.RouteKey
	}
	rk1 := routeKeyByID[reg1.NodeID]
	rk2 := routeKeyByID[reg2.NodeID]
	if rk1 == "" || rk2 == "" {
		t.Fatal("expected non-empty route keys for registered nodes")
	}

	// Save CloudFront config with origins matching the generated route keys
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	origins := []domain.CloudFrontOrigin{
		{OriginID: "origin-1", Host: "node1.example.com", RouteKey: rk1},
		{OriginID: "origin-2", Host: "node2.example.com", RouteKey: rk2},
	}
	originsJSON, _ := json.Marshal(origins)
	memStore.UpdateCloudFrontOrigins(string(originsJSON))
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	// Run bind
	svc := NewBindService(memStore)
	result, err := svc.BindNodes()
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchedCount != 2 {
		t.Fatalf("expected 2 matched, got %d", result.MatchedCount)
	}
	if result.UnmatchedCount != 0 {
		t.Fatalf("expected 0 unmatched, got %d", result.UnmatchedCount)
	}
	if len(result.Bindings) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(result.Bindings))
	}

	// Verify bindings are persisted
	cfg, err := memStore.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BindingsJSON == "" || cfg.BindingsJSON == "[]" {
		t.Fatal("expected bindings to be persisted in config")
	}
	var persistedBindings []domain.CloudFrontBinding
	json.Unmarshal([]byte(cfg.BindingsJSON), &persistedBindings)
	if len(persistedBindings) != 2 {
		t.Fatalf("expected 2 persisted bindings, got %d", len(persistedBindings))
	}
}

func TestBindNodesNoConfig(t *testing.T) {
	memStore := store.NewMemoryStore()
	svc := NewBindService(memStore)
	_, err := svc.BindNodes()
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestBindNodesNoNodes(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	svc := NewBindService(memStore)
	result, err := svc.BindNodes()
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchedCount != 0 {
		t.Fatalf("expected 0 matched, got %d", result.MatchedCount)
	}
	if result.UnmatchedCount != 0 {
		t.Fatalf("expected 0 unmatched, got %d", result.UnmatchedCount)
	}
}

func TestBindNodesNoMatchingOrigins(t *testing.T) {
	memStore := store.NewMemoryStore()

	// Register a node
	_, tok, err := memStore.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node1",
		TTLHours:    24,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = memStore.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: tok,
		Name:           "Node 1",
		Region:         "us-east-1",
		RuntimeFlavor:  "vmess",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Save config with origins that don't match the node's route key
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	origins := []domain.CloudFrontOrigin{
		{OriginID: "origin-x", Host: "other.example.com", RouteKey: "nonexistent-key"},
	}
	originsJSON, _ := json.Marshal(origins)
	memStore.UpdateCloudFrontOrigins(string(originsJSON))
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	svc := NewBindService(memStore)
	result, err := svc.BindNodes()
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchedCount != 0 {
		t.Fatalf("expected 0 matched, got %d", result.MatchedCount)
	}
	if result.UnmatchedCount != 1 {
		t.Fatalf("expected 1 unmatched, got %d", result.UnmatchedCount)
	}
}

func TestBindNodesPartialMatch(t *testing.T) {
	memStore := store.NewMemoryStore()

	// Register two nodes
	_, tok1, err := memStore.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node1",
		TTLHours:    24,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, tok2, err := memStore.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node2",
		TTLHours:    24,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg1, err := memStore.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: tok1,
		Name:           "Node 1",
		Region:         "us-east-1",
		RuntimeFlavor:  "vmess",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = memStore.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: tok2,
		Name:           "Node 2",
		Region:         "us-east-1",
		RuntimeFlavor:  "vmess",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Get route key for first node only
	nodes := memStore.ListNodes()
	var rk1 string
	for _, n := range nodes {
		if n.ID == reg1.NodeID {
			rk1 = n.RouteKey
		}
	}

	// Only create origin for first node
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	origins := []domain.CloudFrontOrigin{
		{OriginID: "origin-1", Host: "node1.example.com", RouteKey: rk1},
	}
	originsJSON, _ := json.Marshal(origins)
	memStore.UpdateCloudFrontOrigins(string(originsJSON))
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	svc := NewBindService(memStore)
	result, err := svc.BindNodes()
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchedCount != 1 {
		t.Fatalf("expected 1 matched, got %d", result.MatchedCount)
	}
	if result.UnmatchedCount != 1 {
		t.Fatalf("expected 1 unmatched, got %d", result.UnmatchedCount)
	}
}

func TestBindNodesEmptyOrigins(t *testing.T) {
	memStore := store.NewMemoryStore()

	// Register a node
	_, tok, err := memStore.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node1",
		TTLHours:    24,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = memStore.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: tok,
		Name:           "Node 1",
		Region:         "us-east-1",
		RuntimeFlavor:  "vmess",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Config exists but no origins set
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	svc := NewBindService(memStore)
	result, err := svc.BindNodes()
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchedCount != 0 {
		t.Fatalf("expected 0 matched, got %d", result.MatchedCount)
	}
	if result.UnmatchedCount != 1 {
		t.Fatalf("expected 1 unmatched, got %d", result.UnmatchedCount)
	}
}
