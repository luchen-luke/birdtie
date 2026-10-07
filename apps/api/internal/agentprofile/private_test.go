package agentprofile_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

func privateFieldsFixture() agentprofile.PrivateFields {
	return agentprofile.PrivateFields{
		PersonalPreferences: []string{"安静环境"}, SocialPreferences: []string{"先通过活动认识"},
		Availability: "我说周末通常有空；这不是时间承诺", PreferredActivityTypes: []string{"羽毛球"},
		TravelPreferences: []string{"优先公共交通"}, InteractionPreferences: []string{"先文字沟通"},
		PrivateCityHistory: "我自己填写的历史城市，不代表当前定位", LanguagePreferences: []string{"中文", "English"},
		AgentNotes: "仅本人保存的说明，不是模型许可或执行指令",
	}
}

func privateBody(fields string) []byte {
	return []byte(`{"expectedVersion":1,"fields":` + fields + `}`)
}

func TestPrivateAgentProfileExplicitFieldsAndCanonicalCopy(t *testing.T) {
	fields := privateFieldsFixture()
	fields.PersonalPreferences = []string{" 安静环境 ", "安静环境", "室内"}
	fields.AgentNotes = "  第一行\r\n第二行\t说明  "
	normalized, err := agentprofile.NormalizePrivateFields(fields)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized.PersonalPreferences, []string{"安静环境", "室内"}) || normalized.AgentNotes != "第一行\n第二行\t说明" {
		t.Fatal("explicit normalization lost order or line boundaries")
	}
	normalized.PersonalPreferences[0] = "caller edit"
	if fields.PersonalPreferences[0] != " 安静环境 " {
		t.Fatal("private input aliased mutable output")
	}
	profile := profileFixture(actorref.Person)
	profile.ProfileVersion = 5
	record, err := agentprofile.NewPrivateRecord(profile, privateFieldsFixture(), true)
	if err != nil || record.SchemaVersion != agentprofile.PrivateSchemaV1 || record.Profile.ProfileVersion != 5 || !record.Configured {
		t.Fatalf("private row invented independent version: %v", err)
	}
	if err = agentprofile.ValidatePrivateRecord(record); err != nil {
		t.Fatal(err)
	}
	clear, err := agentprofile.NewPrivateRecord(profile, agentprofile.PrivateFields{}, false)
	if err != nil || clear.Configured || !agentprofile.PrivateFieldsEmpty(clear.Fields) {
		t.Fatalf("unconfigured shape not explicit: %v", err)
	}
	body, err := json.Marshal(clear.Fields)
	if err != nil || strings.Contains(string(body), "null") {
		t.Fatal("empty typed arrays serialized as null")
	}
}

func TestPrivateAgentProfileNoPublicOrPermissionInheritance(t *testing.T) {
	for _, kind := range []actorref.Type{actorref.Organization, actorref.Business} {
		t.Run(string(kind), func(t *testing.T) {
			record, err := agentprofile.NewPrivateRecord(profileFixture(kind), privateFieldsFixture(), true)
			if !errors.Is(err, agentprofile.ErrInvalid) || !reflect.ValueOf(record).IsZero() {
				t.Fatal("non-Personal private shape accepted")
			}
		})
	}
	record, err := agentprofile.NewPrivateRecord(profileFixture(actorref.Person), privateFieldsFixture(), true)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err = json.Unmarshal(body, &object); err != nil {
		t.Fatal(err)
	}
	if len(object) != 4 || object["schemaVersion"] == nil || object["profile"] == nil || object["fields"] == nil || object["configured"] == nil {
		t.Fatal("private response contract changed")
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(object["fields"], &fields); err != nil || len(fields) != 9 {
		t.Fatal("private fields not a finite nine-field schema")
	}
	for _, forbidden := range []string{"visibility", "publicFields", "confirmed", "grant", "policy", "confidence", "provenance", "model", "provider"} {
		if fields[forbidden] != nil || object[forbidden] != nil {
			t.Fatal("storage fields manufactured sharing/inference authority")
		}
	}
	// Saving explicit owner text does not change the unavailable cognition gate.
	_, gateErr := (agentcognitive.UnavailableCognitivePorts{}).SubmitMemoryCandidate(context.Background(), agentcognitive.CandidateSubmission{})
	if !errors.Is(gateErr, agentcognitive.ErrUnavailable) {
		t.Fatal("private editing enabled analysis/Memory")
	}
}

func TestPrivateAgentProfileRecordStateMatrix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*agentprofile.PrivateRecord)
	}{
		{"unknown schema", func(r *agentprofile.PrivateRecord) { r.SchemaVersion = "v2" }},
		{"missing schema", func(r *agentprofile.PrivateRecord) { r.SchemaVersion = "" }},
		{"org owner", func(r *agentprofile.PrivateRecord) { r.Profile.OwnerType = actorref.Organization }},
		{"business owner", func(r *agentprofile.PrivateRecord) { r.Profile.OwnerType = actorref.Business }},
		{"community owner", func(r *agentprofile.PrivateRecord) { r.Profile.OwnerType = actorref.Community }},
		{"zero metadata version", func(r *agentprofile.PrivateRecord) { r.Profile.ProfileVersion = 0 }},
		{"missing stable Agent", func(r *agentprofile.PrivateRecord) { r.Profile.AgentID = "" }},
		{"false configured with hidden fields", func(r *agentprofile.PrivateRecord) { r.Configured = false }},
		{"overlong note", func(r *agentprofile.PrivateRecord) {
			r.Fields.AgentNotes = strings.Repeat("字", agentprofile.MaxPrivateTextRunes+1)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record, err := agentprofile.NewPrivateRecord(profileFixture(actorref.Person), privateFieldsFixture(), true)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(&record)
			if !errors.Is(agentprofile.ValidatePrivateRecord(record), agentprofile.ErrInvalid) {
				t.Fatal("invalid private resource shape accepted")
			}
		})
	}
}

