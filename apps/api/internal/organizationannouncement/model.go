// Package organizationannouncement is a human-operated, versioned Organization
// publication domain. It performs no broadcast, notification or model action.
package organizationannouncement

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
)

const Schema = "organization-announcement-v1"
const MaxResources = 128
const PreviewTTL = 10 * time.Minute
const Disclaimer = "由组织管理员发布；内容未经独立事实核验，不代表模型分析许可或广播通知。"
const PublicationConsequence = "确认发布后，当前有效的这版公告将允许访客查看；不会发送广播或通知。"

type State string

const (
	Draft     State = "DRAFT"
	Published State = "PUBLISHED"
	Withdrawn State = "WITHDRAWN"
)

type Record struct {
	SchemaVersion         string          `json:"schemaVersion"`
	ID                    string          `json:"id"`
	OrganizationID        string          `json:"organizationId"`
	OrganizationAccountID string          `json:"organizationAccountId"`
	Revision              int64           `json:"revision"`
	Title                 string          `json:"title"`
	Body                  string          `json:"body"`
	Audience              string          `json:"audience"`
	State                 State           `json:"state"`
	ValidUntil            time.Time       `json:"validUntil"`
	CreatedBy             string          `json:"createdBy"`
	UpdatedBy             string          `json:"updatedBy"`
	CreatedAt             time.Time       `json:"createdAt"`
	UpdatedAt             time.Time       `json:"updatedAt"`
	PublishedAt           *time.Time      `json:"publishedAt,omitempty"`
	WithdrawnAt           *time.Time      `json:"withdrawnAt,omitempty"`
	PublicationContext    json.RawMessage `json:"-"`
}
type PublicRecord struct {
	SchemaVersion    string    `json:"schemaVersion"`
	ID               string    `json:"id"`
	OrganizationID   string    `json:"organizationId"`
	OrganizationName string    `json:"organizationName"`
	Revision         int64     `json:"revision"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	Audience         string    `json:"audience"`
	State            State     `json:"state"`
	PublishedAt      time.Time `json:"publishedAt"`
	ValidUntil       time.Time `json:"validUntil"`
	Disclaimer       string    `json:"disclaimer"`
	SessionSnapshot  string    `json:"-"`
}

// PublicAccess is server-side session binding, never a caller-selected permission.
// Anonymous reads have zero fields. A bearer read binds actual token digest and
// the final read additionally binds the first SQL's retained session version.
type PublicAccess struct {
	ViewerID                string
	SessionDigest           [32]byte
	ExpectedSessionSnapshot string
}

func ValidatePublicAccess(a PublicAccess) error {
	if a.ViewerID == "" {
		if a.SessionDigest != ([32]byte{}) || a.ExpectedSessionSnapshot != "" {
			return agentmemory.ErrForbidden
		}
		return nil
	}
	if !Canonical(a.ViewerID) || a.SessionDigest == ([32]byte{}) {
		return agentmemory.ErrForbidden
	}
	if a.ExpectedSessionSnapshot != "" {
		raw, e := hex.DecodeString(a.ExpectedSessionSnapshot)
		if e != nil || len(raw) != 32 || strings.ToLower(a.ExpectedSessionSnapshot) != a.ExpectedSessionSnapshot {
			return agentmemory.ErrInvalid
		}
	}
	return nil
}

type Preview struct {
	ID               string    `json:"id"`
	Announcement     Record    `json:"announcement"`
	ActingPersonID   string    `json:"actingPersonId"`
	OrganizationName string    `json:"organizationName"`
	SourceSnapshot   string    `json:"sourceSnapshot"`
	CreatedAt        time.Time `json:"createdAt"`
	ExpiresAt        time.Time `json:"expiresAt"`
	Consequence      string    `json:"consequence"`
}
type DraftInput struct {
	ExpectedRevision int64     `json:"expectedRevision"`
	Title            string    `json:"title"`
	Body             string    `json:"body"`
	ValidUntil       time.Time `json:"validUntil"`
}
type RevisionInput struct {
	ExpectedRevision int64 `json:"expectedRevision"`
}
type PublishInput struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	PreviewID        string `json:"previewId"`
}
type Store interface {
	ListOrganizationAnnouncements(context.Context, agentorganizationmemory.Access) ([]Record, error)
	ReadOrganizationAnnouncement(context.Context, agentorganizationmemory.Access, string) (Record, error)
	PutOrganizationAnnouncement(context.Context, agentorganizationmemory.Access, string, DraftInput) (Record, error)
	PreviewOrganizationAnnouncement(context.Context, agentorganizationmemory.Access, string, int64) (Preview, error)
	PublishOrganizationAnnouncement(context.Context, agentorganizationmemory.Access, string, PublishInput) (Record, error)
	WithdrawOrganizationAnnouncement(context.Context, agentorganizationmemory.Access, string, int64) (Record, error)
	ReadPublicOrganizationAnnouncement(context.Context, string, string, PublicAccess) (PublicRecord, error)
}

func Canonical(id string) bool   { n, e := agentmemory.NormalizeMemoryID(id); return e == nil && n == id }
func validTime(t time.Time) bool { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }
func text(s string, max int) bool {
	if !utf8.ValidString(s) || len(s) > max || strings.TrimSpace(s) == "" {
		return false
	}
	for _, r := range s {
		if r == utf8.RuneError || (unicode.IsControl(r) && r != '\n' && r != '\t') {
			return false
		}
	}
	return true
}
func NormalizeDraft(in DraftInput, now time.Time) (DraftInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Body = strings.TrimSpace(in.Body)
	in.ValidUntil = in.ValidUntil.UTC().Truncate(time.Microsecond)
	if in.ExpectedRevision < 0 || in.ExpectedRevision == math.MaxInt64 || !text(in.Title, 480) || utf8.RuneCountInString(in.Title) > 160 || !text(in.Body, 6000) || !validTime(now) || !validTime(in.ValidUntil) || !in.ValidUntil.After(now) || in.ValidUntil.After(now.Add(agentmemory.MaxValidity)) {
		return DraftInput{}, agentmemory.ErrInvalid
	}
	return in, nil
}
func ValidateRecord(r Record) error {
	if r.SchemaVersion != Schema || !Canonical(r.ID) || !Canonical(r.OrganizationID) || !Canonical(r.OrganizationAccountID) || !Canonical(r.CreatedBy) || !Canonical(r.UpdatedBy) || r.Revision < 1 || !text(r.Title, 480) || utf8.RuneCountInString(r.Title) > 160 || !text(r.Body, 6000) || r.Audience != "PUBLIC" || !validTime(r.CreatedAt) || !validTime(r.UpdatedAt) || r.UpdatedAt.Before(r.CreatedAt) || !validTime(r.ValidUntil) || !r.ValidUntil.After(r.CreatedAt) {
		return agentmemory.ErrInvalid
	}
	switch r.State {
	case Draft:
		if r.PublishedAt != nil || r.WithdrawnAt != nil || len(r.PublicationContext) != 0 {
			return agentmemory.ErrInvalid
		}
	case Published:
		if r.PublishedAt == nil || !validTime(*r.PublishedAt) || r.PublishedAt.Before(r.CreatedAt) || r.PublishedAt.After(r.UpdatedAt) || !r.ValidUntil.After(*r.PublishedAt) || r.WithdrawnAt != nil || len(r.PublicationContext) == 0 {
			return agentmemory.ErrInvalid
		}
	case Withdrawn:
		if r.WithdrawnAt == nil || !validTime(*r.WithdrawnAt) || r.WithdrawnAt.Before(r.CreatedAt) || r.WithdrawnAt.After(r.UpdatedAt) {
			return agentmemory.ErrInvalid
		}
	default:
		return agentmemory.ErrInvalid
	}
	return nil
}
func ValidatePublic(r PublicRecord, org, id string, now time.Time) error {
	if r.SchemaVersion != Schema || r.ID != id || r.OrganizationID != org || !Canonical(id) || !Canonical(org) || !text(r.OrganizationName, 480) || r.Revision < 1 || !text(r.Title, 480) || !text(r.Body, 6000) || r.State != Published || r.Audience != "PUBLIC" || r.Disclaimer != Disclaimer || !validTime(r.PublishedAt) || !validTime(now) || r.PublishedAt.After(now) || !r.ValidUntil.After(now) {
		return agentmemory.ErrUnavailable
	}
	return nil
}
func DecodeDraft(raw []byte) (DraftInput, error) {
	var in DraftInput
	if decode(raw, []string{"expectedRevision", "title", "body", "validUntil"}, &in) != nil {
		return DraftInput{}, agentmemory.ErrInvalid
	}
	if in.ExpectedRevision < 0 || in.ExpectedRevision == math.MaxInt64 || !text(strings.TrimSpace(in.Title), 480) || !text(strings.TrimSpace(in.Body), 6000) || !validTime(in.ValidUntil) {
		return DraftInput{}, agentmemory.ErrInvalid
	}
	return in, nil
}
func DecodeRevision(raw []byte) (RevisionInput, error) {
	var in RevisionInput
	if decode(raw, []string{"expectedRevision"}, &in) != nil || in.ExpectedRevision < 1 || in.ExpectedRevision == math.MaxInt64 {
		return RevisionInput{}, agentmemory.ErrInvalid
	}
	return in, nil
}
func DecodePublish(raw []byte) (PublishInput, error) {
	var in PublishInput
	if decode(raw, []string{"expectedRevision", "previewId"}, &in) != nil || in.ExpectedRevision < 1 || in.ExpectedRevision == math.MaxInt64 || !Canonical(in.PreviewID) {
		return PublishInput{}, agentmemory.ErrInvalid
	}
	return in, nil
}
func decode(raw []byte, required []string, out any) error {
	normalized, e := agentmemory.NormalizeStructuredValue(raw)
	if e != nil {
		return e
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(normalized, &fields) != nil || len(fields) != len(required) {
		return agentmemory.ErrInvalid
	}
	for _, k := range required {
		if value, ok := fields[k]; !ok || string(value) == "null" {
			return agentmemory.ErrInvalid
		}
	}
	if json.Unmarshal(normalized, out) != nil {
		return agentmemory.ErrInvalid
	}
	return nil
}
