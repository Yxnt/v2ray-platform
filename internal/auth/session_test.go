package auth

import (
	"errors"
	"testing"
	"time"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

func TestSessionIssueAndVerify(t *testing.T) {
	st := store.NewMemoryStore()
	admin, err := st.EnsureAdmin("admin@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager("secret", nil, time.Hour, st)
	token, claims, err := manager.Issue(&domain.Admin{
		ID:    admin.ID,
		Email: admin.Email,
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims.AdminID != admin.ID {
		t.Fatalf("unexpected admin id %q", claims.AdminID)
	}
	if claims.SessionID == "" {
		t.Fatal("expected session id")
	}
	got, err := manager.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Email != "admin@example.com" {
		t.Fatalf("unexpected email %q", got.Email)
	}
}

func TestRevokedSessionFailsVerification(t *testing.T) {
	st := store.NewMemoryStore()
	admin, err := st.EnsureAdmin("admin@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager("secret", nil, time.Hour, st)
	token, claims, err := manager.Issue(admin)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RevokeAdminSession(claims.SessionID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Verify(token); err == nil {
		t.Fatal("expected revoked session to fail verification")
	}
}

func TestPreviousSecretCanVerifyExistingToken(t *testing.T) {
	st := store.NewMemoryStore()
	admin, err := st.EnsureAdmin("admin@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	oldManager := NewManager("old-secret", nil, time.Hour, st)
	token, _, err := oldManager.Issue(admin)
	if err != nil {
		t.Fatal(err)
	}
	newManager := NewManager("new-secret", []string{"old-secret"}, time.Hour, st)
	if _, err := newManager.Verify(token); err != nil {
		t.Fatalf("expected token signed with previous secret to verify, got %v", err)
	}
}

func TestStatelessSessionIssueAndVerify(t *testing.T) {
	manager := NewManager("secret", nil, time.Hour, nil)
	token, claims, err := manager.Issue(&domain.Admin{
		ID:    "admin_1",
		Email: "admin@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims.SessionID != "" {
		t.Fatalf("expected no session id in stateless mode, got %q", claims.SessionID)
	}
	got, err := manager.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
	if got.AdminID != "admin_1" || got.Email != "admin@example.com" {
		t.Fatalf("unexpected claims: %+v", got)
	}
}

// faultyStore is a SessionStore that always returns a transient error from GetAdminSession,
// simulating a Neon cold-start or DB connection failure.
type faultyStore struct {
	*store.MemoryStore
}

func (f *faultyStore) GetAdminSession(_ string) (*domain.AdminSession, error) {
	return nil, errors.New("connection refused")
}

func TestTransientDBErrorDoesNotCause401(t *testing.T) {
	st := store.NewMemoryStore()
	admin, err := st.EnsureAdmin("admin@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	// Issue the token via the real store so a session row exists.
	realManager := NewManager("secret", nil, time.Hour, st)
	token, _, err := realManager.Issue(admin)
	if err != nil {
		t.Fatal(err)
	}
	// Verify via a store that always fails GetAdminSession – simulates Neon outage.
	faultyManager := NewManager("secret", nil, time.Hour, &faultyStore{st})
	got, err := faultyManager.Verify(token)
	if err != nil {
		t.Fatalf("transient DB error should not cause 401, got: %v", err)
	}
	if got.Email != "admin@example.com" {
		t.Fatalf("unexpected email %q", got.Email)
	}
}
