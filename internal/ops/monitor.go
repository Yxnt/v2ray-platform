package ops

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"v2ray-platform/internal/config"
	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

type Monitor struct {
	store  store.Store
	cfg    config.ControlPlaneConfig
	client *http.Client

	stop chan struct{}
	wg   sync.WaitGroup

	mu     sync.Mutex
	alerts map[string]domain.Alert
}

func NewMonitor(st store.Store, cfg config.ControlPlaneConfig) *Monitor {
	return &Monitor{
		store:  st,
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Second},
		stop:   make(chan struct{}),
		alerts: map[string]domain.Alert{},
	}
}

func (m *Monitor) Start() {
	if m.cfg.LifecycleSweepInterval > 0 {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			ticker := time.NewTicker(m.cfg.LifecycleSweepInterval)
			defer ticker.Stop()
			for {
				if err := m.SweepMemberPolicies(time.Now().UTC()); err != nil {
					slog.Error("lifecycle sweep failed", "error", err)
				}
				select {
				case <-m.stop:
					return
				case <-ticker.C:
				}
			}
		}()
	}
	if m.cfg.AlertEvaluationInterval > 0 {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			ticker := time.NewTicker(m.cfg.AlertEvaluationInterval)
			defer ticker.Stop()
			for {
				m.evaluateAlerts(time.Now().UTC())
				select {
				case <-m.stop:
					return
				case <-ticker.C:
				}
			}
		}()
	}
}

func (m *Monitor) Stop() {
	close(m.stop)
	m.wg.Wait()
}

func (m *Monitor) SweepMemberPolicies(now time.Time) error {
	// Build a map of all-time usage and tier definitions.
	usageAllTime := map[string]int64{}
	for _, usage := range m.store.ListMemberUsageSummaries() {
		usageAllTime[usage.MemberID] = usage.TotalBytes
	}
	tiers := map[string]domain.Tier{}
	for _, t := range m.store.ListTiers() {
		tiers[t.ID] = t
	}

	// Pre-build a mapping of memberID → set of node IDs (direct grants + group-based grants).
	nodeIDsForMember := m.buildMemberNodeIndex()

	// Start of the current calendar month (UTC) for monthly-reset tiers.
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	for _, member := range m.store.ListMembers() {
		if member.Status != domain.MemberStatusArchived && member.ExpiresAt != nil && !member.ExpiresAt.After(now) && member.Status != domain.MemberStatusExpired {
			status := domain.MemberStatusExpired
			reason := "expired automatically by control-plane policy"
			if _, err := m.store.UpdateMember(member.ID, store.UpdateMemberInput{
				Status:         &status,
				DisabledReason: &reason,
			}); err != nil {
				return err
			}
			_ = m.store.RecordAuditLog("", "member.auto_expired", "member", member.ID, map[string]any{
				"expires_at": member.ExpiresAt,
			})
			m.rebuildMemberNodes(nodeIDsForMember[member.ID])
			continue
		}

		// Determine effective quota and type.
		// Tier quota takes precedence over the per-member quota_bytes_limit.
		var effectiveQuota int64
		quotaType := "fixed" // per-member limit is treated as fixed (all-time)
		if member.TierID != "" {
			if tier, ok := tiers[member.TierID]; ok && tier.QuotaBytes > 0 {
				effectiveQuota = tier.QuotaBytes
				quotaType = tier.QuotaType
			}
		}
		if effectiveQuota == 0 && member.QuotaBytesLimit > 0 {
			effectiveQuota = member.QuotaBytesLimit
		}
		if effectiveQuota == 0 || member.Status != domain.MemberStatusActive {
			continue
		}

		// Calculate usage according to quota type.
		var usedBytes int64
		if quotaType == "monthly" {
			usedBytes = m.store.GetMemberUsageSince(member.ID, monthStart)
		} else {
			usedBytes = usageAllTime[member.ID]
		}

		if usedBytes >= effectiveQuota {
			status := domain.MemberStatusSuspended
			reason := "quota exceeded automatically by control-plane policy"
			if _, err := m.store.UpdateMember(member.ID, store.UpdateMemberInput{
				Status:         &status,
				DisabledReason: &reason,
			}); err != nil {
				return err
			}
			_ = m.store.RecordAuditLog("", "member.auto_suspended_quota", "member", member.ID, map[string]any{
				"quota_bytes_limit": effectiveQuota,
				"quota_type":        quotaType,
				"observed_total":    usedBytes,
			})
			m.rebuildMemberNodes(nodeIDsForMember[member.ID])
		}
	}
	return nil
}

