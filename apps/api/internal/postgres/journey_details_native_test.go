package postgres

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyjourney"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

func journeyDetailsFixtureSource(t *testing.T) ([]byte, []byte) {
	t.Helper()
	base, _ := nativeAdaptationFixtureInput(t)
	var envelope struct {
		Tables map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(base))
	d.UseNumber()
	if d.Decode(&envelope) != nil {
		t.Fatal("synthetic Journey envelope")
	}
	return legacySyntheticInput(t, func(tables map[string]any) {
		for k, v := range envelope.Tables {
			tables[k] = v
		}
		row := func(table string, values map[string]any) map[string]any {
			r := map[string]any{}
			for _, c := range legacymigration.ColumnNames(table) {
				r[c] = nil
			}
			for k, v := range values {
				r[k] = v
			}
			return r
		}
		const owner = "14600000-0000-4000-8000-000000000010"
		const stamp = "2026-10-13T00:00:00Z"
		const jid = "15800000-0000-4000-8000-000000000010"
		const stop = "15800000-0000-4000-8000-000000000011"
		journey := func(id, title string) map[string]any {
			return row("journeys", map[string]any{"id": id, "owner_id": owner, "working_revision": json.Number("2"), "title": title, "created_at": stamp, "updated_at": stamp, "city": "原城市文字", "date_label": "原日期文字", "start_date": "2026-10-15", "days": json.Number("2"), "visibility": "private", "allow_copy": false, "team_revision": json.Number("1"), "scope_label": "", "operator_created": false})
		}
		dead := journey("15800000-0000-4000-8000-000000000019", "deleted synthetic source")
		dead["deleted_at"] = stamp
		tables["journeys"] = []any{journey(jid, "原私人旅程"), journey("15800000-0000-4000-8000-000000000018", ""), dead}
		tables["journey_stops"] = []any{row("journey_stops", map[string]any{"id": stop, "journey_id": jid, "position": json.Number("0"), "name": "原路段文字", "kind": "city", "source": "manual", "confidence": json.Number("1"), "confirmed": true, "created_at": stamp})}
		arrangements := []any{}
		for i, id := range []string{"15800000-0000-4000-8000-000000000012", "15800000-0000-4000-8000-000000000013"} {
			v := row("journey_arrangements", map[string]any{"id": id, "journey_id": jid, "type": "custom", "name": "私人安排", "day_index": json.Number("1"), "position": json.Number(string(rune('0' + i))), "time_label": "", "private_note": "原备注", "created_at": stamp, "updated_at": stamp, "start_time": "", "end_time": "", "transport_mode": "", "service_number": "", "departure_name": "", "arrival_name": "", "stop_id": stop, "custom_address": "", "source_text": "原来源文字"})
			if i == 0 {
				v["type"], v["place_id"] = "place", "15600000-0000-4000-8000-000000000003"
			}
			arrangements = append(arrangements, v)
		}
		tables["journey_arrangements"] = arrangements
		tables["journey_notes"] = []any{row("journey_notes", map[string]any{"id": "15800000-0000-4000-8000-000000000014", "journey_id": jid, "body": "原整体备注", "position": json.Number("0"), "updated_at": stamp})}
	})
}

func TestJourneyDetailsVerifiedSyntheticProjection(t *testing.T) {
	source, manifest := journeyDetailsFixtureSource(t)
	p, e := legacyjourney.Build(legacyTestRunID, source, manifest)
	if e != nil || p.Counts.Journeys != 2 || p.Counts.Entries != 4 || p.Counts.Stops != 1 || p.Counts.Arrangements != 2 || p.Counts.Notes != 1 || p.Counts.Tombstones != 1 {
		t.Fatal("verified native projection", e, p.Counts)
	}
	for _, j := range p.Journeys {
		if len(j.Details.Itinerary) > 0 && j.Details.Itinerary[1].StopID != j.Details.Itinerary[0].ID {
			t.Fatal("original stop link not retained")
		}
	}
}

