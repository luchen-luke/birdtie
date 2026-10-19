package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacycontext"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
)

// Mutates synthetic exported metadata only, then re-seals its exact fixture
// hashes. No production archive, body, account or source file is used.
func threadIdentityNativeInput(t *testing.T, count int, missing bool) (LegacyPrivateMediaInput, []byte) {
	t.Helper()
	in, raw := legacyContextNativeInput(t)
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(in.Source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		t.Fatal("synthetic source")
	}
	in.Source, in.Manifest = legacySyntheticInput(t, func(tables map[string]any) {
		for key, rows := range doc.Tables {
			out := []any{}
			for _, r := range rows {
				out = append(out, r)
			}
			tables[key] = out
		}
		tables["moment_threads"].([]any)[0].(map[string]any)["entry_count"] = count
		notes := tables["notes"].([]any)
		last := notes[len(notes)-1].(map[string]any)
		if count == 1 {
			last["thread_id"], last["thread_position"] = nil, nil
			tables["moment_thread_legs"] = []any{}
		}
		if missing {
			last["deleted_at"] = "2026-10-14T00:00:00Z"
			tables["moment_thread_legs"] = []any{}
		}
		if count == 2 && !missing {
			// Both entries have two legitimate associated Places. Only the explicit
			// source leg establishes which endpoint is used in the ordered line.
			links := tables["note_places"].([]any)
			for _, old := range append([]any{}, links...) {
				r := map[string]any{}
				for k, v := range old.(map[string]any) {
					r[k] = v
				}
				r["note_id"] = last["id"]
				links = append(links, r)
			}
			tables["note_places"] = links
		}
	})
	var sourceDoc map[string]json.RawMessage
	if json.Unmarshal(in.Source, &sourceDoc) != nil {
		t.Fatal("synthetic header")
	}
	var matched map[string]any
	if json.Unmarshal(in.Matched, &matched) != nil {
		t.Fatal("synthetic matcher")
	}
	matched["source"], matched["sourceFileSha256"] = sourceDoc["source"], legacymedia.Hash(in.Source)
	in.Matched, _ = json.Marshal(matched)
	var seal map[string]any
	if json.Unmarshal(in.AuthenticatedSeal, &seal) != nil {
		t.Fatal("synthetic seal")
	}
	seal["matcherFileSHA256"] = legacymedia.Hash(in.Matched)
	in.AuthenticatedSeal, _ = json.Marshal(seal)
	in.ExpectedSealSHA256 = legacymedia.Hash(in.AuthenticatedSeal)
	return in, raw
}

