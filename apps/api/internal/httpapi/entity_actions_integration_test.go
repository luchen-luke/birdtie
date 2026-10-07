package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

func TestEntityActionHTTPRegisteredBoundOriginalWriters(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	read := func(kind, id string) ea.View {
		w := f.request(t, f.handler, "GET", "/v1/me/entity-actions/"+kind+"/"+id, "", f.tokens[0], 200, nil)
		var out struct {
			Data ea.View `json:"data"`
		}
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil || !out.Data.Valid() {
			t.Fatal(w.Body.String(), e)
		}
		return out.Data
	}
	write := func(method, path, body, token string, v ea.View, op string, want int, duplicate bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Birdtie-Action-Version", v.SourceVersion)
		r.Header.Set("X-Birdtie-Action-Until", v.ValidUntil.Format(time.RFC3339Nano))
		r.Header.Set("X-Birdtie-Action-Operation", op)
		if duplicate {
			r.Header.Add("X-Birdtie-Action-Version", v.SourceVersion)
		}
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal("original bound writer", method, path, op, w.Code, want, w.Body.String())
		}
		return w
	}
	v := read("activity", f.public)
	p := "/v1/activities/" + f.public + "/participations"
	write("POST", p, "{}", f.tokens[0], v, "JOIN", 400, true)
	write("POST", p, "{}", f.tokens[1], v, "JOIN", 409, false)
	write("POST", p, "{}", f.tokens[0], v, "CANCEL_RSVP", 400, false)
	write("POST", p, "{}", f.tokens[0], v, "JOIN", http.StatusCreated, false)
	write("POST", p, "{}", f.tokens[0], v, "JOIN", 409, false)
	v = read("activity", f.public)
	write("DELETE", p+"/me", "", f.tokens[0], v, "CANCEL_RSVP", 200, false)
	v = read("place", f.place)
	w := write("POST", "/v1/me/saved", `{"kind":"place","targetId":"`+f.place+`"}`, f.tokens[0], v, "SAVE", http.StatusCreated, false)
	var saved struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &saved); e != nil || saved.Data.ID == "" {
		t.Fatal("original saved ID", w.Body.String(), e)
	}
	v = read("place", f.place)
	write("DELETE", "/v1/me/saved/"+saved.Data.ID, "", f.tokens[0], v, "UNSAVE", http.StatusNoContent, false)
}

func TestEntityActionHTTPRegisteredCurrentDomainContract(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	for _, id := range []string{f.public, f.private} {
		p := "/v1/me/entity-actions/activity/" + id
		w := f.request(t, f.handler, "GET", p, "", f.tokens[0], 200, nil)
		var out struct {
			Data ea.View `json:"data"`
		}
		if json.Unmarshal(w.Body.Bytes(), &out) != nil || !out.Data.Valid() || out.Data.Entity.ID != id {
			t.Fatal("actual action contract", w.Body.String())
		}
		for _, a := range out.Data.Actions {
			if a.Target.ID != id {
				t.Fatal("original target changed")
			}
			if a.Kind == ea.Save && ((id == f.public && a.State != ea.Available) || (id == f.private && a.State != ea.Unavailable)) {
				t.Fatal("human read widened saved domain", w.Body.String())
			}
		}
		for _, private := range []string{"PRIVATE_INTENT_BODY", "UNREQUESTED_ACTIVITY_BODY_CANARY", "Console", "sourceSnapshot", "Proof", "Seal"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("internal/private source escaped")
			}
		}
		f.request(t, f.handler, "GET", p+"?ownerId="+f.accountIDs[1], "", f.tokens[0], 400, nil)
		f.request(t, f.handler, "GET", p+"?", "", f.tokens[0], 400, nil)
		f.request(t, f.handler, "GET", p, `{"allowed":true}`, f.tokens[0], 400, nil)
		f.request(t, f.handler, "GET", p, "", "", 401, nil)
		org := f.accountIDs[2]
		f.request(t, f.handler, "GET", p, "", f.tokens[0], 400, &org)
		f.request(t, f.handler, "GET", p, "", f.tokens[2], 403, nil)
	}
	f.request(t, f.handler, "GET", "/v1/me/entity-actions/opportunity/"+f.public, "", f.tokens[0], 400, nil)
	// A rejected method is handled by Go's mux, not the action handler. Verify
	// its real 405 without the private-resource helper's handler-only no-store
	// invariant; do not add a write endpoint merely to satisfy that helper.
	req := httptest.NewRequest("POST", "/v1/me/entity-actions/activity/"+f.public, strings.NewReader(`{"action":"EXECUTE"}`))
	req.Header.Set("Authorization", "Bearer "+f.tokens[0])
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, req)
	if w.Code != 405 {
		t.Fatal("unchecked executor appeared", w.Code)
	}
}

func TestEntityActionHTTPRegisteredAnonymousPublicAndNoDowngrade(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	f.handler = contextBuilderHTTPNew(f.store, f.store, f.store)
	path := "/v1/public/entity-actions/activity/" + f.public
	w := f.request(t, f.handler, "GET", path, "", "", 200, nil)
	var out struct {
		Data ea.View `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &out) != nil || !out.Data.Valid() {
		t.Fatal("public closed descriptor", w.Body.String())
	}
	for _, action := range out.Data.Actions {
		if action.Kind == ea.Share {
			if action.State != ea.Available || action.Operation != "EXPORT_PUBLIC" || len(action.AllowedOperations) != 1 {
				t.Fatal("public export must be explicit OS action", action)
			}
		} else if action.Kind != ea.Navigate && action.State != ea.Unavailable {
			t.Fatal("anonymous private action exposed")
		}
	}
	f.request(t, f.handler, "GET", "/v1/public/entity-actions/activity/"+f.private, "", "", 404, nil)
	f.request(t, f.handler, "GET", path, "", f.tokens[0], 401, nil)
	f.request(t, f.handler, "GET", "/v1/me/entity-actions/activity/"+f.public, "", "invalid", 401, nil)
	f.request(t, f.handler, "GET", path+"?actor=public", "", "", 400, nil)
	f.request(t, f.handler, "GET", "/v1/public/entity-actions/person/"+f.accountIDs[0], "", "", 400, nil)
}
