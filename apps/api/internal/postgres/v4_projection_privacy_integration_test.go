package postgres

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/intent"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
)

const v4PrivateLocation = "SAF001_PRIVATE_PRECISE_57.123456789_-2.987654321"

func v4PrivacyNative(t *testing.T) *newPeopleFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	f := newPeoplePair(t, newPeopleTestPool(t))
	f.scopes()
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE host_account_id=ANY($1::uuid[])`, `DELETE FROM intents WHERE owner_account_id=ANY($1::uuid[])`, `DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`} {
			f.exec(q, f.ids)
		}
	})
	f.exec(`UPDATE cities SET map_provider='mapbox',map_center_latitude=57.2,map_center_longitude=-2.1,map_default_zoom=11,map_viewport_source_ref='local:SAF001' WHERE id=$1`, f.cities[0])
	f.exec(`UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=57.22,longitude=-2.12 WHERE id=$1`, f.places[0])
	f.exec(`UPDATE user_profiles SET bio=$2 WHERE account_id=$1`, f.ids[1], v4PrivateLocation)
	return f
}
func v4PrivacyBytes(t *testing.T, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	for _, canary := range []string{v4PrivateLocation, "57.123456789", "-2.987654321", "PRIVATE_PLATFORM_CANARY", "PRIVATE_INTENT_CANARY", "token_sha256", "person_contexts", "xmin", "friendIds"} {
		if strings.Contains(string(b), canary) {
			t.Fatal("private location/source leak", canary, string(b))
		}
	}
}
func v4PrivacySession(t *testing.T, f *newPeopleFixture, who int) mp.Access {
	t.Helper()
	_, d, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[who], d[:])
	return mp.Access{Actor: identity.Actor{ID: f.ids[who], AccountType: "person"}, Digest: d}
}

func TestV4ProjectionPrivacyNativePublicAreaOptInFriendAndBidirectionalBlock(t *testing.T) {
	for _, kind := range []string{"stranger_no_zone", "accepted_friend_no_zone", "stranger_explicit_area", "friend_explicit_area", "forward_block", "reverse_block", "private_profile", "withdrawn"} {
		t.Run(kind, func(t *testing.T) {
			f := v4PrivacyNative(t)
			zone := ""
			if kind != "stranger_no_zone" && kind != "accepted_friend_no_zone" {
				zone = "north"
			}
			if strings.HasPrefix(kind, "accepted_friend") || strings.HasPrefix(kind, "friend_") {
				r, e := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[1], "本人明确合成好友请求")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = f.store.DecideRequest(f.ctx, f.ids[1], r.ID, "accept"); e != nil {
					t.Fatal(e)
				}
			}
			var now time.Time
			if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
				t.Fatal(e)
			}
			source, e := f.store.SubmitIntent(f.ctx, f.ids[1], f.cities[0], intent.Input{Confirmed: true, Topic: "SAF001明确公开兴趣", Details: "PRIVATE_INTENT_CANARY", AvailableFrom: now.Add(-time.Minute), AvailableUntil: now.Add(2 * time.Hour), TimeZone: "UTC", CoarseAreaLabel: "仅城市范围", PublicMapZone: zone, ExpiresAt: now.Add(time.Hour)})
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "forward_block":
				e = f.store.BlockAccount(f.ctx, f.ids[0], f.ids[1])
			case "reverse_block":
				e = f.store.BlockAccount(f.ctx, f.ids[1], f.ids[0])
			case "private_profile":
				f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[1])
			case "withdrawn":
				e = f.store.WithdrawIntent(f.ctx, f.ids[1], source.ID)
			}
			if e != nil {
				t.Fatal(e)
			}
			before := enrichmentAllPublic(t, f.pool, f.ctx)
			result, e := f.store.Search(f.ctx, f.cities[0], f.ids[0], []string{"saf001明确公开兴趣"})
			if e != nil {
				t.Fatal(e)
			}
			hidden := kind == "forward_block" || kind == "reverse_block" || kind == "private_profile" || kind == "withdrawn"
			if hidden {
				if len(result.People) != 0 {
					t.Fatal("forbidden peer projected", result.People)
				}
			} else {
				if len(result.People) != 1 {
					t.Fatal("real public peer missing", result.People)
				}
				p := result.People[0]
				if zone == "" {
					if p.MapLatitude != nil || p.MapLongitude != nil {
						t.Fatal("no public-zone consent yielded person Pin", p)
					}
				} else {
					if p.MapLatitude == nil || p.MapLongitude == nil || math.Abs(*p.MapLatitude-57.24) > 1e-10 || math.Abs(*p.MapLongitude+2.1) > 1e-10 {
						t.Fatal("area must be public City-derived, not personal GPS", p)
					}
				}
			}
			result = agentworkspace.WithContract(result, agentworkspace.Task{}, "saf001-local-native")
			v4PrivacyBytes(t, result)
			for _, item := range result.ResultSet.Items {
				if item.Entity.Type == "person" && item.Anchor != nil && (item.Anchor.Precision != "area" || item.Anchor.PublicZone != "north") {
					t.Fatal("typed Card/Map precise Person source", item)
				}
			}
			if !reflect.DeepEqual(before, enrichmentAllPublic(t, f.pool, f.ctx)) {
				t.Fatal("privacy read/projection wrote domain")
			}
		})
	}
}

func TestV4ProjectionPrivacyNativeNewPeopleMinimumRefusalAndSources(t *testing.T) {
	for _, kind := range []string{"no_opt_in", "online_without_GPS", "place_is_not_user_position", "accepted_friend", "forward_block", "reverse_block", "opt_out", "expired_peer", "private_peer"} {
		t.Run(kind, func(t *testing.T) {
			f := v4PrivacyNative(t)
			if kind != "no_opt_in" {
				f.enable()
			}
			mode, city, place, platform := "ONLINE", -1, "", "PRIVATE_PLATFORM_CANARY"
			if kind == "place_is_not_user_position" {
				mode, city, place, platform = "IN_PERSON", 0, f.places[0], ""
			}
			source := f.activate(f.draft(0, mode, city, place, "", platform))
			peer := f.activate(f.draft(1, mode, city, place, "", strings.ToLower(platform)))
			// A legitimate private declaration is not location authority for peer matching.
			c, e := f.store.DeclareContext(f.ctx, f.ids[1], contextgraph.DeclarationInput{Type: contextgraph.City, SourceKey: f.cities[1], Relation: "current"})
			if e != nil {
				t.Fatal(e)
			}
			_ = c
			switch kind {
			case "accepted_friend":
				r, e := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[1], "明确好友")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = f.store.DecideRequest(f.ctx, f.ids[1], r.ID, "accept"); e != nil {
					t.Fatal(e)
				}
			case "forward_block":
				e = f.store.BlockAccount(f.ctx, f.ids[0], f.ids[1])
			case "reverse_block":
				e = f.store.BlockAccount(f.ctx, f.ids[1], f.ids[0])
			case "opt_out":
				_, e = f.store.SetNewPeopleConsent(f.ctx, f.ids[1], false)
			case "expired_peer":
				f.exec(`UPDATE social_intents SET expires_at=clock_timestamp()+interval '120 milliseconds' WHERE id=$1`, peer.ID)
				time.Sleep(180 * time.Millisecond)
			case "private_peer":
				f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[1])
			}
			if e != nil {
				t.Fatal(e)
			}
			before := enrichmentAllPublic(t, f.pool, f.ctx)
			out, e := f.store.FindNewPeople(f.ctx, f.ids[0], source.ID)
			if kind == "no_opt_in" {
				if !errors.Is(e, newpeople.ErrNotFound) {
					t.Fatal("no opt-in source accepted", out, e)
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				want := 0
				if kind == "online_without_GPS" || kind == "place_is_not_user_position" {
					want = 1
				}
				if len(out.Candidates) != want {
					t.Fatal("current peer privacy boundary", kind, out)
				}
				v4PrivacyBytes(t, out)
				raw, _ := json.Marshal(out)
				for _, key := range []string{"latitude", "longitude", "areaLabel", "placeId", "cityId", "contextId", "onlinePlatform", "RAW_PRIVATE_TITLE_SENTINEL", f.cityContexts[1], f.cities[1]} {
					if strings.Contains(string(raw), key) {
						t.Fatal("over-disclosure", key, string(raw))
					}
				}
			}
			if !reflect.DeepEqual(before, enrichmentAllPublic(t, f.pool, f.ctx)) {
				t.Fatal("matching read wrote domain")
			}
		})
	}
}

func TestV4ProjectionPrivacyNativePublicPlaceAndPrivateOpportunityInbox(t *testing.T) {
	f := v4PrivacyNative(t)
	own := v4PrivacySession(t, f, 0)
	other := v4PrivacySession(t, f, 1)
	var agent string
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES('personal',$1,'active') RETURNING id`, f.ids[0]).Scan(&agent); e != nil {
		t.Fatal(e)
	}
	principal, e := actorref.ParsePrincipal("person", f.ids[0])
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.EnsureAgentProfile(f.ctx, agent, principal); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.PutOwnNotificationPolicy(f.ctx, agentprofile.PrivateAccess{SessionDigest: own.Digest, WorkspacePrincipal: principal}, agentnotification.PutInput{Enabled: true, DefaultRoute: agentnotification.Normal, Rules: []agentnotification.Rule{}, ExpiresAt: now.Add(time.Hour)}); e != nil {
		t.Fatal(e)
	}
	activity := socialNotificationsActivity(t, f, 1, 0)
	source := socialNotificationsIntent(t, f, 0, true)
	f.exec(`UPDATE social_intents SET title='PRIVATE_INTENT_CANARY' WHERE id=$1`, source.ID)
	if _, e := f.store.ActivateSocialIntent(f.ctx, f.ids[0], source.ID); e != nil {
		t.Fatal(e)
	}
	q := mp.Query{CityID: f.cities[0], West: -2.2, South: 57.1, East: -2, North: 57.3}
	public, e := f.store.ReadMapLayers(f.ctx, mp.Access{}, q)
	if e != nil || !mapHas(public, "PLACE", f.places[0]) || !mapHas(public, "ACTIVITY", activity.ID) {
		t.Fatal("explicit public Place/Activity missing", e, public.View)
	}
	v4PrivacyBytes(t, public.View)
	for _, i := range public.View.Items {
		if i.Kind == "PERSON" || i.Kind == "OPPORTUNITY" || i.Anchor.PlaceID != f.places[0] || i.Anchor.Latitude != 57.22 || i.Anchor.Longitude != -2.12 {
			t.Fatal("public Place confused with user location", i)
		}
	}
	before := enrichmentAllPublic(t, f.pool, f.ctx)
	items, e := f.store.ListHumanOpportunities(f.ctx, own.Digest, own.Actor)
	if e != nil || len(items) != 1 || items[0].Entity.ID != activity.ID {
		t.Fatal("native opportunity source", items, e)
	}
	v4PrivacyBytes(t, items)
	b, _ := json.Marshal(items)
	if strings.Contains(string(b), "latitude") || strings.Contains(string(b), "longitude") {
		t.Fatal("minimum matching reason includes coordinates", string(b))
	}
	inbox, e := f.store.ListHumanInbox(f.ctx, own.Digest, own.Actor)
	if e != nil {
		t.Fatal(e)
	}
	if len(inbox) != 1 || inbox[0].ResourceType != "opportunity_available" {
		t.Fatal("actual native notification required", inbox)
	}
	v4PrivacyBytes(t, inbox)
	b, _ = json.Marshal(inbox)
	if strings.Contains(string(b), "latitude") || strings.Contains(string(b), "longitude") {
		t.Fatal("notification includes coordinates", string(b))
	}
	q.Private = true
	receipt, e := f.store.ReadMapLayers(f.ctx, own, q)
	if e != nil || !mapHas(receipt, "OPPORTUNITY", source.ID+":"+activity.ID) {
		t.Fatal("native private opportunity missing", e, receipt.View)
	}
	v4PrivacyBytes(t, receipt.View)
	stranger, e := f.store.ReadMapLayers(f.ctx, other, q)
	if e != nil || len(stranger.View.Items) != 0 {
		t.Fatal("private owner crossover", e, stranger.View)
	}
	if !reflect.DeepEqual(before, enrichmentAllPublic(t, f.pool, f.ctx)) {
		t.Fatal("map/reason/inbox reads wrote domain")
	}
	f.exec(`UPDATE social_intents SET title=title WHERE id=$1`, source.ID)
	if e = f.store.RevalidateMapLayers(f.ctx, own, receipt); !errors.Is(e, mp.ErrChanged) {
		t.Fatal("native source xmin ABA accepted", e)
	}
	fresh, e := f.store.ReadMapLayers(f.ctx, own, q)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, f.places[0])
	if e = f.store.RevalidateMapLayers(f.ctx, own, fresh); e == nil {
		t.Fatal("hidden Place still current source")
	}
	items, e = f.store.ListHumanOpportunities(f.ctx, own.Digest, own.Actor)
	if e != nil || len(items) != 0 {
		t.Fatal("hidden source still matching", e, items)
	}
}
