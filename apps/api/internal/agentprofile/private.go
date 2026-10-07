package agentprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const (
	PrivateSchemaV1       = "private-agent-profile-v1"
	MaxPrivateBodyBytes   = 16 * 1024
	MaxPrivateFieldsBytes = 12 * 1024
	MaxPrivateListItems   = 20
	MaxPrivateItemRunes   = 160
	MaxPrivateTextRunes   = 2000
)

var ErrPrivateAccessJSON = errors.New("private agent profile access is server-only")

// PrivateFields are explicit editable descriptions supplied by the owner. They
// are not inferred traits, calibrated interests, attendance, current location,
// availability commitments or a grant to analyze/share/send data to a model.
// Public Profile stays in identity.Profile; there is no public projection here.
type PrivateFields struct {
	PersonalPreferences    []string `json:"personalPreferences"`
	SocialPreferences      []string `json:"socialPreferences"`
	Availability           string   `json:"availability"`
	PreferredActivityTypes []string `json:"preferredActivityTypes"`
	TravelPreferences      []string `json:"travelPreferences"`
	InteractionPreferences []string `json:"interactionPreferences"`
	PrivateCityHistory     string   `json:"privateCityHistory"`
	LanguagePreferences    []string `json:"languagePreferences"`
	AgentNotes             string   `json:"agentNotes"`
}

// PrivateRecord only represents the owner's current private editing resource.
// Its sole version is Profile.ProfileVersion. Configured=false means there is
// no saved private row and must carry empty fields, not hidden inferred defaults.
type PrivateRecord struct {
	SchemaVersion string        `json:"schemaVersion"`
	Profile       Record        `json:"profile"`
	Fields        PrivateFields `json:"fields"`
	Configured    bool          `json:"configured"`
}

type ReplacePrivateInput struct {
	ExpectedVersion int64         `json:"expectedVersion"`
	Fields          PrivateFields `json:"fields"`
}

func (fields *PrivateFields) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodePrivateFields(raw)
	if err != nil {
		*fields = PrivateFields{}
		return ErrInvalid
	}
	*fields = decoded
	return nil
}

func (input *ReplacePrivateInput) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeReplacePrivateInput(raw)
	if err != nil {
		*input = ReplacePrivateInput{}
		return ErrInvalid
	}
	*input = decoded
	return nil
}

// PrivateAccess is server-only invocation context, never proof decoded from
// a request, model or public/profile grant. Store must authenticate Digest and
// resolve the exact active Person/account/Personal Agent on every operation.
type PrivateAccess struct {
	SessionDigest      [32]byte
	WorkspacePrincipal actorref.PrincipalRef
}

func (PrivateAccess) MarshalJSON() ([]byte, error) { return nil, ErrPrivateAccessJSON }
func (v *PrivateAccess) UnmarshalJSON([]byte) error {
	*v = PrivateAccess{}
	return ErrPrivateAccessJSON
}

func ValidatePrivateAccess(access PrivateAccess) error {
	if access.SessionDigest == ([32]byte{}) || access.WorkspacePrincipal.Type != actorref.Person || !validID(access.WorkspacePrincipal.ID) {
		return ErrForbidden
	}
	return nil // Shape only; the Store still resolves the real current session.
}

func normalizePrivateText(value string, maxRunes int, multiline bool) (string, error) {
	if !utf8.ValidString(value) {
		return "", ErrInvalid
	}
	value = strings.ReplaceAll(value, "\r\n", "\n")
	for _, character := range value {
		if character == utf8.RuneError || (unicode.IsControl(character) && !(multiline && (character == '\n' || character == '\t'))) {
			return "", ErrInvalid
		}
	}
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maxRunes {
		return "", ErrInvalid
	}
	return value, nil
}

func normalizePrivateList(values []string) ([]string, error) {
	if len(values) > MaxPrivateListItems {
		return nil, ErrInvalid
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		item, err := normalizePrivateText(value, MaxPrivateItemRunes, false)
		if err != nil || item == "" {
			return nil, ErrInvalid
		}
		if !seen[item] {
			out = append(out, item)
			seen[item] = true
		}
	}
	return out, nil
}