func TestMapContentThreadIdentityNativeOriginalProvenance(t *testing.T) {
	for _, c := range []struct {
		name    string
		count   int
		missing bool
		want    string
		entries int
	}{{"singleton-container", 1, false, "NOT_A_THREAD", 0}, {"real-thread-multiple-location-projections", 2, false, "AVAILABLE", 2}, {"real-thread-only-one-surviving-source-member", 2, true, "AVAILABLE", 1}} {
		t.Run(c.name, func(t *testing.T) {
			f := legacyMigrationNativeFixture(t)
			in, raw := threadIdentityNativeInput(t, c.count, c.missing)
			plan, e := legacycontext.Build(legacyTestRunID, in.Source, in.Manifest)
			if e != nil || len(plan.Threads) != 1 {
				t.Fatal("synthetic explicit thread plan", e)
			}
			for _, apply := range []func() error{
				func() error { _, e := f.Importer.Apply(f.Context, legacyTestRunID, in.Source, in.Manifest); return e },
				func() error {
					_, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, in.Source, in.Manifest)
					return e
				},
				func() error {
					_, e := f.Importer.ApplyNativeAdaptation(f.Context, legacyTestRunID, in.Source, in.Manifest)
					return e
				},
				func() error {
					_, e := f.Importer.ApplyLegacyPrivateMedia(f.Context, legacyTestRunID, in, func(context.Context, legacymedia.Record) (io.ReadCloser, error) {
						return io.NopCloser(bytes.NewReader(raw)), nil
					})
					return e
				},
				func() error {
					_, e := f.Importer.ApplyContextAdaptation(f.Context, legacyTestRunID, in.Source, in.Manifest)
					return e
				},
			} {
				if e = apply(); e != nil {
					t.Fatal("synthetic independent import phase", e)
				}
			}
			var owner string
			if e = f.Pool.QueryRow(f.Context, `SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user'`).Scan(&owner); e != nil {
				t.Fatal(e)
			}
			digest := sha256.Sum256([]byte("thread-identity83-isolated-owner-session"))
			if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
				t.Fatal(e)
			}
			if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,'synthetic_test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, owner, digest[:]); e != nil {
				t.Fatal(e)
			}
			actor := identity.Actor{ID: owner, AccountType: "person"}
			store := New(f.Pool, false)
			seed := plan.Threads[0].Members[0].MomentID
			got, e := store.ReadHumanMapContentThread(f.Context, digest, actor, seed)
			if e != nil || got.Status != c.want || len(got.Entries) != c.entries {
				t.Fatal("thread classification inferred from container/visible count", got.Status, len(got.Entries), e)
			}
			if c.want == "NOT_A_THREAD" && got.ThreadID != "" {
				t.Fatal("singleton exposed a thread identity")
			}
			if c.want == "AVAILABLE" && got.ThreadID != plan.Threads[0].TargetID {
				t.Fatal("original true thread identity lost")
			}
			if e = got.ResponseProof.RevalidateHumanControl(f.Context); e != nil {
				t.Fatal("final thread proof", e)
			}
			if c.count != 2 || c.missing {
				return
			}
			// Give only these synthetic Places explicit WGS84 coordinates; this tests
			// the route query against real current map leaves, not raw input array order.
			sf := &savedPageNativeFixture{ctx: f.Context, pool: f.Pool, store: store, actor: actor, digest: digest}
			for i, p := range plan.Places {
				if _, e = f.Pool.Exec(f.Context, `UPDATE places SET city_id='aberdeen-gb',coordinate_system='wgs84',latitude=$2,longitude=-2.1,location_precision='point',publication_status='published' WHERE id=$1`, p.TargetID, 57.15+float64(i)*0.02); e != nil {
					t.Fatal(e)
				}
				contributor := interactionNativeActor(t, sf)
				permissionFixture := *sf
				permissionFixture.actor = contributor.actor
				mc144RetainedPlace(t, &permissionFixture, p.TargetID, 0)
			}
			// Original unplaced locations correctly retain imprecise legacy selectors.
			// Explicitly reselect these independently approved synthetic point Places
			// through the original owner control before expecting geographic leaves.
			for _, member := range plan.Threads[0].Members {
				current, err := store.GetHumanMoment(f.Context, digest, actor, member.MomentID)
				if err != nil {
					t.Fatal(err)
				}
				locations := []content.MomentLocationInput{}
				for _, place := range plan.Places {
					locations = append(locations, content.MomentLocationInput{ID: sf.id(t), Kind: "PLACE", PlaceID: place.TargetID, Precision: "place"})
				}
				input := content.MomentInput{Body: current.Body, Title: current.Title, TimePrecision: "unknown", LocationPrecision: "none", Locations: &locations}
				mc144Write(t, sf, content.MomentControlInput{OperationID: sf.id(t), Action: "UPDATE", MomentID: member.MomentID, ExpectedRevision: current.Revision, Input: &input})
			}
			got, e = store.ReadHumanMapContentThread(f.Context, digest, actor, seed)
			if e != nil {
				t.Fatal("current explicitly selected geography", e)
			}
			q := content.MapContentQuery{CityID: "aberdeen-gb", Zoom: 14, Bounds: content.MapContentBounds{West: -2.4, South: 57, East: -1.9, North: 57.3}}
			page, e := store.ReadHumanMapContent(f.Context, digest, actor, q)
			if e != nil || !page.Valid(time.Now()) {
				t.Fatal("actual legacy multi-location map", e)
			}
			marker := map[string]content.MapContentPin{}
			for _, p := range page.Pins {
				marker[p.MarkerID] = p
			}
			edges := 0
			leg := plan.Threads[0].Legs[0]
			for _, p := range page.Pins {
				for _, id := range p.NextMarkerIDs {
					to, ok := marker[id]
					if !ok || p.MomentID != leg.FromMomentID || to.MomentID != leg.ToMomentID || p.PlaceID != leg.FromPlaceID || to.PlaceID != leg.ToPlaceID || p.ThreadPosition+1 != to.ThreadPosition {
						t.Fatal("line ignored explicit original leg/current endpoints")
					}
					edges++
				}
			}
			if len(page.Pins) < 6 || edges != 1 {
				t.Fatal("independent media/place projections merged or explicit leg missing", len(page.Pins), edges)
			}
			if e = page.ResponseProof.RevalidateHumanControl(f.Context); e != nil {
				t.Fatal("final explicit route proof", e)
			}
			// Keep both original Moments readable at the first Place, but exclude the
			// exact destination Place from the query. Never substitute another leaf.
			fromLat := markerWithPlace(page.Pins, leg.FromPlaceID)
			q.Bounds.South, q.Bounds.North = fromLat-0.001, fromLat+0.001
			narrow, e := store.ReadHumanMapContent(f.Context, digest, actor, q)
			if e != nil {
				t.Fatal(e)
			}
			for _, p := range narrow.Pins {
				if len(p.NextMarkerIDs) != 0 {
					t.Fatal("unloaded explicit endpoint bridged to another associated Place")
				}
			}
			// Retiring one original child preserves genuine-thread classification and
			// original missing position while invalidating the encoded old proof.
			mid := plan.Threads[0].Members[1].MomentID
			mc144Write(t, sf, content.MomentControlInput{OperationID: sf.id(t), Action: "WITHDRAW", MomentID: mid, ExpectedRevision: 2})
			retired, e := store.ReadHumanMapContentThread(f.Context, digest, actor, seed)
			if e != nil || retired.Status != "AVAILABLE" || len(retired.Entries) != 2 || retired.Entries[1].Status != "UNAVAILABLE" || retired.Entries[1].Detail != nil {
				t.Fatal("hidden member collapsed real thread or leaked content", e)
			}
			if got.ResponseProof.RevalidateHumanControl(f.Context) == nil || page.ResponseProof.RevalidateHumanControl(f.Context) == nil {
				t.Fatal("late old thread/map proof survived source retirement")
			}
		})
	}
}

func markerWithPlace(pins []content.MapContentPin, place string) float64 {
	for _, p := range pins {
		if p.PlaceID == place {
			return p.Anchor.Latitude
		}
	}
	return 0
}
