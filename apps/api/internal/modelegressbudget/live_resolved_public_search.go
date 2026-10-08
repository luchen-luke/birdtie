package modelegressbudget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode"
)

// These scopes are separate from the original literal-query and v1 source
// scopes. Their values and hashes describe data, never approve an export.
const LiveResolvedSearchScope = "SELF_TASK_PUBLIC_RESOLVED_SEARCH"
const LiveResolvedSourceScope = "SELF_TASK_PUBLIC_RESOLVED_SEARCH_SOURCES"
const LiveResolvedPublicSearchSchema = "birdtie.public-search-context.v2"
const LiveResolvedPublicSearchMaxQueryBytes = 240

// LiveSelectedCity contains only the selected public city. Its ID is a city
// key, not a person, place or result reference. Native storage checks the
// current publication, context, expiry and retained row generations.
type LiveSelectedCity struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	CountryCode string `json:"countryCode"`
}

// LiveResolvedSlots is a closed projection of the current Task's resolved
// search values. There is deliberately no generic filters map, coordinate,
// principal, history, profile, private asset or result ID in this shape.
type LiveResolvedSlots struct {
	Operation          string `json:"operation"`
	Target             string `json:"target"`
	Category           string `json:"category"`
	TimePreference     string `json:"timePreference"`
	DistancePreference string `json:"distancePreference"`
	SearchTerm         string `json:"searchTerm"`
}

type LiveResolvedPublicSearchContext struct {
	SelectedCity  LiveSelectedCity  `json:"selectedCity"`
	ResolvedSlots LiveResolvedSlots `json:"resolvedSlots"`
}

// JSON can reconstruct ordinary public data only. Unknown/private fields are
// rejected, and a decoded context has no native authority or approval.
func (c *LiveResolvedPublicSearchContext) UnmarshalJSON(raw []byte) error {
	if c == nil {
		return ErrInvalid
	}
	*c = LiveResolvedPublicSearchContext{}
	if len(raw) == 0 || len(raw) > 2048 {
		return ErrInvalid
	}
	type contextData LiveResolvedPublicSearchContext
	var data contextData
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&data) != nil || d.Decode(new(any)) != io.EOF {
		return ErrInvalid
	}
	next := LiveResolvedPublicSearchContext(data)
	if ValidateLiveResolvedPublicSearchContext(next) != nil {
		return ErrInvalid
	}
	*c = next
	return nil
}

