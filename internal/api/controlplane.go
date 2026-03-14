package api

import (
	"bytes"
	"context"
	"embed"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"v2ray-platform/internal/auth"
	"v2ray-platform/internal/config"
	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

//go:embed web/*
var webAssets embed.FS

type adminClaimsKey struct{}

// agentBinaryCache holds the latest fetched MD5 checksums for agent binaries.
type agentBinaryCache struct {
	mu     sync.RWMutex
	md5s   map[string]string // arch → md5hex
	fetchedAt time.Time
}

func (c *agentBinaryCache) get(arch string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.md5s[arch]
}

func (c *agentBinaryCache) set(arch, md5hex string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.md5s == nil {
		c.md5s = map[string]string{}
	}
	c.md5s[arch] = md5hex
	c.fetchedAt = time.Now()
}

func (c *agentBinaryCache) age() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.fetchedAt.IsZero() {
		return 999 * time.Hour
	}
	return time.Since(c.fetchedAt)
}

type ControlPlaneService struct {
	store            store.Store
	sessions         *auth.Manager
	alerts           interface{ ListAlerts() []domain.Alert }
	storeMode        string
	serviceName      string
	revisionName     string
	agentDownloadURL string
	agentMD5CacheTTL time.Duration
	agentCache       agentBinaryCache
}

func NewControlPlaneService(st store.Store, sessions *auth.Manager, alerts interface{ ListAlerts() []domain.Alert }, storeMode, serviceName, revisionName, agentDownloadURL string, agentMD5CacheTTL time.Duration) *ControlPlaneService {
	if agentMD5CacheTTL <= 0 {
		agentMD5CacheTTL = 5 * time.Minute
	}
	svc := &ControlPlaneService{
		store:            st,
		sessions:         sessions,
		alerts:           alerts,
		storeMode:        storeMode,
		serviceName:      serviceName,
		revisionName:     revisionName,
		agentDownloadURL: agentDownloadURL,
		agentMD5CacheTTL: agentMD5CacheTTL,
	}
	// Pre-warm agent binary MD5 cache in background.
	if agentDownloadURL != "" {
		go svc.refreshAgentMD5("amd64")
		go svc.refreshAgentMD5("arm64")
	}
	return svc
}

