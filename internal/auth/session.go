package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"v2ray-platform/internal/domain"
)

var ErrInvalidSession = errors.New("invalid session")

type Claims struct {
	SessionID string    `json:"session_id"`
	AdminID   string    `json:"admin_id"`
	Email     string    `json:"email"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SessionStore interface {
	CreateAdminSession(adminID string, expiresAt time.Time) (*domain.AdminSession, error)
	// GetAdminSession returns (session, nil) when found, (nil, nil) when the session
	// does not exist, and (nil, err) for transient/infrastructure errors.
	GetAdminSession(sessionID string) (*domain.AdminSession, error)
	TouchAdminSession(sessionID string, seenAt time.Time) error
}

type Manager struct {
	secret        []byte
	verifySecrets [][]byte
	ttl           time.Duration
	store         SessionStore
}

func NewManager(secret string, previousSecrets []string, ttl time.Duration, store SessionStore) *Manager {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	verifySecrets := make([][]byte, 0, 1+len(previousSecrets))
	verifySecrets = append(verifySecrets, []byte(secret))
	for _, candidate := range previousSecrets {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		verifySecrets = append(verifySecrets, []byte(candidate))
	}
	return &Manager{
		secret:        []byte(secret),
		verifySecrets: verifySecrets,
		ttl:           ttl,
		store:         store,
	}
}

func (m *Manager) Issue(admin *domain.Admin) (string, Claims, error) {
	expiresAt := time.Now().UTC().Add(m.ttl)
	var sessionID string
	if m.store != nil {
		session, err := m.store.CreateAdminSession(admin.ID, expiresAt)
		if err != nil {
			return "", Claims{}, err
		}
		sessionID = session.ID
	}
	claims := Claims{
		SessionID: sessionID,
		AdminID:   admin.ID,
		Email:     admin.Email,
		ExpiresAt: expiresAt,
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", Claims{}, err
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	sig := m.sign(encodedPayload)
	return encodedPayload + "." + sig, claims, nil
}

func (m *Manager) Verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, ErrInvalidSession
	}
	if !m.verifySignature(parts[0], parts[1]) {
		return nil, ErrInvalidSession
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrInvalidSession
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrInvalidSession
	}
	if time.Now().UTC().After(claims.ExpiresAt) {
		return nil, ErrInvalidSession
	}
	if claims.SessionID != "" && m.store != nil {
		session, err := m.store.GetAdminSession(claims.SessionID)
		if err != nil {
			// Transient DB error (e.g. Neon cold-start, connection pool exhaustion).
			// The token's signature and expiry already passed; accept it rather than
			// producing a spurious 401.
			return &claims, nil
		}
		if session == nil {
			// Session definitively does not exist (revoked or deleted).
			return nil, ErrInvalidSession
		}
		if session.AdminID != claims.AdminID || session.RevokedAt != nil || time.Now().UTC().After(session.ExpiresAt) {
			return nil, ErrInvalidSession
		}
		_ = m.store.TouchAdminSession(claims.SessionID, time.Now().UTC())
	}
	return &claims, nil
}

func (m *Manager) sign(payload string) string {
	h := hmac.New(sha256.New, m.secret)
	h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (m *Manager) verifySignature(payload, sig string) bool {
	for _, secret := range m.verifySecrets {
		h := hmac.New(sha256.New, secret)
		h.Write([]byte(payload))
		if hmac.Equal([]byte(sig), []byte(base64.RawURLEncoding.EncodeToString(h.Sum(nil)))) {
			return true
		}
	}
	return false
}
