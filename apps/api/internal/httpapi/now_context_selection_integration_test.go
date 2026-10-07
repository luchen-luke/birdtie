package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	ncs "github.com/birdtie/birdtie/apps/api/internal/nowcontextselection"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type nowSelectionIntercept struct {
	ncs.Gateway
	after func()
}

func (g nowSelectionIntercept) ReadOptions(c context.Context, a ncs.Access) (ncs.OptionsReceipt, error) {
	v, e := g.Gateway.ReadOptions(c, a)
	if e == nil && g.after != nil {
		g.after()
	}
	return v, e
}
func TestNowSelectionHTTPNativeRegisteredOwnerModesAndWire(t *testing.T) {
	// All published City options participate in the native receipt, including
	// other packages' independently valid public catalog changes. This positive
	// fixture owns an actual current-schema database; default package concurrency
	// must not turn unrelated fixtures' catalog writes into a stale receipt.
	modelEgressHTTPOwnedDatabase(t)
	f := contextBuilderHTTPNative(t)
	key := "now006-http-" + f.accountIDs[0]
	own, e := f.store.DeclareContext(f.ctx, f.accountIDs[0], contextgraph.DeclarationInput{Type: contextgraph.Online, SourceKey: key, Relation: "interest"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.DeclareContext(f.ctx, f.accountIDs[0], contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.city, Relation: "current"}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		f.exec(`DELETE FROM person_contexts WHERE context_id=$1`, own.ContextID)
		f.exec(`DELETE FROM contexts WHERE id=$1`, own.ContextID)
	})
	g, e := postgres.NewNowContextSelection(f.store)
	if e != nil {
		t.Fatal(e)
	}
	app := func(port ncs.Gateway) http.Handler {
		return New(f.store, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil, WithNowContextSelection(port))
	} // New returns http.Handler; registered mux is used below.
	request := func(srv http.Handler, method, path, body string, who int, header string) *httptest.ResponseRecorder {
		q := httptest.NewRequest(method, path, strings.NewReader(body))
		q.Header.Set("Authorization", "Bearer "+f.tokens[who])
		q.Header.Set("Content-Type", "application/json")
		if header != "" {
			q.Header[header] = []string{""}
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, q)
		return w
	}
	h := app(g)
	base := "/v1/me/now/context-selection"
	w := request(h, "GET", base+"/options", "", 0, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	t.Log("NOW006_WIRE_OPTIONS " + w.Body.String())
	var v struct {
		Data ncs.Options `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &v) != nil || ncs.ValidateOptions(v.Data, f.accountIDs[0]) != nil {
		t.Fatal("native shape")
	}
	var selected ncs.Option
	for _, x := range v.Data.Items {
		if x.ContextID == own.ContextID {
			selected = x
		}
	}
	if selected.OptionID == "" {
		t.Fatal("missing actual own ONLINE")
	}
	in, _ := json.Marshal(ncs.Input{OptionsToken: v.Data.OptionsToken, OptionID: selected.OptionID})
	w = request(h, "POST", base+"/resolve", string(in), 0, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	t.Log("NOW006_WIRE_SELECTION " + w.Body.String())
	var got struct {
		Data ncs.Selection `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Data.Choice != selected || got.Data.Owner.ID != f.accountIDs[0] {
		t.Fatal("wrong exact native selection")
	}
	for _, x := range []struct {
		method, path, body, header string
		who, code                  int
	}{{"POST", base + "/resolve", string(in), "", 1, 409}, {"GET", base + "/options?", "", "", 0, 400}, {"GET", base + "/options?owner=x", "", "", 0, 400}, {"GET", base + "/options", "{}", "", 0, 400}, {"POST", base + "/resolve", `{"confirmed":true}`, "", 0, 400}, {"POST", base + "/resolve", string(in) + `{}`, "", 0, 400}, {"POST", base + "/resolve", strings.Replace(string(in), `"optionId":`, `"optionId":"dup","optionId":`, 1), "", 0, 400}, {"GET", base + "/options", "", "X-Birdtie-Organization-Workspace", 0, 403}, {"GET", base + "/options", "", "X-Birdtie-Business-Workspace", 0, 403}} {
		w = request(h, x.method, x.path, x.body, x.who, x.header)
		if w.Code != x.code {
			t.Fatal("closed request", x.path, w.Code, x.code, w.Body.String())
		}
	}
	w = request(app(nil), "GET", base+"/options", "", 0, "")
	if w.Code != 503 {
		t.Fatal("missing port not closed", w.Code)
	}
	// Prove the real closed catalog behavior deterministically. The changed City
	// is another actual public option, not the owner's selected ONLINE context.
	otherCity := "now006-catalog-" + f.accountIDs[1]
	oldLabel := "本地合成其他城市回执前标签"
	f.exec(`INSERT INTO cities(id,name,country_code,region,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'GB','LOCAL_SYNTHETIC','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:NOW006','合成维护者',$3)`, otherCity, oldLabel, f.accountIDs[1])
	f.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, otherCity)
	other, err := f.store.DeclareContext(f.ctx, f.accountIDs[1], contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: otherCity, Relation: "current"})
	if err != nil {
		t.Fatal("other actual public City declaration", err)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM person_contexts WHERE context_id=$1`, `DELETE FROM contexts WHERE id=$1`} {
			f.exec(q, other.ContextID)
		}
		f.exec(`DELETE FROM city_contexts WHERE city_id=$1`, otherCity)
		f.exec(`DELETE FROM cities WHERE id=$1`, otherCity)
	})
	catalogChanged := false
	changedCatalog := nowSelectionIntercept{Gateway: g, after: func() {
		if catalogChanged {
			t.Fatal("unexpected repeated options capture")
		}
		catalogChanged = true
		f.exec(`UPDATE cities SET name='本地合成其他城市回执后标签',updated_at=clock_timestamp() WHERE id=$1`, otherCity)
	}}
	// First demonstrate that the precise other City was in a successful native
	// options receipt. The intercepted registered GET then changes its source
	// after ReadOptions and before the handler's encoded-response revalidation.
	w = request(h, "GET", base+"/options", "", 0, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), oldLabel) || !strings.Contains(w.Body.String(), other.ContextID) {
		t.Fatal("other City missing from actual public receipt", w.Code, w.Body.String())
	}
	w = request(app(changedCatalog), "GET", base+"/options", "", 0, "")
	if !catalogChanged || w.Code != 409 || !strings.Contains(w.Body.String(), "now_context_selection_changed") || strings.Contains(w.Body.String(), oldLabel) || strings.Contains(w.Body.String(), key) || strings.Contains(w.Body.String(), "optionsToken") {
		t.Fatal("serialized stale public catalog receipt", w.Code, w.Body.String())
	}
	t.Log("LOCAL_SYNTHETIC other public City change after native ReadOptions: registered GET409, no stale private/public body")
	wrapped := nowSelectionIntercept{Gateway: g, after: func() {
		if e = f.store.RemoveContextDeclaration(f.ctx, f.accountIDs[0], own.ContextID, "interest"); e != nil {
			t.Fatal(e)
		}
	}}
	w = request(app(wrapped), "GET", base+"/options", "", 0, "")
	if w.Code != 409 || strings.Contains(w.Body.String(), key) {
		t.Fatal("serialized withdrawn private option", w.Code, w.Body.String())
	}
}
