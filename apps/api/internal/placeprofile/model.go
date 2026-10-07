package placeprofile

import (
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const SchemaVersion = "place-semantic-v1"

var (
	ErrInvalid     = errors.New("地点语义资料无效")
	ErrDenied      = errors.New("当前会话或城市编辑权限无效")
	ErrConflict    = errors.New("资料版本已变化，请重新读取")
	ErrNotFound    = errors.New("没有仍公开有效的地点语义资料")
	ErrUnavailable = errors.New("地点语义资料暂不可用")
)

var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var code = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)
var currency = regexp.MustCompile(`^[A-Z]{3}$`)

type Price struct {
	Currency string `json:"currency"`
	MinMinor int64  `json:"minMinor"`
	MaxMinor int64  `json:"maxMinor"`
	Unit     string `json:"unit"`
}
type Accessibility struct {
	StepFree         string `json:"stepFree"`
	AccessibleToilet string `json:"accessibleToilet"`
}
type GroupSize struct {
	Min int `json:"min"`
	Max int `json:"max"`
}
type Reservation struct {
	Support string  `json:"support"`
	URL     *string `json:"url"`
}

// All non-nil facts are explicitly submitted public declarations from Source.
// nil is UNKNOWN, never false/free/accessibility/booking evidence. Capacity and
// Venue authorization stay in the original Venue ledger.
type Facts struct {
	Vibe          []string       `json:"vibe"`
	GoodFor       []string       `json:"good_for"`
	Price         *Price         `json:"price"`
	Accessibility *Accessibility `json:"accessibility"`
	GroupSize     *GroupSize     `json:"group_size"`
	Reservation   *Reservation   `json:"reservation"`
	Suitability   []string       `json:"suitability"`
}

type SourceInput struct {
	Label      string    `json:"label"`
	URL        string    `json:"url"`
	RightsNote string    `json:"rightsNote"`
	ObservedAt time.Time `json:"observedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type Assessment struct {
	Kind  string `json:"kind"`
	Level string `json:"level"`
}
type SubmitInput struct {
	OperationID     string      `json:"operationId"`
	ExpectedVersion int64       `json:"expectedVersion"`
	Facts           Facts       `json:"facts"`
	Source          SourceInput `json:"source"`
	Confidence      Assessment  `json:"confidence"`
}
type ReviewInput struct {
	Decision         string `json:"decision"`
	CandidateVersion int64  `json:"candidateVersion"`
	ExpectedVersion  int64  `json:"expectedVersion"`
	Note             string `json:"note"`
}
type WithdrawInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Note            string `json:"note"`
}

type Candidate struct {
	ID              string      `json:"id"`
	PlaceID         string      `json:"placeId"`
	CityID          string      `json:"cityId"`
	Version         int64       `json:"version"`
	ExpectedVersion int64       `json:"expectedVersion"`
	Facts           Facts       `json:"facts"`
	Source          SourceInput `json:"source"`
	Confidence      Assessment  `json:"confidence"`
	Status          string      `json:"status"`
	CreatedAt       time.Time   `json:"createdAt"`
	ReviewedAt      *time.Time  `json:"reviewedAt"`
}
type Source struct {
	Label      string    `json:"label"`
	URL        string    `json:"url"`
	ObservedAt time.Time `json:"observedAt"`
	ReviewedAt time.Time `json:"reviewedAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type Public struct {
	SchemaVersion string     `json:"schemaVersion"`
	PlaceID       string     `json:"placeId"`
	CityID        string     `json:"cityId"`
	Version       int64      `json:"version"`
	Facts         Facts      `json:"facts"`
	Source        Source     `json:"source"`
	Confidence    Assessment `json:"confidence"`
	CheckedAt     time.Time  `json:"checkedAt"`
}
type Claim struct {
	State      string      `json:"state"`
	Value      any         `json:"value"`
	Source     *Source     `json:"source"`
	Confidence *Assessment `json:"confidence"`
}

// Claims makes the source/confidence inheritance explicit for every known
// field. Missing claims are UNKNOWN and inherit no source or confidence.
func (p Public) Claims() map[string]Claim {
	f := p.Facts
	values := map[string]any{"vibe": nil, "good_for": nil, "price": nil, "accessibility": nil, "group_size": nil, "reservation": nil, "suitability": nil}
	if len(f.Vibe) > 0 {
		values["vibe"] = f.Vibe
	}
	if len(f.GoodFor) > 0 {
		values["good_for"] = f.GoodFor
	}
	if f.Price != nil {
		values["price"] = *f.Price
	}
	if f.Accessibility != nil {
		values["accessibility"] = *f.Accessibility
	}
	if f.GroupSize != nil {
		values["group_size"] = *f.GroupSize
	}
	if f.Reservation != nil {
		values["reservation"] = *f.Reservation
	}
	if len(f.Suitability) > 0 {
		values["suitability"] = f.Suitability
	}
	out := make(map[string]Claim, 7)
	for k, v := range values {
		c := Claim{State: "UNKNOWN"}
		if v != nil {
			s, a := p.Source, p.Confidence
			c = Claim{State: "REVIEWED_DECLARATION", Value: v, Source: &s, Confidence: &a}
		}
		out[k] = c
	}
	return out
}

