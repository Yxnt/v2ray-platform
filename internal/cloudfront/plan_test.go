package cloudfront

import (
	"strings"
	"testing"

	"v2ray-platform/internal/domain"
)

func TestPlanInSync(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: managedOriginID("node-1"), RouteKey: "abc123", GroupName: "default"},
	}
	dist := &DistributionState{
		Origins: []OriginState{
			{OriginID: managedOriginID("node-1"), DomainName: "node-1.example.com"},
		},
		Behaviors: []BehaviorState{
			{PathPattern: "/abc123", OriginID: managedOriginID("node-1")},
		},
	}
	nodeHosts := map[string]string{"node-1": "node-1.example.com"}
	nodePaths := map[string]string{"node-1": "/node-1"}

	plan := Plan(bindings, dist, nodeHosts, nodePaths)
	if plan.DriftStatus != "in_sync" {
		t.Fatalf("expected in_sync, got %s", plan.DriftStatus)
	}
	if plan.Actions[0].Action != "noop" {
		t.Fatalf("expected noop, got %s", plan.Actions[0].Action)
	}
	if len(plan.RewriteRoutes) != 1 || plan.RewriteRoutes[0].RouteKey != "abc123" || plan.RewriteRoutes[0].Path != "/node-1" {
		t.Fatalf("expected rewrite route for route key to node path, got %+v", plan.RewriteRoutes)
	}
}

func TestPlanDriftedMissingOrigin(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: managedOriginID("node-1"), RouteKey: "abc123", GroupName: "default"},
	}
	dist := &DistributionState{} // no origins or behaviors in AWS
	nodeHosts := map[string]string{"node-1": "node-1.example.com"}
	nodePaths := map[string]string{"node-1": "/node-1"}

	plan := Plan(bindings, dist, nodeHosts, nodePaths)
	if plan.DriftStatus != "drifted" {
		t.Fatalf("expected drifted, got %s", plan.DriftStatus)
	}
	if plan.Actions[0].Action != "add_route" {
		t.Fatalf("expected add_route, got %s", plan.Actions[0].Action)
	}
}

func TestPlanDriftedHostMismatch(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: managedOriginID("node-1"), RouteKey: "abc123", GroupName: "default"},
	}
	dist := &DistributionState{
		Origins: []OriginState{
			{OriginID: managedOriginID("node-1"), DomainName: "old-host.example.com"},
		},
		Behaviors: []BehaviorState{
			{PathPattern: "/abc123", OriginID: managedOriginID("node-1")},
		},
	}
	nodeHosts := map[string]string{"node-1": "new-host.example.com"}
	nodePaths := map[string]string{"node-1": "/node-1"}

	plan := Plan(bindings, dist, nodeHosts, nodePaths)
	if plan.Actions[0].Action != "replace_route" {
		t.Fatalf("expected replace_route, got %s", plan.Actions[0].Action)
	}
}

func TestPlanExtraOrigin(t *testing.T) {
	bindings := []domain.CloudFrontBinding{}
	dist := &DistributionState{
		Origins: []OriginState{
			{OriginID: managedOriginID("orphan"), DomainName: "orphan.example.com"},
		},
		Behaviors: []BehaviorState{
			{PathPattern: "/orphan", OriginID: managedOriginID("orphan")},
		},
	}

	plan := Plan(bindings, dist, nil, nil)
	if plan.Actions[0].Action != "remove_route" {
		t.Fatalf("expected remove_route, got %s", plan.Actions[0].Action)
	}
}

func TestPlanDoesNotRemoveUnmanagedExtraRoute(t *testing.T) {
	dist := &DistributionState{
		Origins: []OriginState{
			{OriginID: "custom-origin", DomainName: "custom.example.com"},
		},
		Behaviors: []BehaviorState{
			{PathPattern: "/custom", OriginID: "custom-origin"},
		},
	}

	plan := Plan(nil, dist, nil, nil)
	if plan.DriftStatus != "in_sync" {
		t.Fatalf("expected unmanaged extra route to be ignored, got %s", plan.DriftStatus)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Action != "noop" {
		t.Fatalf("expected noop for unmanaged extra route, got %+v", plan.Actions)
	}
}

func TestPlanConflictsWhenDesiredRouteIsOccupiedByUnmanagedOrigin(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: "v2ray-platform-node-node-1", RouteKey: "abc123", GroupName: "default"},
	}
	dist := &DistributionState{
		Origins: []OriginState{
			{OriginID: "custom-origin", DomainName: "custom.example.com"},
		},
		Behaviors: []BehaviorState{
			{PathPattern: "/abc123", OriginID: "custom-origin"},
		},
	}

	plan := Plan(bindings, dist, map[string]string{"node-1": "node-1.example.com"}, map[string]string{"node-1": "/node-1"})
	if plan.DriftStatus != "conflict" {
		t.Fatalf("expected conflict, got %s", plan.DriftStatus)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Action != "conflict" {
		t.Fatalf("expected conflict action, got %+v", plan.Actions)
	}
}

func TestPlanConflictsWhenDesiredNodeIsMissingPublicHost(t *testing.T) {
	bindings := []domain.CloudFrontBinding{
		{NodeID: "node-1", OriginID: managedOriginID("node-1"), RouteKey: "abc123", GroupName: "default"},
	}
	dist := &DistributionState{}

	plan := Plan(bindings, dist, map[string]string{"node-1": ""}, map[string]string{"node-1": "/node-1"})
	if plan.DriftStatus != "conflict" {
		t.Fatalf("expected conflict, got %s", plan.DriftStatus)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("expected one conflict action, got %+v", plan.Actions)
	}
	if plan.Actions[0].Action != "conflict" || plan.Actions[0].RouteKey != "abc123" {
		t.Fatalf("expected missing public_host conflict, got %+v", plan.Actions[0])
	}
	if !strings.Contains(plan.Actions[0].Reason, "public_host") {
		t.Fatalf("expected public_host reason, got %q", plan.Actions[0].Reason)
	}
}
