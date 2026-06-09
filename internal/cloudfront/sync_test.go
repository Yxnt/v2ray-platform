package cloudfront

import (
	"context"
	"errors"
	"strings"
	"testing"

	"v2ray-platform/internal/store"
)

type mockSyncClient struct {
	updateErr  error
	lastUpdate struct {
		actions  []RouteAction
		rewrites []RewriteRoute
	}
}

func (m *mockSyncClient) ListDistributions(ctx context.Context) ([]DistributionSummary, error) {
	return nil, nil
}

func (m *mockSyncClient) GetDistribution(ctx context.Context, id string) (*DistributionState, error) {
	return nil, nil
}

func (m *mockSyncClient) ApplyDistributionRoutes(ctx context.Context, id string, actions []RouteAction, rewrites []RewriteRoute) error {
	m.lastUpdate.actions = append([]RouteAction(nil), actions...)
	m.lastUpdate.rewrites = append([]RewriteRoute(nil), rewrites...)
	return m.updateErr
}

func TestSyncExecutePlanInSync(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	client := &mockSyncClient{}
	svc := NewSyncService(memStore, client)

	plan := &SyncPlan{
		Actions:       []RouteAction{{Action: "noop"}},
		RewriteRoutes: []RewriteRoute{{RouteKey: "key1234", Path: "/node-1"}},
		DriftStatus:   "in_sync",
	}

	result, err := svc.ExecutePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.SyncStatus != "synced" {
		t.Fatalf("expected synced, got %s", result.SyncStatus)
	}
	if result.ActionsApplied != 0 {
		t.Fatalf("expected 0 actions, got %d", result.ActionsApplied)
	}
	if len(client.lastUpdate.rewrites) != 1 {
		t.Fatalf("expected rewrite ensure even when routes are in sync, got %+v", client.lastUpdate.rewrites)
	}
}

func TestSyncExecutePlanWithActions(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	client := &mockSyncClient{}
	svc := NewSyncService(memStore, client)

	plan := &SyncPlan{
		Actions: []RouteAction{
			{Action: "add_route", OriginID: "new-origin", Host: "node1.example.com", RouteKey: "key1234"},
			{Action: "remove_route", OriginID: "old-origin", RouteKey: "old"},
		},
		RewriteRoutes: []RewriteRoute{{RouteKey: "key1234", Path: "/node-1"}},
		DriftStatus:   "drifted",
	}

	result, err := svc.ExecutePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ActionsApplied != 2 {
		t.Fatalf("expected 2 actions, got %d", result.ActionsApplied)
	}
	if len(client.lastUpdate.actions) != 2 {
		t.Fatalf("expected 2 actions sent to client, got %d", len(client.lastUpdate.actions))
	}
	if len(client.lastUpdate.rewrites) != 1 || client.lastUpdate.rewrites[0].Path != "/node-1" {
		t.Fatalf("expected rewrite routes sent to client, got %+v", client.lastUpdate.rewrites)
	}
}

func TestSyncExecutePlanClientError(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	client := &mockSyncClient{updateErr: errors.New("access denied")}
	svc := NewSyncService(memStore, client)

	plan := &SyncPlan{
		Actions: []RouteAction{
			{Action: "add_route", OriginID: "new-origin", Host: "node1.example.com", RouteKey: "key1234"},
		},
		DriftStatus: "drifted",
	}

	result, err := svc.ExecutePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.SyncStatus != "failed" {
		t.Fatalf("expected failed, got %s", result.SyncStatus)
	}
	if result.Error != "access denied" {
		t.Fatalf("expected error message, got %s", result.Error)
	}
}

func TestSyncExecutePlanConflictStopsBeforeAWSMutation(t *testing.T) {
	memStore := store.NewMemoryStore()
	memStore.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID: "test",
		AWSRegion:            "us-east-1",
	})
	memStore.UpdateCloudFrontDistribution("E1234", "d1234.cloudfront.net", "managed")

	client := &mockSyncClient{}
	svc := NewSyncService(memStore, client)

	plan := &SyncPlan{
		Actions: []RouteAction{
			{Action: "conflict", RouteKey: "rk123", OriginID: "custom-origin", Host: "custom.example.com"},
		},
		DriftStatus: "conflict",
	}

	result, err := svc.ExecutePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.SyncStatus != "failed" {
		t.Fatalf("expected failed, got %s", result.SyncStatus)
	}
	if !strings.Contains(result.Error, "conflict") {
		t.Fatalf("expected conflict error, got %s", result.Error)
	}
	if len(client.lastUpdate.actions) != 0 {
		t.Fatalf("expected no AWS mutation attempt, got %+v", client.lastUpdate.actions)
	}
}
