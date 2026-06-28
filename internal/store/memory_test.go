package store

import (
	"strings"
	"testing"
	"time"

	"v2ray-platform/internal/domain"
)

func TestRegisterGrantAndConfigLifecycle(t *testing.T) {
	s := NewMemoryStore()

	bt, plainToken, err := s.CreateBootstrapToken(CreateBootstrapTokenInput{
		Description: "test-node",
		TTLHours:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if bt.ID == "" || plainToken == "" {
		t.Fatal("expected bootstrap token output")
	}

	registered, err := s.RegisterNode(RegisterNodeInput{
		BootstrapToken: plainToken,
		Name:           "node-1",
		Region:         "ap-southeast-1",
		RuntimeFlavor:  "v2ray",
	})
	if err != nil {
		t.Fatal(err)
	}
	if registered.ConfigVersion == 0 {
		t.Fatal("expected initial config version")
	}

	member, err := s.CreateMember(CreateMemberInput{
		Name:  "Alice",
		Email: "alice@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	grant, cred, err := s.CreateGrant(CreateGrantInput{
		NodeID:   registered.NodeID,
		MemberID: member.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if grant.ID == "" || cred.UUID == "" {
		t.Fatal("expected grant and credential")
	}

	rev, err := s.GetNodeConfig(registered.NodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if rev.ConfigVersion < 2 {
		t.Fatalf("expected config version to increase, got %d", rev.ConfigVersion)
	}
}

func TestMemberCredentialsAreIsolatedPerNode(t *testing.T) {
	s := NewMemoryStore()
	member, err := s.CreateMember(CreateMemberInput{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	_, tokenA, err := s.CreateBootstrapToken(CreateBootstrapTokenInput{Description: "node-a", TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	nodeA, err := s.RegisterNode(RegisterNodeInput{BootstrapToken: tokenA, Name: "node-a", Region: "a", RuntimeFlavor: "v2ray"})
	if err != nil {
		t.Fatal(err)
	}
	_, tokenB, err := s.CreateBootstrapToken(CreateBootstrapTokenInput{Description: "node-b", TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	nodeB, err := s.RegisterNode(RegisterNodeInput{BootstrapToken: tokenB, Name: "node-b", Region: "b", RuntimeFlavor: "v2ray"})
	if err != nil {
		t.Fatal(err)
	}
	_, credA, err := s.CreateGrant(CreateGrantInput{NodeID: nodeA.NodeID, MemberID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, credB, err := s.CreateGrant(CreateGrantInput{NodeID: nodeB.NodeID, MemberID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if credA.UUID == credB.UUID || credA.UUID == member.UUID || credB.UUID == member.UUID {
		t.Fatalf("expected node-scoped credentials, got member=%s nodeA=%s nodeB=%s", member.UUID, credA.UUID, credB.UUID)
	}
}

func TestRevokeGrantAndDeleteMemberLifecycle(t *testing.T) {
	s := NewMemoryStore()

	bt, plainToken, err := s.CreateBootstrapToken(CreateBootstrapTokenInput{Description: "node", TTLHours: 1})
	if err != nil || bt.ID == "" {
		t.Fatal(err)
	}
	registered, err := s.RegisterNode(RegisterNodeInput{
		BootstrapToken: plainToken,
		Name:           "node-1",
		Region:         "ap-southeast-1",
		RuntimeFlavor:  "v2ray",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(CreateMemberInput{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	grant, cred, err := s.CreateGrant(CreateGrantInput{NodeID: registered.NodeID, MemberID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAuditLog("admin_1", "grant.created", "grant", grant.ID, map[string]string{"uuid": cred.UUID}); err != nil {
		t.Fatal(err)
	}
	rev, err := s.GetNodeConfig(registered.NodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rev.Config, cred.UUID) {
		t.Fatalf("expected config to include %s", cred.UUID)
	}
	if err := s.RevokeGrant(grant.ID); err != nil {
		t.Fatal(err)
	}
	rev, err = s.GetNodeConfig(registered.NodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rev.Config, cred.UUID) {
		t.Fatalf("expected config to remove %s after revoke", cred.UUID)
	}
	if err := s.DeleteMember(member.ID); err != nil {
		t.Fatal(err)
	}
	if len(s.ListMembers()) != 0 {
		t.Fatal("expected no members after delete")
	}
	logs, _, _ := s.ListAuditLogs(1, 50)
	if len(logs) != 1 {
		t.Fatal("expected one audit log")
	}
}

func TestUsageAggregationLifecycle(t *testing.T) {
	s := NewMemoryStore()
	_, plainToken, err := s.CreateBootstrapToken(CreateBootstrapTokenInput{Description: "node", TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	registered, err := s.RegisterNode(RegisterNodeInput{
		BootstrapToken: plainToken,
		Name:           "node-1",
		Region:         "ap-southeast-1",
		RuntimeFlavor:  "v2ray",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(CreateMemberInput{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	_, cred, err := s.CreateGrant(CreateGrantInput{NodeID: registered.NodeID, MemberID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	err = s.RecordUsage(registered.NodeToken, []domain.UsageSnapshot{{
		CredentialUUID: cred.UUID,
		UplinkBytes:    120,
		DownlinkBytes:  340,
	}})
	if err != nil {
		t.Fatal(err)
	}
	nodeUsage := s.ListNodeUsageSummaries()
	if len(nodeUsage) != 1 || nodeUsage[0].TotalBytes != 460 {
		t.Fatalf("unexpected node usage: %+v", nodeUsage)
	}
	memberUsage := s.ListMemberUsageSummaries()
	if len(memberUsage) != 1 || memberUsage[0].TotalBytes != 460 {
		t.Fatalf("unexpected member usage: %+v", memberUsage)
	}
}

func setupNodeAndMember(t *testing.T) (*MemoryStore, string /*nodeToken*/, string /*memberID*/, string /*credUUID*/) {
	t.Helper()
	s := NewMemoryStore()
	_, plainToken, err := s.CreateBootstrapToken(CreateBootstrapTokenInput{Description: "node", TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := s.RegisterNode(RegisterNodeInput{BootstrapToken: plainToken, Name: "node-1", Region: "ap-southeast-1", RuntimeFlavor: "v2ray"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(CreateMemberInput{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	_, cred, err := s.CreateGrant(CreateGrantInput{NodeID: reg.NodeID, MemberID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	return s, reg.NodeToken, member.ID, cred.UUID
}

func TestSuspendedMemberExcludedFromConfig(t *testing.T) {
	s, nodeToken, memberID, credUUID := setupNodeAndMember(t)

	rev, err := s.GetNodeConfig(nodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rev.Config, credUUID) {
		t.Fatalf("expected config to contain UUID before suspend")
	}

	st := domain.MemberStatusSuspended
	if _, err := s.UpdateMember(memberID, UpdateMemberInput{Status: &st}); err != nil {
		t.Fatal(err)
	}

	rev, err = s.GetNodeConfig(nodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rev.Config, credUUID) {
		t.Fatalf("expected config to NOT contain UUID after suspend")
	}
}

func TestExpiredMemberExcludedFromConfig(t *testing.T) {
	s, nodeToken, memberID, credUUID := setupNodeAndMember(t)

	past := time.Now().UTC().Add(-1 * time.Hour)
	if _, err := s.UpdateMember(memberID, UpdateMemberInput{ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}

	rev, err := s.GetNodeConfig(nodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rev.Config, credUUID) {
		t.Fatalf("expected config to NOT contain UUID after expiry in the past")
	}
}

func TestSaveCloudFrontConfigRetainsSecretsAndMetadata(t *testing.T) {
	s := NewMemoryStore()
	enabled := true
	if err := s.SaveCloudFrontConfig(SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     "enc-ak-1",
		EncryptedSecretAccessKey: "enc-sk-1",
		EncryptedSessionToken:    "enc-st-1",
		AWSRegion:                "us-east-1",
		CustomEntryHost:          "edge.example.com",
		DistributionID:           "DIST123",
		DistributionDomainName:   "d123.cloudfront.net",
		Mode:                     "managed",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveCloudFrontConfig(SaveCloudFrontConfigInput{
		AWSRegion:             "us-west-2",
		CustomEntryHost:       "cf.example.com",
		RetainExistingSecrets: true,
		Mode:                  "adopted",
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EncryptedAccessKeyID != "enc-ak-1" || cfg.EncryptedSecretAccessKey != "enc-sk-1" || cfg.EncryptedSessionToken != "enc-st-1" {
		t.Fatalf("expected secrets to be retained, got %+v", cfg)
	}
	if cfg.CustomEntryHost != "cf.example.com" || cfg.AWSRegion != "us-west-2" || cfg.Mode != "adopted" {
		t.Fatalf("expected metadata update, got %+v", cfg)
	}
}

func TestUpdateCloudFrontSyncStatusCanPreservePreviousSyncState(t *testing.T) {
	s := NewMemoryStore()
	enabled := true
	if err := s.SaveCloudFrontConfig(SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     "enc-ak-1",
		EncryptedSecretAccessKey: "enc-sk-1",
		AWSRegion:                "us-east-1",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCloudFrontSyncStatus(UpdateCloudFrontSyncInput{
		SyncStatus:    "synced",
		DriftStatus:   "in_sync",
		LastSyncError: "",
	}); err != nil {
		t.Fatal(err)
	}
	beforePlan, err := s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if beforePlan.LastSyncedAt == nil || beforePlan.LastSuccessfulSyncAt == nil {
		t.Fatalf("expected initial successful sync timestamps, got %+v", beforePlan)
	}
	time.Sleep(time.Millisecond)
	if err := s.UpdateCloudFrontSyncStatus(UpdateCloudFrontSyncInput{
		PlanJSON:           `[{"action":"noop"}]`,
		DriftStatus:        "drifted",
		PreserveSyncStatus: true,
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SyncStatus != "synced" {
		t.Fatalf("expected sync status to be preserved, got %q", cfg.SyncStatus)
	}
	if cfg.DriftStatus != "drifted" {
		t.Fatalf("expected drift status update, got %q", cfg.DriftStatus)
	}
	if cfg.LastSyncedAt == nil || !cfg.LastSyncedAt.Equal(*beforePlan.LastSyncedAt) {
		t.Fatalf("expected plan preview to preserve last synced timestamp, before=%v after=%v", beforePlan.LastSyncedAt, cfg.LastSyncedAt)
	}
	if cfg.LastSuccessfulSyncAt == nil || !cfg.LastSuccessfulSyncAt.Equal(*beforePlan.LastSuccessfulSyncAt) {
		t.Fatalf("expected plan preview to preserve last successful sync timestamp, before=%v after=%v", beforePlan.LastSuccessfulSyncAt, cfg.LastSuccessfulSyncAt)
	}
}

func TestUpdateCloudFrontDistributionInvalidatesPreviousSyncWhenTargetChanges(t *testing.T) {
	s := NewMemoryStore()
	enabled := true
	if err := s.SaveCloudFrontConfig(SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     "enc-ak-1",
		EncryptedSecretAccessKey: "enc-sk-1",
		AWSRegion:                "us-east-1",
		DistributionID:           "EOLD",
		DistributionDomainName:   "dold.cloudfront.net",
		Mode:                     "managed",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCloudFrontSyncStatus(UpdateCloudFrontSyncInput{
		SyncStatus:    "synced",
		DriftStatus:   "in_sync",
		LastSyncError: "",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCloudFrontDistribution("EOLD", "dold.cloudfront.net", "managed"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSuccessfulSyncAt == nil || cfg.SyncStatus != "synced" {
		t.Fatalf("expected same distribution scan to preserve sync state, got %+v", cfg)
	}

	if err := s.UpdateCloudFrontDistribution("ENEW", "dnew.cloudfront.net", "managed"); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSuccessfulSyncAt != nil {
		t.Fatalf("expected new distribution to clear last successful sync, got %+v", cfg)
	}
	if cfg.SyncStatus != "idle" || cfg.DriftStatus != "" || cfg.LastSyncError != "" {
		t.Fatalf("expected new distribution to reset sync markers, got %+v", cfg)
	}
}

func TestUpdateCloudFrontBindingsInvalidatesPreviousSyncWhenBindingsChange(t *testing.T) {
	s := NewMemoryStore()
	enabled := true
	if err := s.SaveCloudFrontConfig(SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     "enc-ak-1",
		EncryptedSecretAccessKey: "enc-sk-1",
		AWSRegion:                "us-east-1",
		DistributionID:           "E123",
		DistributionDomainName:   "d123.cloudfront.net",
		Mode:                     "managed",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCloudFrontBindings(`[{"nodeId":"node-1","routeKey":"rk1"}]`); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCloudFrontSyncStatus(UpdateCloudFrontSyncInput{
		SyncStatus:    "synced",
		DriftStatus:   "in_sync",
		LastSyncError: "",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCloudFrontBindings(`[{"nodeId":"node-1","routeKey":"rk1"}]`); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSuccessfulSyncAt == nil || cfg.SyncStatus != "synced" {
		t.Fatalf("expected unchanged bindings to preserve sync state, got %+v", cfg)
	}

	if err := s.UpdateCloudFrontBindings(`[{"nodeId":"node-1","routeKey":"rk1"},{"nodeId":"node-2","routeKey":"rk2"}]`); err != nil {
		t.Fatal(err)
	}
	cfg, err = s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSuccessfulSyncAt != nil {
		t.Fatalf("expected changed bindings to clear last successful sync, got %+v", cfg)
	}
	if cfg.SyncStatus != "idle" || cfg.DriftStatus != "" || cfg.LastSyncError != "" || cfg.LastSyncedAt != nil {
		t.Fatalf("expected changed bindings to reset sync markers, got %+v", cfg)
	}
}

func TestSaveCloudFrontConfigInvalidatesPreviousSyncWhenDistributionMetadataChanges(t *testing.T) {
	s := NewMemoryStore()
	enabled := true
	if err := s.SaveCloudFrontConfig(SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     "enc-ak-1",
		EncryptedSecretAccessKey: "enc-sk-1",
		AWSRegion:                "us-east-1",
		DistributionID:           "EOLD",
		DistributionDomainName:   "dold.cloudfront.net",
		Mode:                     "managed",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCloudFrontSyncStatus(UpdateCloudFrontSyncInput{
		SyncStatus:    "synced",
		DriftStatus:   "in_sync",
		LastSyncError: "",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCloudFrontConfig(SaveCloudFrontConfigInput{
		AWSRegion:              "us-east-1",
		DistributionID:         "ENEW",
		DistributionDomainName: "dnew.cloudfront.net",
		RetainExistingSecrets:  true,
		Mode:                   "managed",
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := s.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSuccessfulSyncAt != nil {
		t.Fatalf("expected config distribution change to clear last successful sync, got %+v", cfg)
	}
	if cfg.SyncStatus != "idle" || cfg.DriftStatus != "" || cfg.LastSyncError != "" {
		t.Fatalf("expected config distribution change to reset sync markers, got %+v", cfg)
	}
}

func TestReactivatedMemberAppearsInConfig(t *testing.T) {
	s, nodeToken, memberID, credUUID := setupNodeAndMember(t)

	// Suspend first.
	st := domain.MemberStatusSuspended
	if _, err := s.UpdateMember(memberID, UpdateMemberInput{Status: &st}); err != nil {
		t.Fatal(err)
	}
	rev, err := s.GetNodeConfig(nodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rev.Config, credUUID) {
		t.Fatalf("expected UUID absent while suspended")
	}

	// Reactivate.
	active := domain.MemberStatusActive
	if _, err := s.UpdateMember(memberID, UpdateMemberInput{Status: &active}); err != nil {
		t.Fatal(err)
	}
	rev, err = s.GetNodeConfig(nodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rev.Config, credUUID) {
		t.Fatalf("expected UUID to reappear after reactivation")
	}
}

func TestGroupGrantAppearsInConfigAndUsage(t *testing.T) {
	s := NewMemoryStore()
	_, plainToken, err := s.CreateBootstrapToken(CreateBootstrapTokenInput{Description: "node", TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := s.RegisterNode(RegisterNodeInput{BootstrapToken: plainToken, Name: "node-1", Region: "ap-southeast-1", RuntimeFlavor: "v2ray"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := s.CreateMember(CreateMemberInput{Name: "Alice", Email: "alice@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	group, err := s.CreateNodeGroup("friends", "friends nodes")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeGroupsForNode(reg.NodeID, []string{group.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateGroupGrant(group.ID, member.ID); err != nil {
		t.Fatal(err)
	}

	groupCredentialUUID := derivedGroupCredentialUUID(reg.NodeID, member.ID)
	rev, err := s.GetNodeConfig(reg.NodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rev.Config, member.UUID) {
		t.Fatalf("expected config not to contain member UUID %s", member.UUID)
	}
	if !strings.Contains(rev.Config, groupCredentialUUID) {
		t.Fatalf("expected group credential UUID %s in config", groupCredentialUUID)
	}

	if err := s.RecordUsage(reg.NodeToken, []domain.UsageSnapshot{{
		CredentialUUID: groupCredentialUUID,
		UplinkBytes:    100,
		DownlinkBytes:  200,
	}}); err != nil {
		t.Fatal(err)
	}
	memberUsage := s.ListMemberUsageSummaries()
	if len(memberUsage) != 1 || memberUsage[0].TotalBytes != 300 {
		t.Fatalf("unexpected group-grant usage: %+v", memberUsage)
	}
}
