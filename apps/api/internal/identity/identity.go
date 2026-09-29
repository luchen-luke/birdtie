package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrNotFound     = errors.New("not found")
	ErrInvalidGrant = errors.New("invalid grant")
	ErrConflict     = errors.New("conflict")
)

const tokenPrefix = "bts1_"

type Actor struct {
	ID          string  `json:"id"`
	AccountType string  `json:"accountType"`
	Handle      *string `json:"handle,omitempty"`
}

type Profile struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	Bio         string `json:"bio"`
	Visibility  string `json:"visibility"`
}

type Grant struct {
	ID                 string     `json:"id"`
	RecipientAccountID string     `json:"recipientAccountId"`
	ResourceType       string     `json:"resourceType"`
	ResourceID         string     `json:"resourceId"`
	Purpose            string     `json:"purpose"`
	Actions            []string   `json:"actions"`
	Revision           int64      `json:"revision"`
	CreatedAt          time.Time  `json:"createdAt"`
	ExpiresAt          *time.Time `json:"expiresAt,omitempty"`
	RevokedAt          *time.Time `json:"revokedAt,omitempty"`
}

type Block struct {
	AccountID string    `json:"accountId"`
	CreatedAt time.Time `json:"createdAt"`
}

// AccessStore is the only path from HTTP requests to private identity data.
// Actor IDs are resolved from a server-side session, never from request JSON.
type AccessStore interface {
	Authenticate(context.Context, [32]byte) (Actor, error)
	RevokeSession(context.Context, [32]byte) error
	ReadProfile(context.Context, string, string) (Profile, error)
	ListProfileGrants(context.Context, string) ([]Grant, error)
	GrantProfileRead(context.Context, string, string, time.Time) (Grant, error)
	RevokeProfileGrant(context.Context, string, string) error
	ListBlocks(context.Context, string) ([]Block, error)
	BlockAccount(context.Context, string, string) error
	UnblockAccount(context.Context, string, string) error
}

// ParseBearer accepts one canonical, opaque bearer credential. It returns only
// its digest so handlers cannot accidentally log or persist the raw token.
func ParseBearer(header string) ([32]byte, error) {
	var empty [32]byte
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") ||
		!strings.HasPrefix(fields[1], tokenPrefix) {
		return empty, ErrUnauthorized
	}
	encoded := strings.TrimPrefix(fields[1], tokenPrefix)
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) != 32 || base64.RawURLEncoding.EncodeToString(raw) != encoded {
		return empty, ErrUnauthorized
	}
	return sha256.Sum256([]byte(fields[1])), nil
}

// NewToken creates an opaque credential for a verified, one-time OIDC handoff.
// No endpoint accepts a client-supplied actor as authority to mint a session.
func NewToken() (string, [32]byte, error) {
	var empty [32]byte
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", empty, err
	}
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return token, sha256.Sum256([]byte(token)), nil
}
