package store

import (
	"time"

	"v2ray-platform/internal/domain"
)

// UpdateMemberInput carries the fields that can be patched on an existing member.
// Nil pointer fields are left unchanged.
type UpdateMemberInput struct {
	Name            *string
	Email           *string
	Note            *string
	UUID            *string
	TierID          *string // empty string = clear tier
	Status          *domain.MemberStatus
	ExpiresAt       *time.Time // set to non-nil to write; use ClearExpiry to remove
	ClearExpiry     bool
	QuotaBytesLimit *int64
	DisabledReason  *string
}

type CreateTierInput struct {
	Name        string
	Description string
	QuotaBytes  int64
	QuotaType   string // "monthly" or "fixed"; defaults to "monthly"
}

type UpdateTierInput struct {
	Name        *string
	Description *string
	QuotaBytes  *int64
	QuotaType   *string
}

type Store interface {
	EnsureAdmin(email, passwordHash string) (*domain.Admin, error)
	FindAdminByEmail(email string) (*domain.Admin, error)
	CreateAdminSession(adminID string, expiresAt time.Time) (*domain.AdminSession, error)
	GetAdminSession(sessionID string) (*domain.AdminSession, error)
	TouchAdminSession(sessionID string, seenAt time.Time) error
	RevokeAdminSession(sessionID string) error
	RevokeAdminSessions(adminID string) error
	RevokeAllAdminSessions() error
	CreateBootstrapToken(input CreateBootstrapTokenInput) (*domain.BootstrapToken, string, error)
	ListBootstrapTokens() []domain.BootstrapToken
	RegisterNode(input RegisterNodeInput) (*RegisterNodeOutput, error)
	Heartbeat(input HeartbeatInput) (*domain.Node, error)
	RecordSyncResult(input SyncResultInput) error
	GetNodeConfig(nodeToken string) (*domain.ConfigRevision, error)
	GetNodeConfigByID(nodeID string) (*domain.ConfigRevision, error)
	CreateMember(input CreateMemberInput) (*domain.Member, error)
	UpdateMember(memberID string, input UpdateMemberInput) (*domain.Member, error)
	GetMemberBySubscriptionToken(token string) (*domain.Member, error)
	CreateTier(input CreateTierInput) (*domain.Tier, error)
	UpdateTier(tierID string, input UpdateTierInput) (*domain.Tier, error)
	DeleteTier(tierID string) error
	ListTiers() []domain.Tier
	CreateNodeGroup(name, description string) (*domain.NodeGroup, error)
	UpdateNodeGroup(groupID, name, description string) (*domain.NodeGroup, error)
	DeleteNodeGroup(groupID string) error
	ListNodeGroups() []domain.NodeGroup
	SetNodeGroupsForNode(nodeID string, groupIDs []string) error
	ListNodeGroupMemberships() []domain.NodeGroupMembership
	CreateGroupGrant(groupID, memberID string) error
	DeleteGroupGrant(groupID, memberID string) error
	ListGroupGrantViews() []domain.GroupGrantView
	CreateGrant(input CreateGrantInput) (*domain.AccessGrant, *domain.NodeCredential, error)
	RevokeGrant(grantID string) error
	DeleteMember(memberID string) error
	RecordUsage(nodeToken string, snapshots []domain.UsageSnapshot) error
	ListNodes() []domain.Node
	ListMembers() []domain.Member
	ListGrants() []domain.GrantView
	ListNodeSyncEvents(nodeID string) []domain.NodeSyncEvent
	ListNodeUsageSummaries() []domain.NodeUsageSummary
	ListMemberUsageSummaries() []domain.MemberUsageSummary
	GetMemberUsageSince(memberID string, since time.Time) int64
	GetMemberUsageSinceSplit(memberID string, since time.Time) (uplink, downlink int64)
	RebuildNodeConfig(nodeID string) (*domain.ConfigRevision, error)
	RecordAuditLog(actorAdminID, action, targetType, targetID string, payload any) error
	ListAuditLogs() []domain.AuditLog
	Close() error
}
