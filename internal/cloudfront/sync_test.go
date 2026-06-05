package cloudfront

import (
	"context"
	"errors"
	"testing"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

type mockSyncClient struct {
	updateErr  error
	lastUpdate struct {
		toAdd    []OriginState
		toRemove []string
		toUpdate []OriginState
	}
}

func (m *mockSyncClient) GetDistribution(ctx context.Context, id string) (*DistributionState, error) {
	return nil, nil
}

func (m *mockSyncClient) UpdateOrigins(ctx context.Context, id string, toAdd []OriginState, toRemove []string, toUpdate []OriginState) error {
	m.lastUpdate.toAdd = toAdd
	m.lastUpdate.toRemove = toRemove
	m.lastUpdate.toUpdate = toUpdate
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
		Actions:     []domain.CloudFrontSyncAction{{Action: "noop"}},
		DriftStatus: "in_sync",
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
		Actions: []domain.CloudFrontSyncAction{
			{Action: "add_origin", OriginID: "new-origin", Host: "node1.example.com", RouteKey: "key1234"},
			{Action: "remove_origin", OriginID: "old-origin"},
		},
		DriftStatus: "drifted",
	}

	result, err := svc.ExecutePlan(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.ActionsApplied != 2 {
		t.Fatalf("expected 2 actions, got %d", result.ActionsApplied)
	}
	if len(client.lastUpdate.toAdd) != 1 {
		t.Fatalf("expected 1 add, got %d", len(client.lastUpdate.toAdd))
	}
	if len(client.lastUpdate.toRemove) != 1 {
		t.Fatalf("expected 1 remove, got %d", len(client.lastUpdate.toRemove))
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
		Actions: []domain.CloudFrontSyncAction{
			{Action: "add_origin", OriginID: "new-origin", Host: "node1.example.com", RouteKey: "key1234"},
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
