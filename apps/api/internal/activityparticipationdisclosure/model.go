// Package activityparticipationdisclosure describes a person's explicit public
// registration, never attendance, membership, an invitation or machine consent.
package activityparticipationdisclosure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"io"
	"strings"
	"time"
)

var (
	ErrInvalid     = errors.New("invalid participation disclosure")
	ErrDenied      = errors.New("participation disclosure denied")
	ErrChanged     = errors.New("participation disclosure version changed")
	ErrUnavailable = errors.New("participation disclosure unavailable")
)

const Schema = "human-activity-participation-disclosure-v1"

type Input struct {
	ParticipationID     string     `json:"participationId"`
	Operation           string     `json:"operation"`
	DisclosureExpiresAt *time.Time `json:"disclosureExpiresAt,omitempty"`
}
type Record struct {
	ParticipationID     string     `json:"participationId"`
	ActivityID          string     `json:"activityId"`
	Title               string     `json:"title"`
	Status              string     `json:"status"`
	SourceAvailable     bool       `json:"sourceAvailable"`
	StartsAt            *time.Time `json:"startsAt,omitempty"`
	EndsAt              *time.Time `json:"endsAt,omitempty"`
	SourceExpiresAt     *time.Time `json:"sourceExpiresAt,omitempty"`
	Visibility          string     `json:"visibility"`
	DisclosureExpiresAt *time.Time `json:"disclosureExpiresAt,omitempty"`
	EffectivePublic     bool       `json:"effectivePublic"`
	Attendance          string     `json:"attendance"`
}
type View struct {
	SchemaVersion     string    `json:"schemaVersion"`
	OwnerID           string    `json:"ownerId"`
	AgentID           string    `json:"agentId"`
	ObservedAt        time.Time `json:"observedAt"`
	Records           []Record  `json:"records"`
	Limit             int       `json:"limit"`
	Truncated         bool      `json:"truncated"`
	ModelAccess       bool      `json:"modelAccess"`
	SendAllowed       bool      `json:"sendAllowed"`
	MembershipGranted bool      `json:"membershipGranted"`
}
type Preview struct {
	SchemaVersion string `json:"schemaVersion"`
	OwnerID       string `json:"ownerId"`
	AgentID       string `json:"agentId"`
	Record
	Operation         string     `json:"operation"`
	TargetVisibility  string     `json:"targetVisibility"`
	TargetExpiresAt   *time.Time `json:"targetExpiresAt,omitempty"`
	Preview           string     `json:"preview"`
	ObservedAt        time.Time  `json:"observedAt"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	Consequence       string     `json:"consequence"`
	ModelAccess       bool       `json:"modelAccess"`
	SendAllowed       bool       `json:"sendAllowed"`
	MembershipGranted bool       `json:"membershipGranted"`
}
type Store interface {
	ReadOwnParticipationDisclosures(context.Context, agentprofile.PrivateAccess, bool) (View, error)
	PreviewOwnParticipationDisclosure(context.Context, agentprofile.PrivateAccess, Input) (Preview, error)
	ApproveOwnParticipationDisclosure(context.Context, agentprofile.PrivateAccess, string) (View, error)
}

func validID(id string) bool {
	p, e := actorref.ParsePrincipal("person", id)
	return e == nil && p.ID == id
}
func validateRecord(r Record, at time.Time) bool {
	if !validID(r.ParticipationID) || !validID(r.ActivityID) || r.Title == "" || r.Attendance != "UNKNOWN" || (r.Status != "going" && r.Status != "pending" && r.Status != "cancelled") || (r.Visibility != "PUBLIC" && r.Visibility != "PRIVATE") {
		return false
	}
	if !r.SourceAvailable && (r.Title != "已不可公开展示的活动报名" || r.StartsAt != nil || r.EndsAt != nil || r.SourceExpiresAt != nil) {
		return false
	}
	if r.SourceAvailable && (r.Status != "going" || r.StartsAt == nil || r.EndsAt == nil || r.SourceExpiresAt == nil || !r.StartsAt.Before(*r.EndsAt) || !r.SourceExpiresAt.After(at) || r.SourceExpiresAt.After(*r.EndsAt)) {
		return false
	}
	if r.Visibility == "PRIVATE" && (r.DisclosureExpiresAt != nil || r.EffectivePublic) {
		return false
	}
	if r.Visibility == "PUBLIC" && r.DisclosureExpiresAt == nil {
		return false
	}
	if r.EffectivePublic && (!r.SourceAvailable || r.Visibility != "PUBLIC" || !r.DisclosureExpiresAt.After(at) || r.DisclosureExpiresAt.After(*r.SourceExpiresAt)) {
		return false
	}
	return true
}
func ValidateView(v View, owner string) error {
	if v.SchemaVersion != Schema || v.OwnerID != owner || !validID(owner) || !validID(v.AgentID) || v.ObservedAt.IsZero() || v.Limit != 100 || len(v.Records) > 100 || v.Records == nil || v.ModelAccess || v.SendAllowed || v.MembershipGranted {
		return ErrUnavailable
	}
	seen := map[string]bool{}
	for _, r := range v.Records {
		if !validateRecord(r, v.ObservedAt) || seen[r.ParticipationID] {
			return ErrUnavailable
		}
		seen[r.ParticipationID] = true
	}
	return nil
}
func ValidatePreview(p Preview, owner string, in Input) error {
	if p.SchemaVersion != Schema || p.OwnerID != owner || !validID(owner) || !validID(p.AgentID) || p.ParticipationID != in.ParticipationID || p.Operation != in.Operation || p.TargetVisibility != in.Operation || !reflectExpiry(p.TargetExpiresAt, in.DisclosureExpiresAt) || p.ObservedAt.IsZero() || !p.ExpiresAt.After(p.ObservedAt) || p.ExpiresAt.After(p.ObservedAt.Add(90*time.Second)) || len(p.Preview) < 24 || len(p.Preview) > 24000 || p.Consequence == "" || p.ModelAccess || p.SendAllowed || p.MembershipGranted || !validateRecord(p.Record, p.ObservedAt) {
		return ErrUnavailable
	}
	if p.Operation == "PUBLIC" && (!p.SourceAvailable || p.TargetExpiresAt == nil || !p.TargetExpiresAt.After(p.ObservedAt) || p.TargetExpiresAt.After(p.ObservedAt.Add(24*time.Hour)) || p.TargetExpiresAt.After(*p.SourceExpiresAt) || p.ExpiresAt.After(*p.TargetExpiresAt)) {
		return ErrUnavailable
	}
	return nil
}
func reflectExpiry(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}

func ValidateInput(in Input) error {
	p, e := actorref.ParsePrincipal("person", in.ParticipationID)
	if e != nil || p.ID != strings.ToLower(in.ParticipationID) || (in.Operation != "PUBLIC" && in.Operation != "PRIVATE") {
		return ErrInvalid
	}
	if in.Operation == "PUBLIC" {
		if in.DisclosureExpiresAt == nil || in.DisclosureExpiresAt.IsZero() {
			return ErrInvalid
		}
	} else if in.DisclosureExpiresAt != nil {
		return ErrInvalid
	}
	return nil
}

// Closed JSON: duplicate, null, unknown, trailing data and client confirmation
// are never a concrete server-owned preview.
func DecodeBody(raw []byte, approve bool) (Input, string, error) {
	var in Input
	if len(raw) == 0 || len(raw) > 32768 {
		return in, "", ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return in, "", ErrInvalid
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return in, "", ErrInvalid
		}
		s, ok := k.(string)
		if !ok {
			return in, "", ErrInvalid
		}
		if _, ok = fields[s]; ok {
			return in, "", ErrInvalid
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return in, "", ErrInvalid
		}
		fields[s] = v
	}
	if _, e = d.Token(); e != nil {
		return in, "", ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return in, "", ErrInvalid
	}
	if approve {
		var token string
		if len(fields) != 1 || json.Unmarshal(fields["preview"], &token) != nil || len(token) < 24 || len(token) > 24000 {
			return in, "", ErrInvalid
		}
		return in, token, nil
	}
	for k, v := range fields {
		switch k {
		case "participationId":
			e = json.Unmarshal(v, &in.ParticipationID)
		case "operation":
			e = json.Unmarshal(v, &in.Operation)
		case "disclosureExpiresAt":
			var at time.Time
			e = json.Unmarshal(v, &at)
			at = at.UTC().Truncate(time.Microsecond)
			in.DisclosureExpiresAt = &at
		default:
			return in, "", ErrInvalid
		}
		if e != nil {
			return in, "", ErrInvalid
		}
	}
	if e = ValidateInput(in); e != nil {
		return in, "", e
	}
	return in, "", nil
}
