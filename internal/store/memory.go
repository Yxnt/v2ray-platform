package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/render"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrNotFound     = errors.New("not found")
	ErrConflict     = errors.New("conflict")
)

type RegisterNodeInput struct {
	BootstrapToken string
	Name           string
	Region         string
	PublicHost     string
	Provider       string
	Tags           []string
	RuntimeFlavor  string
}

type HeartbeatInput struct {
	NodeToken            string
	AppliedConfigVersion int64
	PublicHost           string
	Status               string
}

type SyncResultInput struct {
	NodeToken     string
	ConfigVersion int64
	Success       bool
	Message       string
}

type CreateBootstrapTokenInput struct {
	Description string
	TTLHours    int
}

type CreateMemberInput struct {
	Name  string
	Email string
	Note  string
}

type CreateGrantInput struct {
	NodeID   string
	MemberID string
}

type RegisterNodeOutput struct {
	NodeID        string `json:"node_id"`
	NodeToken     string `json:"node_token"`
	ConfigVersion int64  `json:"config_version"`
	Config        string `json:"config"`
}

type MemoryStore struct {
	mu              sync.RWMutex
	admins          map[string]*domain.Admin
	adminSessions   map[string]*domain.AdminSession
	nodes           map[string]*domain.Node
	nodeGroups      map[string]*domain.NodeGroup
	nodeGroupNodes  map[string]map[string]time.Time
	groupGrants     map[string]map[string]time.Time
	members         map[string]*domain.Member
	tiers           map[string]*domain.Tier
	bootstrapTokens map[string]*domain.BootstrapToken
	grants          map[string]*domain.AccessGrant
	credentials     map[string]*domain.NodeCredential
	revisions       map[string]*domain.ConfigRevision
	syncEvents      []*domain.NodeSyncEvent
	auditLogs       []*domain.AuditLog
	usageSnapshots  []memoryUsageSnapshot
}

type memoryUsageSnapshot struct {
	NodeID         string
	MemberID       string
	CredentialUUID string
	UplinkBytes    int64
	DownlinkBytes  int64
	CollectedAt    time.Time
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		admins:          map[string]*domain.Admin{},
		adminSessions:   map[string]*domain.AdminSession{},
		nodes:           map[string]*domain.Node{},
		nodeGroups:      map[string]*domain.NodeGroup{},
		nodeGroupNodes:  map[string]map[string]time.Time{},
		groupGrants:     map[string]map[string]time.Time{},
		members:         map[string]*domain.Member{},
		tiers:           map[string]*domain.Tier{},
		bootstrapTokens: map[string]*domain.BootstrapToken{},
		grants:          map[string]*domain.AccessGrant{},
		credentials:     map[string]*domain.NodeCredential{},
		revisions:       map[string]*domain.ConfigRevision{},
	}
}

func (s *MemoryStore) Close() error {
	return nil
}

