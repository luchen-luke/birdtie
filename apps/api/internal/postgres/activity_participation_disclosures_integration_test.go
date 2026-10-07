package postgres

import (
	"context"
	"encoding/json"
	"errors"
	apd "github.com/birdtie/birdtie/apps/api/internal/activityparticipationdisclosure"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/venue"
	"github.com/jackc/pgx/v5"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

type participationDisclosureFixture struct {
	f                  *interestFixture
	activity, p, peerP string
	expiry             time.Time
}

func participationDisclosureNative(t *testing.T) *participationDisclosureFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("participation disclosure requires actual owned native DB")
	}
	ownedMigrationDatabase(t)
	f := interestNative(t)
	f.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES('aberdeen-gb','合成City','test','GB','Europe/London','published','合成','disposable://activity-disclosure','合成') ON CONFLICT DO NOTHING`)
	act, e := f.s.CreateSocialDraft(f.ctx, f.owner, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: f.owner}, CityID: "aberdeen-gb", Title: "合成逐活动报名", Summary: "仅合成报名，不是到场", Description: "PRIVATE_DESCRIPTION_CANARY", Visibility: "public", StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(3 * time.Hour), TimeZone: "Europe/London"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.PublishSocialActivity(f.ctx, f.owner, act.ID); e != nil {
		t.Fatal(e)
	}
	p, _, e := f.s.JoinActivity(f.ctx, f.a.WorkspacePrincipal.ID, act.ID)
	if e != nil {
		t.Fatal(e)
	}
	peer, _, e := f.s.JoinActivity(f.ctx, f.other.WorkspacePrincipal.ID, act.ID)
	if e != nil {
		t.Fatal(e)
	}
	return &participationDisclosureFixture{f: f, activity: act.ID, p: p.ID, peerP: peer.ID, expiry: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
}
func (f *participationDisclosureFixture) preview(t *testing.T, op string) apd.Preview {
	t.Helper()
	in := apd.Input{ParticipationID: f.p, Operation: op}
	if op == "PUBLIC" {
		in.DisclosureExpiresAt = &f.expiry
	}
	p, e := f.f.s.PreviewOwnParticipationDisclosure(f.f.ctx, f.f.a, in)
	if e != nil {
		t.Fatal(op, e)
	}
	if apd.ValidatePreview(p, f.f.a.WorkspacePrincipal.ID, in) != nil {
		t.Fatal("invalid native preview", p)
	}
	return p
}
func (f *participationDisclosureFixture) approve(t *testing.T, p apd.Preview) apd.View {
	t.Helper()
	v, e := f.f.s.ApproveOwnParticipationDisclosure(f.f.ctx, f.f.a, p.Preview)
	if e != nil || apd.ValidateView(v, f.f.a.WorkspacePrincipal.ID) != nil {
		t.Fatal("approve", e, v)
	}
	return v
}
func (f *participationDisclosureFixture) snapshot() string {
	var raw string
	if e := f.f.row(`SELECT jsonb_build_object('participations',(SELECT jsonb_agg(to_jsonb(p) ORDER BY p.id) FROM activity_participations p WHERE p.activity_id=$1),'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM audit_events a WHERE resource_type='activity_participation_disclosure'),'other',(SELECT coalesce(jsonb_agg(to_jsonb(p) ORDER BY p.id),'[]') FROM inbox_items p WHERE recipient_account_id=$2))::text`, f.activity, f.f.a.WorkspacePrincipal.ID).Scan(&raw); e != nil {
		f.f.t.Fatal(e)
	}
	return raw
}
func (f *participationDisclosureFixture) original() string {
	var raw string
	if e := f.f.row(`SELECT jsonb_agg(jsonb_build_array(id,activity_id,participant_account_id,status,cancelled_at,created_at,updated_at) ORDER BY id)::text FROM activity_participations WHERE activity_id=$1`, f.activity).Scan(&raw); e != nil {
		f.f.t.Fatal(e)
	}
	return raw
}
func disclosureMigration(t *testing.T, f *interestFixture, name string, wantSuccess bool) {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.pool.Exec(f.ctx, string(raw))
	if (e == nil) != wantSuccess {
		t.Fatal(name, e)
	}
	if e != nil {
		f.pool.Exec(f.ctx, "ROLLBACK")
	}
}
func TestParticipationDisclosureNativeCurrentLifecycleAndAudit(t *testing.T) {
	f := participationDisclosureNative(t)
	b := f.f
	before := f.snapshot()
	orig := f.original()
	v, e := b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
	if e != nil || len(v.Records) != 1 || v.Records[0].Visibility != "PRIVATE" || v.Records[0].EffectivePublic {
		t.Fatal(e, v)
	}
	p := f.preview(t, "PUBLIC")
	if f.snapshot() != before {
		t.Fatal("read/preview domain write")
	}
	sealed, e := openParticipationDisclosure(p.Preview)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(sealed)
	if string(raw) == "" || bytesContain(raw, "PRIVATE_DESCRIPTION_CANARY") {
		t.Fatal("private source body sealed")
	}
	got := f.approve(t, p)
	if !got.Records[0].EffectivePublic || got.Records[0].Attendance != "UNKNOWN" || f.original() != orig {
		t.Fatal("disclosure changed RSVP or attendance", got)
	}
	if _, e = b.s.ApproveOwnParticipationDisclosure(b.ctx, b.a, p.Preview); !errors.Is(e, apd.ErrChanged) {
		t.Fatal("same preview repeated", e)
	}
	p = f.preview(t, "PRIVATE")
	f.approve(t, p)
	if f.original() != orig {
		t.Fatal("withdraw cancelled RSVP")
	}
	pub := f.preview(t, "PUBLIC")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := b.s.ApproveOwnParticipationDisclosure(b.ctx, b.a, pub.Preview)
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	ok, bad := 0, 0
	for e := range errs {
		if e == nil {
			ok++
		} else if errors.Is(e, apd.ErrChanged) {
			bad++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || bad != 1 {
		t.Fatal(ok, bad)
	}
	before = f.snapshot()
	for _, sql := range []string{`DELETE FROM audit_events WHERE resource_type='activity_participation_disclosure'`, `UPDATE audit_events SET action='PRIVATE' WHERE resource_type='activity_participation_disclosure'`, `TRUNCATE audit_events`, `UPDATE activity_participations SET disclosure_revision=1 WHERE id=$1`, `UPDATE activity_participations SET id=gen_random_uuid() WHERE id=$1`, `UPDATE activity_participations SET created_at=created_at+interval '1 second' WHERE id=$1`} {
		var e error
		if bytesContain([]byte(sql), "$1") {
			_, e = b.pool.Exec(b.ctx, sql, f.p)
		} else {
			_, e = b.pool.Exec(b.ctx, sql)
		}
		if e == nil {
			t.Fatal("immutable accepted", sql)
		}
	}
	if before != f.snapshot() {
		t.Fatal("guard failure mutated source")
	}
	disclosureMigration(t, b, "078_activity_participation_public_disclosure.down.sql", false)
	old := f.preview(t, "PUBLIC")
	cancelled, e := b.s.CancelParticipation(b.ctx, b.a.WorkspacePrincipal.ID, f.activity)
	if e != nil || cancelled.ID != f.p {
		t.Fatal(e, cancelled)
	}
	join, _, e := b.s.JoinActivity(b.ctx, b.a.WorkspacePrincipal.ID, f.activity)
	if e != nil || join.ID != f.p {
		t.Fatal(e, join)
	}
	v, e = b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
	if e != nil || v.Records[0].Visibility != "PRIVATE" {
		t.Fatal(e, v)
	}
	if _, e = b.s.ApproveOwnParticipationDisclosure(b.ctx, b.a, old.Preview); !errors.Is(e, apd.ErrChanged) {
		t.Fatal("cancel rejoin ABA", e)
	}
}
func bytesContain(raw []byte, s string) bool { return stringsContains(string(raw), s) }
func stringsContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
func TestParticipationDisclosureNativeScopeSourceExpiryAndABA(t *testing.T) {
	for _, mode := range []string{"other-owner", "wrong-session", "organization", "source-private", "source-private-ABA", "city-ABA", "source-expired", "source-cancelled", "pending", "participation-cancelled", "too-long", "after-end", "past", "missing-migration", "disabled-version", "disabled-row-audit", "disabled-truncate"} {
		t.Run(mode, func(t *testing.T) {
			f := participationDisclosureNative(t)
			b := f.f
			p := f.preview(t, "PUBLIC")
			a := b.a
			switch mode {
			case "other-owner":
				a = b.other
			case "wrong-session":
				a.SessionDigest = b.other.SessionDigest
			case "organization":
				a.WorkspacePrincipal.Type = "ORGANIZATION"
			case "source-private":
				b.exec(`UPDATE activities SET visibility='private' WHERE id=$1`, f.activity)
			case "source-private-ABA":
				b.exec(`UPDATE activities SET visibility='private' WHERE id=$1`, f.activity)
				b.exec(`UPDATE activities SET visibility='public' WHERE id=$1`, f.activity)
			case "city-ABA":
				b.exec(`UPDATE cities SET name=name||' B' WHERE id='aberdeen-gb'`)
				b.exec(`UPDATE cities SET name=left(name,length(name)-2) WHERE id='aberdeen-gb'`)
			case "source-expired":
				b.exec(`UPDATE activities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.activity)
			case "source-cancelled":
				if _, e := b.s.CancelSocialActivity(b.ctx, b.owner, f.activity); e != nil {
					t.Fatal(e)
				}
			case "pending":
				b.exec(`UPDATE activity_participations SET status='pending' WHERE id=$1`, f.p)
			case "participation-cancelled":
				if _, e := b.s.CancelParticipation(b.ctx, b.a.WorkspacePrincipal.ID, f.activity); e != nil {
					t.Fatal(e)
				}
			case "too-long", "after-end", "past":
				expiry := time.Now().UTC().Add(25 * time.Hour)
				if mode == "after-end" {
					expiry = time.Now().UTC().Add(4 * time.Hour)
				}
				if mode == "past" {
					expiry = time.Now().UTC().Add(-time.Second)
				}
				if _, e := b.s.PreviewOwnParticipationDisclosure(b.ctx, a, apd.Input{ParticipationID: f.p, Operation: "PUBLIC", DisclosureExpiresAt: &expiry}); e == nil {
					t.Fatal("deadline accepted")
				}
				return
			case "missing-migration":
				disclosureMigration(t, b, "078_activity_participation_public_disclosure.down.sql", true)
			case "disabled-version":
				b.exec(`ALTER TABLE activity_participations DISABLE TRIGGER participation_disclosure_version`)
			case "disabled-row-audit":
				b.exec(`ALTER TABLE audit_events DISABLE TRIGGER participation_disclosure_audit_guard`)
			case "disabled-truncate":
				b.exec(`ALTER TABLE audit_events DISABLE TRIGGER participation_disclosure_audit_truncate_guard`)
			}
			before := f.original()
			got, e := b.s.ApproveOwnParticipationDisclosure(b.ctx, a, p.Preview)
			if e == nil || !reflect.DeepEqual(got, apd.View{}) {
				t.Fatal("invalid authority payload", mode, e, got)
			}
			if before != f.original() {
				t.Fatal("denial mutated RSVP")
			}
		})
	}
}
func TestParticipationDisclosureNativeHiddenWithdrawalAndNaturalExpiry(t *testing.T) {
	f := participationDisclosureNative(t)
	b := f.f
	f.expiry = time.Now().UTC().Add(350 * time.Millisecond).Truncate(time.Microsecond)
	f.approve(t, f.preview(t, "PUBLIC"))
	time.Sleep(400 * time.Millisecond)
	v, e := b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
	if e != nil || v.Records[0].EffectivePublic {
		t.Fatal("natural disclosure expiry", e, v)
	}
	b.exec(`UPDATE activities SET visibility='private' WHERE id=$1`, f.activity)
	v, e = b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
	if e != nil || v.Records[0].Title != "已不可公开展示的活动报名" || v.Records[0].StartsAt != nil || v.Records[0].SourceAvailable {
		t.Fatal("hidden read", e, v)
	}
	f.approve(t, f.preview(t, "PRIVATE"))
	p, e := b.s.GetParticipation(b.ctx, b.a.WorkspacePrincipal.ID, f.activity)
	if e != nil || p.Status != "going" {
		t.Fatal("withdraw cancelled", e, p)
	}
}
func TestParticipationDisclosureNativeRealWaitAndFinalAuditClock(t *testing.T) {
	for _, mode := range []string{"revoke", "expiry", "agent-retire", "block-phantom", "audit-clock", "audit-block"} {
		t.Run(mode, func(t *testing.T) {
			f := participationDisclosureNative(t)
			b := f.f
			p := f.preview(t, "PUBLIC")
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			pattern := "%FROM activities%FOR SHARE OF a%"
			if mode == "agent-retire" {
				pattern = "%FROM accounts%FOR SHARE%"
			}
			if mode == "audit-clock" || mode == "audit-block" {
				sealed, e := openParticipationDisclosure(p.Preview)
				if e != nil {
					t.Fatal(e)
				}
				if mode == "audit-clock" {
					sealed.Expires = time.Now().UTC().Add(250 * time.Millisecond)
				}
				p.Preview, e = sealParticipationDisclosure(sealed)
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`CREATE FUNCTION disclosure_test_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(9043078);RETURN NEW;END $$;CREATE TRIGGER disclosure_test_wait BEFORE INSERT ON audit_events FOR EACH ROW WHEN(NEW.resource_type='activity_participation_disclosure') EXECUTE FUNCTION disclosure_test_wait()`)
				if _, e = held.Exec(b.ctx, `SELECT pg_advisory_xact_lock(9043078)`); e != nil {
					t.Fatal(e)
				}
				pattern = "%INSERT INTO audit_events%"
			} else {
				if mode == "expiry" {
					b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '450 milliseconds' WHERE id=$1`, b.session)
				}
				q := `SELECT id FROM activities WHERE id=$1 FOR UPDATE`
				id := f.activity
				if mode == "agent-retire" {
					q = `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`
					id = b.a.WorkspacePrincipal.ID
				}
				if _, e = held.Exec(b.ctx, q, id); e != nil {
					t.Fatal(e)
				}
			}
			before := f.original()
			done := make(chan error, 1)
			go func() { _, e := b.s.ApproveOwnParticipationDisclosure(b.ctx, b.a, p.Preview); done <- e }()
			interestWaitFor(t, b, pattern)
			switch mode {
			case "revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, b.session)
			case "expiry":
				time.Sleep(520 * time.Millisecond)
			case "agent-retire":
				b.exec(`UPDATE agents SET status='retired' WHERE principal_account_id=$1`, b.a.WorkspacePrincipal.ID)
			case "block-phantom":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.owner, b.a.WorkspacePrincipal.ID)
			case "audit-clock":
				time.Sleep(300 * time.Millisecond)
			case "audit-block":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.owner, b.a.WorkspacePrincipal.ID)
			}
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("wait bypass", mode)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("wait did not end")
			}
			if before != f.original() {
				t.Fatal("failed wait changed RSVP")
			}
			var public int
			if e = b.row(`SELECT count(*) FROM activity_participations WHERE id=$1 AND disclosure_visibility='public'`, f.p).Scan(&public); e != nil || public != 0 {
				t.Fatal(e, public)
			}
		})
	}
}
func TestParticipationDisclosureNativeOSProcessRestart(t *testing.T) {
	f := participationDisclosureNative(t)
	p := f.preview(t, "PUBLIC")
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(exe, "-test.run=^TestParticipationDisclosureProcessHelper$")
	cmd.Env = append(os.Environ(), "BIRDTIE_PARTICIPATION_RESTART=1", "BIRDTIE_PARTICIPATION_SEALED="+p.Preview)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
}
func TestParticipationDisclosureProcessHelper(t *testing.T) {
	if os.Getenv("BIRDTIE_PARTICIPATION_RESTART") != "1" {
		return
	}
	if _, e := openParticipationDisclosure(os.Getenv("BIRDTIE_PARTICIPATION_SEALED")); !errors.Is(e, apd.ErrChanged) {
		t.Fatal(e)
	}
}

