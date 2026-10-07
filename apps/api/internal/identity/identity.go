package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrUnauthorized       = errors.New("unauthorized")
	ErrNotFound           = errors.New("not found")
	ErrInvalidGrant       = errors.New("invalid grant")
	ErrConflict           = errors.New("conflict")
	ErrInvalidProfile     = errors.New("invalid profile")
	ErrProfileUnavailable = errors.New("profile unavailable")
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

type ProfileInput struct {
	DisplayName string `json:"displayName"`
	Bio         string `json:"bio"`
	Visibility  string `json:"visibility"`
}

// HumanProfileStore preserves the original public Profile source while
// rechecking its real session inside the write transaction. initialActor is
// obtained by server Authenticate, never an owner selector from request JSON.
// This interface grants no Agent, organization role, analysis or model access.
type HumanProfileStore interface {
	UpdateHumanProfile(context.Context, [32]byte, Actor, ProfileInput) (Profile, error)
}

// NormalizeHumanProfileInput retains the original byte-length limits and
// trim behavior. Display names contain no control characters; bios may use
// tabs/newlines. This is ordinary explicit user text, not inferred context.
func NormalizeHumanProfileInput(input ProfileInput) (ProfileInput, error) {
	if !utf8.ValidString(input.DisplayName) || !utf8.ValidString(input.Bio) {
		return ProfileInput{}, ErrInvalidProfile
	}
	for _, r := range input.DisplayName {
		if unicode.IsControl(r) {
			return ProfileInput{}, ErrInvalidProfile
		}
	}
	for _, r := range input.Bio {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return ProfileInput{}, ErrInvalidProfile
		}
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Bio = strings.TrimSpace(input.Bio)
	if len(input.DisplayName) < 2 || len(input.DisplayName) > 80 || len(input.Bio) > 500 || (input.Visibility != "public" && input.Visibility != "private") {
		return ProfileInput{}, ErrInvalidProfile
	}
	return input, nil
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
	UpdateOwnProfile(context.Context, string, ProfileInput) (Profile, error)
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
