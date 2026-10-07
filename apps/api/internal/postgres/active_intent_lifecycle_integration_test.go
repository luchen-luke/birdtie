package postgres

import (
	"context"
	"encoding/json"
	"errors"
	ai "github.com/birdtie/birdtie/apps/api/internal/activeintent"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func activeNative(t *testing.T) (*placeMemoryFixture, ai.Access, *humanActiveIntents, socialintent.Record) {
	t.Helper()
	f := placeMemoryNativeFixture(t)
	b := f.private.base
	a, e := b.store.Authenticate(b.ctx, f.private.owner.SessionDigest)
	if e != nil {
		t.Fatal(e)
	}
	g, e := NewHumanActiveIntents(b.store)
	if e != nil {
		t.Fatal(e)
	}
	r, e := b.store.CreateSocialIntentDraft(b.ctx, b.person.ID, socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "合成本轮意图", Constraints: json.RawMessage(`{"category":"badminton","areaLabel":"明确粗区域"}`), Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := b.pool.Exec(ctx, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, b.accounts); e != nil {
			t.Error(e)
		}
	})
	return f, ai.Access{Actor: a, SessionDigest: f.private.owner.SessionDigest}, g.(*humanActiveIntents), r
}
func TestActiveIntentNativeSameIDEditActivateCancelAndNoReadsWrite(t *testing.T) {
	// The complete-public no-write assertion must not observe unrelated writes
	// from other packages running concurrently against the disposable parent DB.
	ownedMigrationDatabase(t)
	f, a, g, r := activeNative(t)
	b := f.private.base
	baseline := enrichmentAllPublic(t, b.pool, b.ctx)
	d, e := g.ReadOwn(b.ctx, a, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.ListOwn(b.ctx, a); e != nil {
		t.Fatal(e)
	}
	if _, e = g.OptionsOwn(b.ctx, a); e != nil {
		t.Fatal(e)
	}
	edit := activeDraft(d.Item.Intent)
	edit.Title = "具体版本新标题"
	edit.Modality = "ONLINE"
	edit.Constraints = json.RawMessage(`{"onlinePlatform":"Zoom","startsAt":"2030-01-01T18:00:00+08:00","endsAt":"2030-01-01T19:00:00+08:00"}`)
	p, e := g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: d.Item.Version, Edit: &edit})
	if e != nil {
		t.Fatal(e)
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != baseline {
		t.Fatal("GET/preview wrote domain")
	}
	got, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
	if e != nil || got.Item.Intent.ID != r.ID || got.Item.Intent.Status != "DRAFT" || got.Item.Intent.Title != edit.Title {
		t.Fatal("same ID edit", e)
	}
	if _, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID); !errors.Is(e, ai.ErrConflict) {
		t.Fatal("replayed approval", e)
	}
	p, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "ACTIVATE", ExpectedVersion: got.Item.Version})
	if e != nil {
		t.Fatal(e)
	}
	got, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
	if e != nil || got.Item.Intent.Status != "ACTIVE" {
		t.Fatal("activate", e)
	}
	edit = activeDraft(got.Item.Intent)
	edit.Title = "修改有效意图必须退回草稿"
	p, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: got.Item.Version, Edit: &edit})
	if e != nil || !strings.Contains(p.Explanation, "草稿") {
		t.Fatal("hidden consequence", e)
	}
	got, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
	if e != nil || got.Item.Intent.Status != "DRAFT" {
		t.Fatal(e)
	}
	p, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "CANCEL", ExpectedVersion: got.Item.Version})
	if e != nil {
		t.Fatal(e)
	}
	got, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
	if e != nil || got.Item.Intent.Status != "CANCELLED" {
		t.Fatal(e)
	}
	if _, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "ACTIVATE", ExpectedVersion: got.Item.Version}); !errors.Is(e, ai.ErrConflict) {
		t.Fatal("terminal reopened", e)
	}
}
func TestActiveIntentNativeVersionsAndSourcePrivacy(t *testing.T) {
	for _, kind := range []string{"intentABA", "placeABA", "cityABA", "metadataABA", "agentRetired", "profileABA", "tasklessRestart", "wrongOwner", "sessionRevoke", "hiddenCancel"} {
		t.Run(kind, func(t *testing.T) {
			f, a, g, r := activeNative(t)
			b := f.private.base
			d, e := g.ReadOwn(b.ctx, a, r.ID)
			if e != nil {
				t.Fatal(e)
			}
			edit := activeDraft(d.Item.Intent)
			edit.Constraints = json.RawMessage(`{"placeId":"` + f.place + `"}`)
			p, e := g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: d.Item.Version, Edit: &edit})
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "intentABA":
				b.exec(`UPDATE social_intents SET title='临时改动',updated_at=clock_timestamp() WHERE id=$1`, r.ID)
				b.exec(`UPDATE social_intents SET title=$2,updated_at=clock_timestamp() WHERE id=$1`, r.ID, r.Title)
			case "placeABA":
				b.exec(`UPDATE places SET summary='实际B版本' WHERE id=$1`, f.place)
				b.exec(`UPDATE places SET summary='不能复制的地点描述' WHERE id=$1`, f.place)
			case "cityABA":
				b.exec(`UPDATE cities SET name='合成B城市' WHERE id=$1`, f.city)
				b.exec(`UPDATE cities SET name='合成027城市' WHERE id=$1`, f.city)
			case "metadataABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, b.personID)
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, b.personID)
			case "agentRetired":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
			case "profileABA":
				b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, a.Actor.ID)
				b.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, a.Actor.ID)
			case "tasklessRestart":
				newG, _ := NewHumanActiveIntents(b.store)
				g = newG.(*humanActiveIntents)
			case "wrongOwner":
				a.Actor.ID = b.other.ID
			case "sessionRevoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			case "hiddenCancel":
				ok, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
				current, e := g.ReadOwn(b.ctx, a, r.ID)
				if e != nil || current.Item.SourceAvailable || current.Item.LocationLabel != "地点暂不可用" {
					t.Fatal("hidden source", e)
				}
				p, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "CANCEL", ExpectedVersion: current.Item.Version})
				if e != nil {
					t.Fatal(e)
				}
				v, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
				if e != nil || v.Item.Intent.ID != ok.Item.Intent.ID || v.Item.Intent.Status != "CANCELLED" {
					t.Fatal("neutral cancellation", e)
				}
				return
			}
			if _, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID); e == nil {
				t.Fatal("changed native boundary accepted", kind)
			}
		})
	}
}
func TestActiveIntentNativeTwoApproversOnlyOneEffect(t *testing.T) {
	f, a, g, r := activeNative(t)
	b := f.private.base
	d, e := g.ReadOwn(b.ctx, a, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	p, e := g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "CANCEL", ExpectedVersion: d.Item.Version})
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	n := 0
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
			if e == nil {
				mu.Lock()
				n++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if n != 1 {
		t.Fatal("not one effect", n)
	}
	var audits int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_id=$2 AND purpose='human_concrete_version'`, a.Actor.ID, r.ID).Scan(&audits); e != nil || audits != 1 {
		t.Fatal(e, audits)
	}
}
func TestActiveIntentNativeAudienceModalityAndCurrentTargets(t *testing.T) {
	for _, audience := range []string{"PRIVATE", "FRIENDS", "PUBLIC", "LOCAL", "COMMUNITY", "INVITE_ONLY"} {
		for _, mode := range []string{"ONLINE", "IN_PERSON", "HYBRID"} {
			t.Run(audience+"_"+mode, func(t *testing.T) {
				f, a, g, r := activeNative(t)
				b := f.private.base
				b.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, a.Actor.ID)
				community := "b1700000-0000-4000-8000-000000000030"
				if audience == "COMMUNITY" {
					b.exec(`INSERT INTO community_memberships(community_id,user_account_id,role,status) VALUES($1,$2,'member','active')`, community, a.Actor.ID)
					t.Cleanup(func() { b.exec(`DELETE FROM community_memberships WHERE user_account_id=$1`, a.Actor.ID) })
				}
				d, e := g.ReadOwn(b.ctx, a, r.ID)
				if e != nil {
					t.Fatal(e)
				}
				edit := activeDraft(d.Item.Intent)
				edit.Audience = audience
				edit.Modality = mode
				switch mode {
				case "ONLINE":
					edit.Constraints = json.RawMessage(`{"onlinePlatform":"Zoom"}`)
				case "IN_PERSON":
					edit.Constraints = json.RawMessage(`{"placeId":"` + f.place + `"}`)
				case "HYBRID":
					edit.Constraints = json.RawMessage(`{"placeId":"` + f.place + `","onlinePlatform":"Zoom"}`)
				}
				switch audience {
				case "LOCAL":
					edit.CityID = f.city
				case "COMMUNITY":
					edit.CommunityID = community
				case "INVITE_ONLY":
					edit.InviteeIDs = []string{b.other.ID}
				}
				p, e := g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: d.Item.Version, Edit: &edit})
				if e != nil {
					t.Fatal(e)
				}
				v, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
				if e != nil {
					t.Fatal(e)
				}
				p, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "ACTIVATE", ExpectedVersion: v.Item.Version})
				if e != nil {
					t.Fatal(e)
				}
				v, e = g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID)
				if e != nil || v.Item.Intent.Audience != audience || v.Item.Intent.Modality != mode || v.Item.Intent.Status != "ACTIVE" {
					t.Fatal(e, v)
				}
			})
		}
	}
}
func TestActiveIntentNativeRealWaitCurrentSessionSourceAndDeadline(t *testing.T) {
	for _, mode := range []string{"session-revoke", "session-expiry", "audit-city-hidden", "audit-preview-expiry", "audit-block-phantom"} {
		t.Run(mode, func(t *testing.T) {
			f, a, g, r := activeNative(t)
			b := f.private.base
			d, e := g.ReadOwn(b.ctx, a, r.ID)
			if e != nil {
				t.Fatal(e)
			}
			edit := activeDraft(d.Item.Intent)
			edit.Constraints = json.RawMessage(`{"placeId":"` + f.place + `"}`)
			if mode == "audit-block-phantom" {
				edit.Audience = "INVITE_ONLY"
				edit.InviteeIDs = []string{b.other.ID}
			}
			p, e := g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: d.Item.Version, Edit: &edit})
			if e != nil {
				t.Fatal(e)
			}
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			pattern := "%FROM sessions%FOR SHARE%"
			if strings.HasPrefix(mode, "audit-") {
				b.exec(`CREATE FUNCTION active_intent_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(9043002);RETURN NEW;END $$;CREATE TRIGGER active_intent_wait BEFORE INSERT ON audit_events FOR EACH ROW WHEN(NEW.purpose='human_concrete_version') EXECUTE FUNCTION active_intent_wait()`)
				t.Cleanup(func() { b.exec(`DROP TRIGGER active_intent_wait ON audit_events;DROP FUNCTION active_intent_wait()`) })
				if _, e = held.Exec(b.ctx, `SELECT pg_advisory_xact_lock(9043002)`); e != nil {
					t.Fatal(e)
				}
				pattern = "%INSERT INTO audit_events%"
			} else {
				if mode == "session-expiry" {
					b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '400 milliseconds' WHERE token_sha256=$1`, a.SessionDigest[:])
				}
				if _, e = held.Exec(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.SessionDigest[:]); e != nil {
					t.Fatal(e)
				}
			}
			original := enrichmentAllPublic(t, b.pool, b.ctx)
			if mode == "audit-preview-expiry" {
				sealed, e := g.open(p.PreviewID)
				if e != nil {
					t.Fatal(e)
				}
				sealed.ExpiresAt = time.Now().Add(250 * time.Millisecond)
				p.PreviewID, e = g.seal(sealed)
				if e != nil {
					t.Fatal(e)
				}
			}
			done := make(chan error, 1)
			go func() { _, e := g.ApproveOwn(b.ctx, a, r.ID, p.PreviewID); done <- e }()
			observed := false
			for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
				var n int
				if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1`, pattern).Scan(&n); e != nil {
					t.Fatal(e)
				}
				if n > 0 {
					observed = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !observed {
				t.Fatal("real wait absent", pattern)
			}
			switch mode {
			case "session-revoke":
				if _, e = held.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:]); e != nil {
					t.Fatal(e)
				}
			case "session-expiry", "audit-preview-expiry":
				time.Sleep(500 * time.Millisecond)
			case "audit-city-hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
			case "audit-block-phantom":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, a.Actor.ID, b.other.ID)
			}
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("late current boundary allowed", mode)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("wait unresolved")
			}
			var title, status string
			var audits int
			if e = b.pool.QueryRow(b.ctx, `SELECT title,status,(SELECT count(*) FROM audit_events WHERE resource_id=$1::text AND purpose='human_concrete_version') FROM social_intents WHERE id=$1::uuid`, r.ID).Scan(&title, &status, &audits); e != nil || title != r.Title || status != "DRAFT" || audits != 0 {
				t.Fatal("failed approve wrote", e, title, status, audits)
			}
			_ = original // Source/session mutations are expected, only the intent/effect must remain unchanged.
		})
	}
}
func TestActiveIntentNativeOSProcessKeyRestart(t *testing.T) {
	if token := os.Getenv("BIRDTIE_ACTIVE_TEST_PREVIEW"); token != "" {
		gateway, e := NewHumanActiveIntents(nil)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = gateway.(*humanActiveIntents).open(token); !errors.Is(e, ai.ErrConflict) {
			t.Fatal("fresh process reused former AEAD approval", e)
		}
		return
	}
	f, a, g, r := activeNative(t)
	d, e := g.ReadOwn(f.private.base.ctx, a, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	p, e := g.PreviewOwn(f.private.base.ctx, a, r.ID, ai.Input{Operation: "CANCEL", ExpectedVersion: d.Item.Version})
	if e != nil {
		t.Fatal(e)
	}
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.CommandContext(f.private.base.ctx, binary, "-test.run=^TestActiveIntentNativeOSProcessKeyRestart$", "-test.v")
	cmd.Env = append(os.Environ(), "BIRDTIE_ACTIVE_TEST_PREVIEW="+p.PreviewID)
	raw, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal("actual isolated process did not reject", e, string(raw))
	}
	t.Log("actual child process fresh-key rejection PASS; opaque test payload not logged")
}
func TestActiveIntentNativePlaceRequiresCurrentCityAndExpiry(t *testing.T) {
	for _, kind := range []string{"hidden-city", "expired-city", "expired-place", "local-city-mismatch"} {
		t.Run(kind, func(t *testing.T) {
			f, a, g, r := activeNative(t)
			b := f.private.base
			d, e := g.ReadOwn(b.ctx, a, r.ID)
			if e != nil {
				t.Fatal(e)
			}
			edit := activeDraft(d.Item.Intent)
			edit.Constraints = json.RawMessage(`{"placeId":"` + f.place + `"}`)
			switch kind {
			case "hidden-city":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.city)
			case "expired-city":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.city)
			case "expired-place":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place)
			case "local-city-mismatch":
				edit.Audience = "LOCAL"
				edit.CityID = "aberdeen-gb"
			}
			d, e = g.ReadOwn(b.ctx, a, r.ID)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = g.PreviewOwn(b.ctx, a, r.ID, ai.Input{Operation: "EDIT", ExpectedVersion: d.Item.Version, Edit: &edit}); !errors.Is(e, ai.ErrDenied) {
				t.Fatal("invalid native Place-city accepted", kind, e)
			}
		})
	}
}
