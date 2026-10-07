// Package businessconsole describes ordinary human merchant management.
// Neither an Agent nor an Organization role grants Business authority.
package businessconsole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

var (
	ErrInvalid     = errors.New("invalid business console input")
	ErrForbidden   = errors.New("business console access forbidden")
	ErrConflict    = errors.New("business console version changed")
	ErrNotFound    = errors.New("business console resource not found")
	ErrUnavailable = errors.New("business console unavailable")
)

const MaxBodyBytes = 24576
const MaxBusinesses = 100

// Access is constructed only from the verified native session and route.
type Access struct {
	SessionDigest  [32]byte `json:"-"`
	ActingPersonID string   `json:"-"`
	BusinessID     string   `json:"-"`
}

func (Access) MarshalJSON() ([]byte, error) { return nil, ErrForbidden }
func (a *Access) UnmarshalJSON([]byte) error {
	if a != nil {
		*a = Access{}
	}
	return ErrForbidden
}
func ValidID(id string) bool {
	normalized, e := agentmemory.NormalizeMemoryID(id)
	return e == nil && normalized == id
}
func ValidateAccess(a Access, businessRequired bool) error {
	if a.SessionDigest == ([32]byte{}) || !ValidID(a.ActingPersonID) || (businessRequired && !ValidID(a.BusinessID)) || (a.BusinessID != "" && !ValidID(a.BusinessID)) {
		return ErrForbidden
	}
	return nil
}
func boundedText(s string, min, max int) bool {
	return utf8.ValidString(s) && strings.TrimSpace(s) == s && utf8.RuneCountInString(s) >= min && utf8.RuneCountInString(s) <= max && !strings.ContainsRune(s, '\x00')
}
func HTTPS(s string) bool {
	u, e := url.Parse(s)
	return e == nil && len(s) <= 2048 && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && !strings.ContainsAny(s, "\x00\r\n\t ")
}

type HoursDay struct {
	Day      int    `json:"day"`
	Closed   bool   `json:"closed"`
	OpensAt  string `json:"opensAt"`
	ClosesAt string `json:"closesAt"`
	NextDay  bool   `json:"nextDay"`
}
type ProfileFacts struct {
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	TimeZone      string     `json:"timeZone"`
	OpeningHours  []HoursDay `json:"openingHours"`
	OfficialLinks []string   `json:"officialLinks"`
}

func ValidateProfile(f ProfileFacts) error {
	if !boundedText(f.Name, 1, 160) || !boundedText(f.Description, 0, 3000) || len(f.OpeningHours) > 7 || len(f.OfficialLinks) > 8 || f.OpeningHours == nil || f.OfficialLinks == nil {
		return ErrInvalid
	}
	if f.TimeZone != "" {
		// Go's Local pseudo-zone depends on the server process and is not
		// a declared business time zone accepted by the native schema.
		if len(f.TimeZone) > 100 || f.TimeZone == "Local" {
			return ErrInvalid
		}
		if _, e := time.LoadLocation(f.TimeZone); e != nil {
			return ErrInvalid
		}
	}
	if len(f.OpeningHours) > 0 && f.TimeZone == "" {
		return ErrInvalid
	}
	days := map[int]bool{}
	for _, d := range f.OpeningHours {
		if d.Day < 1 || d.Day > 7 || days[d.Day] {
			return ErrInvalid
		}
		days[d.Day] = true
		if d.Closed {
			if d.OpensAt != "" || d.ClosesAt != "" || d.NextDay {
				return ErrInvalid
			}
			continue
		}
		o, oe := time.Parse("15:04", d.OpensAt)
		c, ce := time.Parse("15:04", d.ClosesAt)
		if oe != nil || ce != nil || len(d.OpensAt) != 5 || len(d.ClosesAt) != 5 || (!d.NextDay && !c.After(o)) || (d.NextDay && c.After(o)) {
			return ErrInvalid
		}
	}
	seen := map[string]bool{}
	for _, link := range f.OfficialLinks {
		if !HTTPS(link) || seen[link] {
			return ErrInvalid
		}
		seen[link] = true
	}
	return nil
}

type ClaimInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	SourceURL       string `json:"sourceUrl"`
	RightsNote      string `json:"rightsNote"`
}

func ValidateClaim(i ClaimInput) error {
	if i.ExpectedVersion < 0 || i.ExpectedVersion == int64(^uint64(0)>>1) || !boundedText(i.Name, 1, 160) || !boundedText(i.Description, 0, 3000) || !HTTPS(i.SourceURL) || !boundedText(i.RightsNote, 1, 2000) {
		return ErrInvalid
	}
	return nil
}

type ProfileInput struct {
	ExpectedVersion int64        `json:"expectedVersion"`
	Facts           ProfileFacts `json:"facts"`
	SourceURL       string       `json:"sourceUrl"`
	RightsNote      string       `json:"rightsNote"`
	ValidUntil      time.Time    `json:"validUntil"`
}

func ValidateProfileInput(i ProfileInput, now time.Time) error {
	if i.ExpectedVersion < 0 || i.ExpectedVersion == int64(^uint64(0)>>1) || ValidateProfile(i.Facts) != nil || !HTTPS(i.SourceURL) || !boundedText(i.RightsNote, 1, 2000) || i.ValidUntil.UTC().Year() < 1 || i.ValidUntil.UTC().Year() > 9999 || !i.ValidUntil.After(now) || i.ValidUntil.After(now.AddDate(1, 0, 1)) {
		return ErrInvalid
	}
	return nil
}

type VenueFacts struct {
	Suitability []string `json:"suitability"`
	BookingURL  string   `json:"bookingUrl"`
	Note        string   `json:"note"`
}

func ValidateVenueFacts(f VenueFacts) error {
	if f.Suitability == nil || len(f.Suitability) > 16 || !boundedText(f.Note, 0, 2000) || (f.BookingURL != "" && !HTTPS(f.BookingURL)) {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, label := range f.Suitability {
		if !boundedText(label, 1, 80) || seen[label] {
			return ErrInvalid
		}
		seen[label] = true
	}
	return nil
}

type VenueInput struct {
	ExpectedVersion int64      `json:"expectedVersion"`
	Facts           VenueFacts `json:"facts"`
	SourceURL       string     `json:"sourceUrl"`
	RightsNote      string     `json:"rightsNote"`
	ValidUntil      time.Time  `json:"validUntil"`
}

func ValidateVenueInput(i VenueInput, now time.Time) error {
	if i.ExpectedVersion < 0 || i.ExpectedVersion == int64(^uint64(0)>>1) || ValidateVenueFacts(i.Facts) != nil || !HTTPS(i.SourceURL) || !boundedText(i.RightsNote, 1, 2000) || i.ValidUntil.UTC().Year() < 1 || i.ValidUntil.UTC().Year() > 9999 || !i.ValidUntil.After(now) || i.ValidUntil.After(now.AddDate(1, 0, 1)) {
		return ErrInvalid
	}
	return nil
}

type ReviewInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	Decision        string `json:"decision"`
	Note            string `json:"note"`
}

func ValidateReview(i ReviewInput) error {
	if i.ExpectedVersion < 1 || i.ExpectedVersion == int64(^uint64(0)>>1) || (i.Decision != "approve" && i.Decision != "reject" && i.Decision != "revoke") || !boundedText(i.Note, 1, 2000) {
		return ErrInvalid
	}
	return nil
}

type MemberInput struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	TargetPersonID  string `json:"targetPersonId"`
	Action          string `json:"action"`
	Role            string `json:"role"`
}

