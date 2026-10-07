package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	ic "github.com/birdtie/birdtie/apps/api/internal/intentconversion"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type conversionFixture struct {
	f      *participationDisclosureFixture
	g      *humanIntentConversions
	a      ic.Access
	intent socialintent.Record
}

func conversionNative(t *testing.T) *conversionFixture {
	f := participationDisclosureNative(t)
	b := f.f
	b.exec(`UPDATE activities SET modality='online',physical_place_status='not_applicable',category_code='badminton' WHERE id=$1`, f.activity)
	var agent string
	if e := b.row(`SELECT id FROM agents WHERE principal_account_id=$1`, b.a.WorkspacePrincipal.ID).Scan(&agent); e != nil {
		t.Fatal(e)
	}
	if _, e := b.s.EnsureAgentProfile(b.ctx, agent, actorref.PrincipalRef{Type: actorref.Person, ID: b.a.WorkspacePrincipal.ID}); e != nil {
		t.Fatal(e)
	}
	actor, e := b.s.Authenticate(b.ctx, b.a.SessionDigest)
	if e != nil {
		t.Fatal(e)
	}
	intent, e := b.s.CreateSocialIntentDraft(b.ctx, actor.ID, socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "本人找线上羽毛球合成意图", Constraints: json.RawMessage(`{"category":"badminton"}`), Audience: "PRIVATE", Modality: "ONLINE", ExpiresAt: time.Now().UTC().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	intent, e = b.s.ActivateSocialIntent(b.ctx, actor.ID, intent.ID)
	if e != nil {
		t.Fatal(e)
	}
	g, e := NewHumanIntentConversions(b.s)
	if e != nil {
		t.Fatal(e)
	}
	return &conversionFixture{f: f, g: g.(*humanIntentConversions), a: ic.Access{Actor: actor, SessionDigest: b.a.SessionDigest}, intent: intent}
}
func (f *conversionFixture) preview(t *testing.T) ic.Preview {
	t.Helper()
	v, e := f.g.ListOwn(f.f.f.ctx, f.a, f.intent.ID)
	if e != nil {
		t.Fatal("native current options", e)
	}
	if len(v.Choices) != 1 || v.Choices[0].ParticipationID != f.f.p || v.Choices[0].Activity.ActivityID != f.f.activity {
		t.Fatal("real original participation selector", v)
	}
	p, e := f.g.PreviewOwn(f.f.f.ctx, f.a, f.intent.ID, ic.Input{ActivityID: f.f.activity, ExpectedVersion: v.Version})
	if e != nil {
		t.Fatal("specific preview", e)
	}
	if p.Revalidate(f.f.f.ctx) != nil {
		t.Fatal("native preview fence")
	}
	return p
}
func TestIntentConversionNativeCurrentOriginalParticipationAndIdempotent(t *testing.T) {
	f := conversionNative(t)
	b := f.f.f
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	var original string
	if e := b.row(`SELECT to_jsonb(p)::text||p.xmin::text FROM activity_participations p WHERE id=$1`, f.f.p).Scan(&original); e != nil {
		t.Fatal(e)
	}
	p := f.preview(t)
	if before != enrichmentAllPublic(t, b.pool, b.ctx) {
		t.Fatal("GET/preview domain write")
	}
	v, e := f.g.ApproveOwn(b.ctx, f.a, f.intent.ID, p.PreviewID)
	if e != nil {
		t.Fatal("native conversion", e)
	}
	if !v.Committed || v.Intent.Status != "CONVERTED" || v.ActivityID != f.f.activity || v.ParticipationID != f.f.p || v.Intent.ConvertedAt == nil || v.Revalidate(b.ctx) != nil {
		t.Fatal("original sameTx association", v)
	}
	after := enrichmentAllPublic(t, b.pool, b.ctx)
	repeat, e := f.g.ApproveOwn(b.ctx, f.a, f.intent.ID, p.PreviewID)
	if e != nil || !repeat.Committed || after != enrichmentAllPublic(t, b.pool, b.ctx) {
		t.Fatal("retry new effect/lease", e)
	}
	var current string
	b.row(`SELECT to_jsonb(p)::text||p.xmin::text FROM activity_participations p WHERE id=$1`, f.f.p).Scan(&current)
	if current != original {
		t.Fatal("conversion re-created or edited original RSVP")
	}
	view, e := f.g.ListOwn(b.ctx, f.a, f.intent.ID)
	if e != nil || view.Intent.ConvertedParticipationID == nil || *view.Intent.ConvertedParticipationID != f.f.p {
		t.Fatal("unknown result original key recovery", e)
	}
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "085_intent_activity_conversion.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(b.ctx, string(down))
	tx.Rollback(context.Background())
	if e == nil || after != enrichmentAllPublic(t, b.pool, b.ctx) {
		t.Fatal("used085 down lost association or mutated public rows", e)
	}
}
func TestIntentConversionNativeSourceOwnershipABAExpiryAndRestart(t *testing.T) {
	for _, kind := range []string{"source_aba", "intent_aba", "city_hidden", "cancel_rsvp", "session_revoke", "cross_owner", "process_key_changed", "natural_expiry"} {
		t.Run(kind, func(t *testing.T) {
			f := conversionNative(t)
			b := f.f.f
			if kind == "natural_expiry" {
				b.exec(`UPDATE activities SET expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, f.f.activity)
			}
			p := f.preview(t)
			switch kind {
			case "source_aba":
				b.exec(`UPDATE activities SET title='B' WHERE id=$1`, f.f.activity)
				b.exec(`UPDATE activities SET title='合成逐活动报名' WHERE id=$1`, f.f.activity)
			case "intent_aba":
				b.exec(`UPDATE social_intents SET title='B',updated_at=clock_timestamp() WHERE id=$1`, f.intent.ID)
				b.exec(`UPDATE social_intents SET title=$2,updated_at=clock_timestamp() WHERE id=$1`, f.intent.ID, f.intent.Title)
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id='aberdeen-gb'`)
			case "cancel_rsvp":
				if _, e := b.s.CancelParticipation(b.ctx, f.a.Actor.ID, f.f.activity); e != nil {
					t.Fatal(e)
				}
			case "session_revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, b.session)
			case "cross_owner":
				f.a.Actor.ID = b.other.WorkspacePrincipal.ID
			case "process_key_changed":
				g, e := NewHumanIntentConversions(b.s)
				if e != nil {
					t.Fatal(e)
				}
				f.g = g.(*humanIntentConversions)
			case "natural_expiry":
				for {
					var passed bool
					if e := b.row(`SELECT clock_timestamp()>=$1`, p.ExpiresAt).Scan(&passed); e != nil {
						t.Fatal(e)
					}
					if passed {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			before := enrichmentAllPublic(t, b.pool, b.ctx)
			out, e := f.g.ApproveOwn(b.ctx, f.a, f.intent.ID, p.PreviewID)
			if e == nil || out.Committed || before != enrichmentAllPublic(t, b.pool, b.ctx) {
				t.Fatal("stale/cross-subject proof effect", kind, e)
			}
		})
	}
}
func TestIntentConversionNativeUnverifiableConstraintsClosed(t *testing.T) {
	for _, raw := range []string{`{"onlinePlatform":"Zoom"}`, `{"minParticipants":2}`, `{"category":"football"}`} {
		t.Run(raw, func(t *testing.T) {
			f := conversionNative(t)
			b := f.f.f
			b.exec(`UPDATE social_intents SET constraints=$2::jsonb,updated_at=clock_timestamp() WHERE id=$1`, f.intent.ID, raw)
			v, e := f.g.ListOwn(b.ctx, f.a, f.intent.ID)
			if e != nil {
				t.Fatal(e)
			}
			if len(v.Choices) != 0 {
				t.Fatal("guessed missing authority", v)
			}
			_, e = f.g.PreviewOwn(b.ctx, f.a, f.intent.ID, ic.Input{ActivityID: f.f.activity, ExpectedVersion: v.Version})
			if !errors.Is(e, ic.ErrConflict) && !errors.Is(e, ic.ErrUnavailable) {
				t.Fatal("unknown constraints accepted", e)
			}
		})
	}
}

func TestIntentConversionNativeLegacyNullAndSchemaGate(t *testing.T) {
	f := conversionNative(t)
	b := f.f.f
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "085_intent_activity_conversion.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "085_intent_activity_conversion.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.pool.Exec(b.ctx, string(down)); e != nil {
		t.Fatal(e)
	}
	// Historical schema084 represented the terminal status without any bridge.
	// This fixture is not a new production conversion or an inferred association.
	b.exec(`UPDATE social_intents SET status='CONVERTED',updated_at=clock_timestamp() WHERE id=$1`, f.intent.ID)
	var original string
	b.row(`SELECT to_jsonb(i)::text||i.xmin::text FROM social_intents i WHERE id=$1`, f.intent.ID).Scan(&original)
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	if _, e = f.g.ListOwn(b.ctx, f.a, f.intent.ID); !errors.Is(e, ic.ErrUnavailable) || before != enrichmentAllPublic(t, b.pool, b.ctx) {
		t.Fatal("missing085 accepted or wrote", e)
	}
	if _, e = b.pool.Exec(b.ctx, string(up)); e != nil {
		t.Fatal(e)
	}
	var retained string
	b.row(`SELECT (to_jsonb(i)-'converted_activity_id'-'converted_participation_id'-'converted_at'-'conversion_preview_digest')::text||i.xmin::text FROM social_intents i WHERE id=$1`, f.intent.ID).Scan(&retained)
	if original != retained {
		t.Fatal("085 changed original legacy row or xmin")
	}
	v, e := f.g.ListOwn(b.ctx, f.a, f.intent.ID)
	if e != nil || v.Intent.Status != "CONVERTED" || v.Intent.ConvertedActivityID != nil || v.Intent.ConvertedParticipationID != nil {
		t.Fatal("legacy relation invented", e, v)
	}
	if _, e = f.g.PreviewOwn(b.ctx, f.a, f.intent.ID, ic.Input{ActivityID: f.f.activity, ExpectedVersion: v.Version}); !errors.Is(e, ic.ErrConflict) {
		t.Fatal("legacy conversion reopened", e)
	}
	b.exec(`ALTER TABLE social_intents DISABLE TRIGGER guard_intent_activity_conversion`)
	before = enrichmentAllPublic(t, b.pool, b.ctx)
	if _, e = f.g.ListOwn(b.ctx, f.a, f.intent.ID); !errors.Is(e, ic.ErrUnavailable) || before != enrichmentAllPublic(t, b.pool, b.ctx) {
		t.Fatal("disabled schema guard accepted", e)
	}
	b.exec(`ALTER TABLE social_intents ENABLE TRIGGER guard_intent_activity_conversion`)
}
func TestIntentConversionNativeFinalSessionWaitExpiryRejectsAndRollsBack(t *testing.T) {
	f := conversionNative(t)
	b := f.f.f
	// Explicit original native deadline, enough time for real preview setup;
	// the test waits until its actual returned expiry, never renews any proof.
	b.exec(`UPDATE activities SET expires_at=clock_timestamp()+interval '4 seconds' WHERE id=$1`, f.f.activity)
	p := f.preview(t)
	locker, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer locker.Release()
	tx, e := locker.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var pid int32
	tx.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&pid)
	if _, e = tx.Exec(b.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, b.session); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(b.ctx)
	defer cancel()
	go func() { _, e := f.g.ApproveOwn(ctx, f.a, f.intent.ID, p.PreviewID); done <- e }()
	// Match this lock owner and this exact final session row-share request;
	// unrelated schema/table waits cannot satisfy this native barrier.
	for {
		var wait bool
		e = b.row(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity s WHERE $1=ANY(pg_blocking_pids(s.pid)) AND s.query LIKE '%sessions%' AND s.query LIKE '%FOR SHARE%')`, pid).Scan(&wait)
		if e != nil {
			t.Fatal(e)
		}
		if wait {
			break
		}
		select {
		case e := <-done:
			t.Fatal("approval returned before actual final wait", e)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		var expired bool
		b.row(`SELECT clock_timestamp()>=$1`, p.ExpiresAt).Scan(&expired)
		if expired {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	tx.Rollback(context.Background())
	if e = <-done; e == nil {
		t.Fatal("deadline expired during final Session wait committed")
	}
	var status string
	var association *string
	b.row(`SELECT status,converted_activity_id FROM social_intents WHERE id=$1`, f.intent.ID).Scan(&status, &association)
	var audits int
	b.row(`SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='convert'`, f.intent.ID).Scan(&audits)
	if status != "ACTIVE" || association != nil || audits != 0 {
		t.Fatal("expired final wait did not rollback sameTx", status, association, audits)
	}
}

func TestIntentConversionNativeNormalIdleRefreshAndOriginalPreviewCeiling(t *testing.T) {
	for _, kind := range []string{"normal_idle", "original_idle_expired", "absolute_expired", "new_session"} {
		t.Run(kind, func(t *testing.T) {
			f := conversionNative(t)
			b := f.f.f
			if kind == "original_idle_expired" {
				b.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) UPDATE sessions SET idle_expires_at=stamp.n+interval '4 seconds' FROM stamp WHERE id=$1`, b.session)
			}
			if kind == "absolute_expired" {
				b.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) UPDATE sessions SET expires_at=stamp.n+interval '4 seconds',idle_expires_at=stamp.n+interval '4 seconds' FROM stamp WHERE id=$1`, b.session)
			}
			p := f.preview(t)
			for i := 0; i < 3; i++ {
				if _, e := b.s.Authenticate(b.ctx, f.a.SessionDigest); e != nil {
					t.Fatal("legitimate idle refresh", e)
				}
			}
			if kind == "new_session" {
				// Replacement native session identity, never a second authority or grant.
				b.exec(`UPDATE sessions SET id=gen_random_uuid(),created_at=clock_timestamp() WHERE id=$1`, b.session)
			}
			if kind == "original_idle_expired" || kind == "absolute_expired" {
				for {
					var done bool
					b.row(`SELECT clock_timestamp()>=$1`, p.ExpiresAt).Scan(&done)
					if done {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			before := enrichmentAllPublic(t, b.pool, b.ctx)
			v, e := f.g.ApproveOwn(b.ctx, f.a, f.intent.ID, p.PreviewID)
			if kind == "normal_idle" {
				if e != nil || !v.Committed || !v.ObservedAt.Before(p.ExpiresAt) {
					t.Fatal("normal repeated idle update invalidated stable current session", e, v)
				}
			} else if e == nil || v.Committed || before != enrichmentAllPublic(t, b.pool, b.ctx) {
				t.Fatal("old native session or original approved ceiling reused", kind, e)
			}
		})
	}
}

func TestIntentConversionNativeConcretePlaceTimeCategoryAndSourceLimits(t *testing.T) {
	for _, kind := range []string{"exact_place_time", "hidden_place", "time_mismatch", "category_mismatch", "blocked_host"} {
		t.Run(kind, func(t *testing.T) {
			f := conversionNative(t)
			b := f.f.f
			var place string
			if e := b.row(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),'aberdeen-gb','原公开球馆','sports','published','合成','local:PLN_conversion','合成') RETURNING id`).Scan(&place); e != nil {
				t.Fatal(e)
			}
			b.exec(`UPDATE activities SET place_id=$1,modality='in_person',physical_place_status='confirmed' WHERE id=$2`, place, f.f.activity)
			var start, end time.Time
			b.row(`SELECT starts_at,ends_at FROM activities WHERE id=$1`, f.f.activity).Scan(&start, &end)
			raw, _ := json.Marshal(map[string]any{"placeId": place, "category": "badminton", "startsAt": start, "endsAt": end})
			b.exec(`UPDATE social_intents SET modality='IN_PERSON',constraints=$2::jsonb,updated_at=clock_timestamp() WHERE id=$1`, f.intent.ID, string(raw))
			p := f.preview(t)
			if p.Choice.Activity.PlaceID != place || p.Choice.Activity.PlaceName != "原公开球馆" || !p.Choice.Activity.StartsAt.Equal(start) || !p.Choice.Activity.EndsAt.Equal(end) {
				t.Fatal("exact native place/time not preserved", p.Choice)
			}
			switch kind {
			case "hidden_place":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, place)
			case "time_mismatch":
				b.exec(`UPDATE activities SET starts_at=starts_at+interval '1 minute',ends_at=ends_at+interval '1 minute' WHERE id=$1`, f.f.activity)
			case "category_mismatch":
				b.exec(`UPDATE activities SET category_code='football' WHERE id=$1`, f.f.activity)
			case "blocked_host":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) SELECT $1,host_account_id FROM activities WHERE id=$2`, f.a.Actor.ID, f.f.activity)
			}
			before := enrichmentAllPublic(t, b.pool, b.ctx)
			out, e := f.g.ApproveOwn(b.ctx, f.a, f.intent.ID, p.PreviewID)
			if kind == "exact_place_time" {
				if e != nil || !out.Committed {
					t.Fatal("verified concrete place/time category failed", e)
				}
			} else if e == nil || out.Committed || before != enrichmentAllPublic(t, b.pool, b.ctx) {
				t.Fatal("changed explicit source converted", kind, e)
			}
		})
	}
}
