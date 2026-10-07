package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
)

func TestProfileCompletionHTTPNativeCurrentGroupAndMissingFields(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	source := seedHTTPRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", f.tokens[0], 200, nil))
	partial := agentseed.Input{ExpectedSnapshot: source.Snapshot, Action: "SAVE", DisplayName: source.DisplayName, InterestChoice: "SET", Interests: []string{"摄影"}}
	raw, _ := json.Marshal(partial)
	f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), f.tokens[0], 400, nil)
	unchanged := seedHTTPRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", f.tokens[0], 200, nil))
	if unchanged.CurrentCity != nil || unchanged.Intent.BasicIntent != "" || len(unchanged.LanguagePreferences) != 0 || len(unchanged.Interests) != 0 || unchanged.Snapshot != source.Snapshot {
		t.Fatal("missing draft became persisted fact")
	}
	input := agentseed.Input{ExpectedSnapshot: source.Snapshot, Action: "SAVE", DisplayName: "中文本人昵称", CurrentCityID: source.Cities[0].ID, CurrentCitySnapshot: source.Cities[0].Snapshot, LanguagePreferences: []string{"zh-CN"}, BasicIntent: "JUST_EXPLORE", InterestChoice: "SKIP", Interests: []string{}}
	raw, _ = json.Marshal(input)
	source = seedHTTPRecord(t, f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), f.tokens[0], 200, nil))
	input.ExpectedSnapshot = source.Snapshot
	input.CurrentCityID = source.CurrentCity.ID
	input.CurrentCitySnapshot = source.CurrentCity.Snapshot
	input.InterestChoice = "SET"
	input.Interests = []string{"私密渐进摄影偏好"}
	raw, _ = json.Marshal(input)
	next := seedHTTPRecord(t, f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), f.tokens[0], 200, nil))
	if next.DisplayName != source.DisplayName || next.Intent.BasicIntent != source.Intent.BasicIntent || next.CurrentCity.ID != source.CurrentCity.ID || len(next.Interests) != 1 || next.Interests[0] != input.Interests[0] {
		t.Fatal("actual single group result", next)
	}
	f.request(t, f.handler, "PUT", "/v1/me/agent-seed", string(raw), f.tokens[0], 409, nil)
	persisted := seedHTTPRecord(t, f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", f.tokens[0], 200, nil))
	if persisted.Snapshot != next.Snapshot {
		t.Fatal("reopen lost current result")
	}
	projection := f.request(t, f.handler, "GET", "/v1/accounts/"+f.accountIDs[0]+"/profile", "", f.tokens[1], 200, nil)
	if strings.Contains(projection.Body.String(), "私密渐进摄影偏好") || strings.Contains(projection.Body.String(), "JUST_EXPLORE") {
		t.Fatal("private group leaked in public projection")
	}
	f.request(t, f.handler, "GET", "/v1/me/agent-seed", "", f.tokens[2], 403, nil)
	t.Log("LOCAL_SYNTHETIC_ONLY registered human progressive edit; no Agent automatic inference")
}
