package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"v2ray-platform/internal/auth"
	"v2ray-platform/internal/config"
	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

func TestFilterNodes(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/admin/nodes?q=sg&status=online&tag=edge", nil)
	nodes := []domain.Node{
		{ID: "node_1", Name: "sg-1", Region: "ap-southeast-1", Status: domain.NodeStatusOnline, Tags: []string{"edge"}},
		{ID: "node_2", Name: "us-1", Region: "us-west-1", Status: domain.NodeStatusOnline, Tags: []string{"core"}},
	}
	filtered := filterNodes(nodes, req)
	if len(filtered) != 1 || filtered[0].ID != "node_1" {
		t.Fatalf("unexpected filtered nodes: %+v", filtered)
	}
}

func TestFilterMembers(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/admin/members?q=team+a", nil)
	members := []domain.Member{
		{ID: "member_1", Name: "Alice", Email: "alice@example.com", Note: "Team A"},
		{ID: "member_2", Name: "Bob", Email: "bob@example.com", Note: "Team B"},
	}
	filtered := filterMembers(members, req)
	if len(filtered) != 1 || filtered[0].ID != "member_1" {
		t.Fatalf("unexpected filtered members: %+v", filtered)
	}
}

func TestFilterGrants(t *testing.T) {
	now := time.Now().UTC()
	req := httptest.NewRequest("GET", "/api/admin/grants?q=alice", nil)
	grants := []domain.GrantView{
		{ID: "grant_1", NodeID: "node_1", NodeName: "sg-1", MemberID: "member_1", MemberName: "Alice", MemberEmail: "alice@example.com", CreatedAt: now},
		{ID: "grant_2", NodeID: "node_2", NodeName: "us-1", MemberID: "member_2", MemberName: "Bob", MemberEmail: "bob@example.com", CreatedAt: now},
	}
	filtered := filterGrants(grants, req)
	if len(filtered) != 1 || filtered[0].ID != "grant_1" {
		t.Fatalf("unexpected filtered grants: %+v", filtered)
	}
}

func TestNormalizeIDsDeduplicates(t *testing.T) {
	got := normalizeIDs([]string{" member_1 ", "member_1", "", "member_2"})
	if len(got) != 2 || got[0] != "member_1" || got[1] != "member_2" {
		t.Fatalf("unexpected normalized ids: %+v", got)
	}
}

func TestRouterHandlesAPIPreflight(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev")
	router := NewRouter(config.ControlPlaneConfig{}, svc)

	req := httptest.NewRequest(http.MethodOptions, "/api/admin/session", nil)
	req.Header.Set("Origin", "https://admin.example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("unexpected allow-origin header %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "authorization,content-type" {
		t.Fatalf("unexpected allow-headers header %q", got)
	}
}

func TestStatelessMemoryModeLogoutAllReturnsSuccess(t *testing.T) {
	st := store.NewMemoryStore()
	admin, err := st.EnsureAdmin("admin@example.com", "hash")
	if err != nil {
		t.Fatal(err)
	}
	manager := auth.NewManager("secret", nil, time.Hour, nil)
	token, _, err := manager.Issue(admin)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewControlPlaneService(st, manager, nil, "memory", "svc", "rev")
	router := NewRouter(config.ControlPlaneConfig{}, svc)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/logout-all", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for stateless logout-all, got %d", rec.Code)
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload["warning"] == "" {
		t.Fatalf("expected warning in payload, got %+v", payload)
	}
}