// One actual registered API chain on a synthetic owned child database only.
func TestJourneyDetailsNativeOwnedImportEditAndNoResurrection(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	source, manifest := journeyDetailsFixtureSource(t)
	plan, e := legacyjourney.Build(legacyTestRunID, source, manifest)
	if e != nil || plan.Counts.Journeys != 2 || plan.Counts.Entries != 4 || plan.Counts.Tombstones != 1 {
		t.Fatal("complete synthetic native plan", e, plan.Counts)
	}
	if _, e = f.Importer.Apply(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal(e)
	}
	result, e := f.Importer.ApplyJourneyDetails(f.Context, legacyTestRunID, source, manifest)
	if e != nil || result.Counts != plan.Counts {
		t.Fatal("158 import", e)
	}
	var owner, status string
	if e = f.Pool.QueryRow(f.Context, `SELECT m.target_id,a.status FROM legacy_entity_mappings m JOIN accounts a ON a.id=m.target_id WHERE m.entity_kind='user'`).Scan(&owner, &status); e != nil || status != "suspended" {
		t.Fatal("import activated original owner", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	uuid := func() string {
		v, e := legacyMigrationUUID()
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	token, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, uuid(), owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	s := New(f.Pool, false)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	router := httpapi.New(s, s, nil, s, nil, nil, nil, nil, s, nil, nil, nil, false, nil, nil, nil)
	request := func(method, path string, input any) *httptest.ResponseRecorder {
		var b bytes.Buffer
		if input != nil {
			if json.NewEncoder(&b).Encode(input) != nil {
				t.Fatal("synthetic input")
			}
		}
		r := httptest.NewRequest(method, path, &b)
		r.Header.Set("Authorization", "Bearer "+token)
		if input != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	var id string
	for _, j := range plan.Journeys {
		if len(j.Details.Itinerary) > 0 {
			id = j.TargetID
		}
	}
	get := func() content.PersonalCollection {
		w := request(http.MethodGet, "/v1/me/personal-collections/journey/"+id, nil)
		var v struct{ Data content.PersonalCollection }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || v.Data.JourneyDetails == nil || len(v.Data.Entries) != 0 {
			t.Fatal("owned native detailed route", w.Code)
		}
		return v.Data
	}
	v := get()
	if len(v.JourneyDetails.Itinerary) != 4 || v.JourneyDetails.SourceVersion == "" || v.JourneyDetails.Itinerary[0].Origin == nil || v.JourneyDetails.Itinerary[0].Origin.SourceID != "15800000-0000-4000-8000-000000000011" || v.JourneyDetails.Itinerary[1].StopID != v.JourneyDetails.Itinerary[0].ID || v.JourneyDetails.Itinerary[2].PlaceID != "" {
		t.Fatal("stable stop, text-only arrangement or source lost")
	}
	w := request(http.MethodGet, "/v1/me/personal-collections?kind=journey", nil)
	var list struct{ Data content.CollectionPage }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Data.Items) != 2 {
		t.Fatal("same native collection list", w.Code)
	}
	input := func(current content.PersonalCollection) content.CollectionControlInput {
		j := *current.JourneyDetails
		j.SourceVersion = ""
		j.Itinerary = append([]content.JourneyEntry{}, j.Itinerary...)
		for i := range j.Itinerary {
			j.Itinerary[i].Origin = nil
		}
		return content.CollectionControlInput{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: uuid(), Action: "UPDATE", Kind: "journey", CollectionID: id, ExpectedRevision: current.Revision, Input: &content.CollectionControlBody{Kind: "journey", Title: current.Title, Summary: current.Summary, Entries: []content.CollectionControlEntry{}, JourneyDetails: &j}}
	}
	preview := func(p content.CollectionControlInput) content.CollectionControlPreview {
		w := request(http.MethodPost, "/v1/me/personal-collection-controls/preview", p)
		var v struct {
			Data content.CollectionControlPreview
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("detailed concrete preview", w.Code)
		}
		return v.Data
	}
	p := input(v)
	p.Input.Title = ""
	p.Input.JourneyDetails.StartDate = "2026-11-01"
	p.Input.JourneyDetails.EndDate = "2026-11-03"
	p.Input.JourneyDetails.Itinerary[2].Note = "本人修改无地点安排"
	p.Input.JourneyDetails.Itinerary[1], p.Input.JourneyDetails.Itinerary[2] = p.Input.JourneyDetails.Itinerary[2], p.Input.JourneyDetails.Itinerary[1]
	pv := preview(p)
	submission := content.CollectionControlSubmission{CollectionControlInput: p, Approval: pv.Approval}
	for i := 0; i < 2; i++ {
		if w = request(http.MethodPut, "/v1/me/personal-collection-controls/operations/"+p.OperationID, submission); w.Code != 200 {
			t.Fatal("atomic detailed update/replay", w.Code)
		}
	}
	v = get()
	if v.Revision != 2 || v.Title != "" || v.CityID != "" || v.JourneyDetails.StartDate != "2026-11-01" || v.JourneyDetails.Itinerary[1].Note != "本人修改无地点安排" || v.JourneyDetails.Itinerary[1].Origin.SourceStopID != "15800000-0000-4000-8000-000000000011" {
		t.Fatal("native dates/order/text/provenance did not persist")
	}
	var targetPlace string
	for _, entry := range v.JourneyDetails.Itinerary {
		if entry.PlaceID != "" {
			targetPlace = entry.PlaceID
		}
	}
	if _, e = s.AppendHumanCollectionPlaces(f.Context, digest, actor, "journey", id, content.CollectionAppendInput{OperationID: uuid(), ExpectedRevision: v.Revision, CityID: "aberdeen-gb", Entries: []content.CollectionAppendEntry{{PlaceID: targetPlace, LocationVisibility: "hidden"}}}); e == nil {
		t.Fatal("legacy Place append overwrote rich schedule")
	}
	if _, e = s.PublishHumanCollection(f.Context, digest, actor, "journey", id, content.CollectionPublicationInput{OperationID: uuid(), Revision: v.Revision, Entries: []content.CollectionPublicationEntry{{PlaceID: targetPlace, LocationVisibility: "hidden"}}}); e == nil {
		t.Fatal("private detailed Journey published through Place-only contract")
	}
	old, e := s.GetHumanCollection(f.Context, digest, actor, "journey", id)
	if e != nil {
		t.Fatal(e)
	}
	late := input(v)
	latePV := preview(late)
	entryID := v.JourneyDetails.Itinerary[1].ID
	var raw []byte
	if e = f.Pool.QueryRow(f.Context, `SELECT material FROM journey_itinerary_entries WHERE journey_id=$1 AND entry_id=$2`, id, entryID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE journey_itinerary_entries SET material=jsonb_set(material,'{sourceText}','"synthetic temporary"') WHERE journey_id=$1 AND entry_id=$2`, id, entryID); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE journey_itinerary_entries SET material=$3 WHERE journey_id=$1 AND entry_id=$2`, id, entryID, raw); e != nil {
		t.Fatal(e)
	}
	if e = old.ResponseProof.RevalidateCollection(f.Context); e == nil {
		t.Fatal("late owned read survived source ABA")
	}
	if _, e = s.ApplyHumanCollectionControl(f.Context, digest, actor, content.CollectionControlSubmission{CollectionControlInput: late, Approval: latePV.Approval}); e == nil {
		t.Fatal("old preview survived itinerary source ABA")
	}
	remove := input(get())
	remove.Input.JourneyDetails.Itinerary = remove.Input.JourneyDetails.Itinerary[:3]
	rp := preview(remove)
	if w = request(http.MethodPut, "/v1/me/personal-collection-controls/operations/"+remove.OperationID, content.CollectionControlSubmission{CollectionControlInput: remove, Approval: rp.Approval}); w.Code != 200 {
		t.Fatal("native entry removal", w.Code)
	}
	if replay, e := f.Importer.ApplyJourneyDetails(f.Context, legacyTestRunID, source, manifest); e != nil || replay.Counts != result.Counts {
		t.Fatal("original import fact replay", e)
	}
	if fresh := get(); fresh.Revision != 3 || fresh.JourneyDetails.StartDate != "2026-11-01" || len(fresh.JourneyDetails.Itinerary) != 3 {
		t.Fatal("reimport overwrote edited/removed native values")
	}
	peer := uuid()
	peerToken, peerDigest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO accounts(id,account_type,status)VALUES($1,'person','active')`, peer); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, uuid(), peer, peerDigest[:]); e != nil {
		t.Fatal(e)
	}
	ownerToken := token
	token = peerToken
	if w = request(http.MethodGet, "/v1/me/personal-collections/journey/"+id, nil); w.Code != 404 {
		t.Fatal("foreign owner read detailed Journey", w.Code)
	}
	token = ownerToken
	stop := content.CollectionControlInput{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: uuid(), Action: "WITHDRAW", Kind: "journey", CollectionID: id, ExpectedRevision: 3}
	sp := preview(stop)
	if w = request(http.MethodPut, "/v1/me/personal-collection-controls/operations/"+stop.OperationID, content.CollectionControlSubmission{CollectionControlInput: stop, Approval: sp.Approval}); w.Code != 200 {
		t.Fatal("native withdraw", w.Code)
	}
	if _, e = f.Importer.ApplyJourneyDetails(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal(e)
	}
	var current string
	if e = f.Pool.QueryRow(f.Context, `SELECT status FROM journeys WHERE id=$1`, id).Scan(&current); e != nil || current != "withdrawn" {
		t.Fatal("reimport resurrected withdrawn Journey", e)
	}
	if _, e = f.Pool.Exec(f.Context, `DELETE FROM journeys WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Importer.ApplyJourneyDetails(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal(e)
	}
	var resurrected bool
	if e = f.Pool.QueryRow(f.Context, `SELECT EXISTS(SELECT 1 FROM journeys WHERE id=$1)`, id).Scan(&resurrected); e != nil || resurrected {
		t.Fatal("reimport resurrected deleted Journey", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:]); e != nil {
		t.Fatal(e)
	}
	if w = request(http.MethodGet, "/v1/me/personal-collections?kind=journey", nil); w.Code != 401 {
		t.Fatal("revoked Session reused detailed read", w.Code)
	}
}
