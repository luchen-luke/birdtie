package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	apc "github.com/birdtie/birdtie/apps/api/internal/agentprofilecompletion"
)

// Native identities + PG Store + actual New registered routes. The separate
// pure leaf harness is never substituted for this native registered proof.
func TestProfileMemoryCompletionHTTPNativeRegisteredLifecycleAndNegative(t *testing.T) {
	v4PrivacyHTTPDatabase(t)
	f := privateProfileHTTPDBNew(t)
	var ownedDatabase string
	if f.pool.QueryRow(f.ctx, `SELECT current_database()`).Scan(&ownedDatabase) != nil || !strings.HasPrefix(ownedDatabase, "birdtie_saf001_http_") {
		t.Fatal("owned registered HTTP database required")
	}
	t.Logf("PROFILE_COMPLETION owned registered HTTP database %s", ownedDatabase)
	a := agentprofile.PrivateAccess{SessionDigest: contextPurposeHTTPDigest(t, f.tokens[0]), WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	r, e := f.store.ReadOwnAgentPrivateProfile(f.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	fields := agentprofile.PrivateFields{AgentNotes: privateProfileHTTPMarker, SocialPreferences: []string{"其它私密字段"}}
	r, e = f.store.ReplaceOwnAgentPrivateProfile(f.ctx, a, agentprofile.ReplacePrivateInput{ExpectedVersion: r.Profile.ProfileVersion, Fields: fields})
	if e != nil {
		t.Fatal(e)
	}
	var id, previewID string
	if f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&id, &previewID) != nil {
		t.Fatal("owned native IDs")
	}
	raw, _ := json.Marshal(map[string]string{"activityCategory": "hiking", "nature": "human-declaration"})
	memory, e := f.store.PutOwnMemory(f.ctx, a, id, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:hiking", Summary: agentmemorycandidate.Statement("hiking"), StructuredValue: raw, Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)})
	if e != nil {
		t.Fatal(e)
	}
	h := f.handler
	before := f.effects(t)
	w := f.request(t, h, "GET", profileCompletionPath+"/suggestions", "", f.tokens[0], 200, nil)
	s := enrichmentHTTPData[apc.Suggestions](t, w.Body.Bytes())
	if apc.ValidateSuggestions(s) != nil || len(s.Sources) != 1 || s.Sources[0].MemoryID != id {
		t.Fatal("native suggestions")
	}
	input := apc.PreviewInput{PreviewID: previewID, MemoryID: id, MemoryVersion: memory.Version, ExpectedProfileVersion: r.Profile.ProfileVersion}
	w = f.request(t, h, "POST", profileCompletionPath+"/previews", enrichmentHTTPJSON(t, input), f.tokens[0], 200, nil)
	p := enrichmentHTTPData[apc.Preview](t, w.Body.Bytes())
	if apc.ValidatePreview(p) != nil || strings.Contains(w.Body.String(), privateProfileHTTPMarker) {
		t.Fatal("full private profile leaked into single-field review")
	}
	f.request(t, h, "POST", profileCompletionPath+"/previews/"+p.ID+"/accept", `{"confirmed":true}`, f.tokens[0], 400, nil)
	f.request(t, h, "GET", profileCompletionPath+"/previews/"+p.ID, "", f.tokens[1], 403, nil)
	f.request(t, h, "POST", profileCompletionPath+"/previews/"+p.ID+"/accept", enrichmentHTTPJSON(t, apc.AcceptInput{PlanDigest: p.PlanDigest}), f.tokens[1], 403, nil)
	w = f.request(t, h, "POST", profileCompletionPath+"/previews/"+p.ID+"/accept", enrichmentHTTPJSON(t, apc.AcceptInput{PlanDigest: p.PlanDigest}), f.tokens[0], 200, nil)
	receipt := enrichmentHTTPData[apc.Receipt](t, w.Body.Bytes())
	if receipt.State != "COMMITTED" || !receipt.CurrentProfileMatches || strings.Contains(w.Body.String(), agentmemorycandidate.Statement("hiking")) {
		t.Fatal("receipt not bodyless/current native result")
	}
	token := f.newSession(f.accountIDs[0], false, false)
	f.request(t, h, "POST", profileCompletionPath+"/previews/"+p.ID+"/accept", enrichmentHTTPJSON(t, apc.AcceptInput{PlanDigest: p.PlanDigest}), token, 403, nil)
	w = f.request(t, h, "GET", profileCompletionPath+"/previews/"+p.ID, "", token, 200, nil)
	current := enrichmentHTTPData[apc.Receipt](t, w.Body.Bytes())
	if current.State != "COMMITTED" || current.ResultProfileVersion == nil || *current.ResultProfileVersion != *receipt.ResultProfileVersion {
		t.Fatal("new session only exact metadata receipt")
	}
	f.assertEffectsUnchanged(t, before)
	t.Log("REGISTERED_NEW_HANDLER_NATIVE=true NEW_SESSION_POST_FORBIDDEN=true BODYLESS_RECEIPT=true")
	// The old registered private Profile resource remains the actual authority.
	w = f.request(t, f.handler, "GET", "/v1/me/agent-private-profile", "", token, http.StatusOK, nil)
	saved := privateProfileHTTPDBRecord(t, w)
	if saved.Fields.AgentNotes != fields.AgentNotes || len(saved.Fields.PreferredActivityTypes) != 1 {
		t.Fatal("old nine-field API/result was replaced")
	}
}
