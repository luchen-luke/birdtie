package postgres

import (
	"context"
	"encoding/json"
	"errors"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"reflect"
	"strings"
	"testing"
	"time"
)

func humanNewPeopleFixture(t *testing.T) (*newPeopleFixture, mp.Access, []socialintent.Record) {
	f := v4PrivacyNative(t)
	a := v4PrivacySession(t, f, 0)
	out := []socialintent.Record{}
	for _, id := range f.ids[:2] {
		if _, e := f.store.SetNewPeopleConsent(f.ctx, id, true); e != nil {
			t.Fatal(e)
		}
		r, e := f.store.CreateNewPeopleIntent(f.ctx, id, newpeople.DraftInput{Title: "PRIVATE_INTENT_CANARY", Category: f.category, Modality: "ONLINE", OnlinePlatform: "PRIVATE_PLATFORM_CANARY", ExpiresAt: time.Now().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		r, e = f.store.ActivateSocialIntent(f.ctx, id, r.ID)
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, r)
	}
	return f, a, out
}
func TestHumanNewPeopleNativeReceiptMinimumNoWritesAndForgery(t *testing.T) {
	f, a, rows := humanNewPeopleFixture(t)
	before := enrichmentAllPublic(t, f.pool, f.ctx)
	r, e := f.store.ReadHumanNewPeople(f.ctx, a.Actor, a.Digest, rows[0].ID)
	if e != nil || len(r.Response.Candidates) != 1 {
		t.Fatal(e, r.Response)
	}
	v4PrivacyBytes(t, r.Response)
	if _, e = json.Marshal(r); !errors.Is(e, newpeople.ErrForbidden) {
		t.Fatal("receipt serializable", e)
	}
	if e = f.store.RevalidateHumanNewPeople(f.ctx, a.Actor, a.Digest, r); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(before, enrichmentAllPublic(t, f.pool, f.ctx)) {
		t.Fatal("native candidate read/revalidate wrote any domain")
	}
	forged := r
	forged.Response.Candidates = append([]newpeople.Candidate{}, r.Response.Candidates...)
	forged.Response.Candidates[0].DisplayName = "私密伪造名称"
	if e = f.store.RevalidateHumanNewPeople(f.ctx, a.Actor, a.Digest, forged); !errors.Is(e, newpeople.ErrForbidden) {
		t.Fatal("forged DTO accepted", e)
	}
	peer := v4PrivacySession(t, f, 1)
	if e = f.store.RevalidateHumanNewPeople(f.ctx, peer.Actor, peer.Digest, r); e == nil {
		t.Fatal("cross actor receipt")
	}
	fresh := New(f.pool, false)
	if e = fresh.RevalidateHumanNewPeople(f.ctx, a.Actor, a.Digest, r); e != nil {
		t.Fatal("same process new Store cannot revalidate current receipt", e)
	}
}
func TestHumanNewPeopleNativeSourceAndSessionABA(t *testing.T) {
	for _, kind := range []string{"intent-ABA", "block-unblock-ABA", "optin-ABA", "profile-ABA", "session-ABA", "forward-block", "reverse-block", "accepted-friend", "pending-invite", "session-revoke", "expired-source", "expired-session"} {
		t.Run(kind, func(t *testing.T) {
			f, a, rows := humanNewPeopleFixture(t)
			if kind == "expired-source" {
				f.exec(`UPDATE social_intents SET expires_at=clock_timestamp()+interval '140 milliseconds' WHERE id=$1`, rows[1].ID)
			}
			if kind == "expired-session" {
				f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '140 milliseconds' WHERE token_sha256=$1`, a.Digest[:])
			}
			r, e := f.store.ReadHumanNewPeople(f.ctx, a.Actor, a.Digest, rows[0].ID)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "intent-ABA":
				f.exec(`UPDATE social_intents SET title=title WHERE id=$1`, rows[1].ID)
			case "block-unblock-ABA":
				e = f.store.BlockAccount(f.ctx, f.ids[1], f.ids[0])
				if e != nil {
					t.Fatal(e)
				}
				e = f.store.UnblockAccount(f.ctx, f.ids[1], f.ids[0])
			case "optin-ABA":
				if _, e = f.store.SetNewPeopleConsent(f.ctx, f.ids[1], false); e != nil {
					t.Fatal(e)
				}
				if _, e = f.store.SetNewPeopleConsent(f.ctx, f.ids[1], true); e != nil {
					t.Fatal(e)
				}
			case "profile-ABA":
				f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[1])
				f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.ids[1])
			case "session-ABA": // Explicit privileged same-row mutation; no production restore API is claimed.
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.Digest[:])
				f.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, a.Digest[:])
			case "forward-block":
				e = f.store.BlockAccount(f.ctx, f.ids[0], f.ids[1])
			case "reverse-block":
				e = f.store.BlockAccount(f.ctx, f.ids[1], f.ids[0])
			case "accepted-friend", "pending-invite":
				req, x := f.store.CreateFriendRequest(f.ctx, f.ids[0], f.ids[1], "真实独立好友动作")
				e = x
				if e == nil && kind == "accepted-friend" {
					_, e = f.store.DecideRequest(f.ctx, f.ids[1], req.ID, "accept")
				}
			case "session-revoke":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.Digest[:])
			case "expired-source", "expired-session":
				time.Sleep(180 * time.Millisecond)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = f.store.RevalidateHumanNewPeople(f.ctx, a.Actor, a.Digest, r); e == nil {
				t.Fatal("stale candidate receipt survived", kind)
			}
		})
	}
}
func TestHumanNewPeopleNativeRealSessionWaitCurrentSources(t *testing.T) {
	for _, kind := range []string{"revoke", "natural-session-expiry", "opt-out", "forward-block", "reverse-block", "intent-ABA", "profile-ABA", "source-expiry"} {
		t.Run(kind, func(t *testing.T) {
			f, a, rows := humanNewPeopleFixture(t)
			if kind == "natural-session-expiry" {
				f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '400 milliseconds' WHERE token_sha256=$1`, a.Digest[:])
			}
			if kind == "source-expiry" {
				f.exec(`UPDATE social_intents SET expires_at=clock_timestamp()+interval '400 milliseconds' WHERE id=$1`, rows[1].ID)
			}
			r, e := f.store.ReadHumanNewPeople(f.ctx, a.Actor, a.Digest, rows[0].ID)
			if e != nil {
				t.Fatal(e)
			}
			held, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			if _, e = held.Exec(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.Digest[:]); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { done <- f.store.RevalidateHumanNewPeople(f.ctx, a.Actor, a.Digest, r) }()
			seen := false
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				var n int
				if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM sessions%FOR SHARE%'`).Scan(&n); e != nil {
					t.Fatal(e)
				}
				if n > 0 {
					seen = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !seen {
				t.Fatal("actual Session row wait absent")
			}
			switch kind {
			case "revoke":
				_, e = held.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.Digest[:])
			case "natural-session-expiry", "source-expiry":
				time.Sleep(500 * time.Millisecond)
			case "opt-out":
				_, e = f.store.SetNewPeopleConsent(f.ctx, f.ids[1], false)
			case "forward-block":
				e = f.store.BlockAccount(f.ctx, f.ids[0], f.ids[1])
			case "reverse-block":
				e = f.store.BlockAccount(f.ctx, f.ids[1], f.ids[0])
			case "intent-ABA":
				f.exec(`UPDATE social_intents SET title=title WHERE id=$1`, rows[1].ID)
			case "profile-ABA":
				f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[1])
				f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.ids[1])
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = held.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if e == nil {
					t.Fatal("stale candidate released after wait", kind)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native waiter never terminated")
			}
		})
	}
}