var _ pgx.Tx

func TestParticipationDisclosureNativeVenueAndHostCurrentSource(t *testing.T) {
	for _, mode := range []string{"place-hidden", "place-expired", "venue-expired", "candidate-expired", "host-retired"} {
		t.Run(mode, func(t *testing.T) {
			f := participationDisclosureNative(t)
			b := f.f
			var place string
			if e := b.row(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),'aberdeen-gb','合成明确地点','sport','published','合成','disposable://activity-disclosure','合成') RETURNING id`).Scan(&place); e != nil {
				t.Fatal(e)
			}
			for _, id := range []string{b.a.WorkspacePrincipal.ID, b.other.WorkspacePrincipal.ID} {
				b.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role,state) VALUES('aberdeen-gb',$1,'reviewer','active')`, id)
			}
			cap := 10
			c, e := b.s.SubmitVenueCandidate(b.ctx, b.a.WorkspacePrincipal.ID, "aberdeen-gb", place, venue.SubmitInput{Facts: venue.Facts{Capacity: &cap, ReservationSupport: "none", Suitability: []string{}, Amenities: []string{}}, SourceURL: "https://example.invalid/disclosure", RightsNote: "PRIVATE_VENUE_RIGHTS_CANARY", ExpiresAt: time.Now().UTC().Add(2 * time.Hour)})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = b.s.ReviewVenueCandidate(b.ctx, b.other.WorkspacePrincipal.ID, c.ID, venue.ReviewInput{Decision: "approve", Note: "本地合成审核"}); e != nil {
				t.Fatal(e)
			}
			b.exec(`UPDATE activities SET place_id=$2,venue_place_id=$2,modality='in_person',physical_place_status='confirmed' WHERE id=$1`, f.activity, place)
			p := f.preview(t, "PUBLIC")
			sealed, e := openParticipationDisclosure(p.Preview)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(sealed)
			if bytesContain(raw, "PRIVATE_VENUE_RIGHTS_CANARY") {
				t.Fatal("private venue rights sealed")
			}
			f.approve(t, p)
			switch mode {
			case "place-hidden":
				b.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, place)
			case "place-expired":
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, place)
			case "venue-expired":
				b.exec(`UPDATE venues SET expires_at=clock_timestamp()-interval '1 second' WHERE place_id=$1`, place)
			case "candidate-expired":
				b.exec(`UPDATE venue_candidates SET expires_at=created_at+interval '1 microsecond' WHERE id=$1`, c.ID)
			case "host-retired":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.other.WorkspacePrincipal.ID)
			}
			v, e := b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
			if e != nil || len(v.Records) != 1 || v.Records[0].SourceAvailable || v.Records[0].EffectivePublic || v.Records[0].Title != "已不可公开展示的活动报名" || v.Records[0].StartsAt != nil {
				t.Fatal("current invalid source", e, v)
			}
			if _, e = b.s.PreviewOwnParticipationDisclosure(b.ctx, b.a, apd.Input{ParticipationID: f.p, Operation: "PUBLIC", DisclosureExpiresAt: &f.expiry}); e == nil {
				t.Fatal("invalid source preview")
			}
			f.approve(t, f.preview(t, "PRIVATE"))
		})
	}
}
func TestParticipationDisclosureNativeMultiRowReadExpiryCurrentSnapshot(t *testing.T) {
	f := participationDisclosureNative(t)
	b := f.f
	a, e := b.s.CreateSocialDraft(b.ctx, b.owner, activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.owner}, CityID: "aberdeen-gb", Title: "合成第二个报名", Summary: "等待边界", Visibility: "public", StartsAt: time.Now().UTC().Add(time.Hour), EndsAt: time.Now().UTC().Add(3 * time.Hour), TimeZone: "Europe/London"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.s.PublishSocialActivity(b.ctx, b.owner, a.ID); e != nil {
		t.Fatal(e)
	}
	// Initial fixture key controls order, not an existing ID remap or fake approval.
	b.exec(`INSERT INTO activity_participations(id,activity_id,participant_account_id,status) VALUES('ffffffff-ffff-4fff-bfff-ffffffffffff',$1,$2,'going')`, a.ID, b.a.WorkspacePrincipal.ID)
	f.expiry = time.Now().UTC().Add(500 * time.Millisecond).Truncate(time.Microsecond)
	f.approve(t, f.preview(t, "PUBLIC"))
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	if _, e = held.Exec(b.ctx, `SELECT id FROM activity_participations WHERE activity_id=$1 FOR UPDATE`, a.ID); e != nil {
		t.Fatal(e)
	}
	type result struct {
		v apd.View
		e error
	}
	done := make(chan result, 1)
	go func() { v, e := b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false); done <- result{v, e} }()
	interestWaitFor(t, b, "%FROM activity_participations%FOR SHARE%")
	time.Sleep(550 * time.Millisecond)
	held.Commit(b.ctx)
	r := <-done
	if r.e == nil || !reflect.DeepEqual(r.v, apd.View{}) {
		t.Fatal("early effective PUBLIC after later wait", r)
	}
	v, e := b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
	if e != nil || len(v.Records) != 2 {
		t.Fatal("expired source cannot refresh", e, v)
	}
	for _, r := range v.Records {
		if r.EffectivePublic {
			t.Fatal("expired disclosure leaked")
		}
	}
}
func TestParticipationDisclosureNativeCancelAndDownRealWait(t *testing.T) {
	for _, mode := range []string{"cancel", "down"} {
		t.Run(mode, func(t *testing.T) {
			f := participationDisclosureNative(t)
			b := f.f
			p := f.preview(t, "PUBLIC")
			b.exec(`CREATE FUNCTION disclosure_competition_wait() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(9043178);RETURN NEW;END $$;CREATE TRIGGER disclosure_competition_wait BEFORE INSERT ON audit_events FOR EACH ROW WHEN(NEW.resource_type='activity_participation_disclosure') EXECUTE FUNCTION disclosure_competition_wait()`)
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			if _, e = held.Exec(b.ctx, `SELECT pg_advisory_xact_lock(9043178)`); e != nil {
				t.Fatal(e)
			}
			approve := make(chan error, 1)
			go func() { _, e := b.s.ApproveOwnParticipationDisclosure(b.ctx, b.a, p.Preview); approve <- e }()
			interestWaitFor(t, b, "%INSERT INTO audit_events%")
			second := make(chan error, 1)
			if mode == "cancel" {
				go func() { _, e := b.s.CancelParticipation(b.ctx, b.a.WorkspacePrincipal.ID, f.activity); second <- e }()
				interestWaitFor(t, b, "%UPDATE activity_participations%status='cancelled'%")
			} else {
				raw, e := os.ReadFile(filepath.Join("..", "..", "migrations", "078_activity_participation_public_disclosure.down.sql"))
				if e != nil {
					t.Fatal(e)
				}
				go func() { _, e := b.pool.Exec(b.ctx, string(raw)); second <- e }()
				interestWaitFor(t, b, "%used participation disclosure lineage prevents down%")
			}
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			if e = <-approve; e != nil {
				t.Fatal("approved final current version", e)
			}
			e = <-second
			if (mode == "cancel" && e != nil) || (mode == "down" && e == nil) {
				t.Fatal("competing operation", mode, e)
			}
			if mode == "cancel" {
				v, e := b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
				if e != nil || v.Records[0].Visibility != "PRIVATE" || v.Records[0].Status != "cancelled" {
					t.Fatal("cancel did not clear disclosure", e, v)
				}
			} else {
				b.pool.Exec(b.ctx, "ROLLBACK")
				v, e := b.s.ReadOwnParticipationDisclosures(b.ctx, b.a, false)
				if e != nil || !v.Records[0].EffectivePublic {
					t.Fatal("down raced away used source", e, v)
				}
			}
		})
	}
}
