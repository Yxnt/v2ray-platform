package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"v2ray-platform/internal/domain"
)

func credentialEmail(member *domain.Member, nodeID string) string {
	local := strings.ToLower(member.Email)
	local = strings.ReplaceAll(local, "@", "+")
	return fmt.Sprintf("%s+%s@internal.invalid", local, nodeID)
}

func randomToken(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func newID(prefix string) string {
	return prefix + "_" + strings.ToLower(newUUID())
}

func newUUID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func derivedGroupCredentialUUID(nodeID, memberID string) string {
	sum := sha256.Sum256([]byte("group-credential:" + nodeID + ":" + memberID))
	buf := sum[:16]
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(strings.ToLower(tag))
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func cloneNode(node *domain.Node) *domain.Node {
	copied := *node
	copied.Tags = append([]string(nil), node.Tags...)
	return &copied
}

func cloneAdmin(admin *domain.Admin) *domain.Admin {
	copied := *admin
	return &copied
}

func cloneMember(member *domain.Member) *domain.Member {
	copied := *member
	if member.ExpiresAt != nil {
		t := *member.ExpiresAt
		copied.ExpiresAt = &t
	}
	return &copied
}

func cloneBootstrapToken(token *domain.BootstrapToken) *domain.BootstrapToken {
	copied := *token
	return &copied
}

func cloneAdminSession(session *domain.AdminSession) *domain.AdminSession {
	copied := *session
	if session.RevokedAt != nil {
		t := *session.RevokedAt
		copied.RevokedAt = &t
	}
	if session.LastSeenAt != nil {
		t := *session.LastSeenAt
		copied.LastSeenAt = &t
	}
	return &copied
}

func cloneGrant(grant *domain.AccessGrant) *domain.AccessGrant {
	copied := *grant
	return &copied
}

func cloneNodeGroup(group *domain.NodeGroup) *domain.NodeGroup {
	copied := *group
	return &copied
}

func cloneCredential(cred *domain.NodeCredential) *domain.NodeCredential {
	copied := *cred
	return &copied
}

func cloneRevision(rev *domain.ConfigRevision) *domain.ConfigRevision {
	copied := *rev
	return &copied
}

func marshalAuditPayload(payload any) string {
	if payload == nil {
		return "{}"
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return `{"error":"marshal failed"}`
	}
	return string(data)
}