func TestPrivateAgentProfileStrictReplaceDecoderValid(t *testing.T) {
	for _, raw := range []string{
		`{"expectedVersion":1,"fields":{}}`,
		`{"fields":{"agentNotes":"仅供本人","availability":"暂无约定","languagePreferences":["中文","English"]},"expectedVersion":42}`,
		`{"expectedVersion":` + strconv.FormatInt(math.MaxInt64, 10) + `,"fields":{"preferredActivityTypes":["羽毛球😀"]}}`,
	} {
		t.Run(raw[:20], func(t *testing.T) {
			input, err := agentprofile.DecodeReplacePrivateInput([]byte(raw))
			if err != nil || input.ExpectedVersion <= 0 || input.Fields.PersonalPreferences == nil {
				t.Fatalf("valid replacement rejected: %v", err)
			}
			var viaJSON agentprofile.ReplacePrivateInput
			if err = json.Unmarshal([]byte(raw), &viaJSON); err != nil || !reflect.DeepEqual(viaJSON, input) {
				t.Fatal("ordinary JSON bypassed strict typed decoder")
			}
		})
	}
	_, err := agentprofile.DecodePrivateFields([]byte(`{"agentNotes":"\ud83d\ude00"}`))
	if err != nil {
		t.Fatal("valid surrogate pair/emoji rejected")
	}
}

