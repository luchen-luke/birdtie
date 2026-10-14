// Package entitycover owns explicitly approved Activity/Organization covers.
// It grants no entity publication, Agent, model, or storage permission.
package entitycover

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

const MaxVersion int64 = 9007199254740991

var ErrInvalid = errors.New("invalid entity cover")
var ErrChanged = errors.New("entity cover changed")
var ErrForbidden = errors.New("entity cover forbidden")
var ErrNotFound = errors.New("entity cover not found")
var ErrUnavailable = errors.New("entity cover unavailable")

type Access = av.Access

// SessionFrame is an internal original authorization frame. It is deliberately
// separate from SQL source proofs, DTOs and anonymous resource visibility.
type SessionFrame struct {
	ID         string `json:"-"`
	Generation int64  `json:"-"`
}
type originalSessionKey struct{}
type originalSession struct {
	Access  Access
	Frame   SessionFrame
	Invalid bool
}

func WithOriginalSession(ctx context.Context, a Access, f SessionFrame) context.Context {
	if old, ok := ctx.Value(originalSessionKey{}).(originalSession); ok {
		if !old.Invalid && old.Access == a && old.Frame == f {
			return ctx
		}
		old.Invalid = true
		return context.WithValue(ctx, originalSessionKey{}, old)
	}
	return context.WithValue(ctx, originalSessionKey{}, originalSession{Access: a, Frame: f})
}
func OriginalSession(ctx context.Context, a Access) (SessionFrame, bool) {
	v, ok := ctx.Value(originalSessionKey{}).(originalSession)
	return v.Frame, ok && !v.Invalid && v.Access == a
}
func HasOriginalSession(ctx context.Context) bool {
	_, ok := ctx.Value(originalSessionKey{}).(originalSession)
	return ok
}

type SessionStore interface {
	CaptureEntityCoverSession(context.Context, Access) (SessionFrame, error)
	ValidateEntityCoverSession(context.Context, Access, SessionFrame) error
}
type OriginalContent struct {
	Metadata    Metadata `json:"-"`
	Bytes       []byte   `json:"-"`
	SourceProof string   `json:"-"`
}
type OriginalContentStore interface {
	ReadEntityCoverOriginalContent(context.Context, Access, Ref, int64) (OriginalContent, error)
}
type Ref struct {
	Type string `json:"entityType"`
	ID   string `json:"entityId"`
}

func (r Ref) Valid() bool {
	return (r.Type == "activity" || r.Type == "organization") && av.ValidID(r.ID)
}
func Path(r Ref, v int64) string {
	return "/v1/" + map[string]string{"activity": "activities", "organization": "organizations"}[r.Type] + "/" + r.ID + "/cover/" + strconv.FormatInt(v, 10) + "/content"
}

