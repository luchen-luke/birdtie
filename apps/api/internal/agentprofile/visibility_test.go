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

const visibilityCommunityA = "90000000-0000-4000-8000-00000000000a"
const visibilityCommunityB = "90000000-0000-4000-8000-00000000000b"

func visibilityEncodedRules(t *testing.T, rules agentprofile.FieldRules) string {
	t.Helper()
	body, err := json.Marshal(rules)
	if err != nil {
		t.Fatal("could not encode synthetic field rules")
	}
	return string(body)
}

func visibilityBody(rules string) []byte {
	return []byte(`{"expectedVersion":1,"rules":` + rules + `}`)
}

func TestAgentProfileVisibilityDefaultsClosedFieldsAndFreshCopies(t *testing.T) {
	keys := agentprofile.ConfigurableFieldKeys()
	if len(keys) != 11 || keys[0] != agentprofile.FieldDisplayName || keys[1] != agentprofile.FieldBio {
		t.Fatal("configurable fields are not the agreed finite eleven-field source set")
	}
	seen := map[agentprofile.FieldKey]bool{}
	for _, key := range keys {
		if seen[key] {
			t.Fatal("duplicate configurable field")
		}
		seen[key] = true
	}
	keys[0] = "grant"
	if agentprofile.ConfigurableFieldKeys()[0] != agentprofile.FieldDisplayName {
		t.Fatal("caller mutated shared field registry")
	}
	defaults := agentprofile.DefaultFieldRules()
	if !agentprofile.FieldRulesDefault(defaults) {
		t.Fatal("unconfigured defaults not recognized")
	}
	for key, rule := range defaults {
		want := agentprofile.VisibilityPrivate
		if key == agentprofile.FieldDisplayName || key == agentprofile.FieldBio {
			want = agentprofile.VisibilityPublic
		}
		if rule.Visibility != want || rule.CommunityIDs == nil || len(rule.CommunityIDs) != 0 {
			t.Fatal("default privately sourced field inherited a public/granted audience")
		}
	}
	defaults[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
	if agentprofile.FieldRulesDefault(defaults) || !agentprofile.FieldRulesDefault(agentprofile.DefaultFieldRules()) {
		t.Fatal("default rule map was shared or wrong audience recognized")
	}
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldAvailability] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{strings.ToUpper(visibilityCommunityB), visibilityCommunityA}}
	normalized, err := agentprofile.NormalizeFieldRules(rules)
	if err != nil || !reflect.DeepEqual(normalized[agentprofile.FieldAvailability].CommunityIDs, []string{visibilityCommunityA, visibilityCommunityB}) {
		t.Fatal("Community targets not canonical stable sorted UUIDs")
	}
	normalized[agentprofile.FieldAvailability].CommunityIDs[0] = visibilityCommunityB
	if rules[agentprofile.FieldAvailability].CommunityIDs[0] != strings.ToUpper(visibilityCommunityB) {
		t.Fatal("normalized audience IDs aliased input")
	}
	delete(normalized, agentprofile.FieldBio)
	if len(rules) != 11 {
		t.Fatal("normalized rule map aliased input")
	}
}

func TestAgentProfileVisibilityEveryFieldSupportsAllFiveAudiences(t *testing.T) {
	for _, key := range agentprofile.ConfigurableFieldKeys() {
		for _, audience := range []agentprofile.FieldVisibility{agentprofile.VisibilityPublic, agentprofile.VisibilityConnections, agentprofile.VisibilityCommunity, agentprofile.VisibilityPrivate, agentprofile.VisibilityAgentOnly} {
			t.Run(string(key)+" "+string(audience), func(t *testing.T) {
				rules := agentprofile.DefaultFieldRules()
				rule := agentprofile.FieldRule{Visibility: audience}
				if audience == agentprofile.VisibilityCommunity {
					rule.CommunityIDs = []string{visibilityCommunityA}
				}
				rules[key] = rule
				normalized, err := agentprofile.NormalizeFieldRules(rules)
				if err != nil || normalized[key].Visibility != audience || normalized[key].CommunityIDs == nil {
					t.Fatal("finite audience unavailable for a configurable field")
				}
				decoded, err := agentprofile.DecodeFieldRules([]byte(visibilityEncodedRules(t, normalized)))
				if err != nil || !reflect.DeepEqual(decoded, normalized) {
					t.Fatal("finite audience lost in strict stored/wire codec")
				}
			})
		}
	}
}

