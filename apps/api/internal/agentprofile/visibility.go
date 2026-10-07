package agentprofile

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

type FieldKey string
type FieldVisibility string

const (
	FieldDisplayName            FieldKey = "displayName"
	FieldBio                    FieldKey = "bio"
	FieldPersonalPreferences    FieldKey = "personalPreferences"
	FieldSocialPreferences      FieldKey = "socialPreferences"
	FieldAvailability           FieldKey = "availability"
	FieldPreferredActivityTypes FieldKey = "preferredActivityTypes"
	FieldTravelPreferences      FieldKey = "travelPreferences"
	FieldInteractionPreferences FieldKey = "interactionPreferences"
	FieldPrivateCityHistory     FieldKey = "privateCityHistory"
	FieldLanguagePreferences    FieldKey = "languagePreferences"
	FieldAgentNotes             FieldKey = "agentNotes"

	VisibilityPublic      FieldVisibility = "PUBLIC"
	VisibilityConnections FieldVisibility = "CONNECTIONS"
	VisibilityCommunity   FieldVisibility = "COMMUNITY"
	VisibilityPrivate     FieldVisibility = "PRIVATE"
	VisibilityAgentOnly   FieldVisibility = "AGENT_ONLY"

	VisibilitySchemaV1     = "agent-profile-visibility-v1"
	MaxVisibilityBodyBytes = 16 * 1024
	MaxFieldCommunityIDs   = 8
)

var configurableFields = [...]FieldKey{
	FieldDisplayName, FieldBio, FieldPersonalPreferences, FieldSocialPreferences,
	FieldAvailability, FieldPreferredActivityTypes, FieldTravelPreferences,
	FieldInteractionPreferences, FieldPrivateCityHistory, FieldLanguagePreferences, FieldAgentNotes,
}

// FieldRule is the owner's explicit audience choice, not a relationship,
// membership, source permission, current session or model-analysis grant.
// COMMUNITY always names existing groups, never a Community Agent principal.
type FieldRule struct {
	Visibility   FieldVisibility `json:"visibility"`
	CommunityIDs []string        `json:"communityIds"`
}

type FieldRules map[FieldKey]FieldRule

type VisibilityRecord struct {
	SchemaVersion string     `json:"schemaVersion"`
	Profile       Record     `json:"profile"`
	Rules         FieldRules `json:"rules"`
	Configured    bool       `json:"configured"`
}

type ReplaceVisibilityInput struct {
	ExpectedVersion int64      `json:"expectedVersion"`
	Rules           FieldRules `json:"rules"`
}

// ProjectedRecord contains only currently authorized field values. Neither
// source contents omitted by authorization, Agent metadata, rule settings nor
// Community identifiers may be exposed in this external projection.
type ProjectedRecord struct {
	AccountID string                       `json:"accountId"`
	Fields    map[FieldKey]json.RawMessage `json:"fields"`
}

func ConfigurableFieldKeys() []FieldKey {
	return append([]FieldKey(nil), configurableFields[:]...)
}

func configurableField(key FieldKey) bool {
	for _, allowed := range configurableFields {
		if key == allowed {
			return true
		}
	}
	return false
}

// Defaults preserve the existing ordinary public Profile source gate. Its
// legacy private/public setting and profile_view grant still require their own
// current resolver. Nine private defaults never inherit those coarse grants.
func DefaultFieldRules() FieldRules {
	rules := make(FieldRules, len(configurableFields))
	for _, key := range configurableFields {
		visibility := VisibilityPrivate
		if key == FieldDisplayName || key == FieldBio {
			visibility = VisibilityPublic
		}
		rules[key] = FieldRule{Visibility: visibility, CommunityIDs: []string{}}
	}
	return rules
}

func NormalizeFieldRules(input FieldRules) (FieldRules, error) {
	if len(input) != len(configurableFields) {
		return nil, ErrInvalid
	}
	out := make(FieldRules, len(configurableFields))
	for key, rule := range input {
		if !configurableField(key) {
			return nil, ErrInvalid
		}
		switch rule.Visibility {
		case VisibilityPublic, VisibilityConnections, VisibilityCommunity, VisibilityPrivate, VisibilityAgentOnly:
		default:
			return nil, ErrInvalid
		}
		ids := []string{}
		if rule.Visibility == VisibilityCommunity {
			if len(rule.CommunityIDs) == 0 || len(rule.CommunityIDs) > MaxFieldCommunityIDs {
				return nil, ErrInvalid
			}
			seen := make(map[string]bool, len(rule.CommunityIDs))
			for _, id := range rule.CommunityIDs {
				if !validID(id) {
					return nil, ErrInvalid
				}
				id = strings.ToLower(id)
				if seen[id] {
					return nil, ErrInvalid
				}
				seen[id] = true
				ids = append(ids, id)
			}
			sort.Strings(ids)
		} else if len(rule.CommunityIDs) != 0 {
			return nil, ErrInvalid
		}
		out[key] = FieldRule{Visibility: rule.Visibility, CommunityIDs: ids}
	}
	return out, nil
}