// buildMemberNodeIndex returns a map of memberID → set of node IDs that include that
// member's credentials, covering both direct grants and group-based grants.
func (m *Monitor) buildMemberNodeIndex() map[string]map[string]struct{} {
	idx := map[string]map[string]struct{}{}

	// Direct grants: GrantView has NodeID + MemberID.
	for _, g := range m.store.ListGrants() {
		if _, ok := idx[g.MemberID]; !ok {
			idx[g.MemberID] = map[string]struct{}{}
		}
		idx[g.MemberID][g.NodeID] = struct{}{}
	}

	// Group-based grants: cross-join node group memberships × group grants.
	// Build groupID → []nodeID first.
	groupNodes := map[string][]string{}
	for _, nm := range m.store.ListNodeGroupMemberships() {
		groupNodes[nm.GroupID] = append(groupNodes[nm.GroupID], nm.NodeID)
	}
	for _, gg := range m.store.ListGroupGrantViews() {
		for _, nodeID := range groupNodes[gg.GroupID] {
			if _, ok := idx[gg.MemberID]; !ok {
				idx[gg.MemberID] = map[string]struct{}{}
			}
			idx[gg.MemberID][nodeID] = struct{}{}
		}
	}

	return idx
}

// rebuildMemberNodes triggers a config rebuild on each node in the given set.
// Errors are logged but do not fail the sweep.
func (m *Monitor) rebuildMemberNodes(nodeIDs map[string]struct{}) {
	for nodeID := range nodeIDs {
		if _, err := m.store.RebuildNodeConfig(nodeID); err != nil {
			slog.Error("failed to rebuild node config after member status change", "node_id", nodeID, "error", err)
		}
	}
}