func TestAgentProfileVisibilityRuleShapeAndCommunityMatrix(t *testing.T) {
	cases := []struct {
		name string
		edit func(agentprofile.FieldRules)
	}{
		{"missing one source", func(r agentprofile.FieldRules) { delete(r, agentprofile.FieldBio) }},
		{"unknown field", func(r agentprofile.FieldRules) {
			delete(r, agentprofile.FieldBio)
			r["inferredTrait"] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
		}},
		{"source casing", func(r agentprofile.FieldRules) {
			delete(r, agentprofile.FieldBio)
			r["Bio"] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
		}},
		{"empty audience", func(r agentprofile.FieldRules) { r[agentprofile.FieldBio] = agentprofile.FieldRule{} }},
		{"lowercase audience", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: "public"}
		}},
		{"padded audience", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: " PUBLIC "}
		}},
		{"invented close friend", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: "CLOSE_FRIENDS"}
		}},
		{"singular runtime scope is not field audience", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: "CONNECTION"}
		}},
		{"null Community IDs", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity}
		}},
		{"empty Community IDs", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{}}
		}},
		{"zero Community ID", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{"00000000-0000-0000-0000-000000000000"}}
		}},
		{"arbitrary Community label", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{"nearby students"}}
		}},
		{"padded Community ID", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{" " + visibilityCommunityA}}
		}},
		{"duplicate Community", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{visibilityCommunityA, visibilityCommunityA}}
		}},
		{"case equivalent duplicate Community", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{visibilityCommunityA, strings.ToUpper(visibilityCommunityA)}}
		}},
		{"too many Communities", func(r agentprofile.FieldRules) {
			ids := make([]string, 9)
			for index := range ids {
				ids[index] = "90000000-0000-4000-8000-0000000000" + strconv.FormatInt(int64(index+1), 16) + "c"
			}
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: ids}
		}},
	}
	for _, audience := range []agentprofile.FieldVisibility{agentprofile.VisibilityPublic, agentprofile.VisibilityConnections, agentprofile.VisibilityPrivate, agentprofile.VisibilityAgentOnly} {
		value := audience
		cases = append(cases, struct {
			name string
			edit func(agentprofile.FieldRules)
		}{string(value) + " must not carry Community scope", func(r agentprofile.FieldRules) {
			r[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: value, CommunityIDs: []string{visibilityCommunityA}}
		}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rules := agentprofile.DefaultFieldRules()
			tc.edit(rules)
			if normalized, err := agentprofile.NormalizeFieldRules(rules); !errors.Is(err, agentprofile.ErrInvalid) || normalized != nil {
				t.Fatal("invalid rule set not rejected atomically")
			}
		})
	}
	for _, invalid := range []agentprofile.FieldRules{nil, {}, {agentprofile.FieldBio: {Visibility: agentprofile.VisibilityPublic}}} {
		if _, err := agentprofile.NormalizeFieldRules(invalid); !errors.Is(err, agentprofile.ErrInvalid) || agentprofile.FieldRulesDefault(invalid) {
			t.Fatal("missing/partial rules silently granted default audience")
		}
	}
	ids := make([]string, agentprofile.MaxFieldCommunityIDs)
	for index := range ids {
		ids[index] = "90000000-0000-4000-8000-0000000000" + strconv.FormatInt(int64(index+1), 16) + "c"
	}
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: ids}
	if _, err := agentprofile.NormalizeFieldRules(rules); err != nil {
		t.Fatal("exact explicit Community target cardinality rejected")
	}
}

