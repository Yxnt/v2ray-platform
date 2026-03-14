package domain

import "time"

type NodeStatus string

const (
	NodeStatusProvisioning NodeStatus = "provisioning"
	NodeStatusOnline       NodeStatus = "online"
	NodeStatusDegraded     NodeStatus = "degraded"
)

type Admin struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Node struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Region               string     `json:"region"`
	PublicHost           string     `json:"public_host"`
	Provider             string     `json:"provider"`
	Tags                 []string   `json:"tags"`
	RuntimeFlavor        string     `json:"runtime_flavor"`
	Status               NodeStatus `json:"status"`
	LastHeartbeatAt      time.Time  `json:"last_heartbeat_at"`
	CurrentConfigVersion int64      `json:"current_config_version"`
	NodeTokenHash        string     `json:"-"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type MemberStatus string

const (
	MemberStatusActive    MemberStatus = "active"
	MemberStatusSuspended MemberStatus = "suspended"
	MemberStatusExpired   MemberStatus = "expired"
	MemberStatusArchived  MemberStatus = "archived"
)

type Tier struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	QuotaBytes  int64     `json:"quota_bytes"`  // quota in bytes; 0 = unlimited
	QuotaType   string    `json:"quota_type"`   // "monthly" = resets each calendar month; "fixed" = all-time
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Member struct {
	ID                string       `json:"id"`
	UUID              string       `json:"uuid"`
	Name              string       `json:"name"`
	Email             string       `json:"email"`
	Note              string       `json:"note"`
	Status            MemberStatus `json:"status"`
	ExpiresAt         *time.Time   `json:"expires_at,omitempty"`
	QuotaBytesLimit   int64        `json:"quota_bytes_limit"`
	TierID            string       `json:"tier_id,omitempty"`
	SubscriptionToken string       `json:"subscription_token"`
	DisabledReason    string       `json:"disabled_reason,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

type BootstrapToken struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	TokenHash   string     `json:"-"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type AdminSession struct {
	ID         string     `json:"id"`
	AdminID    string     `json:"admin_id"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

type AccessGrant struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"node_id"`
	MemberID  string    `json:"member_id"`
	CreatedAt time.Time `json:"created_at"`
}

type NodeGroup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type NodeGroupMembership struct {
	NodeID    string    `json:"node_id"`
	GroupID   string    `json:"group_id"`
	GroupName string    `json:"group_name"`
	CreatedAt time.Time `json:"created_at"`
}

type GroupGrantView struct {
	GroupID     string    `json:"group_id"`
	GroupName   string    `json:"group_name"`
	MemberID    string    `json:"member_id"`
	MemberName  string    `json:"member_name"`
	MemberEmail string    `json:"member_email"`
	CreatedAt   time.Time `json:"created_at"`
}

type GrantView struct {
	ID          string    `json:"id"`
	NodeID      string    `json:"node_id"`
	NodeName    string    `json:"node_name"`
	MemberID    string    `json:"member_id"`
	MemberName  string    `json:"member_name"`
	MemberEmail string    `json:"member_email"`
	CreatedAt   time.Time `json:"created_at"`
}

type NodeCredential struct {
	ID            string    `json:"id"`
	NodeID        string    `json:"node_id"`
	MemberID      string    `json:"member_id"`
	AccessGrantID string    `json:"access_grant_id"`
	UUID          string    `json:"uuid"`
	Email         string    `json:"email"`
	CreatedAt     time.Time `json:"created_at"`
}

type ConfigRevision struct {
	NodeID        string    `json:"node_id"`
	ConfigVersion int64     `json:"config_version"`
	Config        string    `json:"config,omitempty"`
	ConfigHash    string    `json:"config_hash,omitempty"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type NodeSyncEvent struct {
	ID            string    `json:"id"`
	NodeID        string    `json:"node_id"`
	ConfigVersion int64     `json:"config_version"`
	Success       bool      `json:"success"`
	Message       string    `json:"message"`
	OccurredAt    time.Time `json:"occurred_at"`
}

type AuditLog struct {
	ID           int64     `json:"id"`
	ActorAdminID string    `json:"actor_admin_id,omitempty"`
	Action       string    `json:"action"`
	TargetType   string    `json:"target_type"`
	TargetID     string    `json:"target_id"`
	Payload      string    `json:"payload"`
	CreatedAt    time.Time `json:"created_at"`
}

type UsageSnapshot struct {
	CredentialUUID string    `json:"credential_uuid"`
	UplinkBytes    int64     `json:"uplink_bytes"`
	DownlinkBytes  int64     `json:"downlink_bytes"`
	CollectedAt    time.Time `json:"collected_at"`
}

type NodeUsageSummary struct {
	NodeID        string `json:"node_id"`
	NodeName      string `json:"node_name"`
	UplinkBytes   int64  `json:"uplink_bytes"`
	DownlinkBytes int64  `json:"downlink_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	SnapshotCount int64  `json:"snapshot_count"`
}

type MemberUsageSummary struct {
	MemberID      string `json:"member_id"`
	MemberName    string `json:"member_name"`
	MemberEmail   string `json:"member_email"`
	UplinkBytes   int64  `json:"uplink_bytes"`
	DownlinkBytes int64  `json:"downlink_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
	SnapshotCount int64  `json:"snapshot_count"`
}

type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

type Alert struct {
	Fingerprint string        `json:"fingerprint"`
	Type        string        `json:"type"`
	Severity    AlertSeverity `json:"severity"`
	Status      string        `json:"status"`
	Title       string        `json:"title"`
	Message     string        `json:"message"`
	TargetType  string        `json:"target_type"`
	TargetID    string        `json:"target_id"`
	FirstSeenAt time.Time     `json:"first_seen_at"`
	LastSeenAt  time.Time     `json:"last_seen_at"`
}