func (m *Monitor) ListAlerts() []domain.Alert {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]domain.Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		out = append(out, alert)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeenAt.After(out[j].LastSeenAt)
	})
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func (m *Monitor) evaluateAlerts(now time.Time) {
	current := map[string]domain.Alert{}
	for _, alert := range m.buildAlerts(now) {
		current[alert.Fingerprint] = alert
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for fingerprint, alert := range current {
		if existing, ok := m.alerts[fingerprint]; ok {
			alert.FirstSeenAt = existing.FirstSeenAt
		} else {
			alert.FirstSeenAt = now
			m.sendWebhook(alert)
		}
		alert.LastSeenAt = now
		alert.Status = "active"
		m.alerts[fingerprint] = alert
	}
	for fingerprint, existing := range m.alerts {
		if existing.Status != "active" {
			continue
		}
		if _, ok := current[fingerprint]; ok {
			continue
		}
		existing.Status = "resolved"
		existing.LastSeenAt = now
		m.alerts[fingerprint] = existing
	}
}

func (m *Monitor) buildAlerts(now time.Time) []domain.Alert {
	alerts := make([]domain.Alert, 0)
	offlineAfter := m.cfg.NodeOfflineAfter
	if offlineAfter <= 0 {
		offlineAfter = 2 * time.Minute
	}

	for _, node := range m.store.ListNodes() {
		last := node.LastHeartbeatAt
		if last.IsZero() {
			last = node.CreatedAt
		}
		if now.Sub(last) > offlineAfter {
			alerts = append(alerts, domain.Alert{
				Fingerprint: "node-offline:" + node.ID,
				Type:        "node_offline",
				Severity:    domain.AlertSeverityCritical,
				Title:       "Node heartbeat overdue",
				Message:     "Node " + node.Name + " has not reported heartbeat within the configured timeout.",
				TargetType:  "node",
				TargetID:    node.ID,
			})
		}
	}

	latestSync := map[string]domain.NodeSyncEvent{}
	syncEvts, _, _ := m.store.ListNodeSyncEvents("", 1, 500)
	for _, event := range syncEvts {
		if _, ok := latestSync[event.NodeID]; ok {
			continue
		}
		latestSync[event.NodeID] = event
	}
	for _, event := range latestSync {
		if event.Success {
			continue
		}
		alerts = append(alerts, domain.Alert{
			Fingerprint: "sync-failed:" + event.NodeID,
			Type:        "sync_failed",
			Severity:    domain.AlertSeverityWarning,
			Title:       "Latest config sync failed",
			Message:     strings.TrimSpace(event.Message),
			TargetType:  "node",
			TargetID:    event.NodeID,
		})
	}

	members := map[string]domain.Member{}
	for _, member := range m.store.ListMembers() {
		members[member.ID] = member
	}
	tiers := map[string]domain.Tier{}
	for _, t := range m.store.ListTiers() {
		tiers[t.ID] = t
	}
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, usage := range m.store.ListMemberUsageSummaries() {
		member, ok := members[usage.MemberID]
		if !ok {
			continue
		}
		// Determine effective quota and type (same logic as SweepMemberPolicies).
		var effectiveQuota int64
		quotaType := "fixed"
		if member.TierID != "" {
			if tier, ok2 := tiers[member.TierID]; ok2 && tier.QuotaBytes > 0 {
				effectiveQuota = tier.QuotaBytes
				quotaType = tier.QuotaType
			}
		}
		if effectiveQuota == 0 && member.QuotaBytesLimit > 0 {
			effectiveQuota = member.QuotaBytesLimit
		}
		if effectiveQuota <= 0 {
			continue
		}
		var usedBytes int64
		if quotaType == "monthly" {
			usedBytes = m.store.GetMemberUsageSince(member.ID, monthStart)
		} else {
			usedBytes = usage.TotalBytes
		}
		ratio := float64(usedBytes) / float64(effectiveQuota)
		switch {
		case ratio >= 1:
			alerts = append(alerts, quotaAlert(member, usedBytes, effectiveQuota, "quota-exceeded", domain.AlertSeverityCritical, "Member quota exceeded"))
		case ratio >= 0.95:
			alerts = append(alerts, quotaAlert(member, usedBytes, effectiveQuota, "quota-95", domain.AlertSeverityWarning, "Member quota above 95%"))
		case ratio >= 0.80:
			alerts = append(alerts, quotaAlert(member, usedBytes, effectiveQuota, "quota-80", domain.AlertSeverityInfo, "Member quota above 80%"))
		}
	}
	return alerts
}

func quotaAlert(member domain.Member, used, limit int64, suffix string, severity domain.AlertSeverity, title string) domain.Alert {
	return domain.Alert{
		Fingerprint: "member-" + suffix + ":" + member.ID,
		Type:        suffix,
		Severity:    severity,
		Title:       title,
		Message:     "Member " + member.Email + " has used " + strconv.FormatInt(used, 10) + " bytes out of " + strconv.FormatInt(limit, 10) + ".",
		TargetType:  "member",
		TargetID:    member.ID,
	}
}

func (m *Monitor) sendWebhook(alert domain.Alert) {
	if strings.TrimSpace(m.cfg.AlertWebhookURL) == "" {
		return
	}
	body, err := json.Marshal(alert)
	if err != nil {
		slog.Error("alert webhook marshal failed", "error", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, m.cfg.AlertWebhookURL, bytes.NewReader(body))
	if err != nil {
		slog.Error("alert webhook request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.client.Do(req)
	if err != nil {
		slog.Error("alert webhook delivery failed", "error", err)
		return
	}
	_ = resp.Body.Close()
}