func TestAgentProfileVisibilityRecordMetadataAndNoModelPermission(t *testing.T) {
	meta := profileFixture(actorref.Person)
	meta.ProfileVersion = 42
	rules := agentprofile.DefaultFieldRules()
	record, err := agentprofile.NewVisibilityRecord(meta, rules, false)
	if err != nil || record.SchemaVersion != agentprofile.VisibilitySchemaV1 || record.Profile.ProfileVersion != 42 || record.Configured {
		t.Fatal("unconfigured visibility resource invented another revision or source")
	}
	for _, tc := range []struct {
		name string
		edit func(*agentprofile.VisibilityRecord)
	}{
		{"unknown schema", func(r *agentprofile.VisibilityRecord) { r.SchemaVersion = "agent-profile-visibility-v2" }},
		{"missing metadata", func(r *agentprofile.VisibilityRecord) { r.Profile = agentprofile.Record{} }},
		{"Org owner", func(r *agentprofile.VisibilityRecord) { r.Profile.OwnerType = actorref.Organization }},
		{"Biz owner", func(r *agentprofile.VisibilityRecord) { r.Profile.OwnerType = actorref.Business }},
		{"Community owner", func(r *agentprofile.VisibilityRecord) { r.Profile.OwnerType = actorref.Community }},
		{"missing rules", func(r *agentprofile.VisibilityRecord) { r.Rules = nil }},
		{"unconfigured nondefault", func(r *agentprofile.VisibilityRecord) {
			r.Rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, err := agentprofile.NewVisibilityRecord(meta, agentprofile.DefaultFieldRules(), false)
			if err != nil {
				t.Fatal("invalid visibility test fixture")
			}
			tc.edit(&current)
			if !errors.Is(agentprofile.ValidateVisibilityRecord(current), agentprofile.ErrInvalid) {
				t.Fatal("invalid visibility record accepted")
			}
		})
	}
	rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityAgentOnly}
	record, err = agentprofile.NewVisibilityRecord(meta, rules, true)
	if err != nil || record.Rules[agentprofile.FieldAgentNotes].Visibility != agentprofile.VisibilityAgentOnly {
		t.Fatal("explicit agent-only configuration rejected")
	}
	rules[agentprofile.FieldAgentNotes] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityPublic}
	if record.Rules[agentprofile.FieldAgentNotes].Visibility != agentprofile.VisibilityAgentOnly {
		t.Fatal("visibility record aliased caller policy")
	}
	_, err = (agentcognitive.UnavailableCognitivePorts{}).SubmitMemoryCandidate(context.Background(), agentcognitive.CandidateSubmission{})
	if !errors.Is(err, agentcognitive.ErrUnavailable) {
		t.Fatal("AGENT_ONLY created Memory/provider authority")
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(visibilityEncodedRecord(t, record), &envelope); err != nil || len(envelope) != 4 {
		t.Fatal("visibility owner DTO gained authority/contents fields")
	}
	for _, key := range []string{"schemaVersion", "profile", "rules", "configured"} {
		if envelope[key] == nil {
			t.Fatal("visibility owner contract key missing")
		}
	}
}

func visibilityEncodedRecord(t *testing.T, record agentprofile.VisibilityRecord) []byte {
	t.Helper()
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal("could not encode visibility owner resource")
	}
	return body
}

