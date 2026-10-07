package postgres

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

type businessConsoleFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	store      *Store
	people     []string
	access     []businessconsole.Access
	businesses []string
}

func newBusinessConsoleFixture(t *testing.T) *businessConsoleFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("Business console native tests require an explicitly disposable database")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	var installed bool
	if e = pool.QueryRow(ctx, `SELECT to_regclass('public.business_claim_controls') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("Business console requires actual070", e)
	}
	f := &businessConsoleFixture{ctx: ctx, pool: pool, store: New(pool, false)}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		principals := []string{}
		for _, id := range f.businesses {
			var principal string
			if e := pool.QueryRow(cleanup, `SELECT account_id FROM businesses WHERE id=$1`, id).Scan(&principal); e == nil {
				principals = append(principals, principal)
			}
			for _, table := range []string{"business_console_audit_events", "business_console_membership_controls", "business_console_venue_facts", "business_console_profiles", "business_claim_controls", "business_review_grants", "business_venue_relations", "business_memberships"} {
				if _, e := pool.Exec(cleanup, `DELETE FROM `+table+` WHERE business_id=$1`, id); e != nil {
					t.Error("owned Business cleanup", table, e)
				}
			}
			if _, e := pool.Exec(cleanup, `DELETE FROM businesses WHERE id=$1`, id); e != nil {
				t.Error(e)
			}
		}
		ids := append(append([]string{}, f.people...), principals...)
		for _, query := range []string{`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM accounts WHERE id=ANY($1::uuid[])`} {
			if _, e := pool.Exec(cleanup, query, ids); e != nil {
				t.Error("owned Person cleanup", e)
			}
		}
	})
	for range 4 {
		var id string
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		f.people = append(f.people, id)
		_, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, id, digest[:]); e != nil {
			t.Fatal(e)
		}
		f.access = append(f.access, businessconsole.Access{ActingPersonID: id, SessionDigest: digest})
	}
	return f
}
func (f *businessConsoleFixture) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, query, args...); e != nil {
		t.Fatal(e)
	}
}
func (f *businessConsoleFixture) newBusinessID(t *testing.T) string {
	t.Helper()
	var id string
	if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	f.businesses = append(f.businesses, id)
	return id
}
func (f *businessConsoleFixture) who(index int, id string) businessconsole.Access {
	a := f.access[index]
	a.BusinessID = id
	return a
}
func (f *businessConsoleFixture) claim(t *testing.T, id string) businessconsole.Claim {
	t.Helper()
	c, e := f.store.SubmitBusinessClaim(f.ctx, f.who(0, id), businessconsole.ClaimInput{Name: "合成本地商家", Description: "不作真实经营或合作方证据", SourceURL: "https://example.invalid/owned-claim", RightsNote: "本地测试的经营权声明"})
	if e != nil {
		t.Fatal("real native claim", e)
	}
	return c
}
func (f *businessConsoleFixture) grant(t *testing.T, id string) {
	t.Helper()
	f.exec(t, `INSERT INTO business_review_grants(business_id,reviewer_account_id,permissions,valid_from,valid_until,state,provisioned_by,provision_note) VALUES($1,$2,ARRAY['claim','profile','venue'],clock_timestamp()-interval '1 minute',clock_timestamp()+interval '1 hour','active',$3,'isolated trusted-operations permission fixture')`, id, f.people[1], f.people[3])
}
func businessProfileNativeInput(now time.Time) businessconsole.ProfileInput {
	return businessconsole.ProfileInput{Facts: businessconsole.ProfileFacts{Name: "合成商家资料", Description: "仅开发验收", TimeZone: "Europe/London", OpeningHours: []businessconsole.HoursDay{{Day: 1, OpensAt: "09:00", ClosesAt: "17:00"}}, OfficialLinks: []string{"https://example.invalid/merchant"}}, SourceURL: "https://example.invalid/profile", RightsNote: "管理员明确填写的本地来源", ValidUntil: now.Add(time.Hour).UTC().Truncate(time.Microsecond)}
}
func businessNativeWait(t *testing.T, f *businessConsoleFixture, pid uint32) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		var waiting bool
		if e := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)))`, int(pid)).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("actual Business native lock wait not observed")
}