func TestPrivateAgentProfileStrictWireAuthorityMatrix(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"empty", ``}, {"null body", `null`}, {"array body", `[]`},
		{"empty object", `{}`}, {"missing fields", `{"expectedVersion":1}`}, {"missing version", `{"fields":{}}`},
		{"null fields", `{"expectedVersion":1,"fields":null}`}, {"null version", `{"expectedVersion":null,"fields":{}}`},
		{"string version", `{"expectedVersion":"1","fields":{}}`}, {"boolean version", `{"expectedVersion":true,"fields":{}}`},
		{"decimal version", `{"expectedVersion":1.0,"fields":{}}`}, {"exponent version", `{"expectedVersion":1e0,"fields":{}}`},
		{"zero version", `{"expectedVersion":0,"fields":{}}`}, {"negative version", `{"expectedVersion":-1,"fields":{}}`},
		{"overflow version", `{"expectedVersion":9223372036854775808,"fields":{}}`},
		{"version casing", `{"ExpectedVersion":1,"fields":{}}`}, {"fields casing", `{"expectedVersion":1,"Fields":{}}`},
		{"duplicate expected version", `{"expectedVersion":1,"expectedVersion":2,"fields":{}}`},
		{"duplicate escaped expected version", `{"expectedVersion":1,"\u0065xpectedVersion":2,"fields":{}}`},
		{"duplicate fields", `{"expectedVersion":1,"fields":{},"fields":{}}`},
		{"client owner", `{"expectedVersion":1,"fields":{},"ownerId":"secret-canary"}`},
		{"client owner type", `{"expectedVersion":1,"fields":{},"ownerType":"PERSON"}`},
		{"client Agent", `{"expectedVersion":1,"fields":{},"agentId":"secret-canary"}`},
		{"client authoritative version", `{"expectedVersion":1,"fields":{},"profileVersion":9}`},
		{"client confirmed", `{"expectedVersion":1,"fields":{},"confirmed":true}`},
		{"client grant", `{"expectedVersion":1,"fields":{},"grant":{"approved":true}}`},
		{"client policy", `{"expectedVersion":1,"fields":{},"policy":{"public":true}}`},
		{"client visibility", `{"expectedVersion":1,"fields":{"visibility":"PUBLIC"}}`},
		{"unknown preference", `{"expectedVersion":1,"fields":{"inferredTraits":["secret-canary"]}}`},
		{"nested owner", `{"expectedVersion":1,"fields":{"ownerId":"secret-canary"}}`},
		{"field casing", `{"expectedVersion":1,"fields":{"AgentNotes":"secret-canary"}}`},
		{"duplicate text", `{"expectedVersion":1,"fields":{"agentNotes":"one","agentNotes":"two"}}`},
		{"duplicate array", `{"expectedVersion":1,"fields":{"personalPreferences":[],"personalPreferences":[]}}`},
		{"array nested object", `{"expectedVersion":1,"fields":{"personalPreferences":[{"confirmed":true}]}}`},
		{"array null element", `{"expectedVersion":1,"fields":{"personalPreferences":[null]}}`},
		{"array boolean element", `{"expectedVersion":1,"fields":{"personalPreferences":[true]}}`},
		{"array integer element", `{"expectedVersion":1,"fields":{"personalPreferences":[1]}}`},
		{"text object", `{"expectedVersion":1,"fields":{"agentNotes":{"content":"secret-canary"}}}`},
		{"text array", `{"expectedVersion":1,"fields":{"agentNotes":[]}}`},
		{"multiple objects", `{"expectedVersion":1,"fields":{}} {}`},
		{"trailing null", `{"expectedVersion":1,"fields":{}} null`},
		{"trailing comma", `{"expectedVersion":1,"fields":{},}`},
		{"unpaired surrogate", `{"expectedVersion":1,"fields":{"agentNotes":"\ud800"}}`},
		{"NUL text", `{"expectedVersion":1,"fields":{"agentNotes":"\u0000"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, err := agentprofile.DecodeReplacePrivateInput([]byte(tc.raw))
			if !errors.Is(err, agentprofile.ErrInvalid) || !reflect.ValueOf(input).IsZero() || strings.Contains(err.Error(), "secret-canary") {
				t.Fatal("malformed/private authority input not generically rejected")
			}
			previous := agentprofile.ReplacePrivateInput{ExpectedVersion: 1, Fields: privateFieldsFixture()}
			if err = json.Unmarshal([]byte(tc.raw), &previous); err == nil {
				t.Fatal("generic JSON decoder bypass accepted invalid input")
			}
			// Syntax-invalid JSON is rejected before calling UnmarshalJSON;
			// the input is never returned from DecodeReplacePrivateInput above.
		})
	}
	for _, key := range []string{"personalPreferences", "socialPreferences", "availability", "preferredActivityTypes", "travelPreferences", "interactionPreferences", "privateCityHistory", "languagePreferences", "agentNotes"} {
		t.Run("null "+key, func(t *testing.T) {
			if _, err := agentprofile.DecodeReplacePrivateInput(privateBody(`{"` + key + `":null}`)); !errors.Is(err, agentprofile.ErrInvalid) {
				t.Fatal("null replaced a finite field type")
			}
		})
	}
}

func TestPrivateAgentProfileContentLimitsAndControlMatrix(t *testing.T) {
	for _, value := range []string{"", "  ", "a\nline", "tab\titem", "nul\x00item", "invalid\x80", strings.Repeat("字", agentprofile.MaxPrivateItemRunes+1)} {
		t.Run("invalid list "+strconv.Itoa(len(value)), func(t *testing.T) {
			if _, err := agentprofile.NormalizePrivateFields(agentprofile.PrivateFields{PersonalPreferences: []string{value}}); !errors.Is(err, agentprofile.ErrInvalid) {
				t.Fatal("invalid list item accepted")
			}
		})
	}
	for _, value := range []string{"nul\x00note", "bell\x07note", "invalid\x80", "unpaired\ufffd", strings.Repeat("字", agentprofile.MaxPrivateTextRunes+1)} {
		t.Run("invalid text "+strconv.Itoa(len(value)), func(t *testing.T) {
			if _, err := agentprofile.NormalizePrivateFields(agentprofile.PrivateFields{AgentNotes: value}); !errors.Is(err, agentprofile.ErrInvalid) {
				t.Fatal("invalid note accepted")
			}
		})
	}
	items := make([]string, agentprofile.MaxPrivateListItems+1)
	for index := range items {
		items[index] = strconv.Itoa(index)
	}
	if _, err := agentprofile.NormalizePrivateFields(agentprofile.PrivateFields{LanguagePreferences: items}); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("excessive list accepted")
	}
	if _, err := agentprofile.DecodePrivateFields([]byte(`{"languagePreferences":` + mustPrivateJSON(t, items) + `}`)); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("wire list limit ignored")
	}
	for _, field := range []agentprofile.PrivateFields{
		{PersonalPreferences: []string{strings.Repeat("字", agentprofile.MaxPrivateItemRunes)}},
		{AgentNotes: strings.Repeat("字", agentprofile.MaxPrivateTextRunes)},
		{LanguagePreferences: items[:agentprofile.MaxPrivateListItems]},
	} {
		if _, err := agentprofile.NormalizePrivateFields(field); err != nil {
			t.Fatalf("valid boundary rejected: %v", err)
		}
	}
	tooLarge := agentprofile.PrivateFields{Availability: strings.Repeat("字", agentprofile.MaxPrivateTextRunes),
		PrivateCityHistory: strings.Repeat("字", agentprofile.MaxPrivateTextRunes), AgentNotes: strings.Repeat("字", agentprofile.MaxPrivateTextRunes)}
	if _, err := agentprofile.NormalizePrivateFields(tooLarge); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("aggregate private payload limit ignored")
	}
	if _, err := agentprofile.NormalizeReplacePrivateInput(agentprofile.ReplacePrivateInput{ExpectedVersion: 0}); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("missing optimistic version accepted")
	}
}

func mustPrivateJSON(t *testing.T, value []string) string {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestPrivateAgentProfileBodySizeUTF8AndStoredFields(t *testing.T) {
	valid := `{"expectedVersion":1,"fields":{}}`
	atLimit := []byte(valid + strings.Repeat(" ", agentprofile.MaxPrivateBodyBytes-len(valid)))
	if _, err := agentprofile.DecodeReplacePrivateInput(atLimit); err != nil {
		t.Fatal("exact wire body bound rejected")
	}
	if _, err := agentprofile.DecodeReplacePrivateInput(append(atLimit, ' ')); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("overlarge wire body accepted")
	}
	invalid := append([]byte(valid), byte(0xff))
	if _, err := agentprofile.DecodeReplacePrivateInput(invalid); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("invalid wire UTF8 accepted")
	}
	fields := []byte(`{"agentNotes":"仅本人说明"}`)
	if _, err := agentprofile.DecodePrivateFields(append(fields, []byte(strings.Repeat(" ", 100))...)); err != nil {
		t.Fatal("stored JSON whitespace wrongly became extra private content")
	}
	for _, raw := range []string{`null`, `[]`, `{"agentNotes":null}`, `{"agentNotes":"one","agentNotes":"two"}`, `{"unknown":"secret-canary"}`, `{} {}`} {
		if _, err := agentprofile.DecodePrivateFields([]byte(raw)); !errors.Is(err, agentprofile.ErrInvalid) {
			t.Fatal("stored private fields bypassed strict schema")
		}
		previous := privateFieldsFixture()
		if err := json.Unmarshal([]byte(raw), &previous); err == nil {
			t.Fatal("typed stored fields bypassed strict schema")
		}
	}
}

func TestPrivateAgentProfileAccessCannotBeWireAuthority(t *testing.T) {
	profile := profileFixture(actorref.Person)
	access := agentprofile.PrivateAccess{SessionDigest: [32]byte{1}, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: profile.OwnerID}}
	if err := agentprofile.ValidatePrivateAccess(access); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []actorref.Type{actorref.Organization, actorref.Business, actorref.Community, "CITY", "PERSONAL", "person"} {
		invalid := access
		invalid.WorkspacePrincipal.Type = kind
		if !errors.Is(agentprofile.ValidatePrivateAccess(invalid), agentprofile.ErrForbidden) {
			t.Fatal("non-Person private access accepted")
		}
	}
	invalid := access
	invalid.SessionDigest = [32]byte{}
	if !errors.Is(agentprofile.ValidatePrivateAccess(invalid), agentprofile.ErrForbidden) {
		t.Fatal("anonymous private access accepted")
	}
	invalid = access
	invalid.WorkspacePrincipal.ID = "00000000-0000-0000-0000-000000000000"
	if !errors.Is(agentprofile.ValidatePrivateAccess(invalid), agentprofile.ErrForbidden) {
		t.Fatal("missing principal private access accepted")
	}
	body, err := json.Marshal(access)
	if len(body) != 0 || !errors.Is(err, agentprofile.ErrPrivateAccessJSON) {
		t.Fatal("session digest serialized as model/wire authority")
	}
	previous := access
	err = json.Unmarshal([]byte(`{"SessionDigest":[1],"WorkspacePrincipal":{"type":"PERSON","id":"secret-canary"},"confirmed":true}`), &previous)
	if !errors.Is(err, agentprofile.ErrPrivateAccessJSON) || !reflect.ValueOf(previous).IsZero() || strings.Contains(err.Error(), "secret-canary") {
		t.Fatal("wire supplied private session authority or retained previous facts")
	}
}
