package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/profileavatar"
	"io"
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
	AccountID     string                  `json:"accountId"`
	DisplayName   string                  `json:"displayName"`
	Bio           string                  `json:"bio"`
	Visibility    string                  `json:"visibility"`
	Avatar        *profileavatar.Metadata `json:"avatar,omitempty"`
	ResponseProof any                     `json:"-"`
}

// CurrentProfileStore checks the same current human/session and source after
// encoding a visible profile. Its private proof is never an execution grant.
type ProfileAccess struct {
	Actor         Actor
	SessionDigest [32]byte
}

type CurrentProfileStore interface {
	ReadProfileCurrent(context.Context, ProfileAccess, string) (Profile, error)
	ValidateProfileCurrent(context.Context, ProfileAccess, string, Profile) error
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
	if len(input.DisplayName) < 2 || len(input.DisplayName) > 80 || len(input.Bio) > 500 || !ProfileVisibilityValid(input.Visibility) {
		return ProfileInput{}, ErrInvalidProfile
	}
	return input, nil
}

func ProfileVisibilityValid(value string) bool {
	switch value {
	case "public", "followers", "connections", "close", "private":
		return true
	default:
		return false
	}
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
	Version   int64     `json:"version,omitempty"`
}

// Ordinary human controls share only their private frame and immutable receipt
// encoding. Each domain keeps its own typed input, policy and native effect.
const HumanControlMaxVersion int64 = 8999999999999999

var ErrControlInvalid = errors.New("invalid human control")
var ErrControlChanged = errors.New("human control changed")
var ErrControlUnavailable = errors.New("human control unavailable")

type HumanControlFrame struct {
	ID         string `json:"-"`
	Generation int64  `json:"-"`
}
type HumanControlSessionStore interface {
	CaptureHumanControlSession(context.Context, [32]byte, Actor) (HumanControlFrame, error)
	ValidateHumanControlSession(context.Context, [32]byte, Actor, HumanControlFrame) error
}
type humanControlFrameKey struct{}
type humanControlFrameValue struct {
	Actor   Actor
	Digest  [32]byte
	Frame   HumanControlFrame
	Invalid bool
}

func WithHumanControlFrame(ctx context.Context, d [32]byte, a Actor, f HumanControlFrame) context.Context {
	if old, ok := ctx.Value(humanControlFrameKey{}).(humanControlFrameValue); ok {
		if !old.Invalid && old.Actor.ID == a.ID && old.Actor.AccountType == a.AccountType && old.Digest == d && old.Frame == f {
			return ctx
		}
		old.Invalid = true
		return context.WithValue(ctx, humanControlFrameKey{}, old)
	}
	return context.WithValue(ctx, humanControlFrameKey{}, humanControlFrameValue{Actor: a, Digest: d, Frame: f})
}
func OriginalHumanControlFrame(ctx context.Context, d [32]byte, a Actor) (HumanControlFrame, bool, bool) {
	v, ok := ctx.Value(humanControlFrameKey{}).(humanControlFrameValue)
	return v.Frame, ok && !v.Invalid && v.Actor.ID == a.ID && v.Actor.AccountType == a.AccountType && v.Digest == d, ok
}

type HumanControlProof interface{ RevalidateHumanControl(context.Context) error }
type HumanControlPreview struct {
	OperationID   string            `json:"operationId"`
	Action        string            `json:"action"`
	PayloadDigest string            `json:"payloadDigest"`
	ObservedAt    time.Time         `json:"observedAt"`
	ExpiresAt     time.Time         `json:"expiresAt"`
	Approval      string            `json:"approval"`
	ResponseProof HumanControlProof `json:"-"`
}
type HumanControlReceipt struct {
	OperationID    string     `json:"operationId"`
	Family         string     `json:"family"`
	Status         string     `json:"status"`
	AppliedVersion int64      `json:"appliedVersion"`
	Action         string     `json:"action,omitempty"`
	TargetID       string     `json:"targetId,omitempty"`
	ResultID       string     `json:"resultId,omitempty"`
	PayloadDigest  string     `json:"payloadDigest,omitempty"`
	CommittedAt    *time.Time `json:"committedAt,omitempty"`
	ScopeID        string     `json:"-"`
}

func HumanControlUUID(v string) bool {
	if len(v) != 36 || v != strings.ToLower(v) {
		return false
	}
	for i, b := range []byte(v) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if b != '-' {
				return false
			}
		} else if !(b >= '0' && b <= '9' || b >= 'a' && b <= 'f') {
			return false
		}
	}
	return true
}
func HumanControlTextHash(v string) string {
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}
func HumanControlTupleHash(v []any) string {
	b, _ := json.Marshal(v)
	return HumanControlTextHash(string(b))
}
func HumanControlClosedJSON(raw []byte, required, optional []string) error {
	if len(raw) < 2 || len(raw) > 32768 || !utf8.Valid(raw) {
		return ErrControlInvalid
	}
	allowed := map[string]bool{}
	for _, v := range append(append([]string{}, required...), optional...) {
		allowed[v] = true
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return ErrControlInvalid
	}
	seen := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || !allowed[k] || seen[k] {
			return ErrControlInvalid
		}
		seen[k] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return ErrControlInvalid
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') {
		return ErrControlInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrControlInvalid
	}
	for _, v := range required {
		if !seen[v] {
			return ErrControlInvalid
		}
	}
	return nil
}
func HumanControlApprovalValid(v string, commit bool) bool {
	return (!commit && v == "") || (commit && len(v) > 0 && len(v) <= 8192 && utf8.ValidString(v))
}
func HumanControlNextVersion(v int64) int64 {
	if v >= HumanControlMaxVersion {
		return HumanControlMaxVersion
	}
	return v + 1
}

