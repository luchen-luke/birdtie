package agentrun

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"strings"
	"time"
)

const PageSchema = "agent-run-page-v1"
const PageSize = 20
const PageLease = 2 * time.Minute
const MaxPageCursor = 1024

// A bounded historical control view. Neither its source references nor cursor
// authorize source content, scheduling, retention, model access or recovery.
type Page struct {
	Schema      string                `json:"schema"`
	Owner       actorref.PrincipalRef `json:"owner"`
	AgentID     string                `json:"agentId"`
	Items       []Record              `json:"items"`
	NextCursor  *string               `json:"nextCursor"`
	ObservedAt  time.Time             `json:"observedAt"`
	ExpiresAt   time.Time             `json:"expiresAt"`
	ModelAccess bool                  `json:"modelAccess"`
	Proof       any                   `json:"-"`
}
type PageGateway interface {
	AuthenticateRunPage(context.Context, [32]byte) (identity.Actor, error)
	ReadOwnRunPage(context.Context, agentprofile.PrivateAccess, string) (Page, error)
	RevalidateOwnRunPage(context.Context, agentprofile.PrivateAccess, Page) error
}

func PageCursorShape(v string) bool {
	if len(v) == 0 || len(v) > MaxPageCursor {
		return false
	}
	for _, c := range v {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
func ValidatePage(p Page, owner string, now time.Time) error {
	if p.Schema != PageSchema || p.Owner != (actorref.PrincipalRef{Type: actorref.Person, ID: owner}) || !agentenrichmentpurpose.ValidID(owner) || !agentenrichmentpurpose.ValidID(p.AgentID) || p.Items == nil || len(p.Items) > PageSize || p.ModelAccess || !validTime(p.ObservedAt) || !validTime(p.ExpiresAt) || !p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.Sub(p.ObservedAt) > PageLease || !p.ExpiresAt.After(now) || p.ObservedAt.After(now.Add(time.Second)) {
		return ErrUnavailable
	}
	if p.NextCursor != nil && (!PageCursorShape(*p.NextCursor) || len(p.Items) != PageSize) {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, r := range p.Items {
		if ValidateRecord(r) != nil || r.Owner != p.Owner || r.AgentID != p.AgentID || seen[r.ID] || r.ObservedAt.After(p.ObservedAt) {
			return ErrUnavailable
		}
		seen[r.ID] = true
	}
	return nil
}

// CursorFrame is native metadata sealed under a process key, not a grant.
// A new process key deliberately rejects old cursors; read page one again.
type CursorFrame struct {
	Schema, Owner, Agent, Session, Authority string
	Generation                               int64
	Ceiling, After, Until                    time.Time
	ID                                       string
}

func (f CursorFrame) Valid() bool {
	if f.Schema != PageSchema || !agentenrichmentpurpose.ValidID(f.Owner) || !agentenrichmentpurpose.ValidID(f.Agent) || !agentenrichmentpurpose.ValidID(f.Session) || f.Generation < 1 || len(f.Authority) != 64 || !validTime(f.Ceiling) || !validTime(f.Until) || !f.Until.After(f.Ceiling) || f.Until.Sub(f.Ceiling) > PageLease {
		return false
	}
	return f.ID == "" && f.After.IsZero() || agentenrichmentpurpose.ValidID(f.ID) && validTime(f.After) && !f.After.After(f.Ceiling)
}

type CursorCodec struct{ aead cipher.AEAD }

func NewCursorCodec(key []byte) (*CursorCodec, error) {
	b, e := aes.NewCipher(key)
	if e != nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, ErrUnavailable
	}
	return &CursorCodec{a}, nil
}
func (c *CursorCodec) Seal(f CursorFrame) (string, error) {
	if c == nil || c.aead == nil || !f.Valid() {
		return "", ErrInvalid
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return "", ErrInvalid
	}
	n := make([]byte, c.aead.NonceSize())
	if _, e = io.ReadFull(rand.Reader, n); e != nil {
		return "", ErrUnavailable
	}
	v := base64.RawURLEncoding.EncodeToString(c.aead.Seal(n, n, raw, []byte(PageSchema)))
	if !PageCursorShape(v) {
		return "", ErrInvalid
	}
	return v, nil
}
func (c *CursorCodec) Open(v string) (CursorFrame, error) {
	var f CursorFrame
	if c == nil || c.aead == nil || !PageCursorShape(v) {
		return f, ErrConflict
	}
	raw, e := base64.RawURLEncoding.DecodeString(v)
	if e != nil || len(raw) < c.aead.NonceSize()+c.aead.Overhead() {
		return f, ErrConflict
	}
	raw, e = c.aead.Open(nil, raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():], []byte(PageSchema))
	if e != nil {
		return f, ErrConflict
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || !f.Valid() {
		return CursorFrame{}, ErrConflict
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return CursorFrame{}, ErrConflict
	}
	return f, nil
}
