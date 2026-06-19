package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"v2ray-platform/internal/auth"
	"v2ray-platform/internal/cloudfront"
	"v2ray-platform/internal/config"
	"v2ray-platform/internal/crypto"
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
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{}, svc, nil)

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

func TestAdminWebUIIsServedWithoutCache(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{}, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("expected no-store cache header, got %q", got)
	}
	if !strings.Contains(rec.Body.String(), `<section class="tab-content active" id="tab-nodes">`) {
		t.Fatalf("expected admin index html, got %s", rec.Body.String())
	}
}

func TestPlatformSettingsDefaultUsageCollectionDisabled(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/settings/platform", nil)
	req.Header.Set("X-Admin-Token", "admin-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		UsageCollectionEnabled bool `json:"usageCollectionEnabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.UsageCollectionEnabled {
		t.Fatalf("expected usage collection disabled by default, got %+v", payload)
	}
}

func TestInstallScriptUsesPlatformUsageCollectionSetting(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, nil)

	disabledReq := httptest.NewRequest(http.MethodGet, "/install.sh?token=test-token&name=node-1", nil)
	disabledRec := httptest.NewRecorder()
	router.ServeHTTP(disabledRec, disabledReq)

	if disabledRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for default install script, got %d body=%s", disabledRec.Code, disabledRec.Body.String())
	}
	if !strings.Contains(disabledRec.Body.String(), `NODE_USAGE_SOURCE="disabled"`) {
		t.Fatalf("expected default install script to disable usage collection, got %s", disabledRec.Body.String())
	}

	saveReq := httptest.NewRequest(http.MethodPost, "/api/admin/settings/platform", bytes.NewBufferString(`{
		"usageCollectionEnabled": true
	}`))
	saveReq.Header.Set("X-Admin-Token", "admin-token")
	saveReq.Header.Set("Content-Type", "application/json")
	saveRec := httptest.NewRecorder()
	router.ServeHTTP(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected settings save 200, got %d body=%s", saveRec.Code, saveRec.Body.String())
	}

	enabledReq := httptest.NewRequest(http.MethodGet, "/install.sh?token=test-token&name=node-1", nil)
	enabledRec := httptest.NewRecorder()
	router.ServeHTTP(enabledRec, enabledReq)

	if enabledRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for enabled install script, got %d body=%s", enabledRec.Code, enabledRec.Body.String())
	}
	if !strings.Contains(enabledRec.Body.String(), `NODE_USAGE_SOURCE="runtime"`) {
		t.Fatalf("expected enabled install script to use runtime usage collection, got %s", enabledRec.Body.String())
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
	svc := NewControlPlaneService(st, manager, nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{}, svc, nil)

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

func TestPublicCloudFrontSubscriptionRequiresSuccessfulSync(t *testing.T) {
	st := store.NewMemoryStore()
	token := seedCloudFrontSubscriptionFixture(t, st)
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{}, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/sub/"+token+"/clash-cf.yaml", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when cloudfront has not synced successfully, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPublicCloudFrontSubscriptionUsesEntryHostAndRouteKey(t *testing.T) {
	st := store.NewMemoryStore()
	token := seedCloudFrontSubscriptionFixture(t, st)
	if err := st.UpdateCloudFrontSyncStatus(store.UpdateCloudFrontSyncInput{
		SyncStatus:    "synced",
		DriftStatus:   "in_sync",
		LastSyncError: "",
	}); err != nil {
		t.Fatal(err)
	}
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{}, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/sub/"+token+"/clash-cf.yaml", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `server: "edge.example.com"`) {
		t.Fatalf("expected custom entry host in YAML, got %s", body)
	}
	if !strings.Contains(body, `path: "/`) {
		t.Fatalf("expected route key path in YAML, got %s", body)
	}
	if !strings.Contains(body, "    port: 443\n") {
		t.Fatalf("expected CloudFront YAML to use TLS port 443, got %s", body)
	}
	if !strings.Contains(body, "    tls: true\n") {
		t.Fatalf("expected CloudFront YAML to enable TLS, got %s", body)
	}
	if strings.Contains(body, `server: "node-1.example.com"`) {
		t.Fatalf("expected CloudFront YAML not to fall back to direct host: %s", body)
	}
}