func NewRouter(cfg config.ControlPlaneConfig, svc *ControlPlaneService) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(mustSub(webAssets, "web"))))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/admin/login", svc.handleAdminLogin)
	mux.HandleFunc("GET /api/admin/session", withAdmin(cfg, svc.sessions, svc.handleAdminSession))
	mux.HandleFunc("POST /api/admin/logout", withAdmin(cfg, svc.sessions, svc.handleAdminLogout))
	mux.HandleFunc("POST /api/admin/logout-all", withAdmin(cfg, svc.sessions, svc.handleAdminLogoutAll))
	mux.HandleFunc("GET /api/admin/alerts", withAdmin(cfg, svc.sessions, svc.handleListAlerts))
	mux.HandleFunc("GET /api/admin/bootstrap-tokens", withAdmin(cfg, svc.sessions, svc.handleListBootstrapTokens))
	mux.HandleFunc("POST /api/admin/bootstrap-tokens", withAdmin(cfg, svc.sessions, svc.handleCreateBootstrapToken))
	mux.HandleFunc("POST /api/admin/members", withAdmin(cfg, svc.sessions, svc.handleCreateMember))
	mux.HandleFunc("GET /api/admin/members", withAdmin(cfg, svc.sessions, svc.handleListMembers))
	mux.HandleFunc("POST /api/admin/members/batch-delete", withAdmin(cfg, svc.sessions, svc.handleBatchDeleteMembers))
	mux.HandleFunc("PATCH /api/admin/members/{memberID}", withAdmin(cfg, svc.sessions, svc.handleUpdateMember))
	mux.HandleFunc("DELETE /api/admin/members/{memberID}", withAdmin(cfg, svc.sessions, svc.handleDeleteMember))
	mux.HandleFunc("GET /api/admin/members/{memberID}/clash.yaml", withAdmin(cfg, svc.sessions, svc.handleMemberClashConfig))
	mux.HandleFunc("POST /api/admin/grants", withAdmin(cfg, svc.sessions, svc.handleCreateGrant))
	mux.HandleFunc("GET /api/admin/grants", withAdmin(cfg, svc.sessions, svc.handleListGrants))
	mux.HandleFunc("POST /api/admin/grants/batch-revoke", withAdmin(cfg, svc.sessions, svc.handleBatchRevokeGrants))
	mux.HandleFunc("DELETE /api/admin/grants/{grantID}", withAdmin(cfg, svc.sessions, svc.handleDeleteGrant))
	mux.HandleFunc("POST /api/admin/node-groups", withAdmin(cfg, svc.sessions, svc.handleCreateNodeGroup))
	mux.HandleFunc("GET /api/admin/node-groups", withAdmin(cfg, svc.sessions, svc.handleListNodeGroups))
	mux.HandleFunc("PATCH /api/admin/node-groups/{groupID}", withAdmin(cfg, svc.sessions, svc.handleUpdateNodeGroup))
	mux.HandleFunc("DELETE /api/admin/node-groups/{groupID}", withAdmin(cfg, svc.sessions, svc.handleDeleteNodeGroup))
	mux.HandleFunc("GET /api/admin/node-group-memberships", withAdmin(cfg, svc.sessions, svc.handleListNodeGroupMemberships))
	mux.HandleFunc("POST /api/admin/nodes/{nodeID}/groups", withAdmin(cfg, svc.sessions, svc.handleSetNodeGroups))
	mux.HandleFunc("GET /api/admin/node-group-grants", withAdmin(cfg, svc.sessions, svc.handleListGroupGrants))
	mux.HandleFunc("POST /api/admin/node-groups/{groupID}/grants", withAdmin(cfg, svc.sessions, svc.handleCreateGroupGrant))
	mux.HandleFunc("DELETE /api/admin/node-groups/{groupID}/grants/{memberID}", withAdmin(cfg, svc.sessions, svc.handleDeleteGroupGrant))
	mux.HandleFunc("GET /api/admin/nodes", withAdmin(cfg, svc.sessions, svc.handleListNodes))
	mux.HandleFunc("GET /api/admin/nodes/{nodeID}/config", withAdmin(cfg, svc.sessions, svc.handleGetNodeConfig))
	mux.HandleFunc("GET /api/admin/nodes/{nodeID}/config/revisions", withAdmin(cfg, svc.sessions, svc.handleListNodeConfigRevisions))
	mux.HandleFunc("POST /api/admin/nodes/{nodeID}/rollback-config", withAdmin(cfg, svc.sessions, svc.handleRollbackNodeConfig))
	mux.HandleFunc("POST /api/admin/nodes/{nodeID}/rebuild-config", withAdmin(cfg, svc.sessions, svc.handleRebuildNodeConfig))
	mux.HandleFunc("POST /api/admin/nodes/batch-rebuild", withAdmin(cfg, svc.sessions, svc.handleBatchRebuildNodes))
	mux.HandleFunc("GET /api/admin/usage/nodes", withAdmin(cfg, svc.sessions, svc.handleListNodeUsage))
	mux.HandleFunc("GET /api/admin/usage/members", withAdmin(cfg, svc.sessions, svc.handleListMemberUsage))
	mux.HandleFunc("GET /api/admin/export/{resource}", withAdmin(cfg, svc.sessions, svc.handleExportResource))
	mux.HandleFunc("GET /api/admin/sync-events", withAdmin(cfg, svc.sessions, svc.handleListSyncEvents))
	mux.HandleFunc("GET /api/admin/audit-logs", withAdmin(cfg, svc.sessions, svc.handleListAuditLogs))
	mux.HandleFunc("POST /api/admin/tiers", withAdmin(cfg, svc.sessions, svc.handleCreateTier))
	mux.HandleFunc("GET /api/admin/tiers", withAdmin(cfg, svc.sessions, svc.handleListTiers))
	mux.HandleFunc("PATCH /api/admin/tiers/{tierID}", withAdmin(cfg, svc.sessions, svc.handleUpdateTier))
	mux.HandleFunc("DELETE /api/admin/tiers/{tierID}", withAdmin(cfg, svc.sessions, svc.handleDeleteTier))
	// Public Clash subscription endpoint — authenticated via member's subscription token only.
	mux.HandleFunc("GET /sub/{token}/clash.yaml", svc.handlePublicClashSubscription)
	mux.HandleFunc("POST /api/agent/register", svc.handleAgentRegister)
	mux.HandleFunc("POST /api/agent/heartbeat", svc.handleAgentHeartbeat)
	mux.HandleFunc("GET /api/agent/config", svc.handleAgentConfig)
	mux.HandleFunc("POST /api/agent/sync-result", svc.handleAgentSyncResult)
	mux.HandleFunc("POST /api/agent/usage", svc.handleAgentUsage)
	mux.HandleFunc("GET /install.sh", svc.handleInstallScript)
	mux.HandleFunc("GET /node-agent", svc.handleNodeAgentBinary)
	mux.HandleFunc("GET /node-agent.md5", svc.handleNodeAgentMD5)
	return responseMetadataMiddleware(
		svc.serviceName,
		svc.revisionName,
		svc.storeMode,
		loggingMiddleware(apiCORSMiddleware(mux)),
	)
}

func apiCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
			}
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
				allowHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
				if allowHeaders == "" {
					allowHeaders = "Authorization, Content-Type, X-Admin-Token"
				}
				w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type createBootstrapTokenRequest struct {
	Description string `json:"description"`
	TTLHours    int    `json:"ttl_hours"`
}

type createMemberRequest struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Note  string `json:"note"`
}

type createGrantRequest struct {
	NodeID   string `json:"node_id"`
	MemberID string `json:"member_id"`
}

type createNodeGroupRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// updateMemberRequest is used by PATCH /api/admin/members/{id}.
// All fields are optional; omitted fields leave the member unchanged.
type updateMemberRequest struct {
	Name            *string `json:"name,omitempty"`
	Email           *string `json:"email,omitempty"`
	Note            *string `json:"note,omitempty"`
	UUID            *string `json:"uuid,omitempty"`
	TierID          *string `json:"tier_id,omitempty"`
	Status          *string `json:"status,omitempty"`     // active | suspended | expired
	ExpiresAt       *string `json:"expires_at,omitempty"` // RFC3339 timestamp or empty string to clear
	QuotaBytesLimit *int64  `json:"quota_bytes_limit,omitempty"`
	DisabledReason  *string `json:"disabled_reason,omitempty"`
}

type createTierRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	QuotaBytes  int64  `json:"quota_bytes"`
	QuotaType   string `json:"quota_type"` // "monthly" or "fixed"
}

type updateTierRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	QuotaBytes  *int64  `json:"quota_bytes,omitempty"`
	QuotaType   *string `json:"quota_type,omitempty"`
}

type batchDeleteMembersRequest struct {
	MemberIDs []string `json:"member_ids"`
}

type batchRevokeGrantsRequest struct {
	GrantIDs []string `json:"grant_ids"`
}

type batchRebuildNodesRequest struct {
	NodeIDs []string `json:"node_ids"`
}

type setNodeGroupsRequest struct {
	GroupIDs []string `json:"group_ids"`
}

type groupGrantRequest struct {
	MemberID string `json:"member_id"`
}

type registerAgentRequest struct {
	BootstrapToken string   `json:"bootstrap_token"`
	Name           string   `json:"name"`
	Region         string   `json:"region"`
	PublicHost     string   `json:"public_host"`
	Provider       string   `json:"provider"`
	Tags           []string `json:"tags"`
	RuntimeFlavor  string   `json:"runtime_flavor"`
}

type heartbeatRequest struct {
	AppliedConfigVersion int64  `json:"applied_config_version"`
	PublicHost           string `json:"public_host"`
	Status               string `json:"status"`
	Arch                 string `json:"arch,omitempty"` // e.g. "amd64" or "arm64"
}

type heartbeatResponse struct {
	Status            string `json:"status"`
	AgentMD5          string `json:"agent_md5,omitempty"`
	AgentDownloadURL  string `json:"agent_download_url,omitempty"`
}

type syncResultRequest struct {
	ConfigVersion int64  `json:"config_version"`
	Success       bool   `json:"success"`
	Message       string `json:"message"`
}

type usageRequest struct {
	Snapshots []domain.UsageSnapshot `json:"snapshots"`
}

func (svc *ControlPlaneService) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	admin, err := svc.store.FindAdminByEmail(req.Email)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid credentials"))
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte(req.Password)); err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("invalid credentials"))
		return
	}
	token, claims, err := svc.sessions.Issue(admin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": token,
		"admin": map[string]any{
			"id":    admin.ID,
			"email": admin.Email,
		},
		"expires_at": claims.ExpiresAt,
	})
}

func (svc *ControlPlaneService) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	claims, ok := adminClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	warnings := make([]string, 0, 1)
	if svc.storeMode == "memory" {
		warnings = append(warnings, "running without DATABASE_URL: admin sessions are signed and replica-safe, but logout-all/server-side session revocation requires PostgreSQL")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": claims.SessionID,
		"admin_id":   claims.AdminID,
		"email":      claims.Email,
		"expires_at": claims.ExpiresAt,
		"store_mode": svc.storeMode,
		"service":    svc.serviceName,
		"revision":   svc.revisionName,
		"warnings":   warnings,
	})
}

func (svc *ControlPlaneService) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	claims, ok := adminClaimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	if claims.SessionID != "" {
		if err := svc.store.RevokeAdminSession(claims.SessionID); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	targetType := "admin"
	targetID := claims.AdminID
	if claims.SessionID != "" {
		targetType = "session"
		targetID = claims.SessionID
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "admin.logout", targetType, targetID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleAdminLogoutAll(w http.ResponseWriter, r *http.Request) {
	claims, ok := adminClaimsFromContext(r.Context())
	if !ok || claims.AdminID == "" {
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	if claims.SessionID == "" {
		_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "admin.logout_all", "admin", claims.AdminID, map[string]any{
			"warning": "stateless memory-mode sessions cannot be revoked server-side",
		})
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "ok",
			"warning": "logout-all requires DATABASE_URL when running in stateless memory mode",
		})
		return
	}
	if err := svc.store.RevokeAdminSessions(claims.AdminID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "admin.logout_all", "admin", claims.AdminID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	if svc.alerts == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []domain.Alert{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": svc.alerts.ListAlerts()})
}

func (svc *ControlPlaneService) handleCreateBootstrapToken(w http.ResponseWriter, r *http.Request) {
	var req createBootstrapTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token, plainToken, err := svc.store.CreateBootstrapToken(store.CreateBootstrapTokenInput{
		Description: req.Description,
		TTLHours:    req.TTLHours,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"bootstrap_token_id": token.ID,
		"bootstrap_token":    plainToken,
		"expires_at":         token.ExpiresAt,
	})
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "bootstrap_token.created", "bootstrap_token", token.ID, map[string]any{
		"description": req.Description,
		"ttl_hours":   req.TTLHours,
	})
}

func (svc *ControlPlaneService) handleListBootstrapTokens(w http.ResponseWriter, r *http.Request) {
	items := svc.store.ListBootstrapTokens()
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func (svc *ControlPlaneService) handleCreateMember(w http.ResponseWriter, r *http.Request) {
	var req createMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	member, err := svc.store.CreateMember(store.CreateMemberInput{
		Name:  req.Name,
		Email: req.Email,
		Note:  req.Note,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, member)
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "member.created", "member", member.ID, map[string]any{
		"email": member.Email,
		"name":  member.Name,
	})
}

func (svc *ControlPlaneService) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	memberID := r.PathValue("memberID")
	if memberID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing member id"))
		return
	}
	var req updateMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	input := store.UpdateMemberInput{
		Name:            req.Name,
		Email:           req.Email,
		Note:            req.Note,
		UUID:            req.UUID,
		TierID:          req.TierID,
		QuotaBytesLimit: req.QuotaBytesLimit,
		DisabledReason:  req.DisabledReason,
	}
	if req.Status != nil {
		st := domain.MemberStatus(*req.Status)
		input.Status = &st
	}
	if req.ExpiresAt != nil {
		if *req.ExpiresAt == "" {
			input.ClearExpiry = true
		} else {
			t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
			if err != nil {
				writeError(w, http.StatusBadRequest, errors.New("expires_at must be RFC3339 or empty string"))
				return
			}
			input.ExpiresAt = &t
		}
	}
	member, err := svc.store.UpdateMember(memberID, input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "member.updated", "member", memberID, map[string]any{
		"status":     member.Status,
		"expires_at": member.ExpiresAt,
	})
	writeJSON(w, http.StatusOK, member)
}