func TestAgentProfileVisibilityStrictCodecValidAndBounds(t *testing.T) {
	rules := agentprofile.DefaultFieldRules()
	rules[agentprofile.FieldBio] = agentprofile.FieldRule{Visibility: agentprofile.VisibilityCommunity, CommunityIDs: []string{visibilityCommunityB, strings.ToUpper(visibilityCommunityA)}}
	encoded := visibilityEncodedRules(t, rules)
	decoded, err := agentprofile.DecodeFieldRules([]byte(encoded))
	if err != nil || !reflect.DeepEqual(decoded[agentprofile.FieldBio].CommunityIDs, []string{visibilityCommunityA, visibilityCommunityB}) {
		t.Fatal("strict rules codec failed canonical targets")
	}
	for _, version := range []int64{1, 42, math.MaxInt64} {
		input, err := agentprofile.DecodeReplaceVisibilityInput([]byte(`{"rules":` + encoded + `,"expectedVersion":` + strconv.FormatInt(version, 10) + `}`))
		if err != nil || input.ExpectedVersion != version || !reflect.DeepEqual(input.Rules, decoded) {
			t.Fatal("valid expected source version lost in strict input")
		}
		var viaJSON agentprofile.ReplaceVisibilityInput
		if err = json.Unmarshal([]byte(`{"rules":`+encoded+`,"expectedVersion":`+strconv.FormatInt(version, 10)+`}`), &viaJSON); err != nil || !reflect.DeepEqual(viaJSON, input) {
			t.Fatal("ordinary JSON bypassed typed strict input")
		}
	}
	body := visibilityBody(encoded)
	limited := append(body, []byte(strings.Repeat(" ", agentprofile.MaxVisibilityBodyBytes-len(body)))...)
	if _, err := agentprofile.DecodeReplaceVisibilityInput(limited); err != nil {
		t.Fatal("exact visibility body limit rejected")
	}
	if _, err := agentprofile.DecodeReplaceVisibilityInput(append(limited, ' ')); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("visibility body overflow accepted")
	}
	rawRules := []byte(encoded + strings.Repeat(" ", agentprofile.MaxVisibilityBodyBytes-len(encoded)))
	if _, err := agentprofile.DecodeFieldRules(rawRules); err != nil {
		t.Fatal("exact stored-rules parser limit rejected")
	}
	if _, err := agentprofile.DecodeFieldRules(append(rawRules, ' ')); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("stored-rules parser limit ignored")
	}
	if _, err := agentprofile.NormalizeReplaceVisibilityInput(agentprofile.ReplaceVisibilityInput{ExpectedVersion: 0, Rules: rules}); !errors.Is(err, agentprofile.ErrInvalid) {
		t.Fatal("missing current source version accepted")
	}
}

