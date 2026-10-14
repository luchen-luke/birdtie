package cityseed

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrSourcePermission = errors.New("place source retention or publication permission not confirmed")

const PermissionConfirmed = "CONFIRMED"
const PermissionPending = "PENDING"
const PermissionProhibited = "PROHIBITED"

type SourcePermissionInput struct {
	State          string    `json:"state"`
	Acquisition    string    `json:"acquisition"`
	OriginProvider string    `json:"originProvider"`
	LicenseURL     string    `json:"licenseUrl"`
	LicenseVersion string    `json:"licenseVersion"`
	Attribution    string    `json:"attribution"`
	AcquiredAt     time.Time `json:"acquiredAt"`
	RetentionUntil time.Time `json:"retentionUntil"`
}
type SourcePermission struct {
	ID          string `json:"id"`
	CandidateID string `json:"candidateId"`
	Version     int64  `json:"version"`
	SourcePermissionInput
	SourceURL  string     `json:"sourceUrl"`
	DeclaredBy string     `json:"declaredBy"`
	ReviewedBy *string    `json:"reviewedBy,omitempty"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}
type PlaceFieldInput struct {
	Key           string          `json:"key"`
	Value         json.RawMessage `json:"value"`
	ObservedAt    time.Time       `json:"observedAt"`
	ValidFrom     time.Time       `json:"validFrom"`
	ValidTo       time.Time       `json:"validTo"`
	RefreshDue    time.Time       `json:"refreshDue"`
	ConflictState string          `json:"conflictState"`
}
type PlaceFieldEvidence struct {
	ID                string           `json:"id"`
	PlaceID           string           `json:"placeId"`
	SourceCandidateID string           `json:"sourceCandidateId"`
	FieldKey          string           `json:"fieldKey"`
	Value             json.RawMessage  `json:"value"`
	ObservedAt        time.Time        `json:"observedAt"`
	VerifiedAt        time.Time        `json:"verifiedAt"`
	ValidFrom         time.Time        `json:"validFrom"`
	ValidTo           time.Time        `json:"validTo"`
	RefreshDue        time.Time        `json:"refreshDue"`
	PermissionID      string           `json:"permissionId"`
	PermissionVersion int64            `json:"permissionVersion"`
	Version           int64            `json:"version"`
	ConflictState     string           `json:"conflictState"`
	Permission        SourcePermission `json:"permission"`
	RowToken          string           `json:"-"`
}
type HumanSession struct {
	ActorID string
	Digest  [32]byte
}
type humanSessionKey struct{}

func WithHumanSession(ctx context.Context, actorID string, digest [32]byte) context.Context {
	return context.WithValue(ctx, humanSessionKey{}, HumanSession{ActorID: actorID, Digest: digest})
}
func CurrentHumanSession(ctx context.Context) (HumanSession, bool) {
	if ctx == nil {
		return HumanSession{}, false
	}
	s, ok := ctx.Value(humanSessionKey{}).(HumanSession)
	return s, ok && s.ActorID != "" && s.Digest != ([32]byte{})
}

func SourceTextValid(s string, max int) bool {
	if s == "" || len(s) > max || s != strings.TrimSpace(s) || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || unicode.In(r, unicode.Cf) {
			return false
		}
	}
	return true
}
func SourceURLValid(s string) bool {
	u, e := url.Parse(s)
	return e == nil && SourceTextValid(s, 1000) && u.Scheme == "https" && u.Hostname() != "" && u.User == nil && u.Fragment == "" && strings.Contains(u.Hostname(), ".")
}
func RestrictedSearchOrigin(provider, source string) bool {
	p := strings.ToLower(provider)
	p = strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(p)
	if strings.Contains(p, "tencent") || strings.Contains(p, "searchpro") || p == "wsa" {
		return true
	}
	u, e := url.Parse(source)
	if e != nil {
		return true
	}
	h := strings.ToLower(u.Hostname())
	return h == "tokenhub.tencentmaas.com" || strings.HasSuffix(h, ".tencentmaas.com") || h == "wsa.tencentcloudapi.com" || h == "wsa.tencentcloudapi.com.cn" || h == "searchpro.tencentcloudapi.com"
}

// CONFIRMED is an explicit source-rights declaration, not independent review.
// Retained/public use is additionally gated by the separate native reviewer.
func NormalizeSourcePermission(input SubmitInput, now time.Time) (SourcePermissionInput, []PlaceFieldInput, error) {
	if input.Rights == nil {
		return SourcePermissionInput{}, nil, ErrSourcePermission
	}
	r := *input.Rights
	if r.State != PermissionConfirmed || !SourceURLValid(input.SourceURL) || !SourceURLValid(r.LicenseURL) || !SourceTextValid(r.LicenseVersion, 160) || !SourceTextValid(r.Attribution, 500) || r.AcquiredAt.IsZero() || r.AcquiredAt.After(now) || !r.RetentionUntil.After(now) || r.RetentionUntil.Before(input.ExpiresAt) || RestrictedSearchOrigin(input.ProviderCode, input.SourceURL) || RestrictedSearchOrigin(r.OriginProvider, input.SourceURL) {
		return SourcePermissionInput{}, nil, ErrSourcePermission
	}
	if input.ProviderCode != "" {
		if !SourceTextValid(input.ProviderCode, 80) {
			return SourcePermissionInput{}, nil, ErrSourcePermission
		}
		for _, c := range input.ProviderCode {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-') {
				return SourcePermissionInput{}, nil, ErrSourcePermission
			}
		}
	}
	if v := strings.ToLower(r.LicenseVersion); v == "unknown" || v == "pending" || v == "unverified" {
		return SourcePermissionInput{}, nil, ErrSourcePermission
	}
	switch r.Acquisition {
	case "FIRST_PARTY":
		if r.OriginProvider != "BIRDTIE_FIRST_PARTY" {
			return SourcePermissionInput{}, nil, ErrSourcePermission
		}
	case "DIRECT_AUTHORIZATION":
		if r.OriginProvider != "DIRECT_SOURCE" {
			return SourcePermissionInput{}, nil, ErrSourcePermission
		}
	case "OPEN_LICENSE":
		if r.OriginProvider != "DIRECT_SOURCE" && r.OriginProvider != "OPEN_DATA_DOWNLOAD" {
			return SourcePermissionInput{}, nil, ErrSourcePermission
		}
	default:
		return SourcePermissionInput{}, nil, ErrSourcePermission
	}
	if !input.ExpiresAt.After(now) || len(input.Fields) < 1 || len(input.Fields) > 6 {
		return SourcePermissionInput{}, nil, ErrSourcePermission
	}
	fields := append([]PlaceFieldInput{}, input.Fields...)
	seen := map[string]bool{}
	for i, f := range fields {
		if seen[f.Key] || !ValidPlaceFieldValue(f.Key, f.Value) || f.ObservedAt.IsZero() || f.ObservedAt.After(now) || f.ValidFrom.IsZero() || !f.ValidTo.After(f.ValidFrom) || !f.ValidTo.After(now) || f.ValidTo.After(r.RetentionUntil) || f.ValidTo.After(input.ExpiresAt) || f.RefreshDue.IsZero() || f.RefreshDue.Before(f.ValidFrom) || f.RefreshDue.After(f.ValidTo) || (f.ConflictState != "NONE" && f.ConflictState != "UNRESOLVED") {
			return SourcePermissionInput{}, nil, ErrSourcePermission
		}
		seen[f.Key] = true
		f.ObservedAt = f.ObservedAt.UTC().Truncate(time.Microsecond)
		f.ValidFrom = f.ValidFrom.UTC().Truncate(time.Microsecond)
		f.ValidTo = f.ValidTo.UTC().Truncate(time.Microsecond)
		f.RefreshDue = f.RefreshDue.UTC().Truncate(time.Microsecond)
		fields[i] = f
		if f.Key == "name" {
			var v string
			json.Unmarshal(f.Value, &v)
			if v != input.Name {
				return SourcePermissionInput{}, nil, ErrSourcePermission
			}
		}
		if f.Key == "address" {
			var v string
			json.Unmarshal(f.Value, &v)
			if v != input.AddressLabel {
				return SourcePermissionInput{}, nil, ErrSourcePermission
			}
		}
		if f.Key == "coordinates" {
			var v struct {
				CoordinateSystem    string `json:"coordinateSystem"`
				Latitude, Longitude float64
			}
			json.Unmarshal(f.Value, &v)
			if input.Latitude == nil || input.Longitude == nil || v.Latitude != *input.Latitude || v.Longitude != *input.Longitude || input.LocationPrecision != "point" {
				return SourcePermissionInput{}, nil, ErrSourcePermission
			}
		}
	}
	if !seen["name"] || (input.AddressLabel != "" && !seen["address"]) || (input.Latitude != nil && !seen["coordinates"]) {
		return SourcePermissionInput{}, nil, ErrSourcePermission
	}
	r.AcquiredAt = r.AcquiredAt.UTC().Truncate(time.Microsecond)
	r.RetentionUntil = r.RetentionUntil.UTC().Truncate(time.Microsecond)
	return r, fields, nil
}
func ValidPlaceFieldKey(key string) bool {
	switch key {
	case "name", "address", "coordinates", "food_seafood", "food_spicy", "opening_hours":
		return true
	}
	return false
}
func ValidPlaceFieldValue(key string, raw json.RawMessage) bool {
	if !ValidPlaceFieldKey(key) || len(raw) == 0 || len(raw) > 2000 || !json.Valid(raw) {
		return false
	}
	switch key {
	case "name", "address", "opening_hours":
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return false
		}
		return SourceTextValid(s, 1200)
	case "food_seafood", "food_spicy":
		return string(raw) == "true" || string(raw) == "false"
	case "coordinates":
		var o map[string]json.RawMessage
		if json.Unmarshal(raw, &o) != nil || len(o) != 3 {
			return false
		}
		var c string
		var lat, lng float64
		if json.Unmarshal(o["coordinateSystem"], &c) != nil || c != "wgs84" || json.Unmarshal(o["latitude"], &lat) != nil || json.Unmarshal(o["longitude"], &lng) != nil {
			return false
		}
		return !math.IsNaN(lat) && !math.IsInf(lat, 0) && !math.IsNaN(lng) && !math.IsInf(lng, 0) && lat >= -90 && lat <= 90 && lng >= -180 && lng <= 180
	}
	return false
}
func PermissionUsable(p SourcePermission, at time.Time) bool {
	return p.Version > 0 && p.State == PermissionConfirmed && p.ReviewedBy != nil && *p.ReviewedBy != "" && *p.ReviewedBy != p.DeclaredBy && p.ReviewedAt != nil && !p.ReviewedAt.After(at) && p.RevokedAt == nil && p.AcquiredAt.Before(at.Add(time.Microsecond)) && p.RetentionUntil.After(at)
}