func (svc *ControlPlaneService) handleListMembers(w http.ResponseWriter, r *http.Request) {
	items := filterMembers(svc.store.ListMembers(), r)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func (svc *ControlPlaneService) buildMemberClashYAML(member *domain.Member) []byte {
	// Collect all node IDs the member has access to.
	nodeIDs := map[string]struct{}{}
	for _, g := range svc.store.ListGrants() {
		if g.MemberID == member.ID {
			nodeIDs[g.NodeID] = struct{}{}
		}
	}
	groupIDs := map[string]struct{}{}
	for _, gg := range svc.store.ListGroupGrantViews() {
		if gg.MemberID == member.ID {
			groupIDs[gg.GroupID] = struct{}{}
		}
	}
	for _, m := range svc.store.ListNodeGroupMemberships() {
		if _, ok := groupIDs[m.GroupID]; ok {
			nodeIDs[m.NodeID] = struct{}{}
		}
	}

	type wsOpts struct {
		Path string
	}
	type proxy struct {
		Name    string
		Server  string
		Port    int
		UUID    string
		WsPath  string
	}

	var proxies []proxy
	var proxyNames []string
	for _, node := range svc.store.ListNodes() {
		if _, ok := nodeIDs[node.ID]; !ok {
			continue
		}
		name := node.Name
		if node.Region != "" {
			name = node.Region + " - " + node.Name
		}
		proxies = append(proxies, proxy{
			Name:   name,
			Server: node.PublicHost,
			Port:   80,
			UUID:   member.UUID,
			WsPath: "/" + node.Name,
		})
		proxyNames = append(proxyNames, name)
	}

	var buf bytes.Buffer
	buf.WriteString("mixed-port: 7890\n")
	buf.WriteString("allow-lan: false\n")
	buf.WriteString("mode: rule\n")
	buf.WriteString("log-level: info\n")
	buf.WriteString("external-controller: 127.0.0.1:9090\n\n")
	buf.WriteString("dns:\n")
	buf.WriteString("  enable: true\n")
	buf.WriteString("  ipv6: false\n")
	buf.WriteString("  nameserver:\n")
	buf.WriteString("    - 223.5.5.5\n")
	buf.WriteString("    - 119.29.29.29\n")
	buf.WriteString("  fallback:\n")
	buf.WriteString("    - 8.8.8.8\n")
	buf.WriteString("    - 1.1.1.1\n")
	buf.WriteString("  fallback-filter:\n")
	buf.WriteString("    geoip: true\n\n")

	buf.WriteString("proxies:\n")
	for _, p := range proxies {
		fmt.Fprintf(&buf, "  - name: %q\n", p.Name)
		buf.WriteString("    type: vmess\n")
		fmt.Fprintf(&buf, "    server: %q\n", p.Server)
		fmt.Fprintf(&buf, "    port: %d\n", p.Port)
		fmt.Fprintf(&buf, "    uuid: %q\n", p.UUID)
		buf.WriteString("    alterId: 0\n")
		buf.WriteString("    cipher: auto\n")
		buf.WriteString("    network: ws\n")
		buf.WriteString("    ws-opts:\n")
		fmt.Fprintf(&buf, "      path: %q\n", p.WsPath)
		buf.WriteString("\n")
	}

	buf.WriteString("proxy-groups:\n")
	buf.WriteString("  - name: Proxy\n")
	buf.WriteString("    type: select\n")
	buf.WriteString("    proxies:\n")
	for _, name := range proxyNames {
		fmt.Fprintf(&buf, "      - %q\n", name)
	}
	buf.WriteString("\n")

	buf.WriteString("rules:\n")
	buf.WriteString("  - GEOIP,CN,DIRECT\n")
	buf.WriteString("  - MATCH,Proxy\n")
	return buf.Bytes()
}

func (svc *ControlPlaneService) handleMemberClashConfig(w http.ResponseWriter, r *http.Request) {
	memberID := r.PathValue("memberID")
	if memberID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing member id"))
		return
	}
	var member *domain.Member
	for _, m := range svc.store.ListMembers() {
		if m.ID == memberID {
			mc := m
			member = &mc
			break
		}
	}
	if member == nil {
		writeError(w, http.StatusNotFound, errors.New("member not found"))
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", `attachment; filename="v2-subscription"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(svc.buildMemberClashYAML(member))
}

// handlePublicClashSubscription serves a Clash subscription YAML for the member
// identified by their subscription_token. This endpoint is intentionally public
// (no admin auth) so users can paste the URL into their Clash client.
func (svc *ControlPlaneService) handlePublicClashSubscription(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	if token == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing token"))
		return
	}
	member, err := svc.store.GetMemberBySubscriptionToken(token)
	if err != nil {
		writeError(w, http.StatusNotFound, errors.New("not found"))
		return
	}
	if member.Status != domain.MemberStatusActive {
		writeError(w, http.StatusForbidden, errors.New("account inactive"))
		return
	}

	// Build Subscription-Userinfo header so Clash/Stash/etc. show usage stats.
	// Format: upload=N; download=N; total=N; expire=N
	// https://github.com/Dreamacro/clash/issues/1262
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Determine effective quota and whether it resets monthly.
	var totalQuota int64
	isMonthly := false
	if member.TierID != "" {
		for _, t := range svc.store.ListTiers() {
			if t.ID == member.TierID {
				totalQuota = t.QuotaBytes
				isMonthly = t.QuotaType == "monthly"
				break
			}
		}
	}
	if totalQuota == 0 && member.QuotaBytesLimit > 0 {
		totalQuota = member.QuotaBytesLimit
	}

	var upload, download int64
	usages := svc.store.ListMemberUsageSummaries()
	for _, u := range usages {
		if u.MemberID != member.ID {
			continue
		}
		if isMonthly {
			upload, download = svc.store.GetMemberUsageSinceSplit(member.ID, monthStart)
		} else {
			upload = u.UplinkBytes
			download = u.DownlinkBytes
		}
		break
	}

	userinfo := fmt.Sprintf("upload=%d; download=%d; total=%d", upload, download, totalQuota)
	if member.ExpiresAt != nil {
		userinfo += fmt.Sprintf("; expire=%d", member.ExpiresAt.Unix())
	}
	w.Header().Set("Subscription-Userinfo", userinfo)

	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", `attachment; filename="v2-subscription"`)
	// Clash clients poll this URL; tell them to re-fetch every hour.
	w.Header().Set("Profile-Update-Interval", "1")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(svc.buildMemberClashYAML(member))
}