func TestPublicCloudFrontSubscriptionRequiresResyncAfterBindingsChange(t *testing.T) {
	st := store.NewMemoryStore()
	token := seedCloudFrontSubscriptionFixture(t, st)
	if err := st.UpdateCloudFrontBindings(`[{"nodeId":"node-1","routeKey":"rk1"}]`); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCloudFrontSyncStatus(store.UpdateCloudFrontSyncInput{
		SyncStatus:    "synced",
		DriftStatus:   "in_sync",
		LastSyncError: "",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCloudFrontBindings(`[{"nodeId":"node-1","routeKey":"rk1"},{"nodeId":"node-2","routeKey":"rk2"}]`); err != nil {
		t.Fatal(err)
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{}, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/sub/"+token+"/clash-cf.yaml", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 after bindings changed without resync, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "first successful sync") {
		t.Fatalf("expected resync error, got %s", rec.Body.String())
	}
}

func TestPublicCloudFrontSubscriptionUnavailableWhenDriftDetected(t *testing.T) {
	st := store.NewMemoryStore()
	token := seedCloudFrontSubscriptionFixture(t, st)
	if err := st.UpdateCloudFrontSyncStatus(store.UpdateCloudFrontSyncInput{
		SyncStatus:    "synced",
		DriftStatus:   "in_sync",
		LastSyncError: "",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCloudFrontSyncStatus(store.UpdateCloudFrontSyncInput{
		PlanJSON:           `[{"action":"replace_route"}]`,
		DriftStatus:        "drifted",
		PreserveSyncStatus: true,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{}, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/sub/"+token+"/clash-cf.yaml", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 when drift is detected, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "drift") {
		t.Fatalf("expected drift error, got %s", rec.Body.String())
	}
}

type mockControlPlaneCloudFrontClient struct {
	distributions         []cloudfront.DistributionSummary
	distribution          *cloudfront.DistributionState
	createdDistribution   *cloudfront.DistributionState
	listErr               error
	getErr                error
	applyErr              error
	createErr             error
	lastGetDistributionID string
	lastCreateInput       cloudfront.CreateDistributionInput
	applyCalls            int
	lastApplyActions      []cloudfront.RouteAction
	lastApplyRewrites     []cloudfront.RewriteRoute
}

func (m *mockControlPlaneCloudFrontClient) ListDistributions(_ context.Context) ([]cloudfront.DistributionSummary, error) {
	return m.distributions, m.listErr
}

func (m *mockControlPlaneCloudFrontClient) GetDistribution(_ context.Context, distributionID string) (*cloudfront.DistributionState, error) {
	m.lastGetDistributionID = distributionID
	return m.distribution, m.getErr
}

func (m *mockControlPlaneCloudFrontClient) ApplyDistributionRoutes(_ context.Context, _ string, actions []cloudfront.RouteAction, rewrites []cloudfront.RewriteRoute) error {
	m.applyCalls++
	m.lastApplyActions = actions
	m.lastApplyRewrites = rewrites
	if m.applyErr != nil {
		return m.applyErr
	}
	return nil
}

func (m *mockControlPlaneCloudFrontClient) CreateDistribution(_ context.Context, input cloudfront.CreateDistributionInput) (*cloudfront.DistributionState, error) {
	m.lastCreateInput = input
	return m.createdDistribution, m.createErr
}

func TestListCloudFrontDistributions(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	seedEncryptedCloudFrontConfig(t, st, codec)

	mockClient := &mockControlPlaneCloudFrontClient{
		distributions: []cloudfront.DistributionSummary{
			{DistributionID: "E1234", DomainName: "d1234.cloudfront.net", Status: "Deployed"},
		},
		distribution: &cloudfront.DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []cloudfront.OriginState{
				{OriginID: "v2ray-platform-node-node-1", DomainName: "node-1.example.com"},
			},
			Behaviors: []cloudfront.BehaviorState{
				{PathPattern: "/rk123", OriginID: "v2ray-platform-node-node-1"},
			},
		},
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return mockClient, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/cloudfront/distributions", nil)
	req.Header.Set("X-Admin-Token", "admin-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Items []cloudfront.DistributionSummary `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != 1 || payload.Items[0].DistributionID != "E1234" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if !payload.Items[0].ManagedResourcesDetected {
		t.Fatalf("expected managed resources flag, got %+v", payload.Items[0])
	}
}

func TestCloudFrontConfigSaveEncryptsAndMasksSecrets(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	saveBody := bytes.NewBufferString(`{
		"accessKeyId":"AKIATEST1234567890",
		"secretAccessKey":"super-secret-value",
		"sessionToken":"session-secret-value",
		"region":"us-east-1",
		"enabled":true
	}`)
	saveReq := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/config", saveBody)
	saveReq.Header.Set("X-Admin-Token", "admin-token")
	saveReq.Header.Set("Content-Type", "application/json")
	saveRec := httptest.NewRecorder()

	router.ServeHTTP(saveRec, saveReq)

	if saveRec.Code != http.StatusOK {
		t.Fatalf("expected save 200, got %d body=%s", saveRec.Code, saveRec.Body.String())
	}
	cfg, err := st.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EncryptedAccessKeyID == "AKIATEST1234567890" || cfg.EncryptedSecretAccessKey == "super-secret-value" || cfg.EncryptedSessionToken == "session-secret-value" {
		t.Fatalf("expected stored credentials to be encrypted, got %+v", cfg)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/cloudfront/config", nil)
	getReq.Header.Set("X-Admin-Token", "admin-token")
	getRec := httptest.NewRecorder()

	router.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected get 200, got %d body=%s", getRec.Code, getRec.Body.String())
	}
	body := getRec.Body.String()
	if !strings.Contains(body, `"accessKeyId":"****7890"`) {
		t.Fatalf("expected masked access key, got %s", body)
	}
	if strings.Contains(body, "AKIATEST1234567890") || strings.Contains(body, "super-secret-value") || strings.Contains(body, "session-secret-value") {
		t.Fatalf("expected config response not to leak plaintext credentials, got %s", body)
	}
}

func TestCloudFrontConfigRejectsPartialCredentialUpdate(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	seedEncryptedCloudFrontConfig(t, st, codec)
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/config", bytes.NewBufferString(`{
		"accessKeyId":"AKIANEW1234567890",
		"region":"us-east-1"
	}`))
	req.Header.Set("X-Admin-Token", "admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected partial credential update to fail with 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := st.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	accessKey, err := codec.Decrypt(cfg.EncryptedAccessKeyID)
	if err != nil {
		t.Fatal(err)
	}
	secretKey, err := codec.Decrypt(cfg.EncryptedSecretAccessKey)
	if err != nil {
		t.Fatal(err)
	}
	if accessKey != "AKIATEST123456789" || secretKey != "secret-value" {
		t.Fatalf("expected stored credentials to remain unchanged, got access=%q secret=%q", accessKey, secretKey)
	}
}

func TestCloudFrontConfigRejectsSessionTokenOnlyCredentialUpdate(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	seedEncryptedCloudFrontConfig(t, st, codec)
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/config", bytes.NewBufferString(`{
		"sessionToken":"new-session-token",
		"region":"us-east-1"
	}`))
	req.Header.Set("X-Admin-Token", "admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected sessionToken-only credential update to fail with 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := st.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	accessKey, err := codec.Decrypt(cfg.EncryptedAccessKeyID)
	if err != nil {
		t.Fatal(err)
	}
	secretKey, err := codec.Decrypt(cfg.EncryptedSecretAccessKey)
	if err != nil {
		t.Fatal(err)
	}
	if accessKey != "AKIATEST123456789" || secretKey != "secret-value" {
		t.Fatalf("expected stored credentials to remain unchanged, got access=%q secret=%q", accessKey, secretKey)
	}
}

func TestCloudFrontConfigRequiresMasterKey(t *testing.T) {
	st := store.NewMemoryStore()
	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/cloudfront/config", nil)
	req.Header.Set("X-Admin-Token", "admin-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without master key, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "CLOUDFRONT_MASTER_KEY") {
		t.Fatalf("expected explicit master key error, got %s", rec.Body.String())
	}
}

func TestCloudFrontBindScansSelectedDistribution(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	seedEncryptedCloudFrontConfig(t, st, codec)

	_, plainToken, err := st.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node-1",
		TTLHours:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := st.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: plainToken,
		Name:           "node-1",
		Region:         "ap-southeast-1",
		PublicHost:     "node-1.example.com",
		RuntimeFlavor:  "v2ray",
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes := st.ListNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	routeKey := nodes[0].RouteKey

	mockClient := &mockControlPlaneCloudFrontClient{
		distribution: &cloudfront.DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []cloudfront.OriginState{
				{OriginID: "origin-remote", DomainName: "node-1.example.com"},
			},
			Behaviors: []cloudfront.BehaviorState{
				{PathPattern: "/" + routeKey, OriginID: "origin-remote"},
			},
		},
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return mockClient, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	body := bytes.NewBufferString(`{"distributionId":"E1234"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/bind", body)
	req.Header.Set("X-Admin-Token", "admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if mockClient.lastGetDistributionID != "E1234" {
		t.Fatalf("expected bind flow to scan selected distribution, got %q", mockClient.lastGetDistributionID)
	}

	cfg, err := st.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DistributionID != "E1234" || cfg.DistributionDomainName != "d1234.cloudfront.net" {
		t.Fatalf("expected distribution to be persisted, got %+v", cfg)
	}
	if !strings.Contains(cfg.OriginsJSON, "origin-remote") {
		t.Fatalf("expected scanned origins to be persisted, got %s", cfg.OriginsJSON)
	}
	if !strings.Contains(cfg.BindingsJSON, "v2ray-platform-node-"+reg.NodeID) {
		t.Fatalf("expected bindings to keep managed placeholder, got %s", cfg.BindingsJSON)
	}
	if reg.NodeID == "" {
		t.Fatal("expected register output to include node id")
	}
}

func TestCloudFrontBindCreatesManagedDistributionWhenNoneSelected(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	seedEncryptedCloudFrontConfig(t, st, codec)

	_, plainToken, err := st.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node-1",
		TTLHours:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := st.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: plainToken,
		Name:           "node-1",
		Region:         "ap-southeast-1",
		PublicHost:     "node-1.example.com",
		RuntimeFlavor:  "v2ray",
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes := st.ListNodes()
	routeKey := nodes[0].RouteKey

	mockClient := &mockControlPlaneCloudFrontClient{
		createdDistribution: &cloudfront.DistributionState{
			DistributionID: "ENEW123",
			DomainName:     "dnew123.cloudfront.net",
		},
		distribution: &cloudfront.DistributionState{
			DistributionID: "ENEW123",
			DomainName:     "dnew123.cloudfront.net",
			Origins: []cloudfront.OriginState{
				{OriginID: "v2ray-platform-node-" + reg.NodeID, DomainName: "node-1.example.com"},
			},
			Behaviors: []cloudfront.BehaviorState{
				{PathPattern: "/" + routeKey, OriginID: "v2ray-platform-node-" + reg.NodeID},
			},
		},
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return mockClient, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/bind", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Admin-Token", "admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if mockClient.lastCreateInput.Comment == "" || len(mockClient.lastCreateInput.Nodes) != 1 {
		t.Fatalf("expected create distribution input to include one node, got %+v", mockClient.lastCreateInput)
	}
	cfg, err := st.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DistributionID != "ENEW123" || cfg.DistributionDomainName != "dnew123.cloudfront.net" {
		t.Fatalf("expected created distribution persisted, got %+v", cfg)
	}
}

func TestCloudFrontBindRequiresDistributionWhenNotManagedCreate(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	seedEncryptedCloudFrontConfig(t, st, codec)
	if err := st.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		AWSRegion:             "us-east-1",
		Mode:                  "adopted",
		RetainExistingSecrets: true,
	}); err != nil {
		t.Fatal(err)
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return &mockControlPlaneCloudFrontClient{}, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/bind", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Admin-Token", "admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when adopted mode has no selected distribution, got %d body=%s", rec.Code, rec.Body.String())
	}
	cfg, err := st.GetCloudFrontConfig()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(cfg.BindingsJSON) != "" {
		t.Fatalf("expected bind to leave bindings untouched, got %s", cfg.BindingsJSON)
	}
}

func TestCloudFrontScanUsesSelectedDistributionWhenProvided(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	seedEncryptedCloudFrontConfig(t, st, codec)

	mockClient := &mockControlPlaneCloudFrontClient{
		distribution: &cloudfront.DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []cloudfront.OriginState{
				{OriginID: "origin-remote", DomainName: "node-1.example.com"},
			},
			Behaviors: []cloudfront.BehaviorState{
				{PathPattern: "/rk123", OriginID: "origin-remote"},
			},
		},
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return mockClient, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	body := bytes.NewBufferString(`{"distributionId":"E1234"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/scan", body)
	req.Header.Set("X-Admin-Token", "admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if mockClient.lastGetDistributionID != "E1234" {
		t.Fatalf("expected scan flow to scan selected distribution, got %q", mockClient.lastGetDistributionID)
	}
}

func TestCloudFrontAdminUIContainsWizardShell(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`id="cf-wizard"`,
		`id="cf-current-setup"`,
		`id="cf-stepper"`,
		`data-cf-step="connect"`,
		`data-cf-step="path"`,
		`data-cf-step="target"`,
		`data-cf-step="review"`,
		`data-cf-step="sync"`,
		`Show technical details`,
		`const cloudFrontStepOrder = ['connect', 'path', 'target', 'review', 'sync'];`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront wizard shell to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIContainsCurrentSetupActions(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`Keep current setup`,
		`Change CloudFront setup`,
		`Use existing credentials`,
		`Replace credentials`,
		`Connect and continue`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront setup UI to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIKeepsConnectStepFocusedOnCredentials(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)
	start := strings.Index(html, `function renderCloudFrontConnectStep()`)
	end := strings.Index(html, `function renderCloudFrontDeliverySettings()`)
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("expected CloudFront connect and delivery settings renderers")
	}
	connectStep := html[start:end]
	forbidden := []string{
		`cf-region`,
		`cf-custom-entry-host`,
		`cf-distribution-id`,
		`cf-distribution-domain`,
		`<select id="cf-mode">`,
		`Bound Distribution ID`,
		`Bound Distribution Domain`,
	}
	for _, needle := range forbidden {
		if strings.Contains(connectStep, needle) {
			t.Fatalf("expected CloudFront connect step not to contain %q", needle)
		}
	}
	required := []string{
		`function renderCloudFrontDeliverySettings()`,
		`${renderCloudFrontDeliverySettings()}`,
		`id="cf-region"`,
		`id="cf-custom-entry-host"`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront delivery settings flow to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIClickDelegationUsesClosestIDForWizardCards(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`const target = rawTarget.closest('[id]');`,
		`if (target.id === 'cf-path-existing') return chooseCloudFrontPath('existing');`,
		`if (target.id === 'cf-path-managed') return chooseCloudFrontPath('managed');`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront wizard click delegation to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIKeepCurrentSetupGoesToSyncStep(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`id="cf-keep-current-btn"`,
		`async function keepCurrentCloudFrontSetup()`,
		`renderCloudFrontWizard();`,
		`await prepareCloudFrontSyncStep();`,
		`cloudFrontWizard.plan = null;`,
		`cloudFrontWizard.sync = null;`,
		`setCloudFrontStep('sync');`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront keep-current flow to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIContainsWizardPathChoice(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`Use existing distribution`,
		`Create new managed distribution`,
		`No existing distributions found`,
		`Create a managed distribution instead`,
		`Use selected distribution`,
		`Create distribution`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront wizard path UI to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIContainsReviewAndSyncWizardStates(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`Bind distribution`,
		`Apply to CloudFront`,
		`Re-check changes`,
		`CloudFront path -> WebSocket path`,
		`Show technical details`,
		`Hide technical details`,
		`<h4>Origins</h4>`,
		`<h4>Cache behaviors</h4>`,
		`<h4>Parameters</h4>`,
		`review?.distributionOrigins`,
		`review?.cacheBehaviors`,
		`review?.parameters`,
		`plan?.rewriteRoutes`,
		`No websocket path rewrites planned.`,
		`function canNavigateCloudFrontStep(step)`,
		`return targetIndex <= activeIndex;`,
		`const stepTarget = rawTarget.closest('[data-cf-step]');`,
		`return navigateCloudFrontStep(stepTarget.dataset.cfStep);`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront wizard review/sync UI to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIManagedCreatePersistsStateBeforeBind(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`async function persistCloudFrontManagedCreateState()`,
		`cloudFrontWizard.form.distributionId = '';`,
		`cloudFrontWizard.form.distributionDomainName = '';`,
		`await saveCloudFrontConfig({`,
		`mode: 'managed',`,
		`await persistCloudFrontManagedCreateState();`,
		`const result = await api('/api/admin/cloudfront/bind', {`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront managed-create flow to contain %q", needle)
		}
	}
}

func TestCloudFrontAdminUIResetsWizardStateOnDeleteOrMissingConfig(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`function resetCloudFrontWizardState()`,
		`cloudFrontWizard.currentSetupDismissed = false;`,
		`cloudFrontWizard.step = 'connect';`,
		`resetCloudFrontWizardState();`,
		`document.getElementById('cf-status-drift').textContent = cfg.driftStatus === 'conflict' ? 'Conflict' : cfg.driftStatus === 'drifted' ? 'Drifted' : 'In sync';`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected CloudFront reset/conflict UI to contain %q", needle)
		}
	}
}

func TestAdminUISubscriptionCopyFallsBackWithoutClipboard(t *testing.T) {
	htmlBytes, err := webAssets.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(htmlBytes)

	required := []string{
		`async function copyTextWithFallback(text, promptMessage, onSuccess)`,
		`const clipboard = globalThis.navigator?.clipboard;`,
		`if (clipboard?.writeText) {`,
		`prompt(promptMessage, text);`,
		`await copyTextWithFallback(url, 'Subscription URL (copy manually):', () => {`,
		`await copyTextWithFallback(url, 'Copy CloudFront subscription URL:', () => {`,
	}
	for _, needle := range required {
		if !strings.Contains(html, needle) {
			t.Fatalf("expected subscription copy UI to contain %q", needle)
		}
	}
}

func TestCloudFrontPlanReadsLiveDistributionState(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	routeKey := seedCloudFrontSyncFixture(t, st, codec)

	mockClient := &mockControlPlaneCloudFrontClient{
		distribution: &cloudfront.DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []cloudfront.OriginState{
				{OriginID: "custom-origin", DomainName: "custom.example.com"},
			},
			Behaviors: []cloudfront.BehaviorState{
				{PathPattern: "/" + routeKey, OriginID: "custom-origin"},
			},
		},
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return mockClient, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/plan", nil)
	req.Header.Set("X-Admin-Token", "admin-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if mockClient.lastGetDistributionID != "E1234" {
		t.Fatalf("expected plan to read live distribution, got %q", mockClient.lastGetDistributionID)
	}
	body := rec.Body.String()
	requiredJSON := []string{`"actions"`, `"rewriteRoutes"`, `"driftStatus"`, `"action"`, `"routeKey"`, `"reason"`}
	for _, needle := range requiredJSON {
		if !strings.Contains(body, needle) {
			t.Fatalf("expected plan response body to contain %q, got %s", needle, body)
		}
	}
	var payload cloudfront.SyncPlan
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DriftStatus != "conflict" {
		t.Fatalf("expected live unmanaged conflict, got %+v", payload)
	}
}

func TestCloudFrontSyncReadsLiveDistributionAndBlocksUnmanagedConflict(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	routeKey := seedCloudFrontSyncFixture(t, st, codec)

	mockClient := &mockControlPlaneCloudFrontClient{
		distribution: &cloudfront.DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []cloudfront.OriginState{
				{OriginID: "custom-origin", DomainName: "custom.example.com"},
			},
			Behaviors: []cloudfront.BehaviorState{
				{PathPattern: "/" + routeKey, OriginID: "custom-origin"},
			},
		},
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return mockClient, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/sync", nil)
	req.Header.Set("X-Admin-Token", "admin-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if mockClient.lastGetDistributionID != "E1234" {
		t.Fatalf("expected sync to read live distribution, got %q", mockClient.lastGetDistributionID)
	}
	if mockClient.applyCalls != 0 {
		t.Fatalf("expected sync conflict to stop before AWS mutation, apply calls=%d actions=%+v", mockClient.applyCalls, mockClient.lastApplyActions)
	}
	var payload cloudfront.SyncResult
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.DriftStatus != "conflict" || payload.SyncStatus != "failed" {
		t.Fatalf("expected failed conflict result, got %+v", payload)
	}
}

func TestCloudFrontSyncEnsuresRewriteRoutesWhenDistributionIsInSync(t *testing.T) {
	st := store.NewMemoryStore()
	codec := newCloudFrontTestCodec(t)
	routeKey := seedCloudFrontSyncFixture(t, st, codec)

	nodes := st.ListNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	originID := "v2ray-platform-node-" + nodes[0].ID
	mockClient := &mockControlPlaneCloudFrontClient{
		distribution: &cloudfront.DistributionState{
			DistributionID: "E1234",
			DomainName:     "d1234.cloudfront.net",
			Origins: []cloudfront.OriginState{
				{OriginID: originID, DomainName: "node-1.example.com"},
			},
			Behaviors: []cloudfront.BehaviorState{
				{PathPattern: "/" + routeKey, OriginID: originID},
			},
		},
	}

	svc := NewControlPlaneService(st, auth.NewManager("secret", nil, time.Hour, nil), nil, "memory", "svc", "rev", "", 0)
	svc.cloudFrontClientFactory = func(*domain.CloudFrontConfig) (cloudfront.Client, error) {
		return mockClient, nil
	}
	router := NewRouter(config.ControlPlaneConfig{AdminToken: "admin-token"}, svc, codec)

	req := httptest.NewRequest(http.MethodPost, "/api/admin/cloudfront/sync", nil)
	req.Header.Set("X-Admin-Token", "admin-token")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if mockClient.applyCalls != 1 {
		t.Fatalf("expected sync to ensure rewrite function, apply calls=%d", mockClient.applyCalls)
	}
	if len(mockClient.lastApplyActions) != 0 {
		t.Fatalf("expected no route mutations, got %+v", mockClient.lastApplyActions)
	}
	if len(mockClient.lastApplyRewrites) != 1 || mockClient.lastApplyRewrites[0].RouteKey != routeKey || mockClient.lastApplyRewrites[0].Path != "/node-1" {
		t.Fatalf("expected route rewrite for current node path, got %+v", mockClient.lastApplyRewrites)
	}
}

func newCloudFrontTestCodec(t *testing.T) *crypto.SecretCodec {
	t.Helper()
	codec, err := crypto.NewSecretCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	return codec
}

func seedEncryptedCloudFrontConfig(t *testing.T, st *store.MemoryStore, codec *crypto.SecretCodec) {
	t.Helper()
	encAK, err := codec.Encrypt("AKIATEST123456789")
	if err != nil {
		t.Fatal(err)
	}
	encSK, err := codec.Encrypt("secret-value")
	if err != nil {
		t.Fatal(err)
	}
	enabled := true
	if err := st.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     encAK,
		EncryptedSecretAccessKey: encSK,
		AWSRegion:                "us-east-1",
		Mode:                     "managed",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}
}

func seedCloudFrontSyncFixture(t *testing.T, st *store.MemoryStore, codec *crypto.SecretCodec) string {
	t.Helper()
	_, plainToken, err := st.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node-1",
		TTLHours:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := st.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: plainToken,
		Name:           "node-1",
		Region:         "ap-southeast-1",
		PublicHost:     "node-1.example.com",
		RuntimeFlavor:  "v2ray",
	})
	if err != nil {
		t.Fatal(err)
	}
	nodes := st.ListNodes()
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	routeKey := nodes[0].RouteKey
	enabled := true
	encAK, err := codec.Encrypt("AKIATEST123456789")
	if err != nil {
		t.Fatal(err)
	}
	encSK, err := codec.Encrypt("secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     encAK,
		EncryptedSecretAccessKey: encSK,
		AWSRegion:                "us-east-1",
		DistributionID:           "E1234",
		DistributionDomainName:   "d1234.cloudfront.net",
		Mode:                     "managed",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	bindings := []domain.CloudFrontBinding{
		{NodeID: reg.NodeID, OriginID: "v2ray-platform-node-" + reg.NodeID, RouteKey: routeKey, GroupName: "node-1"},
	}
	bindingsJSON, err := json.Marshal(bindings)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateCloudFrontBindings(string(bindingsJSON)); err != nil {
		t.Fatal(err)
	}
	return routeKey
}

func seedCloudFrontSubscriptionFixture(t *testing.T, st *store.MemoryStore) string {
	t.Helper()
	_, plainToken, err := st.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: "node-1",
		TTLHours:    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := st.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: plainToken,
		Name:           "node-1",
		Region:         "ap-southeast-1",
		PublicHost:     "node-1.example.com",
		RuntimeFlavor:  "v2ray",
	})
	if err != nil {
		t.Fatal(err)
	}
	member, err := st.CreateMember(store.CreateMemberInput{
		Name:  "Alice",
		Email: "alice@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateGrant(store.CreateGrantInput{NodeID: reg.NodeID, MemberID: member.ID}); err != nil {
		t.Fatal(err)
	}
	enabled := true
	if err := st.SaveCloudFrontConfig(store.SaveCloudFrontConfigInput{
		EncryptedAccessKeyID:     "enc-ak",
		EncryptedSecretAccessKey: "enc-sk",
		AWSRegion:                "us-east-1",
		CustomEntryHost:          "edge.example.com",
		DistributionID:           "E1234",
		DistributionDomainName:   "d123.cloudfront.net",
		Mode:                     "managed",
		Enabled:                  &enabled,
	}); err != nil {
		t.Fatal(err)
	}
	return member.SubscriptionToken
}