type Metadata struct {
	Ref
	Version       int64     `json:"version"`
	ThumbnailPath string    `json:"thumbnailPath"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	ByteCount     int       `json:"byteCount"`
	SHA256        string    `json:"sha256"`
	ExpiresAt     time.Time `json:"expiresAt"`
	Attribution   string    `json:"attribution"`
}

func (m Metadata) Valid(now time.Time) bool {
	h, e := hex.DecodeString(m.SHA256)
	return m.Ref.Valid() && m.Version > 0 && m.Version <= MaxVersion && m.ThumbnailPath == Path(m.Ref, m.Version) && m.Width == Width && m.Height == Height && m.ByteCount > 0 && m.ByteCount <= MaxThumbnailBytes && len(h) == 32 && e == nil && strings.ToLower(m.SHA256) == m.SHA256 && m.ExpiresAt.After(now) && !m.ExpiresAt.After(now.Add(61*time.Second)) && validText(m.Attribution, 320, true)
}

// PreserveLease compares newly observed metadata without renewing the emitted
// lease. A source whose real deadline shortened below the old lease is retired.
func PreserveLease(old *Metadata, current *Metadata, now time.Time) bool {
	if old == nil || current == nil {
		return old == nil && current == nil
	}
	if !old.Valid(now) || !current.Valid(now) || current.ExpiresAt.Before(old.ExpiresAt) {
		return false
	}
	c := *current
	c.ExpiresAt = old.ExpiresAt
	if c != *old {
		return false
	}
	*current = c
	return true
}

type Rights struct {
	Basis            string    `json:"basis"`
	Attribution      string    `json:"attribution"`
	LicenseReference string    `json:"licenseReference"`
	RetentionUntil   time.Time `json:"retentionUntil"`
	CanRetain        bool      `json:"canRetain"`
	CanPublish       bool      `json:"canPublish"`
}

func validText(s string, max int, empty bool) bool {
	if !utf8.ValidString(s) || s != strings.TrimSpace(s) || len(s) > max || (!empty && s == "") {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func (r Rights) Valid(now time.Time) bool {
	return (r.Basis == "OWNED" || r.Basis == "LICENSED") && validText(r.Attribution, 320, true) && validText(r.LicenseReference, 1024, r.Basis == "OWNED") && r.CanRetain && r.CanPublish && r.RetentionUntil.After(now) && r.RetentionUntil.Year() <= 9999
}
func (r Rights) Digest() string {
	r.RetentionUntil = r.RetentionUntil.UTC()
	raw, _ := json.Marshal(r)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type State struct {
	Version     int64     `json:"version"`
	Cover       *Metadata `json:"cover"`
	SourceProof string    `json:"-"`
}
type Input struct {
	OperationID     string
	ExpectedVersion int64
	Action          string
	ContentType     string
	Content         []byte
	Rights          *Rights
	Approval        string
}
type PreviewDerivative struct {
	ContentType   string `json:"contentType"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	ByteCount     int    `json:"byteCount"`
	SHA256        string `json:"sha256"`
	ContentBase64 string `json:"contentBase64"`
}
type Approval struct {
	Ref
	OperationID       string             `json:"operationId"`
	ExpectedVersion   int64              `json:"expectedVersion"`
	Action            string             `json:"action"`
	InputSHA256       string             `json:"inputSha256"`
	RightsSHA256      string             `json:"rightsSha256"`
	ExpiresAt         time.Time          `json:"expiresAt"`
	Token             string             `json:"token"`
	PreviewDerivative *PreviewDerivative `json:"previewDerivative,omitempty"`
}
type Receipt struct {
	Ref
	OperationID    string `json:"operationId"`
	Action         string `json:"action"`
	AppliedVersion int64  `json:"appliedVersion"`
	Status         string `json:"status"`
}

func (r Receipt) Valid(ref Ref, op string) bool {
	return r.Ref == ref && ref.Valid() && av.ValidID(op) && r.OperationID == op && ((r.Status == "SUCCEEDED" && (r.Action == "SAVE" || r.Action == "DELETE") && r.AppliedVersion > 0 && r.AppliedVersion <= MaxVersion) || (r.Status == "STOPPED" && r.Action == "STOP" && r.AppliedVersion == 0))
}
func Normalize(in Input, now time.Time) (Derivative, string, string, error) {
	if !av.ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion >= MaxVersion {
		return Derivative{}, "", "", ErrInvalid
	}
	if in.Action == "DELETE" {
		if len(in.Content) != 0 || in.ContentType != "" || in.Rights != nil {
			return Derivative{}, "", "", ErrInvalid
		}
		return Derivative{}, "", "", nil
	}
	if in.Action != "SAVE" || in.Rights == nil || !in.Rights.Valid(now) {
		return Derivative{}, "", "", ErrInvalid
	}
	d, e := Prepare(in.Content, in.ContentType)
	if e != nil {
		return Derivative{}, "", "", e
	}
	return d, d.SHA256, in.Rights.Digest(), nil
}
func Digest(ref Ref, in Input, sum, rights string) string {
	raw, _ := json.Marshal([]any{"ENTITY_COVER_V1", ref, in.OperationID, in.ExpectedVersion, in.Action, sum, rights})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

type Store interface {
	ReadEntityCover(context.Context, Access, Ref) (State, error)
	PreviewEntityCover(context.Context, Access, Ref, Input) (Approval, error)
	ChangeEntityCover(context.Context, Access, Ref, Input) (Receipt, error)
	ReadEntityCoverOperation(context.Context, Access, Ref, string) (Receipt, error)
	StopEntityCoverOperation(context.Context, Access, Ref, string, int64) (Receipt, error)
	ReadEntityCoverContent(context.Context, Access, Ref, int64) (Metadata, []byte, error)
	ValidateEntityCoverProjection(context.Context, Access, Ref, *Metadata, string) error
	ValidateEntityCoverPreview(context.Context, Access, Ref, Input, Approval) error
}