func TestHumanNewPeopleNativeDeclaredMeetupNotPositionAndSourceABA(t *testing.T) {
	for _, kind := range []string{"current", "place-ABA", "hidden-place", "city-ABA", "hidden-city", "expired-place"} {
		t.Run(kind, func(t *testing.T) {
			f := v4PrivacyNative(t)
			a := v4PrivacySession(t, f, 0)
			f.enable()
			source := f.activate(f.draft(0, "IN_PERSON", 0, f.places[0], "", ""))
			peer := f.activate(f.draft(1, "IN_PERSON", 0, f.places[0], "", ""))
			if kind == "expired-place" {
				f.exec(`UPDATE places SET expires_at=clock_timestamp()+interval '150 milliseconds' WHERE id=$1`, f.places[0])
			}
			r, e := f.store.ReadHumanNewPeople(f.ctx, a.Actor, a.Digest, source.ID)
			if e != nil || len(r.Response.Candidates) != 1 || r.Response.Candidates[0].CandidateIntentID != peer.ID {
				t.Fatal(e, r.Response)
			}
			v4PrivacyBytes(t, r.Response)
			raw, _ := json.Marshal(r.Response)
			if strings.Contains(string(raw), f.places[0]) || strings.Contains(string(raw), "latitude") || strings.Contains(string(raw), "longitude") {
				t.Fatal("declared meetup became user location", string(raw))
			}
			switch kind {
			case "place-ABA":
				f.exec(`UPDATE places SET name=name WHERE id=$1`, f.places[0])
			case "hidden-place":
				f.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, f.places[0])
			case "city-ABA":
				f.exec(`UPDATE cities SET name=name WHERE id=$1`, f.cities[0])
			case "hidden-city":
				f.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.cities[0])
			case "expired-place":
				time.Sleep(180 * time.Millisecond)
			}
			e = f.store.RevalidateHumanNewPeople(f.ctx, a.Actor, a.Digest, r)
			if kind == "current" && e != nil {
				t.Fatal(e)
			}
			if kind != "current" && e == nil {
				t.Fatal("stale meetup source accepted", kind)
			}
		})
	}
}