type GrantRevokeControlInput struct {
	OperationID     string `json:"operationId"`
	Action          string `json:"action"`
	GrantID         string `json:"grantId"`
	ExpectedVersion int64  `json:"expectedVersion"`
	Approval        string `json:"approval,omitempty"`
}
type GrantRevokeControlState struct {
	Grant         Grant             `json:"grant"`
	CanRevoke     bool              `json:"canRevoke"`
	ObservedAt    time.Time         `json:"observedAt"`
	ExpiresAt     time.Time         `json:"expiresAt"`
	ResponseProof HumanControlProof `json:"-"`
}
type GrantRevokeControlPreview struct {
	HumanControlPreview
	GrantID         string `json:"grantId"`
	ExpectedVersion int64  `json:"expectedVersion"`
}
type GrantRevokeControlStore interface {
	ReadGrantRevokeControl(context.Context, [32]byte, Actor, string) (GrantRevokeControlState, error)
	PreviewGrantRevokeControl(context.Context, [32]byte, Actor, GrantRevokeControlInput) (GrantRevokeControlPreview, error)
	CommitGrantRevokeControl(context.Context, [32]byte, Actor, GrantRevokeControlInput) (HumanControlReceipt, error)
	ReadGrantRevokeOperation(context.Context, [32]byte, Actor, string) (HumanControlReceipt, error)
	StopGrantRevokeOperation(context.Context, [32]byte, Actor, string) (HumanControlReceipt, error)
}

func NormalizeGrantRevokeControl(in GrantRevokeControlInput, commit bool) (GrantRevokeControlInput, error) {
	if !HumanControlUUID(in.OperationID) || in.Action != "REVOKE" || !HumanControlUUID(in.GrantID) || in.ExpectedVersion < 1 || in.ExpectedVersion > HumanControlMaxVersion || !HumanControlApprovalValid(in.Approval, commit) {
		return in, ErrControlInvalid
	}
	return in, nil
}
func GrantRevokeControlDigest(in GrantRevokeControlInput) string {
	return HumanControlTupleHash([]any{"human-grant-revoke-v1", in.OperationID, in.Action, in.GrantID, in.ExpectedVersion})
}
func DecodeGrantRevokeControl(raw []byte, commit bool) (GrantRevokeControlInput, error) {
	keys := []string{"operationId", "action", "grantId", "expectedVersion"}
	if commit {
		keys = append(keys, "approval")
	}
	var in GrantRevokeControlInput
	if HumanControlClosedJSON(raw, keys, nil) != nil || json.Unmarshal(raw, &in) != nil {
		return in, ErrControlInvalid
	}
	return NormalizeGrantRevokeControl(in, commit)
}

type BlockControlInput struct {
	OperationID     string `json:"operationId"`
	Action          string `json:"action"`
	AccountID       string `json:"accountId"`
	ExpectedVersion int64  `json:"expectedVersion"`
	Approval        string `json:"approval,omitempty"`
}
type BlockControlState struct {
	AccountID     string            `json:"accountId"`
	Version       int64             `json:"version"`
	Blocked       bool              `json:"blocked"`
	CanBlock      bool              `json:"canBlock"`
	CanUnblock    bool              `json:"canUnblock"`
	ObservedAt    time.Time         `json:"observedAt"`
	ExpiresAt     time.Time         `json:"expiresAt"`
	ResponseProof HumanControlProof `json:"-"`
}
type BlockControlPreview struct {
	HumanControlPreview
	AccountID       string `json:"accountId"`
	ExpectedVersion int64  `json:"expectedVersion"`
}
type BlockControlStore interface {
	ReadBlockControl(context.Context, [32]byte, Actor, string) (BlockControlState, error)
	PreviewBlockControl(context.Context, [32]byte, Actor, BlockControlInput) (BlockControlPreview, error)
	CommitBlockControl(context.Context, [32]byte, Actor, BlockControlInput) (HumanControlReceipt, error)
	ReadBlockOperation(context.Context, [32]byte, Actor, string) (HumanControlReceipt, error)
	StopBlockOperation(context.Context, [32]byte, Actor, string) (HumanControlReceipt, error)
}

func NormalizeBlockControl(in BlockControlInput, commit bool) (BlockControlInput, error) {
	if !HumanControlUUID(in.OperationID) || !HumanControlUUID(in.AccountID) || (in.Action != "BLOCK" && in.Action != "UNBLOCK") || in.ExpectedVersion < 0 || in.ExpectedVersion > HumanControlMaxVersion || (in.Action == "BLOCK" && in.ExpectedVersion == HumanControlMaxVersion) || !HumanControlApprovalValid(in.Approval, commit) {
		return in, ErrControlInvalid
	}
	return in, nil
}
func BlockControlDigest(in BlockControlInput) string {
	return HumanControlTupleHash([]any{"human-block-v1", in.OperationID, in.Action, in.AccountID, in.ExpectedVersion})
}
func DecodeBlockControl(raw []byte, commit bool) (BlockControlInput, error) {
	keys := []string{"operationId", "action", "accountId", "expectedVersion"}
	if commit {
		keys = append(keys, "approval")
	}
	var in BlockControlInput
	if HumanControlClosedJSON(raw, keys, nil) != nil || json.Unmarshal(raw, &in) != nil {
		return in, ErrControlInvalid
	}
	return NormalizeBlockControl(in, commit)
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
