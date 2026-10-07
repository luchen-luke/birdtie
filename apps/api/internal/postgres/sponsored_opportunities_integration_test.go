package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	so "github.com/birdtie/birdtie/apps/api/internal/sponsoredopportunity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type sponsorFixture struct {
	b                                                    *businessConsoleFixture
	business, city, activity, place, operator, principal string
	access                                               []so.Access
}

// The down migration is executed against retained native declarations. No
// approved sponsorship is manufactured by SQL for these positive cases.
func TestSponsoredNativeNonemptyDownPreservesAllPublicRows(t *testing.T) {
	// Other Go packages run concurrently against the parent disposable database.
	// Preserve full-row comparisons by using the existing isolated migration DB
	// helper rather than serializing packages or weakening the assertion.
	ownedMigrationDatabase(t)
	down, err := os.ReadFile("../../migrations/071_sponsored_opportunity_declarations.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"grant_only", "pending", "approved", "revoked"} {
		t.Run(state, func(t *testing.T) {
			f := newSponsorFixture(t, false)
			b := f.b
			if state != "grant_only" {
				d := f.submit(t, "ACTIVITY")
				if state != "pending" {
					d = f.review(t, d.ID)
				}
				if state == "revoked" {
					m, e := b.store.ListOwnSponsoredOpportunities(b.ctx, f.access[0], f.business)
					if e != nil {
						t.Fatal(e)
					}
					for _, x := range m.Declarations {
						if x.ID == d.ID {
							d = x
						}
					}
					_, e = b.store.RevokeSponsoredOpportunity(b.ctx, f.access[0], d.ID, so.ReviewInput{ExpectedRevision: d.Revision, Snapshot: d.Snapshot, Decision: "revoke", Note: "非空回滚保护验收撤回", Confirmed: true})
					if e != nil {
						t.Fatal(e)
					}
				}
				// Isolate declaration retention from grant retention.
				b.exec(t, `DELETE FROM sponsored_opportunity_review_grants WHERE city_id=$1`, f.city)
			}
			before := sponsorCompletePublicRows(t, b)
			conn, e := b.pool.Acquire(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			_, e = conn.Exec(b.ctx, string(down))
			_, rollbackErr := conn.Exec(b.ctx, "ROLLBACK")
			conn.Release()
			var p *pgconn.PgError
			if !errors.As(e, &p) || p.Code != "P0001" || p.Message != "nonempty sponsorship declarations or grants must be retained" || rollbackErr != nil {
				t.Fatal("exact down rejection missing", e, rollbackErr)
			}
			if after := sponsorCompletePublicRows(t, b); !reflect.DeepEqual(before, after) {
				t.Fatal("nonempty down changed a complete public row")
			}
			var objects bool
			if e = b.pool.QueryRow(b.ctx, `SELECT to_regclass('public.sponsored_opportunity_declarations') IS NOT NULL AND to_regclass('public.sponsored_opportunity_review_grants') IS NOT NULL AND to_regprocedure('birdtie_sponsor_source_snapshot(uuid,uuid,text,uuid,timestamp with time zone)') IS NOT NULL`).Scan(&objects); e != nil || !objects {
				t.Fatal("nonempty down removed an owned object", e)
			}
			t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual071 down rejected SQLSTATE=P0001 retained_state=%s full_public_rows=unchanged", state)
		})
	}
}

func sponsorCompletePublicRows(t *testing.T, b *businessConsoleFixture) map[string]string {
	t.Helper()
	conn, err := b.pool.Acquire(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(b.ctx, "SET TIME ZONE 'UTC'"); err != nil {
		t.Fatal(err)
	}
	defer conn.Exec(context.Background(), "RESET TIME ZONE")
	rows, err := conn.Query(b.ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename`)
	if err != nil {
		t.Fatal(err)
	}
	tables := []string{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, table := range tables {
		// The names come exclusively from pg_tables; quote every identifier.
		query := fmt.Sprintf(`SELECT coalesce(jsonb_agg(rowdata ORDER BY rowdata::text),'[]'::jsonb)::text FROM (SELECT to_jsonb(t) rowdata FROM public.%s t) s`, `"`+strings.ReplaceAll(table, `"`, `""`)+`"`)
		var raw string
		if err = conn.QueryRow(b.ctx, query).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		out[table] = raw
	}
	return out
}

func TestSponsoredNativeRepeatableReadPoolAndTimeZone(t *testing.T) {
	f := newSponsorFixture(t, true)
	b := f.b
	in := f.input(t, "PLACE")
	config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["default_transaction_isolation"] = "repeatable read"
	config.ConnConfig.RuntimeParams["TimeZone"] = "Pacific/Honolulu"
	pool, err := pgxpool.NewWithConfig(b.ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := New(pool, false)
	m, err := store.ListOwnSponsoredOpportunities(b.ctx, f.access[0], f.business)
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, x := range m.Targets {
		if x.Target.Type == "PLACE" {
			source = x.SourceSnapshot
		}
	}
	if source != in.SourceSnapshot {
		t.Fatal("connection timezone changed an opaque native source")
	}
	d, created, err := store.SubmitSponsoredOpportunity(b.ctx, f.access[0], f.business, in)
	if err != nil || !created {
		t.Fatal(d, created, err)
	}
	// Daily session refresh is not a permission/source epoch.
	if _, err = store.Authenticate(b.ctx, f.access[0].SessionDigest); err != nil {
		t.Fatal(err)
	}
	if repeated, created, e := store.SubmitSponsoredOpportunity(b.ctx, f.access[0], f.business, in); e != nil || created || repeated.ID != d.ID {
		t.Fatal("normal Session last_seen invalidated the same declaration", repeated, created, e)
	}
	f.review(t, d.ID)
	if out, e := store.ReadSponsoredOpportunities(b.ctx, f.access[0], f.targets()); e != nil || len(out.SponsoredOpportunities) != 1 {
		t.Fatal("explicit RC/UTC persistent disclosure failed", out, e)
	}
	// Native current change remains visible even with a pool default of RR.
	b.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, f.place)
	if out, e := store.ReadSponsoredOpportunities(b.ctx, f.access[0], f.targets()); e != nil || len(out.SponsoredOpportunities) != 0 {
		t.Fatal("pool snapshot retained hidden supply", out, e)
	}
}

func TestSponsoredNativeAuditWaitFinalClock(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, late := range []string{"session_revoke", "session_expiry", "review_grant_expiry", "source_change", "request_cancel"} {
		t.Run(late, func(t *testing.T) {
			f := newSponsorFixture(t, false)
			b := f.b
			d := f.submit(t, "ACTIVITY")
			items, err := b.store.ListSponsoredOpportunityReview(b.ctx, f.access[1], f.city)
			if err != nil || len(items) != 1 {
				t.Fatal(items, err)
			}
			in := so.ReviewInput{ExpectedRevision: d.Revision, Snapshot: items[0].Snapshot, Decision: "approve", Note: "审计写等待后重新核验当前许可", Confirmed: true}
			lock, err := b.pool.Begin(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback(context.Background())
			// The declaration UPDATE has already happened in the writer's private
			// transaction when its subsequent audit INSERT blocks on this lock.
			if _, err = lock.Exec(b.ctx, "LOCK TABLE audit_events IN SHARE MODE"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, e := b.store.ReviewSponsoredOpportunity(ctx, f.access[1], d.ID, in)
				done <- e
			}()
			businessNativeWait(t, b, lock.Conn().PgConn().PID())
			switch late {
			case "session_revoke":
				b.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, b.people[1])
			case "session_expiry":
				b.exec(t, `UPDATE sessions SET expires_at=clock_timestamp()+interval '150 milliseconds',idle_expires_at=clock_timestamp()+interval '100 milliseconds' WHERE account_id=$1`, b.people[1])
				time.Sleep(200 * time.Millisecond)
			case "review_grant_expiry":
				b.exec(t, `UPDATE sponsored_opportunity_review_grants SET valid_until=clock_timestamp() WHERE city_id=$1`, f.city)
			case "source_change":
				b.exec(t, `UPDATE activities SET ends_at=ends_at+interval '1 minute' WHERE id=$1`, f.activity)
			case "request_cancel":
				cancel()
			}
			// Include the deliberate control mutation, exclude uncommitted writer
			// effects. Full public rows must be identical after denial/rollback.
			before := sponsorCompletePublicRows(t, b)
			if err = lock.Commit(b.ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if err == nil {
					t.Fatal("final authority accepted after actual audit INSERT wait", late)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("actual audit wait did not finish")
			}
			if after := sponsorCompletePublicRows(t, b); !reflect.DeepEqual(before, after) {
				t.Fatal("denied final-clock operation changed complete public rows", late)
			}
			var state string
			var revision, audit int
			if err = b.pool.QueryRow(b.ctx, `SELECT status,revision,(SELECT count(*) FROM audit_events WHERE resource_id=$1::text AND action='sponsorship_approved') FROM sponsored_opportunity_declarations WHERE id=$1::uuid`, d.ID).Scan(&state, &revision, &audit); err != nil || state != "pending" || revision != 1 || audit != 0 {
				t.Fatal("rejected final write retained a side effect", state, revision, audit, err)
			}
			t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual_audit_insert_wait=%s final_native_guard=DENIED revision=1 audit=0 all_public_rows=unchanged", late)
		})
	}
}

func newSponsorFixture(t *testing.T, withPlace bool) *sponsorFixture {
	t.Helper()
	b := newBusinessConsoleFixture(t)
	f := &sponsorFixture{b: b, business: b.newBusinessID(t)}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM sponsored_opportunity_declarations WHERE business_id=$1`, `DELETE FROM activities WHERE id=$1`, `DELETE FROM sponsored_opportunity_review_grants WHERE city_id=$1`, `DELETE FROM city_editor_memberships WHERE city_id=$1`, `DELETE FROM business_console_venue_facts WHERE business_id=$1`, `DELETE FROM business_venue_relations WHERE business_id=$1`, `DELETE FROM venues WHERE city_id=$1`, `DELETE FROM venue_candidates WHERE city_id=$1`, `DELETE FROM places WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`, `DELETE FROM organizations WHERE id=$1`, `DELETE FROM accounts WHERE id=$1`} {
			v := f.city
			switch {
			case strings.Contains(q, "business_id"):
				v = f.business
			case strings.Contains(q, "activities"):
				v = f.activity
			case strings.Contains(q, "organizations"):
				v = f.operator
			case strings.Contains(q, "accounts"):
				v = f.principal
			}
			if v != "" {
				if _, e := b.pool.Exec(b.ctx, q, v); e != nil {
					t.Error("sponsor owned cleanup", q, e)
				}
			}
		}
	})
	for _, a := range b.access {
		f.access = append(f.access, so.Access{ActorID: a.ActingPersonID, AccountType: "person", SessionDigest: a.SessionDigest})
	}
	b.claim(t, f.business)
	b.grant(t, f.business)
	if _, e := b.store.ReviewBusinessClaim(b.ctx, b.who(1, f.business), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "合成本地独立商家审核"}); e != nil {
		t.Fatal(e)
	}
	f.city = "sponsor-" + strings.ReplaceAll(f.business, "-", "")
	b.exec(t, `INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'本地合成赞助验收城','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:ADS001','合成维护者')`, f.city)
	b.exec(t, `INSERT INTO city_editor_memberships(city_id,account_id,role) VALUES($1,$2,'reviewer'),($1,$3,'reviewer')`, f.city, b.people[1], b.people[2])
	b.exec(t, `INSERT INTO sponsored_opportunity_review_grants(city_id,reviewer_account_id,permission,state,valid_from,valid_until,provisioned_by,provision_note) VALUES($1,$2,'review_sponsorship','active',clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 hour',$3,'CONTROLLED_LOCAL_SYNTHETIC_PERMISSION_ONLY')`, f.city, b.people[1], b.people[3])
	if withPlace {
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&f.principal); e != nil {
			t.Fatal(e)
		}
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO organizations(account_id,organization_type,name) VALUES($1,'venue','合成场地维护组织') RETURNING id`, f.principal).Scan(&f.operator); e != nil {
			t.Fatal(e)
		}
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),$1,'合成公开球馆','sports','published','LOCAL_SYNTHETIC_FIXTURE','local:ADS001','合成维护者') RETURNING id`, f.city).Scan(&f.place); e != nil {
			t.Fatal(e)
		}
		var candidate string
		if e := b.pool.QueryRow(b.ctx, `INSERT INTO venue_candidates(place_id,city_id,submitted_by,capacity,reservation_support,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at,operator_organization_id) VALUES($1,$2,$3,24,'contact','https://qa.example/venue','原场地的独立合成供给fixture，不是赞助批准',clock_timestamp()+interval '1 hour','approved',$4,clock_timestamp(),$5) RETURNING id`, f.place, f.city, b.people[0], b.people[1], f.operator).Scan(&candidate); e != nil {
			t.Fatal(e)
		}
		b.exec(t, `INSERT INTO venues(place_id,city_id,capacity,reservation_support,source_candidate_id,source_url,reviewed_by,reviewed_at,expires_at,operator_organization_id) VALUES($1,$2,24,'contact',$3,'https://qa.example/venue',$4,clock_timestamp(),clock_timestamp()+interval '1 hour',$5)`, f.place, f.city, candidate, b.people[1], f.operator)
		if _, e := b.store.PutBusinessVenueFacts(b.ctx, b.who(0, f.business), f.place, businessconsole.VenueInput{Facts: businessconsole.VenueFacts{Suitability: []string{"羽毛球"}}, SourceURL: "https://qa.example/business-operation", RightsNote: "明确本地合成经营权", ValidUntil: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}); e != nil {
			t.Fatal(e)
		}
		if _, e := b.store.ReviewBusinessVenueFacts(b.ctx, b.who(1, f.business), f.place, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "独立本地合成经营关系审核"}); e != nil {
			t.Fatal(e)
		}
	}
	var start time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '1 hour'`).Scan(&start); e != nil {
		t.Fatal(e)
	}
	draft, e := b.store.CreateSocialDraft(b.ctx, b.people[0], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "BUSINESS", ID: f.business}, CityID: f.city, PlaceID: f.place, Title: "合成公开羽毛球活动", Summary: "仅本地验收", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Europe/London", Visibility: "public"})
	if e != nil {
		t.Fatal("actual native activity draft", e)
	}
	f.activity = draft.ID
	if _, e = b.store.PublishSocialActivity(b.ctx, b.people[0], draft.ID); e != nil {
		t.Fatal("actual native activity publish", e)
	}
	return f
}
func (f *sponsorFixture) input(t *testing.T, kind string) so.SubmitInput {
	t.Helper()
	m, e := f.b.store.ListOwnSponsoredOpportunities(f.b.ctx, f.access[0], f.business)
	if e != nil {
		t.Fatal(e)
	}
	var selected so.EligibleTarget
	for _, x := range m.Targets {
		if x.Target.Type == kind {
			selected = x
			break
		}
	}
	if selected.SourceSnapshot == "" {
		t.Fatal("actual eligible target missing", kind, m)
	}
	var op string
	var at time.Time
	if e = f.b.pool.QueryRow(f.b.ctx, `SELECT gen_random_uuid(),clock_timestamp()`).Scan(&op, &at); e != nil {
		t.Fatal(e)
	}
	return so.SubmitInput{OperationID: op, CityID: f.city, TargetType: kind, TargetID: selected.Target.ID, Statement: "本地合成商业展示声明，不是真实付款或合同证据", SourceURL: "https://qa.example/sponsorship", RightsNote: "仅本地明确合成批准", ObservedAt: at.Add(-time.Minute).UTC().Truncate(time.Microsecond), ExpiresAt: at.Add(30 * time.Minute).UTC().Truncate(time.Microsecond), SourceSnapshot: selected.SourceSnapshot, Confirmed: true}
}
func (f *sponsorFixture) submit(t *testing.T, kind string) so.Declaration {
	t.Helper()
	d, c, e := f.b.store.SubmitSponsoredOpportunity(f.b.ctx, f.access[0], f.business, f.input(t, kind))
	if e != nil || !c || d.Status != "pending" || d.Revision != 1 {
		t.Fatal(d, c, e)
	}
	return d
}
func (f *sponsorFixture) review(t *testing.T, id string) so.Declaration {
	t.Helper()
	items, e := f.b.store.ListSponsoredOpportunityReview(f.b.ctx, f.access[1], f.city)
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range items {
		if d.ID == id {
			q, e := f.b.store.ReviewSponsoredOpportunity(f.b.ctx, f.access[1], id, so.ReviewInput{ExpectedRevision: d.Revision, Snapshot: d.Snapshot, Decision: "approve", Note: "独立明确审核合成商业声明，不证实付款", Confirmed: true})
			if e != nil {
				t.Fatal(e)
			}
			return q
		}
	}
	t.Fatal("review source missing")
	return so.Declaration{}
}
func (f *sponsorFixture) targets() []so.Target {
	out := []so.Target{{Type: "ACTIVITY", ID: f.activity, Title: "合成公开羽毛球活动"}}
	if f.place != "" {
		out = append(out, so.Target{Type: "PLACE", ID: f.place, Title: "合成公开球馆"})
	}
	return out
}
func TestSponsoredNativeLifecycleAndPersistence(t *testing.T) {
	f := newSponsorFixture(t, true)
	b := f.b
	if d, e := b.store.ReadSponsoredOpportunities(b.ctx, so.Access{}, f.targets()); e != nil || len(d.SponsoredOpportunities) != 0 {
		t.Fatal("Business verification invented sponsor", d, e)
	}
	for _, kind := range []string{"ACTIVITY", "PLACE"} {
		t.Run(kind, func(t *testing.T) {
			in := f.input(t, kind)
			d, created, e := b.store.SubmitSponsoredOpportunity(b.ctx, f.access[0], f.business, in)
			if e != nil || !created {
				t.Fatal(d, e)
			}
			repeat, created, e := b.store.SubmitSponsoredOpportunity(b.ctx, f.access[0], f.business, in)
			if e != nil || created || !reflect.DeepEqual(d, repeat) {
				t.Fatal("idempotent submit", repeat, e)
			}
			in.Statement = "冲突的另一声明"
			if _, _, e = b.store.SubmitSponsoredOpportunity(b.ctx, f.access[0], f.business, in); !errors.Is(e, so.ErrConflict) {
				t.Fatal("operation conflict accepted", e)
			}
			q := f.review(t, d.ID)
			if q.Revision != 2 || q.Status != "approved" {
				t.Fatal(q)
			}
			restarted := New(b.pool, false)
			out, e := restarted.ReadSponsoredOpportunities(b.ctx, f.access[0], f.targets())
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, p := range out.SponsoredOpportunities {
				if p.ID == q.ID {
					found = true
					if p.Label != "赞助" || p.Kind != "SPONSORED" || p.Target.Type != kind {
						t.Fatal(p)
					}
					raw, _ := json.Marshal(p)
					if strings.Contains(string(raw), "rightsNote") || strings.Contains(string(raw), "reviewedBy") {
						t.Fatal("private review leak")
					}
				}
			}
			if !found {
				t.Fatal("native persistent sponsor missing", out)
			}
			m, e := b.store.ListOwnSponsoredOpportunities(b.ctx, f.access[0], f.business)
			if e != nil {
				t.Fatal(e)
			}
			for _, x := range m.Declarations {
				if x.ID == q.ID {
					q = x
				}
			}
			inR := so.ReviewInput{ExpectedRevision: q.Revision, Snapshot: q.Snapshot, Decision: "revoke", Note: "本人撤回合成展示", Confirmed: true}
			q, e = b.store.RevokeSponsoredOpportunity(b.ctx, f.access[0], q.ID, inR)
			if e != nil || q.Status != "revoked" || q.Revision != 3 {
				t.Fatal(q, e)
			}
			if q, e = b.store.RevokeSponsoredOpportunity(b.ctx, f.access[0], q.ID, inR); e != nil || q.Revision != 3 {
				t.Fatal("duplicate revoke", q, e)
			}
			out, e = b.store.ReadSponsoredOpportunities(b.ctx, so.Access{}, f.targets())
			if e != nil {
				t.Fatal(e)
			}
			for _, p := range out.SponsoredOpportunities {
				if p.ID == q.ID {
					t.Fatal("revoked sponsor displayed")
				}
			}
		})
	}
}
func TestSponsoredNativeAuthorityBoundaries(t *testing.T) {
	f := newSponsorFixture(t, false)
	b := f.b
	in := f.input(t, "ACTIVITY")
	for _, who := range []int{1, 2, 3} {
		t.Run("nonmanager"+string(rune('0'+who)), func(t *testing.T) {
			if _, _, e := b.store.SubmitSponsoredOpportunity(b.ctx, f.access[who], f.business, in); !errors.Is(e, so.ErrForbidden) {
				t.Fatal(e)
			}
		})
	}
	for _, kind := range []string{"organization", "business"} {
		t.Run(kind, func(t *testing.T) {
			a := f.access[0]
			a.AccountType = kind
			if _, _, e := b.store.SubmitSponsoredOpportunity(b.ctx, a, f.business, in); !errors.Is(e, so.ErrForbidden) {
				t.Fatal(e)
			}
		})
	}
	d := f.submit(t, "ACTIVITY")
	r := so.ReviewInput{ExpectedRevision: 1, Snapshot: d.Snapshot, Decision: "approve", Note: "必须有独立许可", Confirmed: true}
	for _, who := range []int{0, 2, 3} {
		t.Run("reviewdenied"+string(rune('0'+who)), func(t *testing.T) {
			if _, e := b.store.ReviewSponsoredOpportunity(b.ctx, f.access[who], d.ID, r); !errors.Is(e, so.ErrForbidden) {
				t.Fatal(e)
			}
		})
	}
	if _, e := b.store.ListSponsoredOpportunityReview(b.ctx, f.access[2], f.city); !errors.Is(e, so.ErrForbidden) {
		t.Fatal("city reviewer borrowed 070 permission", e)
	}
	if _, e := b.store.ListOwnSponsoredOpportunities(b.ctx, f.access[3], f.business); !errors.Is(e, so.ErrForbidden) {
		t.Fatal("private rights leak", e)
	}
	bad := f.access[0]
	bad.SessionDigest = [32]byte{1}
	if _, e := b.store.ReadSponsoredOpportunities(b.ctx, bad, f.targets()); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("invalid signed session downgraded", e)
	}
}
func TestSponsoredNativeSourceAndABABoundaries(t *testing.T) {
	cases := map[string][2]string{
		"activity cancel":          {`UPDATE activities SET cancelled_at=clock_timestamp() WHERE id=$1`, `UPDATE activities SET cancelled_at=NULL WHERE id=$1`},
		"activity reschedule":      {`UPDATE activities SET starts_at=starts_at+interval '1 minute' WHERE id=$1`, ``},
		"activity hidden":          {`UPDATE activities SET publication_status='hidden' WHERE id=$1`, `UPDATE activities SET publication_status='published' WHERE id=$1`},
		"activity private":         {`UPDATE activities SET visibility='private' WHERE id=$1`, `UPDATE activities SET visibility='public' WHERE id=$1`},
		"submitter suspended":      {`UPDATE accounts SET status='suspended' WHERE id=$1`, `UPDATE accounts SET status='active' WHERE id=$1`},
		"membership removed":       {`UPDATE business_memberships SET status='removed' WHERE business_id=$1 AND role='owner'`, `UPDATE business_memberships SET status='active' WHERE business_id=$1 AND role='owner'`},
		"review grant revoked":     {`UPDATE sponsored_opportunity_review_grants SET state='revoked' WHERE city_id=$1`, `UPDATE sponsored_opportunity_review_grants SET state='active' WHERE city_id=$1`},
		"reviewer city removed":    {`UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1`, `UPDATE city_editor_memberships SET state='active' WHERE city_id=$1`},
		"city hidden":              {`UPDATE cities SET publication_status='hidden' WHERE id=$1`, `UPDATE cities SET publication_status='published' WHERE id=$1`},
		"venue operator suspended": {`UPDATE organizations SET status='suspended' WHERE id=$1`, `UPDATE organizations SET status='active' WHERE id=$1`},
	}
	for name, sql := range cases {
		t.Run(name, func(t *testing.T) {
			f := newSponsorFixture(t, name == "venue operator suspended")
			d := f.review(t, f.submit(t, "ACTIVITY").ID)
			before, e := f.b.store.ReadSponsoredOpportunities(f.b.ctx, so.Access{}, f.targets())
			if e != nil || len(before.SponsoredOpportunities) != 1 {
				t.Fatal(before, e)
			}
			id := f.activity
			switch name {
			case "submitter suspended":
				id = f.b.people[0]
			case "membership removed":
				id = f.business
			case "review grant revoked", "reviewer city removed", "city hidden":
				id = f.city
			case "venue operator suspended":
				id = f.operator
			}
			f.b.exec(t, sql[0], id)
			out, e := f.b.store.ReadSponsoredOpportunities(f.b.ctx, so.Access{}, f.targets())
			if e != nil || len(out.SponsoredOpportunities) != 0 {
				t.Fatal("stale source displayed", name, out, e)
			}
			if sql[1] != "" {
				f.b.exec(t, sql[1], id)
				out, e = f.b.store.ReadSponsoredOpportunities(f.b.ctx, so.Access{}, f.targets())
				if e != nil || len(out.SponsoredOpportunities) != 0 {
					t.Fatal("restore reauthorized old sponsor", name, out, e)
				}
			}
			_ = d
		})
	}
}
func TestSponsoredNativeConcurrentCASAndLateSession(t *testing.T) {
	t.Run("concurrent exact repeat", func(t *testing.T) {
		f := newSponsorFixture(t, false)
		d := f.submit(t, "ACTIVITY")
		items, e := f.b.store.ListSponsoredOpportunityReview(f.b.ctx, f.access[1], f.city)
		if e != nil {
			t.Fatal(e)
		}
		r := so.ReviewInput{ExpectedRevision: 1, Snapshot: items[0].Snapshot, Decision: "approve", Note: "并发同一具体审核", Confirmed: true}
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				x, e := f.b.store.ReviewSponsoredOpportunity(f.b.ctx, f.access[1], d.ID, r)
				if e == nil && x.Revision != 2 {
					e = so.ErrConflict
				}
				errs <- e
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		var n int
		if f.b.pool.QueryRow(f.b.ctx, `SELECT count(*) FROM audit_events WHERE resource_id=$1 AND action='sponsorship_approved'`, d.ID).Scan(&n) != nil || n != 1 {
			t.Fatal("duplicate audit", n)
		}
	})
	for _, late := range []string{"revoke", "expiry", "grant expiry", "source change"} {
		t.Run(late, func(t *testing.T) {
			f := newSponsorFixture(t, false)
			d := f.submit(t, "ACTIVITY")
			items, e := f.b.store.ListSponsoredOpportunityReview(f.b.ctx, f.access[1], f.city)
			if e != nil {
				t.Fatal(e)
			}
			r := so.ReviewInput{ExpectedRevision: 1, Snapshot: items[0].Snapshot, Decision: "approve", Note: "等待期间必须重新核验", Confirmed: true}
			lock, e := f.b.pool.Begin(f.b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(f.b.ctx)
			if _, e = lock.Exec(f.b.ctx, `SELECT id FROM sponsored_opportunity_declarations WHERE id=$1 FOR UPDATE`, d.ID); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { _, e := f.b.store.ReviewSponsoredOpportunity(f.b.ctx, f.access[1], d.ID, r); done <- e }()
			businessNativeWait(t, f.b, lock.Conn().PgConn().PID())
			switch late {
			case "revoke":
				f.b.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.b.people[1])
			case "expiry":
				f.b.exec(t, `UPDATE sessions SET expires_at=clock_timestamp()+interval '150 milliseconds',idle_expires_at=clock_timestamp()+interval '100 milliseconds' WHERE account_id=$1`, f.b.people[1])
				time.Sleep(200 * time.Millisecond)
			case "grant expiry":
				f.b.exec(t, `UPDATE sponsored_opportunity_review_grants SET valid_until=clock_timestamp() WHERE city_id=$1`, f.city)
			case "source change":
				f.b.exec(t, `UPDATE activities SET starts_at=starts_at+interval '1 minute' WHERE id=$1`, f.activity)
			}
			if e = lock.Commit(f.b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				if e == nil {
					t.Fatal("late authority accepted", late)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native wait stuck")
			}
			var state string
			if e = f.b.pool.QueryRow(f.b.ctx, `SELECT status FROM sponsored_opportunity_declarations WHERE id=$1`, d.ID).Scan(&state); e != nil || state != "pending" {
				t.Fatal("denial wrote", state, e)
			}
		})
	}
}
func TestSponsoredNativeInvalidShapesAndTargets(t *testing.T) {
	f := newSponsorFixture(t, false)
	b := f.b
	in := f.input(t, "ACTIVITY")
	for _, name := range []string{"unknown target", "wrong city", "unrelated target", "wrong source", "future observation", "expired", "unconfirmed"} {
		t.Run(name, func(t *testing.T) {
			q := in
			switch name {
			case "unknown target":
				q.TargetType = "PERSON"
			case "wrong city":
				q.CityID = "aberdeen-gb"
			case "unrelated target":
				q.TargetID = b.people[3]
			case "wrong source":
				q.SourceSnapshot = strings.Repeat("0", 64)
			case "future observation":
				q.ObservedAt = time.Now().Add(time.Hour)
			case "expired":
				q.ExpiresAt = time.Now().Add(-time.Hour)
			case "unconfirmed":
				q.Confirmed = false
			}
			if _, _, e := b.store.SubmitSponsoredOpportunity(b.ctx, f.access[0], f.business, q); e == nil {
				t.Fatal("invalid target accepted")
			}
		})
	}
	d := f.submit(t, "ACTIVITY")
	for _, sql := range []string{`UPDATE sponsored_opportunity_declarations SET revision=revision+1,status='approved' WHERE id=$1`, `UPDATE sponsored_opportunity_declarations SET statement='偷偷改内容',revision=revision+1,status='revoked' WHERE id=$1`, `UPDATE sponsored_opportunity_declarations SET revision=2 WHERE id=$1`} {
		tx, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(b.ctx, sql, d.ID)
		_ = tx.Rollback(context.Background())
		if e == nil {
			t.Fatal("raw shape accepted", sql)
		}
	}
	if _, e := b.store.ReadSponsoredOpportunities(b.ctx, so.Access{}, []so.Target{{Type: "ACTIVITY", ID: f.activity, Title: "错误标题"}}); e != nil {
		t.Fatal(e)
	}
	var count int
	if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM sponsored_opportunity_declarations WHERE business_id=$1`, f.business).Scan(&count) != nil || count != 1 {
		t.Fatal("invalid source wrote", count)
	}
}