func TestAgentProfileVisibilityWireRedTeam(t *testing.T) {
	defaults := visibilityEncodedRules(t, agentprofile.DefaultFieldRules())
	for _, tc := range []struct{ name, body string }{
		{"empty body", ``}, {"null body", `null`}, {"array body", `[]`}, {"missing keys", `{}`},
		{"missing rules", `{"expectedVersion":1}`}, {"missing expected version", `{"rules":` + defaults + `}`},
		{"null rules", `{"expectedVersion":1,"rules":null}`}, {"null version", `{"expectedVersion":null,"rules":` + defaults + `}`},
		{"string version", `{"expectedVersion":"1","rules":` + defaults + `}`}, {"boolean version", `{"expectedVersion":true,"rules":` + defaults + `}`},
		{"zero version", `{"expectedVersion":0,"rules":` + defaults + `}`}, {"negative version", `{"expectedVersion":-1,"rules":` + defaults + `}`},
		{"fraction version", `{"expectedVersion":1.0,"rules":` + defaults + `}`}, {"exponent version", `{"expectedVersion":1e0,"rules":` + defaults + `}`},
		{"overflow version", `{"expectedVersion":9223372036854775808,"rules":` + defaults + `}`},
		{"version casing", `{"ExpectedVersion":1,"rules":` + defaults + `}`}, {"rules casing", `{"expectedVersion":1,"Rules":` + defaults + `}`},
		{"duplicate version", `{"expectedVersion":1,"expectedVersion":2,"rules":` + defaults + `}`},
		{"escaped duplicate version", `{"expectedVersion":1,"\u0065xpectedVersion":2,"rules":` + defaults + `}`},
		{"duplicate rules", `{"expectedVersion":1,"rules":` + defaults + `,"rules":` + defaults + `}`},
		{"owner authority", `{"expectedVersion":1,"rules":` + defaults + `,"ownerId":"WIRE_PRIVATE_MARKER"}`},
		{"Agent authority", `{"expectedVersion":1,"rules":` + defaults + `,"agentId":"WIRE_PRIVATE_MARKER"}`},
		{"role authority", `{"expectedVersion":1,"rules":` + defaults + `,"role":"owner"}`},
		{"confirmation authority", `{"expectedVersion":1,"rules":` + defaults + `,"confirmed":true}`},
		{"model grant authority", `{"expectedVersion":1,"rules":` + defaults + `,"grant":{"provider":true}}`},
		{"claimed server policy", `{"expectedVersion":1,"rules":` + defaults + `,"policy":{"allowed":true}}`},
		{"claimed authoritative revision", `{"expectedVersion":1,"rules":` + defaults + `,"profileVersion":2}`},
		{"partial bool flag", `{"expectedVersion":1,"rules":{"bio":{"is_public":true}}}`},
		{"multiple JSON", string(visibilityBody(defaults)) + ` {}`}, {"trailing comma", `{"expectedVersion":1,"rules":` + defaults + `,}`},
		{"invalid UTF8", string(visibilityBody(defaults)) + "\xff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, err := agentprofile.DecodeReplaceVisibilityInput([]byte(tc.body))
			if !errors.Is(err, agentprofile.ErrInvalid) || !reflect.ValueOf(input).IsZero() || strings.Contains(err.Error(), "WIRE_PRIVATE_MARKER") {
				t.Fatal("invalid visibility wire authority not generically/atomically rejected")
			}
			var bypass agentprofile.ReplaceVisibilityInput
			if err := json.Unmarshal([]byte(tc.body), &bypass); err == nil {
				t.Fatal("generic JSON bypass accepted malformed visibility authority")
			}
		})
	}
	for _, field := range agentprofile.ConfigurableFieldKeys() {
		for _, tc := range []struct{ name, raw string }{
			{"null rule", `null`}, {"array rule", `[]`}, {"bare audience", `"PUBLIC"`}, {"missing audience", `{}`},
			{"null audience", `{"visibility":null}`}, {"audience casing", `{"Visibility":"PUBLIC"}`},
			{"duplicate audience", `{"visibility":"PRIVATE","visibility":"PUBLIC"}`},
			{"escaped duplicate audience", `{"visibility":"PRIVATE","\u0076isibility":"PUBLIC"}`},
			{"duplicate Community targets", `{"visibility":"PUBLIC","communityIds":[],"communityIds":[]}`},
			{"null targets", `{"visibility":"PUBLIC","communityIds":null}`},
			{"wrong targets type", `{"visibility":"PUBLIC","communityIds":{}}`},
			{"null target item", `{"visibility":"COMMUNITY","communityIds":[null]}`},
			{"number target item", `{"visibility":"COMMUNITY","communityIds":[1]}`},
			{"target principal injection", `{"visibility":"COMMUNITY","communityIds":[{"type":"COMMUNITY","id":"WIRE_PRIVATE_MARKER"}]}`},
			{"unknown nested grant", `{"visibility":"PUBLIC","grant":true}`},
		} {
			t.Run(string(field)+" "+tc.name, func(t *testing.T) {
				raw := strings.TrimSuffix(defaults, "}") + `,"` + string(field) + `":` + tc.raw + `}`
				// This replaces the existing field, not merely testing a duplicate.
				var rawMap map[string]json.RawMessage
				if err := json.Unmarshal([]byte(defaults), &rawMap); err != nil {
					t.Fatal("invalid finite rules fixture")
				}
				rawMap[string(field)] = json.RawMessage(tc.raw)
				body, err := json.Marshal(rawMap)
				if err != nil {
					t.Fatal("invalid rule matrix fixture")
				}
				if _, err := agentprofile.DecodeFieldRules(body); !errors.Is(err, agentprofile.ErrInvalid) {
					t.Fatal("malformed nested rule accepted")
				}
				if _, err := agentprofile.DecodeFieldRules([]byte(raw)); !errors.Is(err, agentprofile.ErrInvalid) {
					t.Fatal("duplicate configurable source accepted")
				}
			})
		}
	}
}