// FieldRulesDefault checks normalized rules for the unconfigured state. A
// default does not backfill consent, create a sharing row or enable cognition.
func FieldRulesDefault(rules FieldRules) bool {
	if len(rules) != len(configurableFields) {
		return false
	}
	for _, key := range configurableFields {
		rule, exists := rules[key]
		want := VisibilityPrivate
		if key == FieldDisplayName || key == FieldBio {
			want = VisibilityPublic
		}
		if !exists || rule.Visibility != want || len(rule.CommunityIDs) != 0 {
			return false
		}
	}
	return true
}

func NewVisibilityRecord(profile Record, rules FieldRules, configured bool) (VisibilityRecord, error) {
	normalized, err := NormalizeFieldRules(rules)
	if err != nil {
		return VisibilityRecord{}, ErrInvalid
	}
	out := VisibilityRecord{SchemaVersion: VisibilitySchemaV1, Profile: profile, Rules: normalized, Configured: configured}
	if err = ValidateVisibilityRecord(out); err != nil {
		return VisibilityRecord{}, err
	}
	return out, nil
}

func ValidateVisibilityRecord(record VisibilityRecord) error {
	if record.SchemaVersion != VisibilitySchemaV1 || Validate(record.Profile) != nil || record.Profile.OwnerType != actorref.Person {
		return ErrInvalid
	}
	normalized, err := NormalizeFieldRules(record.Rules)
	if err != nil || (!record.Configured && !FieldRulesDefault(normalized)) {
		return ErrInvalid
	}
	return nil
}

func NormalizeReplaceVisibilityInput(input ReplaceVisibilityInput) (ReplaceVisibilityInput, error) {
	if input.ExpectedVersion <= 0 {
		return ReplaceVisibilityInput{}, ErrInvalid
	}
	rules, err := NormalizeFieldRules(input.Rules)
	if err != nil {
		return ReplaceVisibilityInput{}, ErrInvalid
	}
	return ReplaceVisibilityInput{ExpectedVersion: input.ExpectedVersion, Rules: rules}, nil
}

func visibilityJSONCommunityIDs(decoder *json.Decoder) ([]string, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, ErrInvalid
	}
	ids := []string{}
	for decoder.More() {
		if len(ids) >= MaxFieldCommunityIDs {
			return nil, ErrInvalid
		}
		id, err := privateJSONString(decoder)
		if err != nil {
			return nil, ErrInvalid
		}
		ids = append(ids, id)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return nil, ErrInvalid
	}
	return ids, nil
}

func visibilityJSONRule(decoder *json.Decoder) (FieldRule, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return FieldRule{}, ErrInvalid
	}
	rule := FieldRule{}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := privateJSONString(decoder)
		if err != nil || seen[key] {
			return FieldRule{}, ErrInvalid
		}
		seen[key] = true
		switch key {
		case "visibility":
			value, err := privateJSONString(decoder)
			if err != nil {
				return FieldRule{}, ErrInvalid
			}
			rule.Visibility = FieldVisibility(value)
		case "communityIds":
			ids, err := visibilityJSONCommunityIDs(decoder)
			if err != nil {
				return FieldRule{}, ErrInvalid
			}
			rule.CommunityIDs = ids
		default:
			return FieldRule{}, ErrInvalid
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seen["visibility"] {
		return FieldRule{}, ErrInvalid
	}
	return rule, nil
}

func visibilityJSONRules(decoder *json.Decoder) (FieldRules, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalid
	}
	rules := FieldRules{}
	for decoder.More() {
		key, err := privateJSONString(decoder)
		field := FieldKey(key)
		if err != nil || !configurableField(field) {
			return nil, ErrInvalid
		}
		if _, duplicate := rules[field]; duplicate {
			return nil, ErrInvalid
		}
		rule, err := visibilityJSONRule(decoder)
		if err != nil {
			return nil, ErrInvalid
		}
		rules[field] = rule
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, ErrInvalid
	}
	return NormalizeFieldRules(rules)
}

func DecodeFieldRules(raw []byte) (FieldRules, error) {
	if len(raw) == 0 || len(raw) > MaxVisibilityBodyBytes || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	rules, err := visibilityJSONRules(decoder)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	return rules, nil
}

