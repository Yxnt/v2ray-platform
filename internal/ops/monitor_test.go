package ops

import (
	"testing"
	"time"

	"v2ray-platform/internal/config"
	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

func TestSweepMemberPoliciesExpiresAndSuspends(t *testing.T) {
	st := store.NewMemoryStore()
	expiring, err := st.CreateMember(store.CreateMemberInput{Name: "Expiring", Email: "exp@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if _, err := st.UpdateMember(expiring.ID, store.UpdateMemberInput{ExpiresAt: &past}); err != nil {
		t.Fatal(err)
	}

	_, plainToken, err := st.CreateBootstrapToken(store.CreateBootstrapTokenInput{Description: "node", TTLHours: 1})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := st.RegisterNode(store.RegisterNodeInput{BootstrapToken: plainToken, Name: "node-1", Region: "ap-southeast-1", RuntimeFlavor: "v2ray"})
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateMember(store.CreateMemberInput{Name: "Quota", Email: "quota@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(100)
	if _, err := st.UpdateMember(member.ID, store.UpdateMemberInput{QuotaBytesLimit: &limit}); err != nil {
		t.Fatal(err)
	}
	_, cred, err := st.CreateGrant(store.CreateGrantInput{NodeID: reg.NodeID, MemberID: member.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordUsage(reg.NodeToken, []domain.UsageSnapshot{{
		CredentialUUID: cred.UUID,
		UplinkBytes:    100,
		DownlinkBytes:  10,
	}}); err != nil {
		t.Fatal(err)
	}

	monitor := NewMonitor(st, config.ControlPlaneConfig{})
	if err := monitor.SweepMemberPolicies(time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	members := st.ListMembers()
	got := map[string]domain.MemberStatus{}
	for _, item := range members {
		got[item.Email] = item.Status
	}
	if got["exp@example.com"] != domain.MemberStatusExpired {
		t.Fatalf("expected expiring member to be expired, got %+v", got["exp@example.com"])
	}
	if got["quota@example.com"] != domain.MemberStatusSuspended {
		t.Fatalf("expected quota member to be suspended, got %+v", got["quota@example.com"])
	}
}