func (svc *ControlPlaneService) handleDeleteMember(w http.ResponseWriter, r *http.Request) {
	memberID := r.PathValue("memberID")
	if memberID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing member id"))
		return
	}
	if err := svc.store.DeleteMember(memberID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "member.deleted", "member", memberID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleBatchDeleteMembers(w http.ResponseWriter, r *http.Request) {
	var req batchDeleteMembersRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	memberIDs := normalizeIDs(req.MemberIDs)
	if len(memberIDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("member_ids is required"))
		return
	}
	existing := map[string]struct{}{}
	for _, member := range svc.store.ListMembers() {
		existing[member.ID] = struct{}{}
	}
	for _, memberID := range memberIDs {
		if _, ok := existing[memberID]; !ok {
			writeError(w, http.StatusBadRequest, errors.New("unknown member id: "+memberID))
			return
		}
	}
	for _, memberID := range memberIDs {
		if err := svc.store.DeleteMember(memberID); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "member.batch_deleted", "member_batch", strings.Join(memberIDs, ","), map[string]any{
		"member_ids": memberIDs,
		"count":      len(memberIDs),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"deleted_count": len(memberIDs),
	})
}

func (svc *ControlPlaneService) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	var req createGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	grant, cred, err := svc.store.CreateGrant(store.CreateGrantInput{
		NodeID:   req.NodeID,
		MemberID: req.MemberID,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"grant":      grant,
		"credential": cred,
	})
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "grant.created", "grant", grant.ID, map[string]any{
		"node_id":    grant.NodeID,
		"member_id":  grant.MemberID,
		"credential": cred.UUID,
	})
}

func (svc *ControlPlaneService) handleCreateNodeGroup(w http.ResponseWriter, r *http.Request) {
	var req createNodeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	group, err := svc.store.CreateNodeGroup(req.Name, req.Description)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node_group.created", "node_group", group.ID, nil)
	writeJSON(w, http.StatusCreated, group)
}

func (svc *ControlPlaneService) handleListNodeGroups(w http.ResponseWriter, r *http.Request) {
	items := svc.store.ListNodeGroups()
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (svc *ControlPlaneService) handleUpdateNodeGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	var req createNodeGroupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	group, err := svc.store.UpdateNodeGroup(groupID, req.Name, req.Description)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node_group.updated", "node_group", group.ID, nil)
	writeJSON(w, http.StatusOK, group)
}

