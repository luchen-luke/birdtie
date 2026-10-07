// Package sponsoredopportunity keeps reviewed commercial declarations separate
// from organic discovery. Neither a Business claim nor these records grant
// inference, private source, payment, bidding or publication authority elsewhere.
package sponsoredopportunity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

const Version = "sponsored-opportunity-v1"
const MaxBodyBytes = 16384

var (
	ErrInvalid     = errors.New("invalid sponsorship declaration")
	ErrForbidden   = errors.New("sponsorship authority denied")
	ErrConflict    = errors.New("sponsorship snapshot changed")
	ErrNotFound    = errors.New("sponsorship not found")
	ErrUnavailable = errors.New("sponsorship source unavailable")
)

type Access struct {
	ActorID, AccountType string
	SessionDigest        [32]byte
}

func (Access) MarshalJSON() ([]byte, error) { return nil, ErrForbidden }
func (a *Access) UnmarshalJSON([]byte) error {
	if a != nil {
		*a = Access{}
	}
	return ErrForbidden
}
func ValidID(id string) bool { return businessconsole.ValidID(id) }
func ValidateAccess(a Access, anonymous bool) error {
	if anonymous && a == (Access{}) {
		return nil
	}
	if !ValidID(a.ActorID) || a.SessionDigest == ([32]byte{}) || a.AccountType != "person" {
		return ErrForbidden
	}
	return nil
}
func text(s string, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) == s && utf8.RuneCountInString(s) > 0 && utf8.RuneCountInString(s) <= max && !strings.ContainsAny(s, "\x00\r\n\t")
}
func City(s string) bool { return len(s) > 0 && len(s) <= 80 && text(s, 80) }
func Snapshot(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, v := range s {
		if !(v >= '0' && v <= '9' || v >= 'a' && v <= 'f') {
			return false
		}
	}
	return true
}

type Target struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

func ValidateTarget(t Target) error {
	if !ValidID(t.ID) || (t.Type != "ACTIVITY" && t.Type != "PLACE") || !text(t.Title, 240) {
		return ErrInvalid
	}
	return nil
}

