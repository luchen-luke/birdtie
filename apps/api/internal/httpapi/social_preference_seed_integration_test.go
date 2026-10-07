package httpapi

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"reflect"
	"strings"
	"testing"
)

func TestSocialPreferenceSeedHTTPNativeRegisteredPreservation(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	read := privateProfileHTTPDBRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-private-profile", "", f.tokens[0], 200, nil))
	fields := agentprofile.PrivateFields{PersonalPreferences: []string{"PRIVATE_SOCIAL_PERSONAL_CANARY"}, SocialPreferences: []string{"PRIVATE_SOCIAL_EXISTING_CANARY"}, Availability: "PRIVATE_SOCIAL_AVAILABILITY_CANARY", PreferredActivityTypes: []string{"PRIVATE_SOCIAL_ACTIVITY_CANARY"}, TravelPreferences: []string{"PRIVATE_SOCIAL_TRAVEL_CANARY"}, InteractionPreferences: []string{"PRIVATE_SOCIAL_INTERACTION_CANARY"}, PrivateCityHistory: "PRIVATE_SOCIAL_HISTORY_CANARY", LanguagePreferences: []string{"zh-CN"}, AgentNotes: "PRIVATE_SOCIAL_NOTES_CANARY"}
	input := agentprofile.ReplacePrivateInput{ExpectedVersion: read.Profile.ProfileVersion, Fields: fields}
	raw, _ := json.Marshal(input)
	original := privateProfileHTTPDBRecord(t, f.request(t, f.handler, "PUT", "/v1/me/agent-private-profile", string(raw), f.tokens[0], 200, nil))
	input.ExpectedVersion = original.Profile.ProfileVersion
	input.Fields.SocialPreferences = []string{"同一大学", "国际社群", "小群体"}
	raw, _ = json.Marshal(input)
	saved := privateProfileHTTPDBRecord(t, f.request(t, f.handler, "PUT", "/v1/me/agent-private-profile", string(raw), f.tokens[0], 200, nil))
	expected, actual := original.Fields, saved.Fields
	actual.SocialPreferences = expected.SocialPreferences
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("registered API erased eight fields")
	}
	if !reflect.DeepEqual(saved.Fields.SocialPreferences, input.Fields.SocialPreferences) {
		t.Fatal("registered selection")
	}
	f.request(t, f.handler, "PUT", "/v1/me/agent-private-profile", string(raw), f.tokens[0], 409, nil)
	after := privateProfileHTTPDBRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-private-profile", "", f.tokens[0], 200, nil))
	if !reflect.DeepEqual(after.Fields, saved.Fields) || after.Profile.ProfileVersion != saved.Profile.ProfileVersion {
		t.Fatal("native re-read")
	}
	for _, token := range []string{"", f.tokens[2], f.tokens[3]} {
		code := 403
		if token == "" {
			code = 401
		}
		f.request(t, f.handler, "GET", "/v1/me/agent-private-profile", "", token, code, nil)
	}
	public := f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[0], 200, nil)
	if strings.Contains(public.Body.String(), "同一大学") || strings.Contains(public.Body.String(), "socialPreferences") {
		t.Fatal("private social preference leaked to ordinary Profile")
	}
	foreign := privateProfileHTTPDBRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-private-profile", "", f.tokens[1], 200, nil))
	if foreign.Profile.OwnerID != f.accountIDs[1] || len(foreign.Fields.SocialPreferences) != 0 {
		t.Fatal("foreign self read inherited owner content")
	}
	t.Log("LOCAL_SYNTHETIC_ONLY registered native preference; no education/community identity or machine policy activation")
}