func resolvedPublicText(value string, max int, empty bool) bool {
	if !sourceText(value, max, empty) || strings.TrimSpace(value) != value {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func resolvedPublicValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func ValidateLiveResolvedPublicSearchContext(c LiveResolvedPublicSearchContext) error {
	city, slots := c.SelectedCity, c.ResolvedSlots
	if !resolvedPublicText(city.ID, 100, false) || strings.ContainsAny(city.ID, " /\\\"{}[]") || !resolvedPublicText(city.Name, 160, false) || len(city.CountryCode) != 2 || city.CountryCode[0] < 'A' || city.CountryCode[0] > 'Z' || city.CountryCode[1] < 'A' || city.CountryCode[1] > 'Z' {
		return ErrInvalid
	}
	if !resolvedPublicValue(slots.Target, "FIND_ACTIVITY", "FIND_PLACE", "FIND_ORGANIZATION") {
		return ErrInvalid
	}
	switch slots.Operation {
	case "FIND_ACTIVITY", "FIND_PLACE", "FIND_ORGANIZATION":
		if slots.Target != slots.Operation {
			return ErrInvalid
		}
	case "AREA_DISCOVERY":
		if slots.Target != "FIND_ACTIVITY" {
			return ErrInvalid
		}
	case "REFINE_RESULTS", "COMPARE_RESULTS":
	default:
		return ErrInvalid
	}
	if !resolvedPublicValue(slots.Category, "", "badminton", "basketball", "football", "sports", "culture") || !resolvedPublicValue(slots.TimePreference, "", "anytime", "today", "tonight", "tomorrow", "weekend") || !resolvedPublicValue(slots.DistancePreference, "", "closer") || !resolvedPublicText(slots.SearchTerm, LiveResolvedPublicSearchMaxQueryBytes, true) {
		return ErrInvalid
	}
	return nil
}

// CompileLiveResolvedPublicSearchQuery adds only the selected public city and
// resolved slots to the current literal question. Order and vocabulary are
// fixed; no history is inferred and no overlong query is silently truncated.
// "closer" means the native city-centre preference, not a user's coordinates.
func CompileLiveResolvedPublicSearchQuery(currentQuery string, c LiveResolvedPublicSearchContext) (string, error) {
	if ValidateLiveResolvedPublicSearchContext(c) != nil || !sourceText(currentQuery, LiveResolvedPublicSearchMaxQueryBytes, false) {
		return "", ErrInvalid
	}
	slots := c.ResolvedSlots
	target := map[string]string{"FIND_ACTIVITY": "activities", "FIND_PLACE": "places", "FIND_ORGANIZATION": "organizations"}[slots.Target]
	parts := []string{c.SelectedCity.Name, c.SelectedCity.CountryCode, target}
	for _, value := range []string{slots.Category, slots.TimePreference, slots.SearchTerm} {
		if value != "" {
			parts = append(parts, value)
		}
	}
	if slots.DistancePreference == "closer" {
		parts = append(parts, "closer to city centre")
	}
	parts = append(parts, strings.TrimSpace(currentQuery))
	query := strings.Join(parts, " ")
	if len(query) > LiveResolvedPublicSearchMaxQueryBytes {
		return "", ErrInvalid
	}
	return query, nil
}

// LiveResolvedPublicSearchPayload retains the exact current question in one
// user message. Public slots and source passages are untrusted JSON data;
// this helper cannot change the registered system prompt or add tools.
func LiveResolvedPublicSearchPayload(currentQuery string, c LiveResolvedPublicSearchContext, sources []LivePublicSource) (string, error) {
	if _, err := CompileLiveResolvedPublicSearchQuery(currentQuery, c); err != nil {
		return "", err
	}
	if _, err := LivePublicSourcesDigest(sources); err != nil {
		return "", err
	}
	raw, err := json.Marshal(struct {
		Schema              string `json:"schema"`
		CurrentQuery        string `json:"currentQuery"`
		SourceTrust         string `json:"sourceTrust"`
		PublicSearchContext struct {
			SelectedCity  LiveSelectedCity   `json:"selectedCity"`
			ResolvedSlots LiveResolvedSlots  `json:"resolvedSlots"`
			Sources       []LivePublicSource `json:"sources"`
		} `json:"publicSearchContext"`
	}{LiveResolvedPublicSearchSchema, currentQuery, LivePublicSearchTrust, struct {
		SelectedCity  LiveSelectedCity   `json:"selectedCity"`
		ResolvedSlots LiveResolvedSlots  `json:"resolvedSlots"`
		Sources       []LivePublicSource `json:"sources"`
	}{c.SelectedCity, c.ResolvedSlots, sources}})
	if err != nil || len(raw) > LivePublicSearchMaxPayloadBytes {
		return "", ErrInvalid
	}
	return string(raw), nil
}

// ResolvedContextEvidenceDigest is computed by native storage from canonical
// public context plus retained CITY/context generations. A supplied hash is
// not a grant. The native store re-reads and compares it at each execution gate.
type LiveResolvedDigestInput struct {
	Input                         LiveDigestInput
	ResolvedContextEvidenceDigest string
}

type LiveResolvedSourceExportDigestInput struct {
	Input                         LiveSourceExportDigestInput
	ResolvedContextEvidenceDigest string
}

func resolvedPublicEgressDigest(domain, scope, base, contextDigest string) (string, error) {
	if !liveHash(contextDigest) {
		return "", ErrInvalid
	}
	raw, err := json.Marshal(struct{ Scope, Schema, BaseDigest, ResolvedContextEvidenceDigest string }{scope, LiveResolvedPublicSearchSchema, base, contextDigest})
	if err != nil {
		return "", ErrInvalid
	}
	h := sha256.Sum256(append([]byte(domain+"\x00"), raw...))
	return hex.EncodeToString(h[:]), nil
}

func DigestLiveResolvedSearch(v LiveResolvedDigestInput, p LivePrice, now time.Time) (string, error) {
	if v.Input.Scope != LiveResolvedSearchScope || v.Input.Purpose != LiveSearchPurpose || p.Kind != LiveCall || !liveHash(v.ResolvedContextEvidenceDigest) {
		return "", ErrInvalid
	}
	base := v.Input
	base.Scope = Scope // Reuse the original arithmetic/fact validator only.
	inner, err := DigestLive(base, p, now)
	if err != nil {
		return "", err
	}
	return resolvedPublicEgressDigest("birdtie.model-egress.resolved-public-search.v1", LiveResolvedSearchScope, inner, v.ResolvedContextEvidenceDigest)
}

func DigestLiveResolvedSourceExport(v LiveResolvedSourceExportDigestInput, p LivePrice, now time.Time) (string, error) {
	if v.Input.Input.Scope != LiveResolvedSourceScope || !HasNativeLiveInputBound(p) || !liveHash(v.ResolvedContextEvidenceDigest) {
		return "", ErrInvalid
	}
	base := v.Input
	base.Input.Scope = LivePublicSearchScope // No widening of v1's public entry point.
	inner, err := DigestLiveSourceExport(base, p, now)
	if err != nil {
		return "", err
	}
	return resolvedPublicEgressDigest("birdtie.model-egress.resolved-public-sources.v1", LiveResolvedSourceScope, inner, v.ResolvedContextEvidenceDigest)
}