func ValidID(id string) bool { return uuid.MatchString(id) }
func validText(s string, min, max int) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) != s || len([]rune(s)) < min || len([]rune(s)) > max {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func ValidURL(s string) bool {
	if !validText(s, 1, 1000) || strings.ContainsAny(s, "@#") {
		return false
	}
	u, e := url.Parse(s)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.Fragment == "" && u.Opaque == ""
}
func validCodes(a []string) bool {
	if len(a) > 16 {
		return false
	}
	seen := map[string]bool{}
	for _, s := range a {
		if !code.MatchString(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}
func ValidateFacts(f Facts) error {
	if !validCodes(f.Vibe) || !validCodes(f.GoodFor) || !validCodes(f.Suitability) {
		return ErrInvalid
	}
	if len(f.Vibe)+len(f.GoodFor)+len(f.Suitability) == 0 && f.Price == nil && f.Accessibility == nil && f.GroupSize == nil && f.Reservation == nil {
		return ErrInvalid
	}
	if p := f.Price; p != nil {
		if !currency.MatchString(p.Currency) || p.MinMinor < 0 || p.MaxMinor < p.MinMinor || p.MaxMinor > 100000000 || (p.Unit != "per_person" && p.Unit != "per_visit" && p.Unit != "per_hour") {
			return ErrInvalid
		}
	}
	if a := f.Accessibility; a != nil {
		if (a.StepFree != "yes" && a.StepFree != "no" && a.StepFree != "unknown") || (a.AccessibleToilet != "yes" && a.AccessibleToilet != "no" && a.AccessibleToilet != "unknown") || (a.StepFree == "unknown" && a.AccessibleToilet == "unknown") {
			return ErrInvalid
		}
	}
	if g := f.GroupSize; g != nil {
		if g.Min < 1 || g.Max < g.Min || g.Max > 1000 {
			return ErrInvalid
		}
	}
	if r := f.Reservation; r != nil {
		switch r.Support {
		case "none", "contact":
			if r.URL != nil {
				return ErrInvalid
			}
		case "external_url":
			if r.URL == nil || !ValidURL(*r.URL) {
				return ErrInvalid
			}
		default:
			return ErrInvalid
		}
	}
	return nil
}
func ValidateAssessment(a Assessment) error {
	if a.Kind != "EDITOR_ASSESSMENT_UNCALIBRATED" || (a.Level != "LOW" && a.Level != "MEDIUM" && a.Level != "HIGH") {
		return ErrInvalid
	}
	return nil
}
func ValidateSubmit(in SubmitInput, now time.Time) error {
	if !ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion > 1000000000 || ValidateFacts(in.Facts) != nil || ValidateAssessment(in.Confidence) != nil || !validText(in.Source.Label, 2, 100) || !validText(in.Source.RightsNote, 10, 1000) || !ValidURL(in.Source.URL) || in.Source.ObservedAt.IsZero() || in.Source.ObservedAt.After(now) || !in.Source.ExpiresAt.After(now) || in.Source.ExpiresAt.After(now.Add(365*24*time.Hour)) {
		return ErrInvalid
	}
	return nil
}
func ValidateReview(in ReviewInput) error {
	if (in.Decision != "approve" && in.Decision != "reject") || in.CandidateVersion != 1 || in.ExpectedVersion < 0 || in.ExpectedVersion > 1000000000 || !validText(in.Note, 10, 1000) {
		return ErrInvalid
	}
	return nil
}
func ValidateWithdraw(in WithdrawInput) error {
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 1000000000 || !validText(in.Note, 10, 1000) {
		return ErrInvalid
	}
	return nil
}
func ValidatePublic(p Public, now time.Time) error {
	if p.SchemaVersion != SchemaVersion || !ValidID(p.PlaceID) || !validText(p.CityID, 1, 80) || p.Version < 1 || ValidateFacts(p.Facts) != nil || ValidateAssessment(p.Confidence) != nil || !validText(p.Source.Label, 2, 100) || !ValidURL(p.Source.URL) || p.Source.ObservedAt.IsZero() || p.Source.ObservedAt.After(p.Source.ReviewedAt) || p.Source.ReviewedAt.After(now) || !p.Source.ExpiresAt.After(now) {
		return ErrInvalid
	}
	return nil
}

// CanonicalFacts is bounded input to SQL storage/hash, not a model input.
func CanonicalFacts(f Facts) ([]byte, error) {
	if ValidateFacts(f) != nil {
		return nil, ErrInvalid
	}
	b, e := json.Marshal(f)
	if e != nil || len(b) > 8192 {
		return nil, ErrInvalid
	}
	return b, nil
}