func (svc *ControlPlaneService) handleDeleteNodeGroup(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	if err := svc.store.DeleteNodeGroup(groupID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node_group.deleted", "node_group", groupID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleListNodeGroupMemberships(w http.ResponseWriter, r *http.Request) {
	items := svc.store.ListNodeGroupMemberships()
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (svc *ControlPlaneService) handleSetNodeGroups(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	var req setNodeGroupsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := svc.store.SetNodeGroupsForNode(nodeID, normalizeIDs(req.GroupIDs)); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node_group.memberships_set", "node", nodeID, map[string]any{"group_ids": req.GroupIDs})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleListGroupGrants(w http.ResponseWriter, r *http.Request) {
	items := svc.store.ListGroupGrantViews()
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (svc *ControlPlaneService) handleCreateGroupGrant(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	var req groupGrantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := svc.store.CreateGroupGrant(groupID, req.MemberID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node_group.grant_created", "node_group", groupID, map[string]any{"member_id": req.MemberID})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleDeleteGroupGrant(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("groupID")
	memberID := r.PathValue("memberID")
	if err := svc.store.DeleteGroupGrant(groupID, memberID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node_group.grant_deleted", "node_group", groupID, map[string]any{"member_id": memberID})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleListGrants(w http.ResponseWriter, r *http.Request) {
	items := filterGrants(svc.store.ListGrants(), r)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func (svc *ControlPlaneService) handleDeleteGrant(w http.ResponseWriter, r *http.Request) {
	grantID := r.PathValue("grantID")
	if grantID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing grant id"))
		return
	}
	if err := svc.store.RevokeGrant(grantID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "grant.revoked", "grant", grantID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleBatchRevokeGrants(w http.ResponseWriter, r *http.Request) {
	var req batchRevokeGrantsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	grantIDs := normalizeIDs(req.GrantIDs)
	if len(grantIDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("grant_ids is required"))
		return
	}
	existing := map[string]struct{}{}
	for _, grant := range svc.store.ListGrants() {
		existing[grant.ID] = struct{}{}
	}
	for _, grantID := range grantIDs {
		if _, ok := existing[grantID]; !ok {
			writeError(w, http.StatusBadRequest, errors.New("unknown grant id: "+grantID))
			return
		}
	}
	for _, grantID := range grantIDs {
		if err := svc.store.RevokeGrant(grantID); err != nil {
			writeStoreError(w, err)
			return
		}
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "grant.batch_revoked", "grant_batch", strings.Join(grantIDs, ","), map[string]any{
		"grant_ids": grantIDs,
		"count":     len(grantIDs),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "ok",
		"revoked_count": len(grantIDs),
	})
}

func (svc *ControlPlaneService) handleListNodes(w http.ResponseWriter, r *http.Request) {
	items := filterNodes(svc.store.ListNodes(), r)
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"count": len(items),
	})
}

func (svc *ControlPlaneService) handleListNodeConfigRevisions(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	revisions, err := svc.store.ListNodeConfigRevisions(nodeID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": revisions})
}

func (svc *ControlPlaneService) handleRollbackNodeConfig(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	var req struct {
		ConfigVersion int64 `json:"config_version"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConfigVersion == 0 {
		writeError(w, http.StatusBadRequest, errors.New("config_version required"))
		return
	}
	rev, err := svc.store.RollbackNodeConfig(nodeID, req.ConfigVersion)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node.config_rolled_back", "node", nodeID, map[string]any{
		"rolled_back_to_version": req.ConfigVersion,
		"new_version":            rev.ConfigVersion,
	})
	writeJSON(w, http.StatusOK, rev)
}

func (svc *ControlPlaneService) handleRebuildNodeConfig(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing node id"))
		return
	}
	rev, err := svc.store.RebuildNodeConfig(nodeID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node.config_rebuilt", "node", nodeID, map[string]any{
		"config_version": rev.ConfigVersion,
	})
	writeJSON(w, http.StatusOK, rev)
}

func (svc *ControlPlaneService) handleGetNodeConfig(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("nodeID")
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing node id"))
		return
	}
	rev, err := svc.store.GetNodeConfigByID(nodeID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Also fetch last sync event to show applied status
	events := svc.store.ListNodeSyncEvents(nodeID)
	var lastSync *domain.NodeSyncEvent
	if len(events) > 0 {
		lastSync = &events[0]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"config_version": rev.ConfigVersion,
		"config_json":    rev.Config,
		"last_sync":      lastSync,
	})
}

func (svc *ControlPlaneService) handleBatchRebuildNodes(w http.ResponseWriter, r *http.Request) {
	var req batchRebuildNodesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	nodeIDs := normalizeIDs(req.NodeIDs)
	if len(nodeIDs) == 0 {
		writeError(w, http.StatusBadRequest, errors.New("node_ids is required"))
		return
	}
	revisions := make([]*domain.ConfigRevision, 0, len(nodeIDs))
	for _, nodeID := range nodeIDs {
		rev, err := svc.store.RebuildNodeConfig(nodeID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		revisions = append(revisions, rev)
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "node.config_batch_rebuilt", "node_batch", strings.Join(nodeIDs, ","), map[string]any{
		"node_ids": nodeIDs,
		"count":    len(nodeIDs),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"count":     len(revisions),
		"revisions": revisions,
	})
}

func (svc *ControlPlaneService) handleListNodeUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": svc.store.ListNodeUsageSummaries()})
}

func (svc *ControlPlaneService) handleListMemberUsage(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": svc.store.ListMemberUsageSummaries()})
}

func (svc *ControlPlaneService) handleExportResource(w http.ResponseWriter, r *http.Request) {
	resource := strings.TrimSpace(r.PathValue("resource"))
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}

	var (
		filename string
		rows     [][]string
		payload  any
	)
	switch resource {
	case "members":
		items := svc.store.ListMembers()
		payload = map[string]any{"items": items}
		rows = append(rows, []string{"id", "name", "email", "note", "status", "expires_at", "quota_bytes_limit", "disabled_reason", "created_at", "updated_at"})
		for _, item := range items {
			expires := ""
			if item.ExpiresAt != nil {
				expires = item.ExpiresAt.Format(time.RFC3339)
			}
			rows = append(rows, []string{item.ID, item.Name, item.Email, item.Note, string(item.Status), expires, fmt.Sprintf("%d", item.QuotaBytesLimit), item.DisabledReason, item.CreatedAt.Format(time.RFC3339), item.UpdatedAt.Format(time.RFC3339)})
		}
		filename = "members"
	case "nodes":
		items := svc.store.ListNodes()
		payload = map[string]any{"items": items}
		rows = append(rows, []string{"id", "name", "region", "public_host", "provider", "tags", "runtime_flavor", "status", "last_heartbeat_at", "current_config_version", "created_at", "updated_at"})
		for _, item := range items {
			rows = append(rows, []string{item.ID, item.Name, item.Region, item.PublicHost, item.Provider, strings.Join(item.Tags, ","), item.RuntimeFlavor, string(item.Status), item.LastHeartbeatAt.Format(time.RFC3339), fmt.Sprintf("%d", item.CurrentConfigVersion), item.CreatedAt.Format(time.RFC3339), item.UpdatedAt.Format(time.RFC3339)})
		}
		filename = "nodes"
	case "grants":
		items := svc.store.ListGrants()
		payload = map[string]any{"items": items}
		rows = append(rows, []string{"id", "node_id", "node_name", "member_id", "member_name", "member_email", "created_at"})
		for _, item := range items {
			rows = append(rows, []string{item.ID, item.NodeID, item.NodeName, item.MemberID, item.MemberName, item.MemberEmail, item.CreatedAt.Format(time.RFC3339)})
		}
		filename = "grants"
	case "audit-logs":
		items := svc.store.ListAuditLogs()
		payload = map[string]any{"items": items}
		rows = append(rows, []string{"id", "actor_admin_id", "action", "target_type", "target_id", "payload", "created_at"})
		for _, item := range items {
			rows = append(rows, []string{fmt.Sprintf("%d", item.ID), item.ActorAdminID, item.Action, item.TargetType, item.TargetID, item.Payload, item.CreatedAt.Format(time.RFC3339)})
		}
		filename = "audit-logs"
	default:
		writeError(w, http.StatusNotFound, errors.New("unknown export resource"))
		return
	}

	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "export.downloaded", "export", resource, map[string]any{"format": format})
	if format == "csv" {
		var buf bytes.Buffer
		writer := csv.NewWriter(&buf)
		if err := writer.WriteAll(rows); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename+".csv"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename+".json"))
	writeJSON(w, http.StatusOK, payload)
}

func (svc *ControlPlaneService) handleListSyncEvents(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": svc.store.ListNodeSyncEvents(r.URL.Query().Get("node_id")),
	})
}

func (svc *ControlPlaneService) handleListAuditLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": svc.store.ListAuditLogs()})
}

// ── Tier handlers ─────────────────────────────────────────────────────────────

func (svc *ControlPlaneService) handleCreateTier(w http.ResponseWriter, r *http.Request) {
	var req createTierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, errors.New("name is required"))
		return
	}
	tier, err := svc.store.CreateTier(store.CreateTierInput{
		Name:        req.Name,
		Description: req.Description,
		QuotaBytes:  req.QuotaBytes,
		QuotaType:   req.QuotaType,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "tier.created", "tier", tier.ID, req)
	writeJSON(w, http.StatusCreated, tier)
}

func (svc *ControlPlaneService) handleListTiers(w http.ResponseWriter, r *http.Request) {
	items := svc.store.ListTiers()
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "count": len(items)})
}

func (svc *ControlPlaneService) handleUpdateTier(w http.ResponseWriter, r *http.Request) {
	tierID := r.PathValue("tierID")
	if tierID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing tier id"))
		return
	}
	var req updateTierRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	tier, err := svc.store.UpdateTier(tierID, store.UpdateTierInput{
		Name:        req.Name,
		Description: req.Description,
		QuotaBytes:  req.QuotaBytes,
		QuotaType:   req.QuotaType,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "tier.updated", "tier", tierID, req)
	writeJSON(w, http.StatusOK, tier)
}

func (svc *ControlPlaneService) handleDeleteTier(w http.ResponseWriter, r *http.Request) {
	tierID := r.PathValue("tierID")
	if tierID == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing tier id"))
		return
	}
	if err := svc.store.DeleteTier(tierID); err != nil {
		writeStoreError(w, err)
		return
	}
	_ = svc.store.RecordAuditLog(actorAdminID(r.Context()), "tier.deleted", "tier", tierID, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleAgentRegister(w http.ResponseWriter, r *http.Request) {
	var req registerAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	out, err := svc.store.RegisterNode(store.RegisterNodeInput{
		BootstrapToken: req.BootstrapToken,
		Name:           req.Name,
		Region:         req.Region,
		PublicHost:     req.PublicHost,
		Provider:       req.Provider,
		Tags:           req.Tags,
		RuntimeFlavor:  req.RuntimeFlavor,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (svc *ControlPlaneService) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	var req heartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if _, err := svc.store.Heartbeat(store.HeartbeatInput{
		NodeToken:            token,
		AppliedConfigVersion: req.AppliedConfigVersion,
		PublicHost:           req.PublicHost,
		Status:               req.Status,
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	resp := heartbeatResponse{Status: "ok"}
	if svc.agentDownloadURL != "" {
		arch := req.Arch
		if arch == "" {
			arch = "amd64"
		}
		// Refresh MD5 cache if stale (configurable TTL, default 5 minutes).
		if svc.agentCache.age() > svc.agentMD5CacheTTL {
			go svc.refreshAgentMD5(arch)
		}
		if md5hex := svc.agentCache.get(arch); md5hex != "" {
			downloadURL := svc.agentDownloadURL
			if arch == "arm64" {
				downloadURL = strings.ReplaceAll(downloadURL, "amd64", "arm64")
			}
			resp.AgentMD5 = md5hex
			resp.AgentDownloadURL = downloadURL
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (svc *ControlPlaneService) handleAgentConfig(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r.Header.Get("Authorization"))
	rev, err := svc.store.GetNodeConfig(token)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (svc *ControlPlaneService) handleAgentSyncResult(w http.ResponseWriter, r *http.Request) {
	var req syncResultRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if err := svc.store.RecordSyncResult(store.SyncResultInput{
		NodeToken:     token,
		ConfigVersion: req.ConfigVersion,
		Success:       req.Success,
		Message:       req.Message,
	}); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (svc *ControlPlaneService) handleAgentUsage(w http.ResponseWriter, r *http.Request) {
	var req usageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token := bearerToken(r.Header.Get("Authorization"))
	if err := svc.store.RecordUsage(token, req.Snapshots); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func filterNodes(items []domain.Node, r *http.Request) []domain.Node {
	q := normalizeSearch(r.URL.Query().Get("q"))
	status := normalizeSearch(r.URL.Query().Get("status"))
	region := normalizeSearch(r.URL.Query().Get("region"))
	tag := normalizeSearch(r.URL.Query().Get("tag"))
	filtered := make([]domain.Node, 0, len(items))
	for _, item := range items {
		if status != "" && normalizeSearch(string(item.Status)) != status {
			continue
		}
		if region != "" && !strings.Contains(normalizeSearch(item.Region), region) {
			continue
		}
		if tag != "" && !nodeHasTag(item, tag) {
			continue
		}
		if q != "" && !matchesAny(q, item.ID, item.Name, item.Region, item.PublicHost, item.Provider, item.RuntimeFlavor, strings.Join(item.Tags, " ")) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func filterMembers(items []domain.Member, r *http.Request) []domain.Member {
	q := normalizeSearch(r.URL.Query().Get("q"))
	statusFilter := normalizeSearch(r.URL.Query().Get("status"))
	filtered := make([]domain.Member, 0, len(items))
	for _, item := range items {
		if statusFilter != "" && normalizeSearch(string(item.Status)) != statusFilter {
			continue
		}
		if q != "" && !matchesAny(q, item.ID, item.Name, item.Email, item.Note) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func filterGrants(items []domain.GrantView, r *http.Request) []domain.GrantView {
	q := normalizeSearch(r.URL.Query().Get("q"))
	nodeID := normalizeSearch(r.URL.Query().Get("node_id"))
	memberID := normalizeSearch(r.URL.Query().Get("member_id"))
	filtered := make([]domain.GrantView, 0, len(items))
	for _, item := range items {
		if nodeID != "" && normalizeSearch(item.NodeID) != nodeID {
			continue
		}
		if memberID != "" && normalizeSearch(item.MemberID) != memberID {
			continue
		}
		if q != "" && !matchesAny(q, item.ID, item.NodeID, item.NodeName, item.MemberID, item.MemberName, item.MemberEmail) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func normalizeIDs(ids []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func normalizeSearch(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func matchesAny(query string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(normalizeSearch(value), query) {
			return true
		}
	}
	return false
}

func nodeHasTag(node domain.Node, query string) bool {
	for _, tag := range node.Tags {
		if normalizeSearch(tag) == query {
			return true
		}
	}
	return false
}

func withAdmin(cfg config.ControlPlaneConfig, sessions *auth.Manager, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if claims, ok := claimsFromSession(r, sessions); ok {
			next(w, r.WithContext(withAdminClaims(r.Context(), *claims)))
			return
		}
		if cfg.AdminToken != "" && r.Header.Get("X-Admin-Token") == cfg.AdminToken {
			next(w, r.WithContext(withAdminClaims(r.Context(), auth.Claims{
				AdminID: "legacy-admin",
				Email:   "legacy-admin@local",
			})))
			return
		}
		writeError(w, http.StatusUnauthorized, errors.New("unauthorized"))
	}
}

func claimsFromSession(r *http.Request, sessions *auth.Manager) (*auth.Claims, bool) {
	token := bearerToken(r.Header.Get("Authorization"))
	if token == "" {
		return nil, false
	}
	claims, err := sessions.Verify(token)
	if err != nil {
		return nil, false
	}
	return claims, true
}

func bearerToken(header string) string {
	header = strings.TrimSpace(header)
	if header == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer"))
}

func withAdminClaims(ctx context.Context, claims auth.Claims) context.Context {
	return context.WithValue(ctx, adminClaimsKey{}, claims)
}

func adminClaimsFromContext(ctx context.Context) (auth.Claims, bool) {
	claims, ok := ctx.Value(adminClaimsKey{}).(auth.Claims)
	return claims, ok
}

func actorAdminID(ctx context.Context) string {
	claims, ok := adminClaimsFromContext(ctx)
	if !ok || claims.AdminID == "" || claims.AdminID == "legacy-admin" {
		return ""
	}
	return claims.AdminID
}

var _ = domain.Admin{}
