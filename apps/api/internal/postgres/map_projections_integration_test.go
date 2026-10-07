package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"strings"
	"testing"
	"time"
)

func TestMapProjectionNativeOriginalActivityBusinessOrganizationAndOwnOpportunity(t *testing.T) {
	f := newSponsorFixture(t, true) // Existing original claim, venue review and activity publication writers; LOCAL_SYNTHETIC only.
	b := f.b
	b.exec(t, `UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=57.2,longitude=-2.1 WHERE id=$1`, f.place)
	b.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status) VALUES($1,$2,'owner','active')`, f.operator, b.people[0])
	b.exec(t, `UPDATE organizations SET verification_status='verified',visibility='public' WHERE id=$1`, f.operator)
	t.Cleanup(func() {
		b.exec(t, `DELETE FROM organization_map_location_audit WHERE organization_id=$1`, f.operator)
		b.exec(t, `DELETE FROM organization_map_locations WHERE organization_id=$1`, f.operator)
		b.exec(t, `DELETE FROM organization_memberships WHERE organization_id=$1`, f.operator)
	})
	if _, e := b.store.SubmitMapLocation(b.ctx, b.people[0], f.operator, f.city, 57.21, -2.11); e != nil {
		t.Fatal("native explicit submitted org point", e)
	}
	q := mp.Query{CityID: f.city, West: -2.2, South: 57.1, East: -2, North: 57.3}
	r, e := b.store.ReadMapLayers(b.ctx, mp.Access{}, q)
	if e != nil || !mapHas(r, "PLACE", f.place) || !mapHas(r, "ACTIVITY", f.activity) || !mapHas(r, "BUSINESS", f.business) || mapHas(r, "ORGANIZATION", f.operator) {
		t.Fatal("native public source/pending excludes org", e, r.View)
	}
	if e = b.store.ReviewMapLocation(b.ctx, b.people[1], f.operator, "approve", "明确合成本地公开审核"); e != nil {
		t.Fatal(e)
	}
	r, e = b.store.ReadMapLayers(b.ctx, mp.Access{}, q)
	if e != nil || !mapHas(r, "ORGANIZATION", f.operator) {
		t.Fatal("explicit approved org", e, r.View)
	}
	for _, i := range r.View.Items {
		if i.Kind == "BUSINESS" && (i.Anchor.PlaceID != f.place || i.Entity.ID != f.business || i.Detail.ID != f.business) {
			t.Fatal("Business anchor/ID", i)
		}
	}
	own := mp.Access{Actor: identity.Actor{ID: b.people[2], AccountType: "person"}, Digest: b.access[2].SessionDigest}
	constraints, _ := json.Marshal(map[string]string{"category": "badminton", "placeId": f.place})
	intent, e := b.store.CreateSocialIntentDraft(b.ctx, own.Actor.ID, socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "PRIVATE_INTENT_BODY_CANARY", Constraints: constraints, Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		b.exec(t, `DELETE FROM audit_events WHERE resource_type='social_intent' AND resource_id=$1`, intent.ID)
		b.exec(t, `DELETE FROM social_intents WHERE id=$1`, intent.ID)
	})
	if _, e = b.store.ActivateSocialIntent(b.ctx, own.Actor.ID, intent.ID); e != nil {
		t.Fatal(e)
	}
	q.Private = true
	r, e = b.store.ReadMapLayers(b.ctx, own, q)
	expected := intent.ID + ":" + f.activity
	if e != nil || len(r.View.Items) != 1 || !mapHas(r, "OPPORTUNITY", expected) {
		t.Fatal("original current opportunity", e, r.View)
	}
	raw, _ := json.Marshal(r.View)
	for _, s := range []string{"PRIVATE_INTENT_BODY_CANARY", "creatorAccountId", "reasonCodes", "TiedPeople", "rightsNote", own.Actor.ID} {
		if strings.Contains(string(raw), s) {
			t.Fatal("private source body leak", s)
		}
	}
	if e = b.store.RevalidateMapLayers(b.ctx, own, r); e != nil {
		t.Fatal("same original private source", e)
	}
	b.exec(t, `UPDATE social_intents SET title=title WHERE id=$1`, intent.ID)
	if e = b.store.RevalidateMapLayers(b.ctx, own, r); !errors.Is(e, mp.ErrChanged) {
		t.Fatal("private Intent same-value ABA accepted", e)
	}
	other := mp.Access{Actor: identity.Actor{ID: b.people[1], AccountType: "person"}, Digest: b.access[1].SessionDigest}
	stranger, e := b.store.ReadMapLayers(b.ctx, other, q)
	if e != nil || len(stranger.View.Items) != 0 {
		t.Fatal("other owner private source", e, stranger.View)
	}
	if _, e = b.store.ReadMapLayers(b.ctx, mp.Access{}, q); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("anonymous opportunity", e)
	}
	q.Private = false
	b.exec(t, `UPDATE activities SET visibility='invite_only' WHERE id=$1`, f.activity)
	latest, e := b.store.ReadMapLayers(b.ctx, own, q)
	if e != nil || mapHas(latest, "ACTIVITY", f.activity) || mapHas(latest, "OPPORTUNITY", expected) {
		t.Fatal("private activity never map source", e)
	}
	q.Private = true
	latest, e = b.store.ReadMapLayers(b.ctx, own, q)
	if e != nil || len(latest.View.Items) != 0 {
		t.Fatal("private activity opportunity omitted", e, latest.View)
	}
	q.Private = false
	if e = b.store.HideMapLocation(b.ctx, b.people[0], f.operator); e != nil {
		t.Fatal(e)
	}
	b.exec(t, `UPDATE business_venue_relations SET status='revoked' WHERE business_id=$1 AND place_id=$2`, f.business, f.place)
	latest, e = b.store.ReadMapLayers(b.ctx, mp.Access{}, q)
	if e != nil || mapHas(latest, "BUSINESS", f.business) || mapHas(latest, "ORGANIZATION", f.operator) {
		t.Fatal("revoked/hidden public pin", e)
	}
}

func mapProjectionFixture(t *testing.T) (*placeMemoryFixture, mp.Query) {
	t.Helper()
	f := placeMemoryNativeFixture(t)
	f.private.base.exec(`UPDATE places SET coordinate_system='wgs84',location_precision='point',latitude=57.2,longitude=-2.1 WHERE id=$1`, f.place)
	return f, mp.Query{CityID: f.city, West: -2.2, South: 57.1, East: -2, North: 57.3}
}
func mapHas(r mp.Receipt, k, id string) bool {
	for _, i := range r.View.Items {
		if i.Kind == k && i.ID == id {
			return true
		}
	}
	return false
}
func TestMapProjectionNativeRealPublicMomentAndNoPrivatePayload(t *testing.T) {
	f, q := mapProjectionFixture(t)
	b := f.private.base
	author := momentPublicationOrdinary(t, f)
	reader := momentPublicationOrdinary(t, f)
	m := momentPublicationDraft(t, f, author)
	r, e := b.store.ReadMapLayers(b.ctx, mp.Access{}, q)
	if e != nil || !mapHas(r, "PLACE", f.place) || mapHas(r, "MOMENT", m.ID) {
		t.Fatal("draft/no real place", e, r.View)
	}
	momentPublicationPublish(t, f, author, m)
	a := mp.Access{Actor: reader.actor, Digest: reader.digest}
	r, e = b.store.ReadMapLayers(b.ctx, a, q)
	if e != nil || !mapHas(r, "MOMENT", m.ID) {
		t.Fatal("published current anchor", e, r.View)
	}
	if e = b.store.RevalidateMapLayers(b.ctx, a, r); e != nil {
		t.Fatal("current genuine receipt", e)
	}
	raw, _ := json.Marshal(r.View)
	for _, canary := range []string{"公开节选", "occurredAt", "authorAccountId", author.actor.ID, "token_sha256", "xmin", "Proof", "Seal", "rightsNote", "profile", "IntentID"} {
		if strings.Contains(string(raw), canary) {
			t.Fatal("private payload", canary)
		}
	}
	r.View.Items[0].Title = "tampered"
	if e = b.store.RevalidateMapLayers(b.ctx, a, r); !errors.Is(e, mp.ErrChanged) {
		t.Fatal("forged receipt", e)
	}
	if e = b.store.WithdrawHumanMoment(b.ctx, author.digest, author.actor, m.ID, m.Revision+1); e != nil {
		t.Fatal(e)
	}
	latest, e := b.store.ReadMapLayers(b.ctx, a, q)
	if e != nil || mapHas(latest, "MOMENT", m.ID) {
		t.Fatal("withdrawn map leak", e)
	}
}
func TestMapProjectionNativeCurrentSourceAndAccountABA(t *testing.T) {
	for _, mode := range []string{"moment_aba", "author_aba", "place_aba", "city_aba", "block_reader", "block_author", "session_revoke", "session_aba", "place_expiry", "city_expiry", "no_point"} {
		t.Run(mode, func(t *testing.T) {
			f, q := mapProjectionFixture(t)
			b := f.private.base
			author := momentPublicationOrdinary(t, f)
			reader := momentPublicationOrdinary(t, f)
			m := momentPublicationDraft(t, f, author)
			momentPublicationPublish(t, f, author, m)
			a := mp.Access{Actor: reader.actor, Digest: reader.digest}
			r, e := b.store.ReadMapLayers(b.ctx, a, q)
			if e != nil {
				t.Fatal("first", e)
			}
			switch mode {
			case "moment_aba":
				b.exec(`UPDATE moments SET title=title WHERE id=$1`, m.ID)
			case "author_aba":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, author.actor.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, author.actor.ID)
			case "place_aba":
				b.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, f.place)
				b.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, f.place)
			case "city_aba":
				b.exec(`UPDATE cities SET publication_status='draft' WHERE id=$1`, f.city)
				b.exec(`UPDATE cities SET publication_status='published' WHERE id=$1`, f.city)
			case "block_reader":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, reader.actor.ID, author.actor.ID)
			case "block_author":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, author.actor.ID, reader.actor.ID)
			case "session_revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, reader.digest[:])
			case "session_aba":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, reader.digest[:])
				b.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, reader.digest[:])
			case "place_expiry":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place)
			case "city_expiry":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.city)
			case "no_point":
				b.exec(`UPDATE places SET location_precision='area',latitude=NULL,longitude=NULL WHERE id=$1`, f.place)
			}
			if e = b.store.RevalidateMapLayers(b.ctx, a, r); e == nil {
				t.Fatal("stale private/source/ABA allowed", mode)
			}
			if strings.HasPrefix(mode, "block_") || mode == "place_expiry" || mode == "no_point" {
				latest, e := b.store.ReadMapLayers(b.ctx, a, q)
				if e != nil {
					t.Fatal(e)
				}
				if mapHas(latest, "MOMENT", m.ID) {
					t.Fatal("source still visible", mode)
				}
			}
		})
	}
}
func TestMapProjectionNativeNaturalSourceDeadlineAndTableWait(t *testing.T) {
	for _, mode := range []string{"natural_place_deadline", "city_table_wait"} {
		t.Run(mode, func(t *testing.T) {
			f, q := mapProjectionFixture(t)
			b := f.private.base
			reader := momentPublicationOrdinary(t, f)
			a := mp.Access{Actor: reader.actor, Digest: reader.digest}
			if mode == "natural_place_deadline" {
				b.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '500 milliseconds' WHERE id=$1`, f.place)
				r, e := b.store.ReadMapLayers(b.ctx, a, q)
				if e != nil || !mapHas(r, "PLACE", f.place) {
					t.Fatal("real finite source", e)
				}
				until := time.Now().Add(4 * time.Second)
				for {
					var expired bool
					if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM places WHERE id=$1`, f.place).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					if time.Now().After(until) {
						t.Fatal("real source deadline never reached")
					}
					time.Sleep(10 * time.Millisecond)
				}
				if e = b.store.RevalidateMapLayers(b.ctx, a, r); !errors.Is(e, mp.ErrChanged) {
					t.Fatal("naturally expired source accepted", e)
				}
				latest, e := b.store.ReadMapLayers(b.ctx, a, q)
				if e != nil || mapHas(latest, "PLACE", f.place) {
					t.Fatal("expired point displayed", e)
				}
				return
			}
			r, e := b.store.ReadMapLayers(b.ctx, a, q)
			if e != nil {
				t.Fatal(e)
			}
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(b.ctx, `LOCK TABLE cities IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			if _, e = lock.Exec(b.ctx, `UPDATE cities SET publication_status='draft' WHERE id=$1`, f.city); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- b.store.RevalidateMapLayers(b.ctx, a, r) }()
			deadline := time.Now().Add(4 * time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%LOCK TABLE accounts%')`).Scan(&waiting); e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("no actual native source relation wait")
			}
			if e = lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if !errors.Is(e, mp.ErrNotFound) {
					t.Fatal("hidden City after real wait", e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("source wait stuck")
			}
		})
	}
}

func TestMapProjectionNativeSessionWaitAndNaturalExpiry(t *testing.T) {
	f, q := mapProjectionFixture(t)
	b := f.private.base
	reader := momentPublicationOrdinary(t, f)
	a := mp.Access{Actor: reader.actor, Digest: reader.digest}
	r, e := b.store.ReadMapLayers(b.ctx, a, q)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, reader.digest[:]); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- b.store.RevalidateMapLayers(b.ctx, a, r) }()
	deadline := time.Now().Add(3 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE '%sessions%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("no real session wait")
	}
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("late revoke", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait never ended")
	}
}