func (s *MemoryStore) EnsureAdmin(email, passwordHash string) (*domain.Admin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	email = strings.ToLower(strings.TrimSpace(email))
	for _, admin := range s.admins {
		if admin.Email == email {
			return cloneAdmin(admin), nil
		}
	}
	now := time.Now().UTC()
	admin := &domain.Admin{
		ID:           newID("admin"),
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	s.admins[admin.ID] = admin
	return cloneAdmin(admin), nil
}

func (s *MemoryStore) FindAdminByEmail(email string) (*domain.Admin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	email = strings.ToLower(strings.TrimSpace(email))
	for _, admin := range s.admins {
		if admin.Email == email {
			return cloneAdmin(admin), nil
		}
	}
	return nil, ErrNotFound
}

func (s *MemoryStore) CreateAdminSession(adminID string, expiresAt time.Time) (*domain.AdminSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	session := &domain.AdminSession{
		ID:        newID("sess"),
		AdminID:   adminID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.adminSessions[session.ID] = session
	return cloneAdminSession(session), nil
}

func (s *MemoryStore) GetAdminSession(sessionID string) (*domain.AdminSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.adminSessions[sessionID]
	if !ok {
		return nil, nil
	}
	return cloneAdminSession(session), nil
}

func (s *MemoryStore) TouchAdminSession(sessionID string, seenAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.adminSessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	session.LastSeenAt = &seenAt
	session.UpdatedAt = seenAt
	return nil
}

func (s *MemoryStore) RevokeAdminSession(sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.adminSessions[sessionID]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	session.RevokedAt = &now
	session.UpdatedAt = now
	return nil
}

func (s *MemoryStore) RevokeAdminSessions(adminID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	for _, session := range s.adminSessions {
		if session.AdminID != adminID || session.RevokedAt != nil {
			continue
		}
		session.RevokedAt = &now
		session.UpdatedAt = now
	}
	return nil
}

func (s *MemoryStore) RevokeAllAdminSessions() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	for _, session := range s.adminSessions {
		if session.RevokedAt != nil {
			continue
		}
		session.RevokedAt = &now
		session.UpdatedAt = now
	}
	return nil
}

func (s *MemoryStore) CreateBootstrapToken(input CreateBootstrapTokenInput) (*domain.BootstrapToken, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	plainToken, err := randomToken(24)
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	token := &domain.BootstrapToken{
		ID:          newID("bt"),
		Description: input.Description,
		TokenHash:   sha256Hex(plainToken),
		CreatedAt:   now,
	}
	if input.TTLHours > 0 {
		expiresAt := now.Add(time.Duration(input.TTLHours) * time.Hour)
		token.ExpiresAt = &expiresAt
	}
	s.bootstrapTokens[token.ID] = token
	return cloneBootstrapToken(token), plainToken, nil
}

func (s *MemoryStore) ListBootstrapTokens() []domain.BootstrapToken {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.BootstrapToken, 0, len(s.bootstrapTokens))
	for _, token := range s.bootstrapTokens {
		out = append(out, *cloneBootstrapToken(token))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *MemoryStore) RegisterNode(input RegisterNodeInput) (*RegisterNodeOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	bt, err := s.findBootstrapTokenLocked(input.BootstrapToken)
	if err != nil {
		return nil, err
	}
	if bt.UsedAt != nil {
		return nil, fmt.Errorf("%w: bootstrap token already used", ErrConflict)
	}
	now := time.Now().UTC()
	bt.UsedAt = &now

	nodeToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	node := &domain.Node{
		ID:            newID("node"),
		Name:          input.Name,
		Region:        input.Region,
		PublicHost:    input.PublicHost,
		Provider:      input.Provider,
		Tags:          normalizeTags(input.Tags),
		RuntimeFlavor: firstNonEmpty(input.RuntimeFlavor, "v2ray"),
		Status:        domain.NodeStatusProvisioning,
		NodeTokenHash: sha256Hex(nodeToken),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	s.nodes[node.ID] = node
	revision, err := s.rebuildNodeConfigLocked(node.ID)
	if err != nil {
		return nil, err
	}
	return &RegisterNodeOutput{
		NodeID:        node.ID,
		NodeToken:     nodeToken,
		ConfigVersion: revision.ConfigVersion,
		Config:        revision.Config,
	}, nil
}

func (s *MemoryStore) Heartbeat(input HeartbeatInput) (*domain.Node, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, err := s.findNodeByTokenLocked(input.NodeToken)
	if err != nil {
		return nil, err
	}
	node.LastHeartbeatAt = time.Now().UTC()
	node.UpdatedAt = node.LastHeartbeatAt
	if input.PublicHost != "" {
		node.PublicHost = input.PublicHost
	}
	switch strings.TrimSpace(input.Status) {
	case "degraded":
		node.Status = domain.NodeStatusDegraded
	default:
		node.Status = domain.NodeStatusOnline
	}
	if input.AppliedConfigVersion > node.CurrentConfigVersion {
		node.CurrentConfigVersion = input.AppliedConfigVersion
	}
	return cloneNode(node), nil
}

func (s *MemoryStore) RecordSyncResult(input SyncResultInput) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, err := s.findNodeByTokenLocked(input.NodeToken)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	s.syncEvents = append(s.syncEvents, &domain.NodeSyncEvent{
		ID:            newID("sync"),
		NodeID:        node.ID,
		ConfigVersion: input.ConfigVersion,
		Success:       input.Success,
		Message:       input.Message,
		OccurredAt:    now,
	})
	if input.Success {
		node.CurrentConfigVersion = input.ConfigVersion
		node.Status = domain.NodeStatusOnline
	} else {
		node.Status = domain.NodeStatusDegraded
	}
	node.UpdatedAt = now
	return nil
}

func (s *MemoryStore) GetNodeConfig(nodeToken string) (*domain.ConfigRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, err := s.findNodeByTokenLocked(nodeToken)
	if err != nil {
		return nil, err
	}
	return s.getNodeConfigByIDLocked(node.ID)
}

func (s *MemoryStore) GetNodeConfigByID(nodeID string) (*domain.ConfigRevision, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getNodeConfigByIDLocked(nodeID)
}

func (s *MemoryStore) getNodeConfigByIDLocked(nodeID string) (*domain.ConfigRevision, error) {
	revision, ok := s.revisions[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRevision(revision), nil
}

func (s *MemoryStore) CreateMember(input CreateMemberInput) (*domain.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	member := &domain.Member{
		ID:                newID("member"),
		UUID:              newUUID(),
		Name:              input.Name,
		Email:             strings.ToLower(strings.TrimSpace(input.Email)),
		Note:              input.Note,
		Status:            domain.MemberStatusActive,
		SubscriptionToken: newUUID(),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	s.members[member.ID] = member
	return cloneMember(member), nil
}

func (s *MemoryStore) UpdateMember(memberID string, input UpdateMemberInput) (*domain.Member, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	member, ok := s.members[memberID]
	if !ok {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	if input.Name != nil {
		member.Name = strings.TrimSpace(*input.Name)
	}
	if input.Email != nil {
		member.Email = strings.ToLower(strings.TrimSpace(*input.Email))
	}
	if input.Note != nil {
		member.Note = *input.Note
	}
	if input.UUID != nil {
		member.UUID = *input.UUID
		// Sync all node_credentials for this member.
		for _, cred := range s.credentials {
			if cred.MemberID == memberID {
				cred.UUID = member.UUID
			}
		}
	}
	if input.TierID != nil {
		member.TierID = *input.TierID
	}
	if input.Status != nil {
		member.Status = *input.Status
	}
	if input.ClearExpiry {
		member.ExpiresAt = nil
	} else if input.ExpiresAt != nil {
		t := *input.ExpiresAt
		member.ExpiresAt = &t
	}
	if input.QuotaBytesLimit != nil {
		member.QuotaBytesLimit = *input.QuotaBytesLimit
	}
	if input.DisabledReason != nil {
		member.DisabledReason = *input.DisabledReason
	}
	member.UpdatedAt = now

	// Rebuild config for all nodes this member has grants on.
	affectedNodes := map[string]struct{}{}
	for _, grant := range s.grants {
		if grant.MemberID == memberID {
			affectedNodes[grant.NodeID] = struct{}{}
		}
	}
	for nodeID := range affectedNodes {
		if _, err := s.rebuildNodeConfigLocked(nodeID); err != nil {
			return nil, err
		}
	}
	return cloneMember(member), nil
}

func (s *MemoryStore) CreateNodeGroup(name, description string) (*domain.NodeGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	group := &domain.NodeGroup{
		ID:          newID("group"),
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.nodeGroups[group.ID] = group
	return cloneNodeGroup(group), nil
}

func (s *MemoryStore) UpdateNodeGroup(groupID, name, description string) (*domain.NodeGroup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	group, ok := s.nodeGroups[groupID]
	if !ok {
		return nil, ErrNotFound
	}
	group.Name = strings.TrimSpace(name)
	group.Description = strings.TrimSpace(description)
	group.UpdatedAt = time.Now().UTC()
	return cloneNodeGroup(group), nil
}

func (s *MemoryStore) DeleteNodeGroup(groupID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodeGroups[groupID]; !ok {
		return ErrNotFound
	}
	delete(s.nodeGroups, groupID)
	delete(s.groupGrants, groupID)
	for nodeID, groups := range s.nodeGroupNodes {
		if _, ok := groups[groupID]; ok {
			delete(groups, groupID)
			if len(groups) == 0 {
				delete(s.nodeGroupNodes, nodeID)
			}
			if _, err := s.rebuildNodeConfigLocked(nodeID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MemoryStore) ListNodeGroups() []domain.NodeGroup {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.NodeGroup, 0, len(s.nodeGroups))
	for _, group := range s.nodeGroups {
		out = append(out, *cloneNodeGroup(group))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *MemoryStore) SetNodeGroupsForNode(nodeID string, groupIDs []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[nodeID]; !ok {
		return ErrNotFound
	}
	next := map[string]time.Time{}
	now := time.Now().UTC()
	for _, groupID := range groupIDs {
		if _, ok := s.nodeGroups[groupID]; !ok {
			return ErrNotFound
		}
		next[groupID] = now
	}
	s.nodeGroupNodes[nodeID] = next
	_, err := s.rebuildNodeConfigLocked(nodeID)
	return err
}

func (s *MemoryStore) ListNodeGroupMemberships() []domain.NodeGroupMembership {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.NodeGroupMembership, 0)
	for nodeID, groups := range s.nodeGroupNodes {
		for groupID, createdAt := range groups {
			group := s.nodeGroups[groupID]
			name := ""
			if group != nil {
				name = group.Name
			}
			out = append(out, domain.NodeGroupMembership{
				NodeID:    nodeID,
				GroupID:   groupID,
				GroupName: name,
				CreatedAt: createdAt,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *MemoryStore) CreateGroupGrant(groupID, memberID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodeGroups[groupID]; !ok {
		return ErrNotFound
	}
	if _, ok := s.members[memberID]; !ok {
		return ErrNotFound
	}
	if s.groupGrants[groupID] == nil {
		s.groupGrants[groupID] = map[string]time.Time{}
	}
	s.groupGrants[groupID][memberID] = time.Now().UTC()
	for nodeID, groups := range s.nodeGroupNodes {
		if _, ok := groups[groupID]; ok {
			if _, err := s.rebuildNodeConfigLocked(nodeID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MemoryStore) DeleteGroupGrant(groupID, memberID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	members, ok := s.groupGrants[groupID]
	if !ok {
		return ErrNotFound
	}
	if _, ok := members[memberID]; !ok {
		return ErrNotFound
	}
	delete(members, memberID)
	for nodeID, groups := range s.nodeGroupNodes {
		if _, ok := groups[groupID]; ok {
			if _, err := s.rebuildNodeConfigLocked(nodeID); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *MemoryStore) ListGroupGrantViews() []domain.GroupGrantView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.GroupGrantView, 0)
	for groupID, members := range s.groupGrants {
		group := s.nodeGroups[groupID]
		for memberID, createdAt := range members {
			member := s.members[memberID]
			view := domain.GroupGrantView{
				GroupID:   groupID,
				CreatedAt: createdAt,
			}
			if group != nil {
				view.GroupName = group.Name
			}
			if member != nil {
				view.MemberID = member.ID
				view.MemberName = member.Name
				view.MemberEmail = member.Email
			}
			out = append(out, view)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *MemoryStore) CreateGrant(input CreateGrantInput) (*domain.AccessGrant, *domain.NodeCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.nodes[input.NodeID]; !ok {
		return nil, nil, ErrNotFound
	}
	member, ok := s.members[input.MemberID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	for _, grant := range s.grants {
		if grant.NodeID == input.NodeID && grant.MemberID == input.MemberID {
			return nil, nil, fmt.Errorf("%w: grant already exists", ErrConflict)
		}
	}
	now := time.Now().UTC()
	grant := &domain.AccessGrant{
		ID:        newID("grant"),
		NodeID:    input.NodeID,
		MemberID:  input.MemberID,
		CreatedAt: now,
	}
	cred := &domain.NodeCredential{
		ID:            newID("cred"),
		NodeID:        input.NodeID,
		MemberID:      input.MemberID,
		AccessGrantID: grant.ID,
		UUID:          member.UUID,
		Email:         credentialEmail(member, input.NodeID),
		CreatedAt:     now,
	}
	s.grants[grant.ID] = grant
	s.credentials[cred.ID] = cred
	if _, err := s.rebuildNodeConfigLocked(input.NodeID); err != nil {
		return nil, nil, err
	}
	return cloneGrant(grant), cloneCredential(cred), nil
}

func (s *MemoryStore) RevokeGrant(grantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	grant, ok := s.grants[grantID]
	if !ok {
		return ErrNotFound
	}
	delete(s.grants, grantID)
	for credID, cred := range s.credentials {
		if cred.AccessGrantID == grantID {
			delete(s.credentials, credID)
		}
	}
	_, err := s.rebuildNodeConfigLocked(grant.NodeID)
	return err
}

func (s *MemoryStore) DeleteMember(memberID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.members[memberID]; !ok {
		return ErrNotFound
	}
	delete(s.members, memberID)
	affectedNodes := map[string]struct{}{}
	for grantID, grant := range s.grants {
		if grant.MemberID == memberID {
			affectedNodes[grant.NodeID] = struct{}{}
			delete(s.grants, grantID)
		}
	}
	for credID, cred := range s.credentials {
		if cred.MemberID == memberID {
			affectedNodes[cred.NodeID] = struct{}{}
			delete(s.credentials, credID)
		}
	}
	for nodeID := range affectedNodes {
		if _, err := s.rebuildNodeConfigLocked(nodeID); err != nil {
			return err
		}
	}
	return nil
}

func (s *MemoryStore) RecordUsage(nodeToken string, snapshots []domain.UsageSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, err := s.findNodeByTokenLocked(nodeToken)
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots {
		var memberID string
		for _, cred := range s.credentials {
			if cred.NodeID == node.ID && cred.UUID == snapshot.CredentialUUID {
				memberID = cred.MemberID
				break
			}
		}
		if memberID == "" {
			for groupID := range s.nodeGroupNodes[node.ID] {
				for candidateMemberID := range s.groupGrants[groupID] {
					if m, ok := s.members[candidateMemberID]; ok && m.UUID == snapshot.CredentialUUID {
						memberID = candidateMemberID
						break
					}
				}
				if memberID != "" {
					break
				}
			}
		}
		collectedAt := snapshot.CollectedAt
		if collectedAt.IsZero() {
			collectedAt = time.Now().UTC()
		}
		s.usageSnapshots = append(s.usageSnapshots, memoryUsageSnapshot{
			NodeID:         node.ID,
			MemberID:       memberID,
			CredentialUUID: snapshot.CredentialUUID,
			UplinkBytes:    snapshot.UplinkBytes,
			DownlinkBytes:  snapshot.DownlinkBytes,
			CollectedAt:    collectedAt,
		})
	}
	return nil
}

func (s *MemoryStore) ListNodes() []domain.Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		out = append(out, *cloneNode(node))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *MemoryStore) ListMembers() []domain.Member {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.Member, 0, len(s.members))
	for _, member := range s.members {
		out = append(out, *cloneMember(member))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *MemoryStore) ListGrants() []domain.GrantView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.GrantView, 0, len(s.grants))
	for _, grant := range s.grants {
		node := s.nodes[grant.NodeID]
		member := s.members[grant.MemberID]
		view := domain.GrantView{
			ID:        grant.ID,
			NodeID:    grant.NodeID,
			MemberID:  grant.MemberID,
			CreatedAt: grant.CreatedAt,
		}
		if node != nil {
			view.NodeName = node.Name
		}
		if member != nil {
			view.MemberName = member.Name
			view.MemberEmail = member.Email
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *MemoryStore) ListNodeSyncEvents(nodeID string) []domain.NodeSyncEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var out []domain.NodeSyncEvent
	for _, event := range s.syncEvents {
		if nodeID == "" || event.NodeID == nodeID {
			out = append(out, *event)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OccurredAt.After(out[j].OccurredAt)
	})
	return out
}

func (s *MemoryStore) ListNodeUsageSummaries() []domain.NodeUsageSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summaries := map[string]*domain.NodeUsageSummary{}
	for _, snapshot := range s.usageSnapshots {
		summary, ok := summaries[snapshot.NodeID]
		if !ok {
			summary = &domain.NodeUsageSummary{
				NodeID: snapshot.NodeID,
			}
			if node := s.nodes[snapshot.NodeID]; node != nil {
				summary.NodeName = node.Name
			}
			summaries[snapshot.NodeID] = summary
		}
		summary.UplinkBytes += snapshot.UplinkBytes
		summary.DownlinkBytes += snapshot.DownlinkBytes
		summary.TotalBytes += snapshot.UplinkBytes + snapshot.DownlinkBytes
		summary.SnapshotCount++
	}
	out := make([]domain.NodeUsageSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeName < out[j].NodeName })
	return out
}

func (s *MemoryStore) ListMemberUsageSummaries() []domain.MemberUsageSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summaries := map[string]*domain.MemberUsageSummary{}
	for _, snapshot := range s.usageSnapshots {
		if snapshot.MemberID == "" {
			continue
		}
		summary, ok := summaries[snapshot.MemberID]
		if !ok {
			summary = &domain.MemberUsageSummary{
				MemberID: snapshot.MemberID,
			}
			if member := s.members[snapshot.MemberID]; member != nil {
				summary.MemberName = member.Name
				summary.MemberEmail = member.Email
			}
			summaries[snapshot.MemberID] = summary
		}
		summary.UplinkBytes += snapshot.UplinkBytes
		summary.DownlinkBytes += snapshot.DownlinkBytes
		summary.TotalBytes += snapshot.UplinkBytes + snapshot.DownlinkBytes
		summary.SnapshotCount++
	}
	out := make([]domain.MemberUsageSummary, 0, len(summaries))
	for _, summary := range summaries {
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MemberEmail < out[j].MemberEmail })
	return out
}

func (s *MemoryStore) GetMemberUsageSince(memberID string, since time.Time) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total int64
	for _, snap := range s.usageSnapshots {
		if snap.MemberID == memberID && !snap.CollectedAt.Before(since) {
			total += snap.UplinkBytes + snap.DownlinkBytes
		}
	}
	return total
}

func (s *MemoryStore) RebuildNodeConfig(nodeID string) (*domain.ConfigRevision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.rebuildNodeConfigLocked(nodeID)
}

func (s *MemoryStore) RecordAuditLog(actorAdminID, action, targetType, targetID string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.auditLogs = append(s.auditLogs, &domain.AuditLog{
		ID:           int64(len(s.auditLogs) + 1),
		ActorAdminID: actorAdminID,
		Action:       action,
		TargetType:   targetType,
		TargetID:     targetID,
		Payload:      marshalAuditPayload(payload),
		CreatedAt:    time.Now().UTC(),
	})
	return nil
}

func (s *MemoryStore) ListAuditLogs() []domain.AuditLog {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]domain.AuditLog, 0, len(s.auditLogs))
	for _, log := range s.auditLogs {
		out = append(out, *log)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *MemoryStore) rebuildNodeConfigLocked(nodeID string) (*domain.ConfigRevision, error) {
	node, ok := s.nodes[nodeID]
	if !ok {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	creds := make([]domain.NodeCredential, 0)
	seenMembers := map[string]struct{}{}
	for _, cred := range s.credentials {
		if cred.NodeID != nodeID {
			continue
		}
		// Skip credentials for inactive or expired members.
		if m, ok := s.members[cred.MemberID]; ok {
			if m.Status != domain.MemberStatusActive {
				continue
			}
			if m.ExpiresAt != nil && m.ExpiresAt.Before(now) {
				continue
			}
		}
		creds = append(creds, *cloneCredential(cred))
		seenMembers[cred.MemberID] = struct{}{}
	}
	for groupID := range s.nodeGroupNodes[nodeID] {
		for memberID := range s.groupGrants[groupID] {
			if _, ok := seenMembers[memberID]; ok {
				continue
			}
			member, ok := s.members[memberID]
			if !ok {
				continue
			}
			if member.Status != domain.MemberStatusActive {
				continue
			}
			if member.ExpiresAt != nil && member.ExpiresAt.Before(now) {
				continue
			}
			creds = append(creds, domain.NodeCredential{
				ID:            "derived-" + groupID + "-" + memberID,
				NodeID:        nodeID,
				MemberID:      memberID,
				AccessGrantID: "group:" + groupID,
				UUID:          member.UUID,
				Email:         credentialEmail(member, nodeID),
				CreatedAt:     now,
			})
			seenMembers[memberID] = struct{}{}
		}
	}
	sort.Slice(creds, func(i, j int) bool {
		return creds[i].CreatedAt.Before(creds[j].CreatedAt)
	})
	configText, err := render.RenderNodeConfig(*node, creds)
	if err != nil {
		return nil, err
	}
	revision, ok := s.revisions[nodeID]
	if !ok {
		revision = &domain.ConfigRevision{NodeID: nodeID}
	}
	revision.ConfigVersion++
	revision.Config = configText
	revision.UpdatedAt = now
	s.revisions[nodeID] = revision
	node.UpdatedAt = now
	return cloneRevision(revision), nil
}

func (s *MemoryStore) findBootstrapTokenLocked(plainToken string) (*domain.BootstrapToken, error) {
	tokenHash := sha256Hex(strings.TrimSpace(plainToken))
	now := time.Now().UTC()
	for _, token := range s.bootstrapTokens {
		if token.TokenHash != tokenHash {
			continue
		}
		if token.ExpiresAt != nil && token.ExpiresAt.Before(now) {
			return nil, fmt.Errorf("%w: bootstrap token expired", ErrUnauthorized)
		}
		return token, nil
	}
	return nil, ErrUnauthorized
}

func (s *MemoryStore) findNodeByTokenLocked(plainToken string) (*domain.Node, error) {
	tokenHash := sha256Hex(strings.TrimSpace(plainToken))
	for _, node := range s.nodes {
		if node.NodeTokenHash == tokenHash {
			return node, nil
		}
	}
	return nil, ErrUnauthorized
}

// ── Tier CRUD ──────────────────────────────────────────────────────────────────

func (s *MemoryStore) CreateTier(input CreateTierInput) (*domain.Tier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	quotaType := strings.TrimSpace(input.QuotaType)
	if quotaType != "monthly" && quotaType != "fixed" {
		quotaType = "monthly"
	}
	tier := &domain.Tier{
		ID:          newUUID(),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		QuotaBytes:  input.QuotaBytes,
		QuotaType:   quotaType,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.tiers[tier.ID] = tier
	return tier, nil
}

func (s *MemoryStore) UpdateTier(tierID string, input UpdateTierInput) (*domain.Tier, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tier, ok := s.tiers[tierID]
	if !ok {
		return nil, ErrNotFound
	}
	if input.Name != nil {
		tier.Name = strings.TrimSpace(*input.Name)
	}
	if input.Description != nil {
		tier.Description = strings.TrimSpace(*input.Description)
	}
	if input.QuotaBytes != nil {
		tier.QuotaBytes = *input.QuotaBytes
	}
	if input.QuotaType != nil {
		qt := strings.TrimSpace(*input.QuotaType)
		if qt == "monthly" || qt == "fixed" {
			tier.QuotaType = qt
		}
	}
	tier.UpdatedAt = time.Now().UTC()
	return tier, nil
}

func (s *MemoryStore) DeleteTier(tierID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tiers[tierID]; !ok {
		return ErrNotFound
	}
	delete(s.tiers, tierID)
	return nil
}

func (s *MemoryStore) ListTiers() []domain.Tier {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]domain.Tier, 0, len(s.tiers))
	for _, t := range s.tiers {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *MemoryStore) GetMemberBySubscriptionToken(token string) (*domain.Member, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, m := range s.members {
		if m.SubscriptionToken == token {
			return cloneMember(m), nil
		}
	}
	return nil, ErrNotFound
}
