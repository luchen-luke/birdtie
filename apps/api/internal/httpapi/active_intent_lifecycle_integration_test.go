package httpapi

import (
	"context"
	"encoding/json"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestActiveIntentHTTPNativeRegisteredPreviewAndOriginalID(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	gateway, e := postgres.NewHumanActiveIntents(f.store)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if _, e = f.store.EnsureAgentProfile(f.ctx, f.agentIDs[i], actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[i]}); e != nil {
			t.Fatal(e)
		}
	}
	r, e := f.store.CreateSocialIntentDraft(f.ctx, f.accountIDs[0], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "合成HTTP本人意图", Constraints: json.RawMessage(`{}`), Modality: "ONLINE", Audience: "PRIVATE", ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e = f.pool.Exec(ctx, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
	})
	h := New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithHumanActiveIntents(gateway))
	base := "/v1/me/active-social-intents"
	request := func(method, path, body string, who int, org bool) *httptest.ResponseRecorder {
		q := httptest.NewRequest(method, path, strings.NewReader(body))
		q.Header.Set("Authorization", "Bearer "+f.tokens[who])
		q.Header.Set("Content-Type", "application/json")
		if org {
			q.Header.Set("X-Birdtie-Organization-Workspace", r.ID)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, q)
		return w
	}
	w := request("GET", base+"/"+r.ID, "", 0, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	t.Log("NOW002_WIRE_DETAIL " + w.Body.String())
	var current struct {
		Data ai.Detail `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &current); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{base, base + "/options"} {
		w = request("GET", path, "", 0, false)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		t.Log("NOW002_WIRE_READ " + path + " " + w.Body.String())
	}
	for _, x := range []struct {
		who  int
		org  bool
		want int
	}{{1, false, 404}, {0, true, 403}} {
		w = request("GET", base+"/"+r.ID, "", x.who, x.org)
		if w.Code != x.want {
			t.Fatal("owner boundary", w.Code, w.Body.String())
		}
	}
	// Preserve the original stable ID through a real multi-field edit before
	// cancellation. JSONB ordering and UTC normalization are real wire contracts.
	edit := ai.DraftOf(current.Data.Item.Intent)
	edit.Title = "合成具体结构化版本"
	edit.Modality = "HYBRID"
	edit.Constraints = json.RawMessage(`{"category":"badminton","areaLabel":"合成区域","onlinePlatform":"Zoom","minParticipants":2,"maxParticipants":4,"startsAt":"2030-01-01T18:00:00+08:00","endsAt":"2030-01-01T19:00:00+08:00"}`)
	payloadEdit, _ := json.Marshal(ai.Input{Operation: "EDIT", ExpectedVersion: current.Data.Item.Version, Edit: &edit})
	w = request("POST", base+"/"+r.ID+"/preview", string(payloadEdit), 0, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	t.Log("NOW002_WIRE_EDIT_PREVIEW " + w.Body.String())
	var editedPreview struct {
		Data ai.Preview `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &editedPreview); e != nil {
		t.Fatal(e)
	}
	approvedEdit, _ := json.Marshal(map[string]string{"previewId": editedPreview.Data.PreviewID})
	w = request("POST", base+"/"+r.ID+"/approve", string(approvedEdit), 0, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	t.Log("NOW002_WIRE_EDIT_RECEIPT " + w.Body.String())
	var editedReceipt struct {
		Data ai.Receipt `json:"data"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &editedReceipt); e != nil || editedReceipt.Data.Item.Intent.ID != r.ID || editedReceipt.Data.Item.Intent.Status != "DRAFT" {
		t.Fatal("registered same-ID edit", e)
	}
	current.Data.Item = editedReceipt.Data.Item
	if w = request("POST", base+"/"+r.ID+"/approve", `{"confirmed":true}`, 0, false); w.Code != 400 {
		t.Fatal("legacy boolean gained concrete authority", w.Code)
	}
	payload, _ := json.Marshal(ai.Input{Operation: "CANCEL", ExpectedVersion: current.Data.Item.Version})
	w = request("POST", base+"/"+r.ID+"/preview", string(payload), 0, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var p struct {
		Data ai.Preview `json:"data"`
	}
	t.Log("NOW002_WIRE_PREVIEW " + w.Body.String())
	if e = json.Unmarshal(w.Body.Bytes(), &p); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]string{"previewId": p.Data.PreviewID})
	w = request("POST", base+"/"+r.ID+"/approve", string(body), 0, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var got struct {
		Data ai.Receipt `json:"data"`
	}
	t.Log("NOW002_WIRE_RECEIPT " + w.Body.String())
	if e = json.Unmarshal(w.Body.Bytes(), &got); e != nil || got.Data.Item.Intent.ID != r.ID || got.Data.Item.Intent.Status != "CANCELLED" {
		t.Fatal(e, w.Body.String())
	}
	if w = request("POST", base+"/"+r.ID+"/approve", string(body), 0, false); w.Code != 409 {
		t.Fatal("replay", w.Code, w.Body.String())
	}
}
