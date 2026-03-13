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
	usageByMember := map[string]int64{}
	for _, usage := range m.store.ListMemberUsageSummaries() {
		usageByMember[usage.MemberID] = usage.TotalBytes
	}
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
			continue
		}
		if member.Status == domain.MemberStatusActive && member.QuotaBytesLimit > 0 && usageByMember[member.ID] >= member.QuotaBytesLimit {
			status := domain.MemberStatusSuspended
			reason := "quota exceeded automatically by control-plane policy"
			if _, err := m.store.UpdateMember(member.ID, store.UpdateMemberInput{
				Status:         &status,
				DisabledReason: &reason,
			}); err != nil {
				return err
			}
			_ = m.store.RecordAuditLog("", "member.auto_suspended_quota", "member", member.ID, map[string]any{
				"quota_bytes_limit": member.QuotaBytesLimit,
				"observed_total":    usageByMember[member.ID],
			})
		}
	}
	return nil
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
	for _, event := range m.store.ListNodeSyncEvents("") {
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
	for _, usage := range m.store.ListMemberUsageSummaries() {
		member, ok := members[usage.MemberID]
		if !ok || member.QuotaBytesLimit <= 0 {
			continue
		}
		ratio := float64(usage.TotalBytes) / float64(member.QuotaBytesLimit)
		switch {
		case ratio >= 1:
			alerts = append(alerts, quotaAlert(member, usage.TotalBytes, "quota-exceeded", domain.AlertSeverityCritical, "Member quota exceeded"))
		case ratio >= 0.95:
			alerts = append(alerts, quotaAlert(member, usage.TotalBytes, "quota-95", domain.AlertSeverityWarning, "Member quota above 95%"))
		case ratio >= 0.80:
			alerts = append(alerts, quotaAlert(member, usage.TotalBytes, "quota-80", domain.AlertSeverityInfo, "Member quota above 80%"))
		}
	}
	return alerts
}

func quotaAlert(member domain.Member, total int64, suffix string, severity domain.AlertSeverity, title string) domain.Alert {
	return domain.Alert{
		Fingerprint: "member-" + suffix + ":" + member.ID,
		Type:        suffix,
		Severity:    severity,
		Title:       title,
		Message:     "Member " + member.Email + " has used " + strconv.FormatInt(total, 10) + " bytes out of " + strconv.FormatInt(member.QuotaBytesLimit, 10) + ".",
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
