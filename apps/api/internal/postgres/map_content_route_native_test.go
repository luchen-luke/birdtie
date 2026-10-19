package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
)

// Disposable native rows only. No provider call, production identity or archive
// mutation is required to verify selector/leg authority and its final proof.
func TestMapContentThreadRouteNativeSavedLegAndRetirement(t *testing.T) {
	f := savedPageNative(t)
	_, fromPlace := f.bookmark(t)
	_, toPlace := f.bookmark(t)
	f.exec(t, `UPDATE places SET latitude=57.18,longitude=-2.12 WHERE id=$1`, toPlace)
	in := threadNativeInput(t, f)
	for i, p := range []string{fromPlace, toPlace} {
		in.Items[i].Input.PlaceID = p
		in.Items[i].Input.LocationPrecision = "place"
	}
	published, err := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, in))
	if err != nil {
		t.Fatal("explicit current native thread", err)
	}
	q := content.MapContentQuery{CityID: "aberdeen-gb", Zoom: 14, Bounds: content.MapContentBounds{West: -2.4, South: 57, East: -1.9, North: 57.3}}
	queryFor := func() content.MapContentThreadRouteQuery {
		t.Helper()
		page, e := f.store.ReadHumanMapContent(f.ctx, f.digest, f.actor, q)
		if e != nil {
			t.Fatal(e)
		}
		out := content.MapContentThreadRouteQuery{ThreadID: published.ThreadID, ExpectedProvider: "mapbox"}
		for _, p := range page.Pins {
			if p.ThreadID != published.ThreadID {
				continue
			}
			endpoint := content.MapContentRouteEndpoint{ViewportPath: page.ViewportPath, MarkerID: p.MarkerID, SourceVersion: p.SourceVersion}
			if p.ThreadPosition == 0 {
				out.From = endpoint
			} else if p.ThreadPosition == 1 {
				out.To = endpoint
			}
		}
		if !out.Valid() {
			t.Fatal("both exact original leaves required")
		}
		return out
	}
	query := queryFor()
	missing, e := f.store.ReadHumanMapContentThreadRoutePlan(f.ctx, f.digest, f.actor, query)
	if e != nil || missing.CanPlan || missing.Route.Status != "NOT_AVAILABLE" {
		t.Fatal("generic adjacency is not a saved route", e)
	}
	f.exec(t, `INSERT INTO moment_thread_route_legs(id,thread_id,from_moment_id,to_moment_id,from_position,to_position,mode,from_place_id,to_place_id)VALUES($1,$2,$3,$4,0,1,'walking',$5,$6)`, f.id(t), published.ThreadID, published.Members[0].MomentID, published.Members[1].MomentID, fromPlace, toPlace)
	query = queryFor()
	plan, e := f.store.ReadHumanMapContentThreadRoutePlan(f.ctx, f.digest, f.actor, query)
	if e != nil || !plan.CanPlan || plan.Request.Mode != "walking" || plan.Request.From.CountryCode != "GB" || plan.Request.From.Latitude != 57.15 || plan.Request.To.Latitude != 57.18 {
		t.Fatal("saved mode and current authoritative coordinates", plan.CanPlan, e)
	}
	if e = plan.Route.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal("valid exact proof", e)
	}
	if missing.Route.ResponseProof.RevalidateHumanControl(f.ctx) == nil {
		t.Fatal("negative cache survived newly saved leg")
	}
	forged := query
	forged.To.SourceVersion = strings.Repeat("f", 64)
	if _, e = f.store.ReadHumanMapContentThreadRoutePlan(f.ctx, f.digest, f.actor, forged); e == nil {
		t.Fatal("forged original source accepted")
	}
	reversed := query
	reversed.From, reversed.To = query.To, query.From
	reverse, e := f.store.ReadHumanMapContentThreadRoutePlan(f.ctx, f.digest, f.actor, reversed)
	if e != nil || reverse.CanPlan {
		t.Fatal("reverse caller invented saved order", e)
	}
	mc144Write(t, f, content.MomentControlInput{OperationID: f.id(t), Action: "WITHDRAW", MomentID: published.Members[1].MomentID, ExpectedRevision: 2})
	if e = plan.Route.ResponseProof.RevalidateHumanControl(f.ctx); e == nil {
		t.Fatal("late geometry proof survived endpoint withdrawal")
	}
	if plan.Route.ValidUntil.After(plan.Route.ObservedAt.Add(30 * time.Second)) {
		t.Fatal("source lease extended")
	}
}

func TestMapContentThreadRouteNativeSamePlaceAndRecommendation(t *testing.T) {
	for _, same := range []bool{true, false} {
		t.Run(map[bool]string{true: "same_confirmed_place", false: "saved_recommended"}[same], func(t *testing.T) {
			f := savedPageNative(t)
			_, fromPlace := f.bookmark(t)
			toPlace := fromPlace
			if !same {
				_, toPlace = f.bookmark(t)
				f.exec(t, `UPDATE places SET latitude=57.18,longitude=-2.12 WHERE id=$1`, toPlace)
			}
			in := threadNativeInput(t, f)
			for i, p := range []string{fromPlace, toPlace} {
				in.Items[i].Input.PlaceID = p
				in.Items[i].Input.LocationPrecision = "place"
			}
			published, e := f.store.CommitMomentThread(f.ctx, f.digest, f.actor, threadNativeApprove(t, f, in))
			if e != nil {
				t.Fatal(e)
			}
			f.exec(t, `INSERT INTO moment_thread_route_legs(id,thread_id,from_moment_id,to_moment_id,from_position,to_position,mode,from_place_id,to_place_id)VALUES($1,$2,$3,$4,0,1,'recommended',$5,$6)`, f.id(t), published.ThreadID, published.Members[0].MomentID, published.Members[1].MomentID, fromPlace, toPlace)
			q := content.MapContentQuery{CityID: "aberdeen-gb", Zoom: 14, Bounds: content.MapContentBounds{West: -2.4, South: 57, East: -1.9, North: 57.3}}
			page, e := f.store.ReadHumanMapContent(f.ctx, f.digest, f.actor, q)
			if e != nil {
				t.Fatal(e)
			}
			request := content.MapContentThreadRouteQuery{ThreadID: published.ThreadID, ExpectedProvider: "mapbox"}
			for _, p := range page.Pins {
				if p.ThreadID == published.ThreadID {
					v := content.MapContentRouteEndpoint{ViewportPath: page.ViewportPath, MarkerID: p.MarkerID, SourceVersion: p.SourceVersion}
					if p.ThreadPosition == 0 {
						request.From = v
					} else {
						request.To = v
					}
				}
			}
			plan, e := f.store.ReadHumanMapContentThreadRoutePlan(f.ctx, f.digest, f.actor, request)
			if e != nil || plan.CanPlan == same {
				t.Fatal("same Place suppression or saved recommendation", plan.CanPlan, e)
			}
			if !same && plan.Request.Mode != "recommended" {
				t.Fatal("approved saved recommendation lost")
			}
			if !same {
				mismatch := request
				mismatch.ExpectedProvider = "amap"
				blocked, err := f.store.ReadHumanMapContentThreadRoutePlan(f.ctx, f.digest, f.actor, mismatch)
				if err != nil || blocked.CanPlan || blocked.Route.Status != "BLOCKED" || blocked.Route.SourceVersion == plan.Route.SourceVersion {
					t.Fatal("supplier context expanded instead of narrowing", err)
				}
			}
		})
	}
}