func TestAgentProfileVisibilityProjectionTypesAndMinimalDisclosure(t *testing.T) {
	profile := profileFixture(actorref.Person)
	fields := map[agentprofile.FieldKey]json.RawMessage{
		agentprofile.FieldDisplayName: json.RawMessage(`" 历史显示名原文 "`), agentprofile.FieldBio: json.RawMessage(`"历史公开简介"`),
		agentprofile.FieldPersonalPreferences: json.RawMessage(`["安静环境"]`), agentprofile.FieldSocialPreferences: json.RawMessage(`["先通过活动认识"]`),
		agentprofile.FieldAvailability: json.RawMessage(`"明确自行描述，不是时间承诺"`), agentprofile.FieldPreferredActivityTypes: json.RawMessage(`["羽毛球"]`),
		agentprofile.FieldTravelPreferences: json.RawMessage(`["公共交通"]`), agentprofile.FieldInteractionPreferences: json.RawMessage(`["先文字交流"]`),
		agentprofile.FieldPrivateCityHistory: json.RawMessage(`"明确用户历史描述"`), agentprofile.FieldLanguagePreferences: json.RawMessage(`["中文","English"]`),
		agentprofile.FieldAgentNotes: json.RawMessage(`"明确用户说明"`),
	}
	projected := agentprofile.ProjectedRecord{AccountID: profile.OwnerID, Fields: fields}
	if err := agentprofile.ValidateProjectedRecord(projected); err != nil {
		t.Fatal("authorized typed projection shape rejected")
	}
	body, err := json.Marshal(projected)
	if err != nil {
		t.Fatal("could not encode projection")
	}
	var envelope map[string]json.RawMessage
	if err = json.Unmarshal(body, &envelope); err != nil || len(envelope) != 2 || envelope["accountId"] == nil || envelope["fields"] == nil {
		t.Fatal("external projection exposed source/policy metadata")
	}
	for _, forbidden := range []string{"profile", "rules", "configured", "communityIds", "agentId", "profileVersion", "grant", "consent", "purpose"} {
		if envelope[forbidden] != nil {
			t.Fatal("external projection has an authorization/configuration field")
		}
	}
	if string(projected.Fields[agentprofile.FieldDisplayName]) != `" 历史显示名原文 "` {
		t.Fatal("validation rewrote historical public source text")
	}
	for _, value := range []json.RawMessage{json.RawMessage(`""`), json.RawMessage(`"` + strings.Repeat("字", 1500) + `"`), json.RawMessage(`"\ud83d\ude00"`), json.RawMessage(`"合法替代字符�"`), json.RawMessage(`"literal \\ud800"`)} {
		if err := agentprofile.ValidateProjectedRecord(agentprofile.ProjectedRecord{AccountID: profile.OwnerID, Fields: map[agentprofile.FieldKey]json.RawMessage{agentprofile.FieldDisplayName: value}}); err != nil {
			t.Fatal("historical public read inherited a new editing/normalization limit")
		}
	}
	if err := agentprofile.ValidateProjectedRecord(agentprofile.ProjectedRecord{AccountID: profile.OwnerID, Fields: map[agentprofile.FieldKey]json.RawMessage{}}); err != nil {
		t.Fatal("empty finite projection shape rejected before Store ACL can decide not-found")
	}
}