// NormalizePrivateFields returns fresh, bounded canonical values. Omitted
// fields become empty arrays/strings; no default preferences or city are guessed.
func NormalizePrivateFields(input PrivateFields) (PrivateFields, error) {
	out := PrivateFields{}
	lists := []struct {
		input  []string
		output *[]string
	}{
		{input.PersonalPreferences, &out.PersonalPreferences}, {input.SocialPreferences, &out.SocialPreferences},
		{input.PreferredActivityTypes, &out.PreferredActivityTypes}, {input.TravelPreferences, &out.TravelPreferences},
		{input.InteractionPreferences, &out.InteractionPreferences}, {input.LanguagePreferences, &out.LanguagePreferences},
	}
	for _, list := range lists {
		value, err := normalizePrivateList(list.input)
		if err != nil {
			return PrivateFields{}, ErrInvalid
		}
		*list.output = value
	}
	texts := []struct {
		input  string
		output *string
	}{
		{input.Availability, &out.Availability}, {input.PrivateCityHistory, &out.PrivateCityHistory}, {input.AgentNotes, &out.AgentNotes},
	}
	for _, text := range texts {
		value, err := normalizePrivateText(text.input, MaxPrivateTextRunes, true)
		if err != nil {
			return PrivateFields{}, ErrInvalid
		}
		*text.output = value
	}
	body, err := json.Marshal(out)
	if err != nil || len(body) > MaxPrivateFieldsBytes {
		return PrivateFields{}, ErrInvalid
	}
	return out, nil
}

// PrivateFieldsEmpty checks a normalized replacement for the explicit clear
// operation. Store must still CAS the metadata version before deleting the row.
func PrivateFieldsEmpty(fields PrivateFields) bool {
	return len(fields.PersonalPreferences) == 0 && len(fields.SocialPreferences) == 0 && fields.Availability == "" &&
		len(fields.PreferredActivityTypes) == 0 && len(fields.TravelPreferences) == 0 && len(fields.InteractionPreferences) == 0 &&
		fields.PrivateCityHistory == "" && len(fields.LanguagePreferences) == 0 && fields.AgentNotes == ""
}

func NewPrivateRecord(profile Record, fields PrivateFields, configured bool) (PrivateRecord, error) {
	normalized, err := NormalizePrivateFields(fields)
	if err != nil {
		return PrivateRecord{}, ErrInvalid
	}
	record := PrivateRecord{SchemaVersion: PrivateSchemaV1, Profile: profile, Fields: normalized, Configured: configured}
	if err = ValidatePrivateRecord(record); err != nil {
		return PrivateRecord{}, err
	}
	return record, nil
}

func ValidatePrivateRecord(record PrivateRecord) error {
	if record.SchemaVersion != PrivateSchemaV1 || Validate(record.Profile) != nil || record.Profile.OwnerType != actorref.Person {
		return ErrInvalid
	}
	fields, err := NormalizePrivateFields(record.Fields)
	if err != nil || (!record.Configured && !PrivateFieldsEmpty(fields)) {
		return ErrInvalid
	}
	return nil
}

func NormalizeReplacePrivateInput(input ReplacePrivateInput) (ReplacePrivateInput, error) {
	if input.ExpectedVersion <= 0 {
		return ReplacePrivateInput{}, ErrInvalid
	}
	fields, err := NormalizePrivateFields(input.Fields)
	if err != nil {
		return ReplacePrivateInput{}, ErrInvalid
	}
	return ReplacePrivateInput{ExpectedVersion: input.ExpectedVersion, Fields: fields}, nil
}

func privateJSONString(decoder *json.Decoder) (string, error) {
	token, err := decoder.Token()
	if err != nil {
		return "", ErrInvalid
	}
	value, ok := token.(string)
	if !ok {
		return "", ErrInvalid
	}
	return value, nil
}