func TestBusinessConsoleNativeClaimAndProfile(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	c := f.claim(t, id)
	if c.State != "pending" || c.Version != 1 {
		t.Fatal(c)
	}
	f.grant(t, id)
	in := businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "独立本地权限角色审核合成资料，不代表真实商家核验"}
	if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(0, id), in); !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal("owner self-review accepted", e)
	}
	c, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), in)
	if e != nil || c.State != "verified" || c.Version != 2 {
		t.Fatal(c, e)
	}
	if replay, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), in); e != nil || replay.Version != 2 {
		t.Fatal("native review replay", replay, e)
	}
	var now time.Time
	if e = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	profile := businessProfileNativeInput(now)
	p, e := f.store.PutBusinessProfile(f.ctx, f.who(0, id), profile)
	if e != nil || p.State != "pending" || p.Version != 1 {
		t.Fatal(p, e)
	}
	if p, e = f.store.PutBusinessProfile(f.ctx, f.who(0, id), profile); e != nil || p.Version != 1 {
		t.Fatal("same draft duplicated", p, e)
	}
	p, e = f.store.ReviewBusinessProfile(f.ctx, f.who(1, id), in)
	if e != nil || p.State != "verified" || p.Version != 2 {
		t.Fatal(p, e)
	}
	profile.ExpectedVersion = 2
	profile.Facts.Description = "下一具体版本的新声明"
	p, e = f.store.PutBusinessProfile(f.ctx, f.who(0, id), profile)
	if e != nil || p.State != "pending" || p.Version != 3 || p.ReviewedBy != nil || p.ReviewedAt != nil {
		t.Fatal("edited facts inherited verification", p, e)
	}
	current, e := f.store.ReadBusinessConsole(f.ctx, f.who(0, id))
	if e != nil || !current.CanManage || !current.CanManageMembers || current.CanReview || current.Profile == nil || current.Profile.State != "pending" {
		t.Fatal(current, e)
	}
	var agents int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=ANY($1::uuid[])`, f.people).Scan(&agents); e != nil || agents != 0 {
		t.Fatal("ordinary human required/created Agent", agents, e)
	}
	f.exec(t, `UPDATE business_review_grants SET permissions=ARRAY['claim'] WHERE business_id=$1`, id)
	reviewer, e := f.store.ReadBusinessConsole(f.ctx, f.who(1, id))
	if e != nil || reviewer.Claim == nil || reviewer.Profile != nil || len(reviewer.Venues) != 0 || len(reviewer.Members) != 0 {
		t.Fatal("review scope leaked materials", reviewer, e)
	}
}
func TestBusinessConsoleNativeOwnerTransferReplay(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{TargetPersonID: f.people[2], Action: "grant", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	in := businessconsole.MemberInput{ExpectedVersion: 1, TargetPersonID: f.people[2], Action: "transfer_owner", Role: "owner"}
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), in); e != nil {
		t.Fatal("initial owner transfer", e)
	}
	c, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), in)
	if e != nil || c.MembershipVersion != 2 {
		t.Fatal("same transfer outcome not resolved", e)
	}
	if _, e = f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 2, TargetPersonID: f.people[3], Action: "grant", Role: "admin"}); !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal("former owner retained owner power", e)
	}
	var owners int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_memberships WHERE business_id=$1 AND role='owner' AND status='active'`, id).Scan(&owners); e != nil || owners != 1 {
		t.Fatal("owner uniqueness/restoration", owners, e)
	}
}

