package cloudfront

import (
	"testing"

	"v2ray-platform/internal/domain"
)

func TestPlanInSync(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: "origin-node-1", RouteKey: "abc123", GroupName: "default"},
	}
	origins := []OriginState{
		{OriginID: "origin-node-1", DomainName: "node-1.example.com", PathPrefix: "/abc123"},
	}
	nodeHosts := map[string]string{"node-1": "node-1.example.com"}
	routeKeys := map[string]string{"node-1": "abc123"}

	plan := Plan(bindings, origins, nodeHosts, routeKeys)
	if plan.DriftStatus != "in_sync" {
		t.Fatalf("expected in_sync, got %s", plan.DriftStatus)
	}
	if plan.Actions[0].Action != "noop" {
		t.Fatalf("expected noop, got %s", plan.Actions[0].Action)
	}
}

func TestPlanDriftedMissingOrigin(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: "origin-node-1", RouteKey: "abc123", GroupName: "default"},
	}
	origins := []OriginState{} // no origins in AWS
	nodeHosts := map[string]string{"node-1": "node-1.example.com"}

	plan := Plan(bindings, origins, nodeHosts, nil)
	if plan.DriftStatus != "drifted" {
		t.Fatalf("expected drifted, got %s", plan.DriftStatus)
	}
	if plan.Actions[0].Action != "add_origin" {
		t.Fatalf("expected add_origin, got %s", plan.Actions[0].Action)
	}
}

func TestPlanDriftedHostMismatch(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: "origin-node-1", RouteKey: "abc123", GroupName: "default"},
	}
	origins := []OriginState{
		{OriginID: "origin-node-1", DomainName: "old-host.example.com", PathPrefix: "/abc123"},
	}
	nodeHosts := map[string]string{"node-1": "new-host.example.com"}

	plan := Plan(bindings, origins, nodeHosts, nil)
	if plan.Actions[0].Action != "replace_origin" {
		t.Fatalf("expected replace_origin, got %s", plan.Actions[0].Action)
	}
}

func TestPlanExtraOrigin(t *testing.T) {
	bindings := []domain.CloudFrontBinding{}
	origins := []OriginState{
		{OriginID: "origin-orphan", DomainName: "orphan.example.com", PathPrefix: "/orphan"},
	}

	plan := Plan(bindings, origins, nil, nil)
	if plan.Actions[0].Action != "remove_origin" {
		t.Fatalf("expected remove_origin, got %s", plan.Actions[0].Action)
	}
}
