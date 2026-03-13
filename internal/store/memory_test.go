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
	if len(s.ListAuditLogs()) != 1 {
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

	// The member's UUID is now used directly (same across all nodes).
	memberUUID := member.UUID
	rev, err := s.GetNodeConfig(reg.NodeToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rev.Config, memberUUID) {
		t.Fatalf("expected member UUID %s in config", memberUUID)
	}

	if err := s.RecordUsage(reg.NodeToken, []domain.UsageSnapshot{{
		CredentialUUID: memberUUID,
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