func privateJSONList(decoder *json.Decoder) ([]string, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, ErrInvalid
	}
	values := []string{}
	for decoder.More() {
		if len(values) >= MaxPrivateListItems {
			return nil, ErrInvalid
		}
		value, err := privateJSONString(decoder)
		if err != nil {
			return nil, ErrInvalid
		}
		values = append(values, value)
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim(']') {
		return nil, ErrInvalid
	}
	return values, nil
}

func privateJSONFields(decoder *json.Decoder) (PrivateFields, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return PrivateFields{}, ErrInvalid
	}
	fields := PrivateFields{}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := privateJSONString(decoder)
		if err != nil || seen[key] {
			return PrivateFields{}, ErrInvalid
		}
		seen[key] = true
		var list *[]string
		var text *string
		switch key {
		case "personalPreferences":
			list = &fields.PersonalPreferences
		case "socialPreferences":
			list = &fields.SocialPreferences
		case "availability":
			text = &fields.Availability
		case "preferredActivityTypes":
			list = &fields.PreferredActivityTypes
		case "travelPreferences":
			list = &fields.TravelPreferences
		case "interactionPreferences":
			list = &fields.InteractionPreferences
		case "privateCityHistory":
			text = &fields.PrivateCityHistory
		case "languagePreferences":
			list = &fields.LanguagePreferences
		case "agentNotes":
			text = &fields.AgentNotes
		default:
			return PrivateFields{}, ErrInvalid
		}
		if list != nil {
			value, err := privateJSONList(decoder)
			if err != nil {
				return PrivateFields{}, ErrInvalid
			}
			*list = value
		} else {
			value, err := privateJSONString(decoder)
			if err != nil {
				return PrivateFields{}, ErrInvalid
			}
			*text = value
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return PrivateFields{}, ErrInvalid
	}
	return NormalizePrivateFields(fields)
}

// DecodePrivateFields is also used for stored schema-v1 rows. Unlike the usual
// json.Unmarshal it rejects duplicate/case-insensitive/unknown keys and null.
func DecodePrivateFields(raw []byte) (PrivateFields, error) {
	// Stored jsonb formatting may add whitespace. The semantic normalized body
	// still has the 12 KiB bound; the transport/parser has the 16 KiB bound.
	if len(raw) == 0 || len(raw) > MaxPrivateBodyBytes || !utf8.Valid(raw) {
		return PrivateFields{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	fields, err := privateJSONFields(decoder)
	if err != nil {
		return PrivateFields{}, ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return PrivateFields{}, ErrInvalid
	}
	return fields, nil
}

// DecodeReplacePrivateInput's only authority-independent wire fields are an
// optimistic expected version and the explicit private editing payload. There
// is no owner/Agent/profileVersion/visibility/confirmed/grant/policy override.
func DecodeReplacePrivateInput(raw []byte) (ReplacePrivateInput, error) {
	if len(raw) == 0 || len(raw) > MaxPrivateBodyBytes || !utf8.Valid(raw) {
		return ReplacePrivateInput{}, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ReplacePrivateInput{}, ErrInvalid
	}
	input := ReplacePrivateInput{}
	seen := map[string]bool{}
	for decoder.More() {
		key, err := privateJSONString(decoder)
		if err != nil || seen[key] {
			return ReplacePrivateInput{}, ErrInvalid
		}
		seen[key] = true
		switch key {
		case "expectedVersion":
			token, err := decoder.Token()
			if err != nil {
				return ReplacePrivateInput{}, ErrInvalid
			}
			number, ok := token.(json.Number)
			if !ok {
				return ReplacePrivateInput{}, ErrInvalid
			}
			version, err := strconv.ParseInt(number.String(), 10, 64)
			if err != nil || version <= 0 {
				return ReplacePrivateInput{}, ErrInvalid
			}
			input.ExpectedVersion = version
		case "fields":
			fields, err := privateJSONFields(decoder)
			if err != nil {
				return ReplacePrivateInput{}, ErrInvalid
			}
			input.Fields = fields
		default:
			return ReplacePrivateInput{}, ErrInvalid
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') || !seen["expectedVersion"] || !seen["fields"] {
		return ReplacePrivateInput{}, ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return ReplacePrivateInput{}, ErrInvalid
	}
	return NormalizeReplacePrivateInput(input)
}