func TestBusinessConsoleNativeRemoveSuspendedPerson(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{TargetPersonID: f.people[2], Action: "grant", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.people[2])
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 1, TargetPersonID: f.people[2], Action: "grant", Role: "member"}); !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal("inactive Person granted authority", e)
	}
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 1, TargetPersonID: f.people[2], Action: "transfer_owner", Role: "owner"}); !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal("inactive Person received ownership", e)
	}
	in := businessconsole.MemberInput{ExpectedVersion: 1, TargetPersonID: f.people[2], Action: "remove"}
	if current, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), in); e != nil || current.MembershipVersion != 2 {
		t.Fatal("active owner cannot revoke inactive Person", current.MembershipVersion, e)
	}
	if current, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), in); e != nil || current.MembershipVersion != 2 {
		t.Fatal("inactive Person removal replay failed", e)
	}
	var count int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1 AND action='member_remove' AND resource_id=$2`, id, f.people[2]).Scan(&count); e != nil || count != 1 {
		t.Fatal("removal must have exactly one durable audit", count, e)
	}
}
func TestBusinessConsoleNativeRemovedSourceCannotApprove(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	f.grant(t, id)
	if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "合成核验"}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{TargetPersonID: f.people[2], Action: "grant", Role: "admin"}); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.PutBusinessProfile(f.ctx, f.who(2, id), businessProfileNativeInput(now)); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 1, TargetPersonID: f.people[2], Action: "remove"}); e != nil {
		t.Fatal(e)
	}
	_, e := f.store.ReviewBusinessProfile(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "旧提交人已撤权，不能批准"})
	if !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal("removed pending-source administrator approved", e)
	}
	var state string
	if e = f.pool.QueryRow(f.ctx, `SELECT state FROM business_console_profiles WHERE business_id=$1`, id).Scan(&state); e != nil || state != "pending" {
		t.Fatal("denial changed facts", state, e)
	}
}
func TestBusinessConsoleNativeListRechecksEarlierGrant(t *testing.T) {
	f := newBusinessConsoleFixture(t)
	first := f.newBusinessID(t)
	second := f.newBusinessID(t)
	ordered := []string{first, second}
	slices.Sort(ordered)
	first, second = ordered[0], ordered[1]
	f.claim(t, first)
	f.claim(t, second)
	f.grant(t, first)
	f.grant(t, second)
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(f.ctx)
	if _, e = lock.Exec(f.ctx, `SELECT business_id FROM business_claim_controls WHERE business_id=$1 FOR UPDATE`, second); e != nil {
		t.Fatal(e)
	}
	type response struct {
		items []businessconsole.Business
		err   error
	}
	done := make(chan response, 1)
	go func() { items, e := f.store.ListBusinessConsoles(f.ctx, f.access[1]); done <- response{items, e} }()
	businessNativeWait(t, f, lock.Conn().PgConn().PID())
	f.exec(t, `UPDATE business_review_grants SET state='revoked' WHERE business_id=$1 AND reviewer_account_id=$2`, first, f.people[1])
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case result := <-done:
		if result.err == nil || len(result.items) != 0 {
			t.Fatal("earlier revoked Business escaped assembled list", result.items, result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("list did not finish")
	}
}

func TestBusinessConsoleNativeVenueOperatorSuspension(t *testing.T) {
	for _, operation := range []string{"submit", "approve", "lifecycle", "source_expired", "claim_revoked", "review_scope"} {
		t.Run(operation, func(t *testing.T) {
			f := newBusinessConsoleFixture(t)
			id := f.newBusinessID(t)
			f.claim(t, id)
			f.grant(t, id)
			if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "本地合成经营权核验"}); e != nil {
				t.Fatal(e)
			}
			var place, candidate, organization, principal string
			t.Cleanup(func() {
				for _, q := range []string{`DELETE FROM business_console_venue_facts WHERE place_id=$1`, `DELETE FROM business_venue_relations WHERE place_id=$1`, `DELETE FROM venues WHERE place_id=$1`, `DELETE FROM venue_candidates WHERE place_id=$1`, `DELETE FROM places WHERE id=$1`} {
					ownedSocialFixtureCleanup(t, f.ctx, f.pool, q, place)
				}
				ownedSocialFixtureCleanup(t, f.ctx, f.pool, `DELETE FROM organizations WHERE id=$1`, organization)
				ownedSocialFixtureCleanup(t, f.ctx, f.pool, `DELETE FROM accounts WHERE id=$1`, principal)
			})
			if e := f.pool.QueryRow(f.ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'organization') RETURNING id`).Scan(&principal); e != nil {
				t.Fatal(e)
			}
			if e := f.pool.QueryRow(f.ctx, `INSERT INTO organizations(account_id,organization_type,name) VALUES($1,'venue','本地合成场地运营组织') RETURNING id`, principal).Scan(&organization); e != nil {
				t.Fatal(e)
			}
			if e := f.pool.QueryRow(f.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),'aberdeen-gb','本地合成商家场地','sports','published','LOCAL_SYNTHETIC_FIXTURE','local:biz-console','合成维护者') RETURNING id`).Scan(&place); e != nil {
				t.Fatal(e)
			}
			if e := f.pool.QueryRow(f.ctx, `INSERT INTO venue_candidates(place_id,city_id,submitted_by,capacity,reservation_support,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at,operator_organization_id) VALUES($1,'aberdeen-gb',$2,24,'contact','https://example.invalid/venue','本地明确合成资料和审核，不是现实经营证明',clock_timestamp()+interval '1 hour','approved',$3,clock_timestamp(),$4) RETURNING id`, place, f.people[0], f.people[1], organization).Scan(&candidate); e != nil {
				t.Fatal(e)
			}
			f.exec(t, `INSERT INTO venues(place_id,city_id,capacity,reservation_support,source_candidate_id,source_url,reviewed_by,reviewed_at,expires_at,operator_organization_id) VALUES($1,'aberdeen-gb',24,'contact',$2,'https://example.invalid/venue',$3,clock_timestamp(),clock_timestamp()+interval '1 hour',$4)`, place, candidate, f.people[1], organization)
			in := businessconsole.VenueInput{Facts: businessconsole.VenueFacts{Suitability: []string{"羽毛球"}, BookingURL: "https://example.invalid/booking", Note: "本地合成场地事实"}, SourceURL: "https://example.invalid/operation", RightsNote: "本地明确经营权声明", ValidUntil: time.Now().UTC().Add(30 * time.Minute).Truncate(time.Microsecond)}
			if _, e := f.store.PutBusinessVenueFacts(f.ctx, f.who(0, id), place, in); e != nil {
				t.Fatal("positive current operator submission", e)
			}
			if operation == "lifecycle" {
				review := businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "仅核验本地合成场地具体版本"}
				if _, e := f.store.ReviewBusinessVenueFacts(f.ctx, f.who(0, id), place, review); !errors.Is(e, businessconsole.ErrForbidden) {
					t.Fatal("owner venue self-review", e)
				}
				v, e := f.store.ReviewBusinessVenueFacts(f.ctx, f.who(1, id), place, review)
				if e != nil || v.Version != 2 || v.State != "verified" || v.OperationStatus != "verified" {
					t.Fatal("venue native approve", v, e)
				}
				v, e = f.store.ReviewBusinessVenueFacts(f.ctx, f.who(1, id), place, review)
				if e != nil || v.Version != 2 {
					t.Fatal("venue exact review replay", v, e)
				}
				in.ExpectedVersion = 2
				in.Facts.Note = "具体下一版本不会继承材料核验"
				v, e = f.store.PutBusinessVenueFacts(f.ctx, f.who(0, id), place, in)
				if e != nil || v.Version != 3 || v.State != "pending" || v.ReviewedBy != nil {
					t.Fatal("venue changed source inherited approval", v, e)
				}
				if _, e = f.store.ReviewBusinessVenueFacts(f.ctx, f.who(1, id), place, review); !errors.Is(e, businessconsole.ErrConflict) {
					t.Fatal("old venue approval accepted", e)
				}
				review.ExpectedVersion = 3
				v, e = f.store.ReviewBusinessVenueFacts(f.ctx, f.who(1, id), place, review)
				if e != nil || v.Version != 4 || v.State != "verified" {
					t.Fatal(v, e)
				}
				review.ExpectedVersion = 4
				review.Decision = "revoke"
				review.Note = "本地合成核验撤回"
				v, e = f.store.ReviewBusinessVenueFacts(f.ctx, f.who(1, id), place, review)
				if e != nil || v.Version != 5 || v.State != "revoked" || v.OperationStatus != "revoked" {
					t.Fatal("revocation left verified operation", v, e)
				}
				var capacity int
				var originalSource string
				if e = f.pool.QueryRow(f.ctx, `SELECT capacity,source_url FROM venues WHERE place_id=$1`, place).Scan(&capacity, &originalSource); e != nil || capacity != 24 || originalSource != "https://example.invalid/venue" {
					t.Fatal("business side facts overwrote public venue", capacity, originalSource, e)
				}
				return
			}
			var auditBefore int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1`, id).Scan(&auditBefore); e != nil {
				t.Fatal(e)
			}
			switch operation {
			case "source_expired":
				f.exec(t, `UPDATE venues SET expires_at=clock_timestamp()-interval '1 second' WHERE place_id=$1`, place)
			case "claim_revoked":
				if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 2, Decision: "revoke", Note: "本地明确撤回经营核验"}); e != nil {
					t.Fatal(e)
				}
				if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1`, id).Scan(&auditBefore); e != nil {
					t.Fatal(e)
				}
			case "review_scope":
				f.exec(t, `UPDATE business_review_grants SET permissions=ARRAY['claim'] WHERE business_id=$1`, id)
			default:
				f.exec(t, `UPDATE organizations SET status='suspended' WHERE id=$1`, organization)
			}
			var e error
			if operation == "submit" {
				in.ExpectedVersion = 1
				in.Facts.Note = "停用后应拒绝的新声明"
				_, e = f.store.PutBusinessVenueFacts(f.ctx, f.who(0, id), place, in)
			} else {
				_, e = f.store.ReviewBusinessVenueFacts(f.ctx, f.who(1, id), place, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "停用后不能批准"})
			}
			if !errors.Is(e, businessconsole.ErrForbidden) {
				t.Fatal("inactive public Venue operator accepted", e)
			}
			var auditAfter int
			var version int64
			var state string
			if e = f.pool.QueryRow(f.ctx, `SELECT version,state FROM business_console_venue_facts WHERE business_id=$1 AND place_id=$2`, id, place).Scan(&version, &state); e != nil || version != 1 || state != "pending" {
				t.Fatal("denial changed facts", version, state, e)
			}
			if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1`, id).Scan(&auditAfter); e != nil || auditAfter != auditBefore {
				t.Fatal("denial added audit", auditAfter, e)
			}
		})
	}
}