func ValidateMember(i MemberInput) error {
	if i.ExpectedVersion < 0 || i.ExpectedVersion == int64(^uint64(0)>>1) || !ValidID(i.TargetPersonID) {
		return ErrInvalid
	}
	switch i.Action {
	case "grant":
		if i.Role != "admin" && i.Role != "member" {
			return ErrInvalid
		}
	case "remove":
		if i.Role != "" {
			return ErrInvalid
		}
	case "transfer_owner":
		if i.Role != "owner" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type Business struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ClaimStatus string `json:"claimStatus"`
	Role        string `json:"role"`
}
type ReviewMetadata struct {
	Version     int64      `json:"version"`
	State       string     `json:"state"`
	SourceURL   string     `json:"sourceUrl"`
	RightsNote  string     `json:"rightsNote"`
	SubmittedBy string     `json:"submittedBy"`
	ReviewedBy  *string    `json:"reviewedBy"`
	ReviewedAt  *time.Time `json:"reviewedAt"`
	ReviewNote  string     `json:"reviewNote"`
}
type Claim struct {
	ReviewMetadata
	Name        string `json:"name"`
	Description string `json:"description"`
}
type Profile struct {
	ReviewMetadata
	Facts      ProfileFacts `json:"facts"`
	ValidUntil time.Time    `json:"validUntil"`
}
type Venue struct {
	ReviewMetadata
	PlaceID         string     `json:"placeId"`
	PlaceName       string     `json:"placeName"`
	Facts           VenueFacts `json:"facts"`
	ValidUntil      time.Time  `json:"validUntil"`
	OperationStatus string     `json:"operationStatus"`
}
type Member struct {
	PersonID string `json:"personId"`
	Role     string `json:"role"`
	Status   string `json:"status"`
}
type Console struct {
	Business          Business `json:"business"`
	Claim             *Claim   `json:"claim"`
	Profile           *Profile `json:"profile"`
	Venues            []Venue  `json:"venues"`
	Members           []Member `json:"members"`
	MembershipVersion int64    `json:"membershipVersion"`
	CanManage         bool     `json:"canManage"`
	CanReview         bool     `json:"canReview"`
	ReviewPermissions []string `json:"reviewPermissions"`
	CanManageMembers  bool     `json:"canManageMembers"`
}
type Store interface {
	ValidateBusinessAccess(context.Context, Access) (time.Time, error)
	ListBusinessConsoles(context.Context, Access) ([]Business, error)
	ReadBusinessConsole(context.Context, Access) (Console, error)
	SubmitBusinessClaim(context.Context, Access, ClaimInput) (Claim, error)
	ReviewBusinessClaim(context.Context, Access, ReviewInput) (Claim, error)
	PutBusinessProfile(context.Context, Access, ProfileInput) (Profile, error)
	ReviewBusinessProfile(context.Context, Access, ReviewInput) (Profile, error)
	PutBusinessVenueFacts(context.Context, Access, string, VenueInput) (Venue, error)
	ReviewBusinessVenueFacts(context.Context, Access, string, ReviewInput) (Venue, error)
	ChangeBusinessMember(context.Context, Access, MemberInput) (Console, error)
}

// StrictObject rejects duplicate keys throughout the bounded document.
// Every field is mandatory (including explicit empty arrays); null/unknown
// fields cannot manufacture a reviewer, identity or approval.
func StrictObject(raw []byte, keys ...string) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes {
		return nil, ErrInvalid
	}
	if !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	nodes := 0
	value, e := readJSONValue(decoder, 0, &nodes)
	if e != nil {
		return nil, ErrInvalid
	}
	if _, e = decoder.Token(); e != io.EOF {
		return nil, ErrInvalid
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, ErrInvalid
	}
	normalized, e := json.Marshal(value)
	if e != nil {
		return nil, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(normalized, &fields) != nil || len(fields) != len(keys) {
		return nil, ErrInvalid
	}
	for _, k := range keys {
		v, ok := fields[k]
		if !ok || string(v) == "null" {
			return nil, ErrInvalid
		}
	}
	return normalized, nil
}
func readJSONValue(d *json.Decoder, depth int, nodes *int) (any, error) {
	*nodes++
	if depth > 10 || *nodes > 1024 {
		return nil, ErrInvalid
	}
	token, e := d.Token()
	if e != nil {
		return nil, ErrInvalid
	}
	delimiter, complex := token.(json.Delim)
	if !complex {
		return token, nil
	}
	switch delimiter {
	case '{':
		values := map[string]any{}
		for d.More() {
			key, e := d.Token()
			k, ok := key.(string)
			if e != nil || !ok || len(k) > 100 {
				return nil, ErrInvalid
			}
			if _, exists := values[k]; exists {
				return nil, ErrInvalid
			}
			v, e := readJSONValue(d, depth+1, nodes)
			if e != nil {
				return nil, e
			}
			values[k] = v
		}
		end, e := d.Token()
		if e != nil || end != json.Delim('}') {
			return nil, ErrInvalid
		}
		return values, nil
	case '[':
		values := []any{}
		for d.More() {
			v, e := readJSONValue(d, depth+1, nodes)
			if e != nil {
				return nil, e
			}
			values = append(values, v)
		}
		end, e := d.Token()
		if e != nil || end != json.Delim(']') {
			return nil, ErrInvalid
		}
		return values, nil
	default:
		return nil, ErrInvalid
	}
}
func DecodeClaim(raw []byte) (ClaimInput, error) {
	n, e := StrictObject(raw, "expectedVersion", "name", "description", "sourceUrl", "rightsNote")
	var i ClaimInput
	if e != nil || json.Unmarshal(n, &i) != nil || ValidateClaim(i) != nil {
		return i, ErrInvalid
	}
	return i, nil
}
func DecodeReview(raw []byte) (ReviewInput, error) {
	n, e := StrictObject(raw, "expectedVersion", "decision", "note")
	var i ReviewInput
	if e != nil || json.Unmarshal(n, &i) != nil || ValidateReview(i) != nil {
		return i, ErrInvalid
	}
	return i, nil
}
func DecodeMember(raw []byte) (MemberInput, error) {
	n, e := StrictObject(raw, "expectedVersion", "targetPersonId", "action", "role")
	var i MemberInput
	if e != nil || json.Unmarshal(n, &i) != nil || ValidateMember(i) != nil {
		return i, ErrInvalid
	}
	return i, nil
}
func DecodeProfile(raw []byte) (ProfileInput, error) {
	n, e := StrictObject(raw, "expectedVersion", "facts", "sourceUrl", "rightsNote", "validUntil")
	var i ProfileInput
	if e != nil || json.Unmarshal(n, &i) != nil {
		return i, ErrInvalid
	}
	var f map[string]json.RawMessage
	_ = json.Unmarshal(n, &f)
	if _, e = StrictObject(f["facts"], "name", "description", "timeZone", "openingHours", "officialLinks"); e != nil {
		return i, e
	}
	var facts map[string]json.RawMessage
	_ = json.Unmarshal(f["facts"], &facts)
	var days []json.RawMessage
	if json.Unmarshal(facts["openingHours"], &days) != nil {
		return i, ErrInvalid
	}
	for _, day := range days {
		if _, e = StrictObject(day, "day", "closed", "opensAt", "closesAt", "nextDay"); e != nil {
			return i, e
		}
	}
	if ValidateProfile(i.Facts) != nil {
		return i, ErrInvalid
	}
	return i, nil
}
func DecodeVenue(raw []byte) (VenueInput, error) {
	n, e := StrictObject(raw, "expectedVersion", "facts", "sourceUrl", "rightsNote", "validUntil")
	var i VenueInput
	if e != nil || json.Unmarshal(n, &i) != nil {
		return i, ErrInvalid
	}
	var f map[string]json.RawMessage
	_ = json.Unmarshal(n, &f)
	if _, e = StrictObject(f["facts"], "suitability", "bookingUrl", "note"); e != nil {
		return i, e
	}
	if ValidateVenueFacts(i.Facts) != nil {
		return i, ErrInvalid
	}
	return i, nil
}
