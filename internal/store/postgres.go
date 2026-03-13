package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
	_ "github.com/lib/pq"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/render"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(databaseURL string, maxOpenConns, maxIdleConns int, connMaxLifetime time.Duration) (*PostgresStore, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	if maxOpenConns > 0 {
		db.SetMaxOpenConns(maxOpenConns)
	}
	if maxIdleConns > 0 {
		db.SetMaxIdleConns(maxIdleConns)
	}
	if connMaxLifetime > 0 {
		db.SetConnMaxLifetime(connMaxLifetime)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) DB() *sql.DB {
	return s.db
}

func (s *PostgresStore) EnsureAdmin(email, passwordHash string) (*domain.Admin, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	admin, err := s.FindAdminByEmail(email)
	if err == nil {
		return admin, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	now := time.Now().UTC()
	admin = &domain.Admin{
		ID:           newID("admin"),
		Email:        email,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err = s.db.Exec(
		`INSERT INTO admins (id, email, password_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		admin.ID, admin.Email, admin.PasswordHash, admin.CreatedAt, admin.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	return cloneAdmin(admin), nil
}

func (s *PostgresStore) FindAdminByEmail(email string) (*domain.Admin, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	var admin domain.Admin
	err := s.db.QueryRow(
		`SELECT id, email, password_hash, created_at, updated_at
		 FROM admins
		 WHERE email = $1`,
		email,
	).Scan(&admin.ID, &admin.Email, &admin.PasswordHash, &admin.CreatedAt, &admin.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapPQError(err)
	}
	return &admin, nil
}

func (s *PostgresStore) CreateAdminSession(adminID string, expiresAt time.Time) (*domain.AdminSession, error) {
	now := time.Now().UTC()
	session := &domain.AdminSession{
		ID:        newID("sess"),
		AdminID:   adminID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.Exec(
		`INSERT INTO admin_sessions (id, admin_id, expires_at, revoked_at, last_seen_at, created_at, updated_at)
		 VALUES ($1, $2, $3, NULL, NULL, $4, $5)`,
		session.ID, session.AdminID, session.ExpiresAt, session.CreatedAt, session.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	return cloneAdminSession(session), nil
}

func (s *PostgresStore) GetAdminSession(sessionID string) (*domain.AdminSession, error) {
	var session domain.AdminSession
	var revokedAt sql.NullTime
	var lastSeenAt sql.NullTime
	err := s.db.QueryRow(
		`SELECT id, admin_id, expires_at, revoked_at, last_seen_at, created_at, updated_at
		 FROM admin_sessions
		 WHERE id = $1`,
		sessionID,
	).Scan(&session.ID, &session.AdminID, &session.ExpiresAt, &revokedAt, &lastSeenAt, &session.CreatedAt, &session.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapPQError(err)
	}
	if revokedAt.Valid {
		session.RevokedAt = &revokedAt.Time
	}
	if lastSeenAt.Valid {
		session.LastSeenAt = &lastSeenAt.Time
	}
	return &session, nil
}

func (s *PostgresStore) TouchAdminSession(sessionID string, seenAt time.Time) error {
	res, err := s.db.Exec(
		`UPDATE admin_sessions
		 SET last_seen_at = $2, updated_at = $2
		 WHERE id = $1`,
		sessionID, seenAt,
	)
	if err != nil {
		return mapPQError(err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) RevokeAdminSession(sessionID string) error {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE admin_sessions
		 SET revoked_at = COALESCE(revoked_at, $2), updated_at = $2
		 WHERE id = $1`,
		sessionID, now,
	)
	if err != nil {
		return mapPQError(err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) RevokeAdminSessions(adminID string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE admin_sessions
		 SET revoked_at = COALESCE(revoked_at, $2), updated_at = $2
		 WHERE admin_id = $1`,
		adminID, now,
	)
	return mapPQError(err)
}

func (s *PostgresStore) RevokeAllAdminSessions() error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE admin_sessions
		 SET revoked_at = COALESCE(revoked_at, $1), updated_at = $1`,
		now,
	)
	return mapPQError(err)
}

func (s *PostgresStore) CreateBootstrapToken(input CreateBootstrapTokenInput) (*domain.BootstrapToken, string, error) {
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
	_, err = s.db.Exec(
		`INSERT INTO bootstrap_tokens (id, description, token_hash, expires_at, used_at, created_at)
		 VALUES ($1, $2, $3, $4, NULL, $5)`,
		token.ID, token.Description, token.TokenHash, token.ExpiresAt, token.CreatedAt,
	)
	if err != nil {
		return nil, "", mapPQError(err)
	}
	return cloneBootstrapToken(token), plainToken, nil
}

func (s *PostgresStore) ListBootstrapTokens() []domain.BootstrapToken {
	rows, err := s.db.Query(
		`SELECT id, description, token_hash, expires_at, used_at, created_at
		 FROM bootstrap_tokens
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return []domain.BootstrapToken{}
	}
	defer rows.Close()
	out := make([]domain.BootstrapToken, 0)
	for rows.Next() {
		var item domain.BootstrapToken
		var expiresAt sql.NullTime
		var usedAt sql.NullTime
		if err := rows.Scan(&item.ID, &item.Description, &item.TokenHash, &expiresAt, &usedAt, &item.CreatedAt); err == nil {
			if expiresAt.Valid {
				item.ExpiresAt = &expiresAt.Time
			}
			if usedAt.Valid {
				item.UsedAt = &usedAt.Time
			}
			out = append(out, item)
		}
	}
	return out
}

func (s *PostgresStore) RegisterNode(input RegisterNodeInput) (*RegisterNodeOutput, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	tokenHash := sha256Hex(strings.TrimSpace(input.BootstrapToken))
	now := time.Now().UTC()
	res, err := tx.Exec(
		`UPDATE bootstrap_tokens
		 SET used_at = $2
		 WHERE token_hash = $1
		   AND used_at IS NULL
		   AND (expires_at IS NULL OR expires_at >= $2)`,
		tokenHash, now,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, fmt.Errorf("%w: invalid, expired, or already-used bootstrap token", ErrUnauthorized)
	}

	nodeToken, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	node := domain.Node{
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
	tagsJSON, err := json.Marshal(node.Tags)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(
		`INSERT INTO nodes
		 (id, name, region, public_host, provider, tags, runtime_flavor, status, last_heartbeat_at, current_config_version, node_token_hash, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, NULL, 0, $9, $10, $11)`,
		node.ID, node.Name, node.Region, node.PublicHost, node.Provider, string(tagsJSON), node.RuntimeFlavor, node.Status, node.NodeTokenHash, node.CreatedAt, node.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}

	revision, err := s.rebuildNodeConfigTx(tx, node.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &RegisterNodeOutput{
		NodeID:        node.ID,
		NodeToken:     nodeToken,
		ConfigVersion: revision.ConfigVersion,
		Config:        revision.Config,
	}, nil
}

func (s *PostgresStore) Heartbeat(input HeartbeatInput) (*domain.Node, error) {
	now := time.Now().UTC()
	node, err := s.findNodeByToken(input.NodeToken)
	if err != nil {
		return nil, err
	}
	status := domain.NodeStatusOnline
	if strings.TrimSpace(input.Status) == "degraded" {
		status = domain.NodeStatusDegraded
	}
	if input.PublicHost != "" {
		node.PublicHost = input.PublicHost
	}
	node.LastHeartbeatAt = now
	node.UpdatedAt = now
	node.Status = status
	if input.AppliedConfigVersion > node.CurrentConfigVersion {
		node.CurrentConfigVersion = input.AppliedConfigVersion
	}
	_, err = s.db.Exec(
		`UPDATE nodes
		 SET public_host = $2,
		     status = $3,
		     last_heartbeat_at = $4,
		     current_config_version = $5,
		     updated_at = $6
		 WHERE id = $1`,
		node.ID, node.PublicHost, node.Status, node.LastHeartbeatAt, node.CurrentConfigVersion, node.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	return cloneNode(node), nil
}

func (s *PostgresStore) RecordSyncResult(input SyncResultInput) error {
	node, err := s.findNodeByToken(input.NodeToken)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	status := domain.NodeStatusDegraded
	if input.Success {
		status = domain.NodeStatusOnline
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(
		`INSERT INTO node_sync_events (id, node_id, config_version, success, message, occurred_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		newID("sync"), node.ID, input.ConfigVersion, input.Success, input.Message, now,
	)
	if err != nil {
		return mapPQError(err)
	}
	_, err = tx.Exec(
		`UPDATE nodes
		 SET status = $2,
		     current_config_version = GREATEST(current_config_version, $3),
		     updated_at = $4
		 WHERE id = $1`,
		node.ID, status, input.ConfigVersion, now,
	)
	if err != nil {
		return mapPQError(err)
	}
	return tx.Commit()
}

func (s *PostgresStore) GetNodeConfig(nodeToken string) (*domain.ConfigRevision, error) {
	node, err := s.findNodeByToken(nodeToken)
	if err != nil {
		return nil, err
	}
	return s.GetNodeConfigByID(node.ID)
}

func (s *PostgresStore) GetNodeConfigByID(nodeID string) (*domain.ConfigRevision, error) {
	var rev domain.ConfigRevision
	err := s.db.QueryRow(
		`SELECT node_id, config_version, config_json::text, created_at
		 FROM node_config_revisions
		 WHERE node_id = $1
		 ORDER BY config_version DESC
		 LIMIT 1`,
		nodeID,
	).Scan(&rev.NodeID, &rev.ConfigVersion, &rev.Config, &rev.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapPQError(err)
	}
	return &rev, nil
}

func (s *PostgresStore) CreateMember(input CreateMemberInput) (*domain.Member, error) {
	now := time.Now().UTC()
	member := &domain.Member{
		ID:        newID("member"),
		UUID:      newUUID(),
		Name:      input.Name,
		Email:     strings.ToLower(strings.TrimSpace(input.Email)),
		Note:      input.Note,
		Status:    domain.MemberStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}
	// subscription_token defaults to gen_random_uuid() in DB; read it back.
	err := s.db.QueryRow(
		`INSERT INTO members (id, uuid, name, email, note, status, quota_bytes_limit, disabled_reason, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 RETURNING subscription_token`,
		member.ID, member.UUID, member.Name, member.Email, member.Note,
		member.Status, member.QuotaBytesLimit, member.DisabledReason,
		member.CreatedAt, member.UpdatedAt,
	).Scan(&member.SubscriptionToken)
	if err != nil {
		return nil, mapPQError(err)
	}
	return cloneMember(member), nil
}

func (s *PostgresStore) UpdateMember(memberID string, input UpdateMemberInput) (*domain.Member, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	member, err := s.getMemberByIDTx(tx, memberID)
	if err != nil {
		return nil, err
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
	var tierIDParam interface{}
	if member.TierID != "" {
		tierIDParam = member.TierID
	}
	_, err = tx.Exec(
		`UPDATE members
		 SET name = $2, email = $3, note = $4, uuid = $5, tier_id = $6, status = $7, expires_at = $8, quota_bytes_limit = $9, disabled_reason = $10, updated_at = $11
		 WHERE id = $1`,
		member.ID, member.Name, member.Email, member.Note, member.UUID, tierIDParam,
		member.Status, member.ExpiresAt, member.QuotaBytesLimit, member.DisabledReason, member.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	// If UUID changed, sync all existing node_credentials for this member.
	if input.UUID != nil {
		if _, err = tx.Exec(
			`UPDATE node_credentials SET credential_uuid = $1 WHERE member_id = $2`,
			member.UUID, member.ID,
		); err != nil {
			return nil, mapPQError(err)
		}
	}
	// Rebuild config for all nodes this member has grants on.
	affectedRows, err := tx.Query(`SELECT DISTINCT node_id FROM member_access_grants WHERE member_id = $1`, memberID)
	if err != nil {
		return nil, mapPQError(err)
	}
	affectedNodes := make([]string, 0)
	for affectedRows.Next() {
		var nodeID string
		if err := affectedRows.Scan(&nodeID); err != nil {
			affectedRows.Close()
			return nil, mapPQError(err)
		}
		affectedNodes = append(affectedNodes, nodeID)
	}
	affectedRows.Close()
	for _, nodeID := range affectedNodes {
		if _, err := s.rebuildNodeConfigTx(tx, nodeID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return cloneMember(member), nil
}

func (s *PostgresStore) CreateNodeGroup(name, description string) (*domain.NodeGroup, error) {
	now := time.Now().UTC()
	group := &domain.NodeGroup{
		ID:          newID("group"),
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(
		`INSERT INTO node_groups (id, name, description, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		group.ID, group.Name, group.Description, group.CreatedAt, group.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	return cloneNodeGroup(group), nil
}

func (s *PostgresStore) UpdateNodeGroup(groupID, name, description string) (*domain.NodeGroup, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE node_groups
		 SET name = $2, description = $3, updated_at = $4
		 WHERE id = $1`,
		groupID, strings.TrimSpace(name), strings.TrimSpace(description), now,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rows == 0 {
		return nil, ErrNotFound
	}
	return s.getNodeGroup(groupID)
}

func (s *PostgresStore) DeleteNodeGroup(groupID string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT DISTINCT node_id FROM node_group_memberships WHERE group_id = $1`, groupID)
	if err != nil {
		return mapPQError(err)
	}
	var affected []string
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			rows.Close()
			return mapPQError(err)
		}
		affected = append(affected, nodeID)
	}
	rows.Close()
	res, err := tx.Exec(`DELETE FROM node_groups WHERE id = $1`, groupID)
	if err != nil {
		return mapPQError(err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	for _, nodeID := range affected {
		if _, err := s.rebuildNodeConfigTx(tx, nodeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) ListNodeGroups() []domain.NodeGroup {
	rows, err := s.db.Query(
		`SELECT id, name, description, created_at, updated_at
		 FROM node_groups
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return []domain.NodeGroup{}
	}
	defer rows.Close()
	out := make([]domain.NodeGroup, 0)
	for rows.Next() {
		var group domain.NodeGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.CreatedAt, &group.UpdatedAt); err == nil {
			out = append(out, group)
		}
	}
	return out
}

func (s *PostgresStore) SetNodeGroupsForNode(nodeID string, groupIDs []string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := s.getNodeByIDTx(tx, nodeID); err != nil {
		return err
	}
	for _, groupID := range groupIDs {
		if _, err := s.getNodeGroupTx(tx, groupID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM node_group_memberships WHERE node_id = $1`, nodeID); err != nil {
		return mapPQError(err)
	}
	now := time.Now().UTC()
	for _, groupID := range groupIDs {
		if _, err := tx.Exec(
			`INSERT INTO node_group_memberships (node_id, group_id, created_at)
			 VALUES ($1, $2, $3)`,
			nodeID, groupID, now,
		); err != nil {
			return mapPQError(err)
		}
	}
	if _, err := s.rebuildNodeConfigTx(tx, nodeID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) ListNodeGroupMemberships() []domain.NodeGroupMembership {
	rows, err := s.db.Query(
		`SELECT m.node_id, m.group_id, g.name, m.created_at
		 FROM node_group_memberships m
		 JOIN node_groups g ON g.id = m.group_id
		 ORDER BY m.created_at ASC`,
	)
	if err != nil {
		return []domain.NodeGroupMembership{}
	}
	defer rows.Close()
	out := make([]domain.NodeGroupMembership, 0)
	for rows.Next() {
		var item domain.NodeGroupMembership
		if err := rows.Scan(&item.NodeID, &item.GroupID, &item.GroupName, &item.CreatedAt); err == nil {
			out = append(out, item)
		}
	}
	return out
}

func (s *PostgresStore) CreateGroupGrant(groupID, memberID string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := s.getNodeGroupTx(tx, groupID); err != nil {
		return err
	}
	if _, err := s.getMemberByIDTx(tx, memberID); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO member_node_group_grants (group_id, member_id, created_at)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (group_id, member_id) DO NOTHING`,
		groupID, memberID, time.Now().UTC(),
	); err != nil {
		return mapPQError(err)
	}
	rows, err := tx.Query(`SELECT DISTINCT node_id FROM node_group_memberships WHERE group_id = $1`, groupID)
	if err != nil {
		return mapPQError(err)
	}
	var affected []string
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			rows.Close()
			return mapPQError(err)
		}
		affected = append(affected, nodeID)
	}
	rows.Close()
	for _, nodeID := range affected {
		if _, err := s.rebuildNodeConfigTx(tx, nodeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) DeleteGroupGrant(groupID, memberID string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`DELETE FROM member_node_group_grants WHERE group_id = $1 AND member_id = $2`, groupID, memberID)
	if err != nil {
		return mapPQError(err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	rows, err := tx.Query(`SELECT DISTINCT node_id FROM node_group_memberships WHERE group_id = $1`, groupID)
	if err != nil {
		return mapPQError(err)
	}
	var affected []string
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			rows.Close()
			return mapPQError(err)
		}
		affected = append(affected, nodeID)
	}
	rows.Close()
	for _, nodeID := range affected {
		if _, err := s.rebuildNodeConfigTx(tx, nodeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) ListGroupGrantViews() []domain.GroupGrantView {
	rows, err := s.db.Query(
		`SELECT g.group_id, ng.name, g.member_id, m.name, m.email, g.created_at
		 FROM member_node_group_grants g
		 JOIN node_groups ng ON ng.id = g.group_id
		 JOIN members m ON m.id = g.member_id
		 ORDER BY g.created_at ASC`,
	)
	if err != nil {
		return []domain.GroupGrantView{}
	}
	defer rows.Close()
	out := make([]domain.GroupGrantView, 0)
	for rows.Next() {
		var item domain.GroupGrantView
		if err := rows.Scan(&item.GroupID, &item.GroupName, &item.MemberID, &item.MemberName, &item.MemberEmail, &item.CreatedAt); err == nil {
			out = append(out, item)
		}
	}
	return out
}

func (s *PostgresStore) CreateGrant(input CreateGrantInput) (*domain.AccessGrant, *domain.NodeCredential, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	node, err := s.getNodeByIDTx(tx, input.NodeID)
	if err != nil {
		return nil, nil, err
	}
	member, err := s.getMemberByIDTx(tx, input.MemberID)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	grant := &domain.AccessGrant{
		ID:        newID("grant"),
		NodeID:    input.NodeID,
		MemberID:  input.MemberID,
		CreatedAt: now,
	}
	_, err = tx.Exec(
		`INSERT INTO member_access_grants (id, node_id, member_id, created_at)
		 VALUES ($1, $2, $3, $4)`,
		grant.ID, grant.NodeID, grant.MemberID, grant.CreatedAt,
	)
	if err != nil {
		return nil, nil, mapPQError(err)
	}
	cred := &domain.NodeCredential{
		ID:            newID("cred"),
		NodeID:        node.ID,
		MemberID:      member.ID,
		AccessGrantID: grant.ID,
		UUID:          member.UUID,
		Email:         credentialEmail(member, node.ID),
		CreatedAt:     now,
	}
	_, err = tx.Exec(
		`INSERT INTO node_credentials (id, node_id, member_id, access_grant_id, credential_uuid, email, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		cred.ID, cred.NodeID, cred.MemberID, cred.AccessGrantID, cred.UUID, cred.Email, cred.CreatedAt,
	)
	if err != nil {
		return nil, nil, mapPQError(err)
	}
	if _, err := s.rebuildNodeConfigTx(tx, node.ID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return cloneGrant(grant), cloneCredential(cred), nil
}

func (s *PostgresStore) RevokeGrant(grantID string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var nodeID string
	err = tx.QueryRow(`SELECT node_id FROM member_access_grants WHERE id = $1`, grantID).Scan(&nodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return mapPQError(err)
	}
	if _, err := tx.Exec(`DELETE FROM member_access_grants WHERE id = $1`, grantID); err != nil {
		return mapPQError(err)
	}
	if _, err := s.rebuildNodeConfigTx(tx, nodeID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) DeleteMember(memberID string) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT DISTINCT node_id FROM member_access_grants WHERE member_id = $1`, memberID)
	if err != nil {
		return mapPQError(err)
	}
	affectedNodes := make([]string, 0)
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			rows.Close()
			return mapPQError(err)
		}
		affectedNodes = append(affectedNodes, nodeID)
	}
	rows.Close()
	res, err := tx.Exec(`DELETE FROM members WHERE id = $1`, memberID)
	if err != nil {
		return mapPQError(err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	for _, nodeID := range affectedNodes {
		if _, err := s.rebuildNodeConfigTx(tx, nodeID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) RecordUsage(nodeToken string, snapshots []domain.UsageSnapshot) error {
	node, err := s.findNodeByToken(nodeToken)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, snapshot := range snapshots {
		var memberID any
		var resolvedMemberID string
		err := tx.QueryRow(
			`SELECT member_id
			 FROM node_credentials
			 WHERE node_id = $1 AND credential_uuid = $2`,
			node.ID, snapshot.CredentialUUID,
		).Scan(&resolvedMemberID)
		if errors.Is(err, sql.ErrNoRows) {
			resolvedMemberID = ""
		} else if err != nil {
			return mapPQError(err)
		}
		if resolvedMemberID == "" {
			// Fall back: look up by member UUID for group-access credentials.
			err2 := tx.QueryRow(
				`SELECT mg.member_id
				 FROM node_group_memberships ngm
				 JOIN member_node_group_grants mg ON mg.group_id = ngm.group_id
				 JOIN members m ON m.id = mg.member_id
				 WHERE ngm.node_id = $1 AND m.uuid = $2
				 LIMIT 1`,
				node.ID, snapshot.CredentialUUID,
			).Scan(&resolvedMemberID)
			if err2 != nil && !errors.Is(err2, sql.ErrNoRows) {
				return mapPQError(err2)
			}
		}
		if resolvedMemberID != "" {
			memberID = resolvedMemberID
		}
		collectedAt := snapshot.CollectedAt
		if collectedAt.IsZero() {
			collectedAt = time.Now().UTC()
		}
		if _, err := tx.Exec(
			`INSERT INTO usage_snapshots (node_id, member_id, credential_uuid, uplink_bytes, downlink_bytes, collected_at)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			node.ID, memberID, snapshot.CredentialUUID, snapshot.UplinkBytes, snapshot.DownlinkBytes, collectedAt,
		); err != nil {
			return mapPQError(err)
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) ListNodes() []domain.Node {
	rows, err := s.db.Query(
		`SELECT id, name, region, public_host, provider, tags::text, runtime_flavor, status, last_heartbeat_at, current_config_version, node_token_hash, created_at, updated_at
		 FROM nodes
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return []domain.Node{}
	}
	defer rows.Close()
	out := make([]domain.Node, 0)
	for rows.Next() {
		node, err := scanNode(rows)
		if err == nil {
			out = append(out, *node)
		}
	}
	return out
}

func (s *PostgresStore) ListMembers() []domain.Member {
	rows, err := s.db.Query(
		`SELECT id, uuid, name, email, note, status, expires_at, quota_bytes_limit, tier_id, subscription_token, disabled_reason, created_at, updated_at
		 FROM members
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return []domain.Member{}
	}
	defer rows.Close()
	out := make([]domain.Member, 0)
	for rows.Next() {
		member := scanMember(rows)
		if member != nil {
			out = append(out, *member)
		}
	}
	return out
}

func (s *PostgresStore) ListGrants() []domain.GrantView {
	rows, err := s.db.Query(
		`SELECT g.id, g.node_id, n.name, g.member_id, m.name, m.email, g.created_at
		 FROM member_access_grants g
		 JOIN nodes n ON n.id = g.node_id
		 JOIN members m ON m.id = g.member_id
		 ORDER BY g.created_at ASC`,
	)
	if err != nil {
		return []domain.GrantView{}
	}
	defer rows.Close()
	out := make([]domain.GrantView, 0)
	for rows.Next() {
		var grant domain.GrantView
		if err := rows.Scan(&grant.ID, &grant.NodeID, &grant.NodeName, &grant.MemberID, &grant.MemberName, &grant.MemberEmail, &grant.CreatedAt); err == nil {
			out = append(out, grant)
		}
	}
	return out
}

func (s *PostgresStore) ListNodeSyncEvents(nodeID string) []domain.NodeSyncEvent {
	query := `SELECT id, node_id, config_version, success, message, occurred_at
	          FROM node_sync_events`
	args := []any{}
	if nodeID != "" {
		query += ` WHERE node_id = $1`
		args = append(args, nodeID)
	}
	query += ` ORDER BY occurred_at DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return []domain.NodeSyncEvent{}
	}
	defer rows.Close()
	out := make([]domain.NodeSyncEvent, 0)
	for rows.Next() {
		var event domain.NodeSyncEvent
		if err := rows.Scan(&event.ID, &event.NodeID, &event.ConfigVersion, &event.Success, &event.Message, &event.OccurredAt); err == nil {
			out = append(out, event)
		}
	}
	return out
}

func (s *PostgresStore) ListNodeUsageSummaries() []domain.NodeUsageSummary {
	rows, err := s.db.Query(
		`SELECT n.id, n.name, COALESCE(SUM(u.uplink_bytes), 0), COALESCE(SUM(u.downlink_bytes), 0), COALESCE(COUNT(u.id), 0)
		 FROM nodes n
		 LEFT JOIN usage_snapshots u ON u.node_id = n.id
		 GROUP BY n.id, n.name
		 ORDER BY n.name ASC`,
	)
	if err != nil {
		return []domain.NodeUsageSummary{}
	}
	defer rows.Close()
	out := make([]domain.NodeUsageSummary, 0)
	for rows.Next() {
		var summary domain.NodeUsageSummary
		if err := rows.Scan(&summary.NodeID, &summary.NodeName, &summary.UplinkBytes, &summary.DownlinkBytes, &summary.SnapshotCount); err == nil {
			summary.TotalBytes = summary.UplinkBytes + summary.DownlinkBytes
			out = append(out, summary)
		}
	}
	return out
}

func (s *PostgresStore) ListMemberUsageSummaries() []domain.MemberUsageSummary {
	rows, err := s.db.Query(
		`SELECT m.id, m.name, m.email, COALESCE(SUM(u.uplink_bytes), 0), COALESCE(SUM(u.downlink_bytes), 0), COALESCE(COUNT(u.id), 0)
		 FROM members m
		 LEFT JOIN usage_snapshots u ON u.member_id = m.id
		 GROUP BY m.id, m.name, m.email
		 ORDER BY m.email ASC`,
	)
	if err != nil {
		return []domain.MemberUsageSummary{}
	}
	defer rows.Close()
	out := make([]domain.MemberUsageSummary, 0)
	for rows.Next() {
		var summary domain.MemberUsageSummary
		if err := rows.Scan(&summary.MemberID, &summary.MemberName, &summary.MemberEmail, &summary.UplinkBytes, &summary.DownlinkBytes, &summary.SnapshotCount); err == nil {
			summary.TotalBytes = summary.UplinkBytes + summary.DownlinkBytes
			out = append(out, summary)
		}
	}
	return out
}

func (s *PostgresStore) RebuildNodeConfig(nodeID string) (*domain.ConfigRevision, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rev, err := s.rebuildNodeConfigTx(tx, nodeID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return rev, nil
}

func (s *PostgresStore) RecordAuditLog(actorAdminID, action, targetType, targetID string, payload any) error {
	var actor any
	if actorAdminID != "" {
		actor = actorAdminID
	}
	_, err := s.db.Exec(
		`INSERT INTO audit_logs (actor_admin_id, action, target_type, target_id, payload)
		 VALUES ($1, $2, $3, $4, $5::jsonb)`,
		actor, action, targetType, targetID, marshalAuditPayload(payload),
	)
	return mapPQError(err)
}

func (s *PostgresStore) ListAuditLogs() []domain.AuditLog {
	rows, err := s.db.Query(
		`SELECT id, COALESCE(actor_admin_id, ''), action, target_type, target_id, payload::text, created_at
		 FROM audit_logs
		 ORDER BY created_at DESC, id DESC
		 LIMIT 200`,
	)
	if err != nil {
		return []domain.AuditLog{}
	}
	defer rows.Close()
	out := make([]domain.AuditLog, 0)
	for rows.Next() {
		var log domain.AuditLog
		if err := rows.Scan(&log.ID, &log.ActorAdminID, &log.Action, &log.TargetType, &log.TargetID, &log.Payload, &log.CreatedAt); err == nil {
			out = append(out, log)
		}
	}
	return out
}

func (s *PostgresStore) findNodeByToken(nodeToken string) (*domain.Node, error) {
	var node *domain.Node
	err := withRetryableRow(s.db.QueryRow(
		`SELECT id, name, region, public_host, provider, tags::text, runtime_flavor, status, last_heartbeat_at, current_config_version, node_token_hash, created_at, updated_at
		 FROM nodes
		 WHERE node_token_hash = $1`,
		sha256Hex(strings.TrimSpace(nodeToken)),
	), func(row scanner) error {
		var err error
		node, err = scanNode(row)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, mapPQError(err)
	}
	return node, nil
}

func (s *PostgresStore) getNodeByIDTx(tx *sql.Tx, nodeID string) (*domain.Node, error) {
	var node *domain.Node
	err := withRetryableRow(tx.QueryRow(
		`SELECT id, name, region, public_host, provider, tags::text, runtime_flavor, status, last_heartbeat_at, current_config_version, node_token_hash, created_at, updated_at
		 FROM nodes
		 WHERE id = $1`,
		nodeID,
	), func(row scanner) error {
		var err error
		node, err = scanNode(row)
		return err
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return node, mapPQError(err)
}

func (s *PostgresStore) getNodeGroup(groupID string) (*domain.NodeGroup, error) {
	return s.getNodeGroupTx(s.db, groupID)
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func (s *PostgresStore) getNodeGroupTx(q queryRower, groupID string) (*domain.NodeGroup, error) {
	var group domain.NodeGroup
	err := q.QueryRow(
		`SELECT id, name, description, created_at, updated_at
		 FROM node_groups
		 WHERE id = $1`,
		groupID,
	).Scan(&group.ID, &group.Name, &group.Description, &group.CreatedAt, &group.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapPQError(err)
	}
	return &group, nil
}

func (s *PostgresStore) getMemberByIDTx(tx *sql.Tx, memberID string) (*domain.Member, error) {
	row := tx.QueryRow(
		`SELECT id, uuid, name, email, note, status, expires_at, quota_bytes_limit, tier_id, subscription_token, disabled_reason, created_at, updated_at
		 FROM members
		 WHERE id = $1`,
		memberID,
	)
	member := scanMember(row)
	if member == nil {
		// Query itself failed; check for no-rows via a fresh attempt
		var dummy string
		err := tx.QueryRow(`SELECT id FROM members WHERE id = $1`, memberID).Scan(&dummy)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, ErrNotFound
	}
	return member, nil
}

func (s *PostgresStore) rebuildNodeConfigTx(tx *sql.Tx, nodeID string) (*domain.ConfigRevision, error) {
	node, err := s.getNodeByIDTx(tx, nodeID)
	if err != nil {
		return nil, err
	}
	// Only include credentials for members that are active and not expired.
	rows, err := tx.Query(
		`SELECT nc.id, nc.node_id, nc.member_id, nc.access_grant_id, nc.credential_uuid, nc.email, nc.created_at
		 FROM node_credentials nc
		 JOIN members m ON m.id = nc.member_id
		 WHERE nc.node_id = $1
		   AND m.status = 'active'
		   AND (m.expires_at IS NULL OR m.expires_at > NOW())
		 ORDER BY nc.created_at ASC`,
		nodeID,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	defer rows.Close()
	creds := make([]domain.NodeCredential, 0)
	seenMembers := map[string]struct{}{}
	for rows.Next() {
		var cred domain.NodeCredential
		if err := rows.Scan(&cred.ID, &cred.NodeID, &cred.MemberID, &cred.AccessGrantID, &cred.UUID, &cred.Email, &cred.CreatedAt); err != nil {
			return nil, mapPQError(err)
		}
		creds = append(creds, cred)
		seenMembers[cred.MemberID] = struct{}{}
	}
	groupRows, err := tx.Query(
		`SELECT DISTINCT m.id, m.uuid, m.name, m.email, m.note, m.status, m.expires_at, m.quota_bytes_limit, m.tier_id, m.subscription_token, m.disabled_reason, m.created_at, m.updated_at, ngm.group_id
		 FROM node_group_memberships ngm
		 JOIN member_node_group_grants mg ON mg.group_id = ngm.group_id
		 JOIN members m ON m.id = mg.member_id
		 WHERE ngm.node_id = $1
		   AND m.status = 'active'
		   AND (m.expires_at IS NULL OR m.expires_at > NOW())`,
		nodeID,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	defer groupRows.Close()
	now := time.Now().UTC()
	for groupRows.Next() {
		var member domain.Member
		var expiresAt sql.NullTime
		var tierID sql.NullString
		var groupID string
		if err := groupRows.Scan(
			&member.ID,
			&member.UUID,
			&member.Name,
			&member.Email,
			&member.Note,
			&member.Status,
			&expiresAt,
			&member.QuotaBytesLimit,
			&tierID,
			&member.SubscriptionToken,
			&member.DisabledReason,
			&member.CreatedAt,
			&member.UpdatedAt,
			&groupID,
		); err != nil {
			return nil, mapPQError(err)
		}
		if expiresAt.Valid {
			member.ExpiresAt = &expiresAt.Time
		}
		if tierID.Valid {
			member.TierID = tierID.String
		}
		if _, ok := seenMembers[member.ID]; ok {
			continue
		}
		creds = append(creds, domain.NodeCredential{
			ID:            "derived-" + groupID + "-" + member.ID,
			NodeID:        nodeID,
			MemberID:      member.ID,
			AccessGrantID: "group:" + groupID,
			UUID:          member.UUID,
			Email:         credentialEmail(&member, nodeID),
			CreatedAt:     now,
		})
		seenMembers[member.ID] = struct{}{}
	}
	sort.Slice(creds, func(i, j int) bool { return creds[i].CreatedAt.Before(creds[j].CreatedAt) })
	configText, err := render.RenderNodeConfig(*node, creds)
	if err != nil {
		return nil, err
	}
	var nextVersion int64
	err = tx.QueryRow(`SELECT COALESCE(MAX(config_version), 0) + 1 FROM node_config_revisions WHERE node_id = $1`, nodeID).Scan(&nextVersion)
	if err != nil {
		return nil, mapPQError(err)
	}
	now = time.Now().UTC()
	_, err = tx.Exec(
		`INSERT INTO node_config_revisions (node_id, config_version, config_json, config_hash, created_at)
		 VALUES ($1, $2, $3::jsonb, $4, $5)`,
		nodeID, nextVersion, configText, sha256Hex(configText), now,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	_, err = tx.Exec(`UPDATE nodes SET updated_at = $2 WHERE id = $1`, nodeID, now)
	if err != nil {
		return nil, mapPQError(err)
	}
	return &domain.ConfigRevision{
		NodeID:        nodeID,
		ConfigVersion: nextVersion,
		Config:        configText,
		UpdatedAt:     now,
	}, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func withRetryableRow(row *sql.Row, fn func(scanner) error) error {
	return fn(row)
}

func scanNode(row scanner) (*domain.Node, error) {
	var node domain.Node
	var rawTags string
	var heartbeat sql.NullTime
	if err := row.Scan(
		&node.ID,
		&node.Name,
		&node.Region,
		&node.PublicHost,
		&node.Provider,
		&rawTags,
		&node.RuntimeFlavor,
		&node.Status,
		&heartbeat,
		&node.CurrentConfigVersion,
		&node.NodeTokenHash,
		&node.CreatedAt,
		&node.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if heartbeat.Valid {
		node.LastHeartbeatAt = heartbeat.Time
	}
	if rawTags != "" {
		if err := json.Unmarshal([]byte(rawTags), &node.Tags); err != nil {
			return nil, err
		}
	}
	return &node, nil
}

func scanMember(row scanner) *domain.Member {
	var member domain.Member
	var expiresAt sql.NullTime
	var tierID sql.NullString
	if err := row.Scan(
		&member.ID,
		&member.UUID,
		&member.Name,
		&member.Email,
		&member.Note,
		&member.Status,
		&expiresAt,
		&member.QuotaBytesLimit,
		&tierID,
		&member.SubscriptionToken,
		&member.DisabledReason,
		&member.CreatedAt,
		&member.UpdatedAt,
	); err != nil {
		return nil
	}
	if expiresAt.Valid {
		member.ExpiresAt = &expiresAt.Time
	}
	if tierID.Valid {
		member.TierID = tierID.String
	}
	return &member
}

func mapPQError(err error) error {
	if err == nil {
		return nil
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		switch pqErr.Code {
		case "23505":
			return fmt.Errorf("%w: %s", ErrConflict, pqErr.Constraint)
		case "23503":
			return fmt.Errorf("%w: %s", ErrNotFound, pqErr.Constraint)
		}
	}
	return err
}

// ── Tier CRUD ──────────────────────────────────────────────────────────────────

func (s *PostgresStore) CreateTier(input CreateTierInput) (*domain.Tier, error) {
	now := time.Now().UTC()
	tier := &domain.Tier{
		ID:          newUUID(),
		Name:        strings.TrimSpace(input.Name),
		Description: strings.TrimSpace(input.Description),
		QuotaBytes:  input.QuotaBytes,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.Exec(
		`INSERT INTO tiers (id, name, description, quota_bytes, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		tier.ID, tier.Name, tier.Description, tier.QuotaBytes, tier.CreatedAt, tier.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	return tier, nil
}

func (s *PostgresStore) UpdateTier(tierID string, input UpdateTierInput) (*domain.Tier, error) {
	now := time.Now().UTC()
	row := s.db.QueryRow(
		`SELECT id, name, description, quota_bytes, created_at, updated_at FROM tiers WHERE id = $1`,
		tierID,
	)
	var tier domain.Tier
	if err := row.Scan(&tier.ID, &tier.Name, &tier.Description, &tier.QuotaBytes, &tier.CreatedAt, &tier.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
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
	tier.UpdatedAt = now
	_, err := s.db.Exec(
		`UPDATE tiers SET name = $2, description = $3, quota_bytes = $4, updated_at = $5 WHERE id = $1`,
		tier.ID, tier.Name, tier.Description, tier.QuotaBytes, tier.UpdatedAt,
	)
	if err != nil {
		return nil, mapPQError(err)
	}
	return &tier, nil
}

func (s *PostgresStore) DeleteTier(tierID string) error {
	res, err := s.db.Exec(`DELETE FROM tiers WHERE id = $1`, tierID)
	if err != nil {
		return mapPQError(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListTiers() []domain.Tier {
	rows, err := s.db.Query(
		`SELECT id, name, description, quota_bytes, created_at, updated_at FROM tiers ORDER BY created_at ASC`,
	)
	if err != nil {
		return []domain.Tier{}
	}
	defer rows.Close()
	out := make([]domain.Tier, 0)
	for rows.Next() {
		var t domain.Tier
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.QuotaBytes, &t.CreatedAt, &t.UpdatedAt); err == nil {
			out = append(out, t)
		}
	}
	return out
}

func (s *PostgresStore) GetMemberBySubscriptionToken(token string) (*domain.Member, error) {
	row := s.db.QueryRow(
		`SELECT id, uuid, name, email, note, status, expires_at, quota_bytes_limit, tier_id, subscription_token, disabled_reason, created_at, updated_at
		 FROM members WHERE subscription_token = $1`,
		token,
	)
	member := scanMember(row)
	if member == nil {
		return nil, ErrNotFound
	}
	return member, nil
}