func TestAgentProfileVisibilityProjectionRejectsMalformedOrNoncanonicalValues(t *testing.T) {
	owner := profileFixture(actorref.Person).OwnerID
	for _, tc := range []struct {
		name  string
		key   agentprofile.FieldKey
		value json.RawMessage
	}{
		{"unknown property", "grant", json.RawMessage(`true`)},
		{"wrong field casing", "Bio", json.RawMessage(`"private"`)},
		{"nil value", agentprofile.FieldBio, nil}, {"null public text", agentprofile.FieldBio, json.RawMessage(`null`)},
		{"array public text", agentprofile.FieldBio, json.RawMessage(`[]`)}, {"object public text", agentprofile.FieldBio, json.RawMessage(`{"private":"marker"}`)},
		{"number public text", agentprofile.FieldBio, json.RawMessage(`42`)}, {"multiple public values", agentprofile.FieldBio, json.RawMessage(`"one" "two"`)},
		{"invalid UTF8", agentprofile.FieldBio, json.RawMessage("\"\xff\"")}, {"lone high surrogate", agentprofile.FieldBio, json.RawMessage(`"\ud800"`)},
		{"lone low surrogate", agentprofile.FieldBio, json.RawMessage(`"\udc00"`)}, {"high followed by non-low", agentprofile.FieldBio, json.RawMessage(`"\ud800\u0001"`)},
		{"null private array", agentprofile.FieldPersonalPreferences, json.RawMessage(`null`)},
		{"private array is string", agentprofile.FieldPersonalPreferences, json.RawMessage(`"text"`)},
		{"private array contains null", agentprofile.FieldPersonalPreferences, json.RawMessage(`[null]`)},
		{"private array contains number", agentprofile.FieldPersonalPreferences, json.RawMessage(`[1]`)},
		{"private array duplicate", agentprofile.FieldPersonalPreferences, json.RawMessage(`["same","same"]`)},
		{"private array not trimmed", agentprofile.FieldPersonalPreferences, json.RawMessage(`[" same "]`)},
		{"private array too long item", agentprofile.FieldLanguagePreferences, json.RawMessage(`["` + strings.Repeat("字", 161) + `"]`)},
		{"private scalar not trimmed", agentprofile.FieldAgentNotes, json.RawMessage(`" source "`)},
		{"private scalar wrong type", agentprofile.FieldAvailability, json.RawMessage(`false`)},
		{"private scalar null", agentprofile.FieldPrivateCityHistory, json.RawMessage(`null`)},
		{"private scalar noncanonical CRLF", agentprofile.FieldAgentNotes, json.RawMessage(`"one\r\ntwo"`)},
		{"private scalar controls", agentprofile.FieldAgentNotes, json.RawMessage(`"\u0000"`)},
		{"private scalar too long", agentprofile.FieldAgentNotes, json.RawMessage(`"` + strings.Repeat("a", 2001) + `"`)},
		{"total projection too big", agentprofile.FieldBio, json.RawMessage(`"` + strings.Repeat("a", agentprofile.MaxVisibilityBodyBytes) + `"`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !errors.Is(agentprofile.ValidateProjectedRecord(agentprofile.ProjectedRecord{AccountID: owner, Fields: map[agentprofile.FieldKey]json.RawMessage{tc.key: tc.value}}), agentprofile.ErrInvalid) {
				t.Fatal("malformed/noncanonical projection accepted")
			}
		})
	}
	for _, invalid := range []agentprofile.ProjectedRecord{{AccountID: owner}, {AccountID: "", Fields: map[agentprofile.FieldKey]json.RawMessage{}}, {AccountID: "00000000-0000-0000-0000-000000000000", Fields: map[agentprofile.FieldKey]json.RawMessage{}}} {
		if !errors.Is(agentprofile.ValidateProjectedRecord(invalid), agentprofile.ErrInvalid) {
			t.Fatal("missing account/map projection accepted")
		}
	}
	privateAggregate := agentprofile.ProjectedRecord{AccountID: owner, Fields: map[agentprofile.FieldKey]json.RawMessage{
		agentprofile.FieldAvailability:       json.RawMessage(`"` + strings.Repeat("字", 2000) + `"`),
		agentprofile.FieldPrivateCityHistory: json.RawMessage(`"` + strings.Repeat("字", 2000) + `"`),
		agentprofile.FieldAgentNotes:         json.RawMessage(`"` + strings.Repeat("a", 1500) + `"`),
	}}
	if !errors.Is(agentprofile.ValidateProjectedRecord(privateAggregate), agentprofile.ErrInvalid) {
		t.Fatal("private projection aggregate limit bypassed original private source limit")
	}
}