func DecodeReplaceVisibilityInput(raw []byte) (ReplaceVisibilityInput, error) {
	if len(raw) == 0 || len(raw) > MaxVisibilityBodyBytes || !utf8.Valid(raw) {
		return ReplaceVisibilityInput{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ReplaceVisibilityInput{}, ErrInvalid
	}
	input := ReplaceVisibilityInput{}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := privateJSONString(decoder)
		if err != nil || seen[key] {
			return ReplaceVisibilityInput{}, ErrInvalid
		}
		seen[key] = true
		switch key {
		case "expectedVersion":
			token, err := decoder.Token()
			if err != nil {
				return ReplaceVisibilityInput{}, ErrInvalid
			}
			number, ok := token.(json.Number)
			if !ok {
				return ReplaceVisibilityInput{}, ErrInvalid
			}
			version, err := strconv.ParseInt(number.String(), 10, 64)
			if err != nil || version <= 0 {
				return ReplaceVisibilityInput{}, ErrInvalid
			}
			input.ExpectedVersion = version
		case "rules":
			rules, err := visibilityJSONRules(decoder)
			if err != nil {
				return ReplaceVisibilityInput{}, ErrInvalid
			}
			input.Rules = rules
		default:
			return ReplaceVisibilityInput{}, ErrInvalid
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seen["expectedVersion"] || !seen["rules"] {
		return ReplaceVisibilityInput{}, ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return ReplaceVisibilityInput{}, ErrInvalid
	}
	return NormalizeReplaceVisibilityInput(input)
}

func (rules *FieldRules) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeFieldRules(raw)
	if err != nil {
		*rules = nil
		return ErrInvalid
	}
	*rules = decoded
	return nil
}

func (input *ReplaceVisibilityInput) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeReplaceVisibilityInput(raw)
	if err != nil {
		*input = ReplaceVisibilityInput{}
		return ErrInvalid
	}
	*input = decoded
	return nil
}

// JSON accepts UTF-16 escape syntax; unpaired surrogates would otherwise be
// silently replaced by encoding/json. Preserve valid historical public text,
// including a literal U+FFFD, while rejecting lossy malformed escape input.
func visibilityJSONStringValue(raw []byte) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '"' || trimmed[len(trimmed)-1] != '"' || !utf8.Valid(trimmed) || !json.Valid(trimmed) {
		return "", ErrInvalid
	}
	for index := 1; index < len(trimmed)-1; index++ {
		if trimmed[index] != '\\' {
			continue
		}
		index++
		if trimmed[index] != 'u' {
			continue
		}
		code, err := strconv.ParseUint(string(trimmed[index+1:index+5]), 16, 16)
		if err != nil {
			return "", ErrInvalid
		}
		index += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return "", ErrInvalid
		}
		if code >= 0xd800 && code <= 0xdbff {
			if index+6 >= len(trimmed) || trimmed[index+1] != '\\' || trimmed[index+2] != 'u' {
				return "", ErrInvalid
			}
			low, err := strconv.ParseUint(string(trimmed[index+3:index+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return "", ErrInvalid
			}
			index += 6
		}
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err != nil || !utf8.ValidString(text) {
		return "", ErrInvalid
	}
	return text, nil
}

func visibilityPrivateList(key FieldKey) bool {
	switch key {
	case FieldPersonalPreferences, FieldSocialPreferences, FieldPreferredActivityTypes,
		FieldTravelPreferences, FieldInteractionPreferences, FieldLanguagePreferences:
		return true
	default:
		return false
	}
}

// ValidateProjectedRecord is only a shape/minimal-disclosure check. The Store
// must resolve current source revision, source ACL, field rule, accepted Tie,
// explicit Community memberships, Block and session before constructing it.
// AGENT_ONLY is not made available to a model/provider by any DTO here.
func ValidateProjectedRecord(record ProjectedRecord) error {
	if !validID(record.AccountID) || record.Fields == nil {
		return ErrInvalid
	}
	private := map[FieldKey]json.RawMessage{}
	for key, value := range record.Fields {
		if !configurableField(key) || len(value) == 0 || len(value) > MaxVisibilityBodyBytes || !utf8.Valid(value) || !json.Valid(value) {
			return ErrInvalid
		}
		if key == FieldDisplayName || key == FieldBio {
			// Read historical source values without applying new editing limits,
			// whitespace rewriting, NFC normalization or inferred defaults.
			if _, err := visibilityJSONStringValue(value); err != nil {
				return ErrInvalid
			}
			continue
		}
		private[key] = value
		if visibilityPrivateList(key) {
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
				return ErrInvalid
			}
			var items []string
			if err := json.Unmarshal(value, &items); err != nil {
				return ErrInvalid
			}
			normalized, err := normalizePrivateList(items)
			if err != nil || len(normalized) != len(items) {
				return ErrInvalid
			}
			for index := range items {
				if items[index] != normalized[index] {
					return ErrInvalid
				}
			}
		} else {
			text, err := visibilityJSONStringValue(value)
			if err != nil {
				return ErrInvalid
			}
			normalized, err := normalizePrivateText(text, MaxPrivateTextRunes, true)
			if err != nil || normalized != text {
				return ErrInvalid
			}
		}
	}
	encodedPrivate, err := json.Marshal(private)
	if err != nil {
		return ErrInvalid
	}
	if _, err = DecodePrivateFields(encodedPrivate); err != nil {
		return ErrInvalid
	}
	body, err := json.Marshal(record)
	if err != nil || len(body) > MaxVisibilityBodyBytes {
		return ErrInvalid
	}
	return nil
}