type Sponsor struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}
type Source struct {
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
	ReviewedAt time.Time `json:"reviewedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type Public struct {
	ID        string    `json:"id"`
	Revision  int64     `json:"revision"`
	Kind      string    `json:"kind"`
	Label     string    `json:"label"`
	Sponsor   Sponsor   `json:"sponsor"`
	Target    Target    `json:"target"`
	Source    Source    `json:"source"`
	CheckedAt time.Time `json:"checkedAt"`
}

func ValidatePublic(p Public) error {
	if !ValidID(p.ID) || p.Revision < 1 || p.Revision > 9007199254740991 || p.Kind != "SPONSORED" || p.Label != "赞助" || p.Sponsor.Type != "BUSINESS" || !ValidID(p.Sponsor.ID) || !text(p.Sponsor.Name, 160) || ValidateTarget(p.Target) != nil || !businessconsole.HTTPS(p.Source.URL) || !validTime(p.Source.ObservedAt) || !validTime(p.Source.ReviewedAt) || !validTime(p.Source.ExpiresAt) || !validTime(p.CheckedAt) || p.Source.ObservedAt.After(p.Source.ReviewedAt) || p.Source.ReviewedAt.After(p.CheckedAt) || !p.Source.ExpiresAt.After(p.CheckedAt) || p.Source.ExpiresAt.Sub(p.Source.ObservedAt) > 30*24*time.Hour {
		return ErrInvalid
	}
	return nil
}

type Disclosure struct {
	CommercialTrustVersion string   `json:"commercialTrustVersion"`
	SponsoredStatus        string   `json:"sponsoredStatus"`
	SponsoredOpportunities []Public `json:"sponsoredOpportunities"`
}

func Empty(available bool) Disclosure {
	state := "unavailable"
	if available {
		state = "available"
	}
	return Disclosure{Version, state, []Public{}}
}
func ValidateDisclosure(d Disclosure, eligible []Target) error {
	if d.CommercialTrustVersion != Version || (d.SponsoredStatus != "available" && d.SponsoredStatus != "unavailable") || d.SponsoredOpportunities == nil || len(d.SponsoredOpportunities) > 5 || (d.SponsoredStatus == "unavailable" && len(d.SponsoredOpportunities) > 0) {
		return ErrInvalid
	}
	targets := map[string]string{}
	for _, t := range eligible {
		if ValidateTarget(t) != nil {
			return ErrInvalid
		}
		targets[t.Type+":"+t.ID] = t.Title
	}
	ids := map[string]bool{}
	seen := map[string]bool{}
	for _, p := range d.SponsoredOpportunities {
		k := p.Target.Type + ":" + p.Target.ID
		if ValidatePublic(p) != nil || ids[p.ID] || seen[k] || targets[k] != p.Target.Title {
			return ErrInvalid
		}
		ids[p.ID] = true
		seen[k] = true
	}
	return nil
}

type SubmitInput struct {
	OperationID    string    `json:"operationId"`
	CityID         string    `json:"cityId"`
	TargetType     string    `json:"targetType"`
	TargetID       string    `json:"targetId"`
	Statement      string    `json:"statement"`
	SourceURL      string    `json:"sourceUrl"`
	RightsNote     string    `json:"rightsNote"`
	ObservedAt     time.Time `json:"observedAt"`
	ExpiresAt      time.Time `json:"expiresAt"`
	SourceSnapshot string    `json:"sourceSnapshot"`
	Confirmed      bool      `json:"confirmed"`
}

func validTime(t time.Time) bool { return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 }
func ValidateSubmit(i SubmitInput, now time.Time) error {
	if !ValidID(i.OperationID) || !City(i.CityID) || !ValidID(i.TargetID) || (i.TargetType != "ACTIVITY" && i.TargetType != "PLACE") || !text(i.Statement, 1000) || !businessconsole.HTTPS(i.SourceURL) || !text(i.RightsNote, 2000) || !Snapshot(i.SourceSnapshot) || !i.Confirmed || !validTime(i.ObservedAt) || !validTime(i.ExpiresAt) || i.ObservedAt.After(now) || i.ObservedAt.Before(now.Add(-30*24*time.Hour)) || !i.ExpiresAt.After(now) || i.ExpiresAt.After(i.ObservedAt.Add(30*24*time.Hour)) {
		return ErrInvalid
	}
	return nil
}

type ReviewInput struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	Snapshot         string `json:"snapshot"`
	Decision         string `json:"decision"`
	Note             string `json:"note"`
	Confirmed        bool   `json:"confirmed"`
}

func ValidateReview(i ReviewInput, revoke bool) error {
	if i.ExpectedRevision < 1 || i.ExpectedRevision >= 9007199254740991 || !Snapshot(i.Snapshot) || !text(i.Note, 2000) || !i.Confirmed || (!revoke && i.Decision != "approve" && i.Decision != "reject") || (revoke && i.Decision != "revoke") {
		return ErrInvalid
	}
	return nil
}

type Declaration struct {
	ID            string     `json:"id"`
	BusinessID    string     `json:"businessId"`
	CityID        string     `json:"cityId"`
	Revision      int64      `json:"revision"`
	TargetType    string     `json:"targetType"`
	TargetID      string     `json:"targetId"`
	Statement     string     `json:"statement"`
	SourceURL     string     `json:"sourceUrl"`
	RightsNote    string     `json:"rightsNote"`
	ObservedAt    time.Time  `json:"observedAt"`
	ExpiresAt     time.Time  `json:"expiresAt"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"createdAt"`
	ReviewedAt    *time.Time `json:"reviewedAt"`
	ReviewNote    string     `json:"reviewNote"`
	SourceCurrent bool       `json:"sourceCurrent"`
	Snapshot      string     `json:"snapshot"`
}
type EligibleTarget struct {
	Target         Target `json:"target"`
	CityID         string `json:"cityId"`
	SourceSnapshot string `json:"sourceSnapshot"`
}
type Management struct {
	Declarations []Declaration    `json:"declarations"`
	Targets      []EligibleTarget `json:"targets"`
}
type Store interface {
	SubmitSponsoredOpportunity(context.Context, Access, string, SubmitInput) (Declaration, bool, error)
	ListOwnSponsoredOpportunities(context.Context, Access, string) (Management, error)
	ListSponsoredOpportunityReview(context.Context, Access, string) ([]Declaration, error)
	ReviewSponsoredOpportunity(context.Context, Access, string, ReviewInput) (Declaration, error)
	RevokeSponsoredOpportunity(context.Context, Access, string, ReviewInput) (Declaration, error)
	ReadSponsoredOpportunities(context.Context, Access, []Target) (Disclosure, error)
}

// Closed decoder rejects duplicate keys, null values, unknown/case-alias fields,
// invalid UTF-8 and overlarge/deep JSON before typed decoding. No source/role
// fields are accepted as authority.
func decode(raw []byte, v any, keys []string) error {
	if len(raw) == 0 || len(raw) > MaxBodyBytes || !utf8.Valid(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	nodes := 0
	var parse func(int) (any, error)
	parse = func(depth int) (any, error) {
		nodes++
		if depth > 6 || nodes > 100 {
			return nil, ErrInvalid
		}
		t, e := d.Token()
		if e != nil || t == nil {
			return nil, ErrInvalid
		}
		if delim, ok := t.(json.Delim); ok {
			if delim != '{' {
				return nil, ErrInvalid
			}
			m := map[string]any{}
			for d.More() {
				key, e := d.Token()
				s, ok := key.(string)
				if e != nil || !ok {
					return nil, ErrInvalid
				}
				if _, ok = m[s]; ok {
					return nil, ErrInvalid
				}
				val, e := parse(depth + 1)
				if e != nil {
					return nil, e
				}
				m[s] = val
			}
			if _, e = d.Token(); e != nil {
				return nil, ErrInvalid
			}
			return m, nil
		}
		return t, nil
	}
	obj, e := parse(0)
	if e != nil {
		return ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return ErrInvalid
	}
	m, ok := obj.(map[string]any)
	if !ok || len(m) != len(keys) {
		return ErrInvalid
	}
	for _, k := range keys {
		if _, ok = m[k]; !ok {
			return ErrInvalid
		}
	}
	b, e := json.Marshal(obj)
	if e != nil {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if e = dec.Decode(v); e != nil {
		return ErrInvalid
	}
	return nil
}
func DecodeSubmit(raw []byte) (SubmitInput, error) {
	var i SubmitInput
	e := decode(raw, &i, []string{"operationId", "cityId", "targetType", "targetId", "statement", "sourceUrl", "rightsNote", "observedAt", "expiresAt", "sourceSnapshot", "confirmed"})
	return i, e
}
func DecodeReview(raw []byte) (ReviewInput, error) {
	var i ReviewInput
	e := decode(raw, &i, []string{"expectedRevision", "snapshot", "decision", "note", "confirmed"})
	return i, e
}
