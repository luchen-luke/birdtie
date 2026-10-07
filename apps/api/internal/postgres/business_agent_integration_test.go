package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentbusiness"
	"github.com/birdtie/birdtie/apps/api/internal/businessconsole"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func businessKnowledgeNative(t *testing.T) (*businessConsoleFixture, string) {
	t.Helper()
	f := newBusinessConsoleFixture(t)
	id := f.newBusinessID(t)
	f.claim(t, id)
	f.grant(t, id)
	review := businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "本地合成资料独立审核，不是现实核验"}
	if _, e := f.store.ReviewBusinessClaim(f.ctx, f.who(1, id), review); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.PutBusinessProfile(f.ctx, f.who(0, id), businessProfileNativeInput(now)); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.ReviewBusinessProfile(f.ctx, f.who(1, id), review); e != nil {
		t.Fatal(e)
	}
	return f, id
}
func TestBusinessKnowledgeNativeCurrentOwnerAdmin(t *testing.T) {
	f, id := businessKnowledgeNative(t)
	q := agentbusiness.Query{Query: "营业时间"}
	snap, e := f.store.ReadOwnBusinessKnowledge(f.ctx, f.who(0, id))
	if e != nil || snap.Profile == nil || snap.Profile.Version != 2 || snap.Principal.ID == id {
		t.Fatal(snap, e)
	}
	if _, e = f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{Action: "grant", Role: "admin", TargetPersonID: f.people[2]}); e != nil {
		t.Fatal(e)
	}
	for _, i := range []int{0, 2} {
		a, e := agentbusiness.NewService(f.store).Answer(f.ctx, f.who(i, id), q)
		if e != nil || a.Status != "known" || len(a.Tools) != 0 {
			t.Fatal(a, e)
		}
	}
	for _, i := range []int{1, 3} {
		if _, e := f.store.ReadOwnBusinessKnowledge(f.ctx, f.who(i, id)); !errors.Is(e, businessconsole.ErrForbidden) {
			t.Fatal("reviewer/outsider got admin knowledge", e)
		}
	}
	// A real member is not an admin and never inherits inference authority.
	if _, e = f.store.ChangeBusinessMember(f.ctx, f.who(0, id), businessconsole.MemberInput{ExpectedVersion: 1, Action: "grant", Role: "member", TargetPersonID: f.people[3]}); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.ReadOwnBusinessKnowledge(f.ctx, f.who(3, id)); !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal(e)
	}
	var before, after int
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1`, id).Scan(&before)
	for range 100 {
		if _, e = agentbusiness.NewService(f.store).Answer(f.ctx, f.who(0, id), q); e != nil {
			t.Fatal(e)
		}
	}
	f.pool.QueryRow(f.ctx, `SELECT count(*) FROM business_console_audit_events WHERE business_id=$1`, id).Scan(&after)
	if before != after {
		t.Fatal("read wrote audit", before, after)
	}
	var count int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, snap.Principal.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("read activated Business Agent", count, e)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, e = f.store.ReadOwnBusinessKnowledge(ctx, f.who(0, id)); e == nil {
		t.Fatal("cancel returned source")
	}
}
func TestBusinessKnowledgeNativeSourceVersionsAndUnknown(t *testing.T) {
	f, id := businessKnowledgeNative(t)
	a := f.who(0, id)
	old, e := f.store.ReadOwnBusinessKnowledge(f.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	var now time.Time
	f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now)
	in := businessProfileNativeInput(now)
	in.ExpectedVersion = 2
	in.Facts.Description = "待审核的新秘密材料"
	if _, e = f.store.PutBusinessProfile(f.ctx, a, in); e != nil {
		t.Fatal(e)
	}
	if e = f.store.ValidateOwnBusinessKnowledge(f.ctx, a, old.SourceVersion); !errors.Is(e, agentbusiness.ErrChanged) {
		t.Fatal("replaced source survived", e)
	}
	answer, e := agentbusiness.NewService(f.store).Answer(f.ctx, a, agentbusiness.Query{Query: "商家介绍"})
	if e != nil || answer.Status != "unknown" || len(answer.Sources) != 0 {
		t.Fatal(answer, e)
	}
	if _, e = f.store.ReviewBusinessProfile(f.ctx, f.who(1, id), businessconsole.ReviewInput{ExpectedVersion: 3, Decision: "approve", Note: "重新核验当前版本"}); e != nil {
		t.Fatal(e)
	}
	current, e := f.store.ReadOwnBusinessKnowledge(f.ctx, a)
	if e != nil || current.Profile.Version != 4 || current.SourceVersion == old.SourceVersion {
		t.Fatal(current, e)
	}
	// Exactly current DB time is expired, not an artificial Go clock fixture.
	f.exec(t, `UPDATE business_console_profiles SET version=version+1,valid_until=clock_timestamp() WHERE business_id=$1`, id)
	answer, e = agentbusiness.NewService(f.store).Answer(f.ctx, a, agentbusiness.Query{Query: "商家介绍"})
	if e != nil || answer.Status != "unknown" {
		t.Fatal(answer, e)
	}
	f.exec(t, `UPDATE businesses SET claim_status='revoked' WHERE id=$1`, id)
	if _, e = f.store.ReadOwnBusinessKnowledge(f.ctx, a); !errors.Is(e, businessconsole.ErrForbidden) {
		t.Fatal("revoked identity answered", e)
	}
}
func TestBusinessKnowledgeNativeSourceWaitRevocation(t *testing.T) {
	for _, change := range []string{"session", "membership", "source"} {
		t.Run(change, func(t *testing.T) {
			f, id := businessKnowledgeNative(t)
			lock, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(f.ctx)
			if _, e = lock.Exec(f.ctx, `LOCK TABLE business_console_profiles IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			result := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(f.ctx, 6*time.Second)
				defer cancel()
				_, e := f.store.ReadOwnBusinessKnowledge(ctx, f.who(0, id))
				result <- e
			}()
			businessNativeWait(t, f, lock.Conn().PgConn().PID())
			switch change {
			case "session":
				f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.access[0].SessionDigest[:])
			case "membership":
				f.exec(t, `UPDATE business_memberships SET status='removed' WHERE business_id=$1 AND user_account_id=$2`, id, f.people[0])
			case "source":
				if _, e = lock.Exec(f.ctx, `UPDATE business_console_profiles SET version=version+1,state='revoked',review_note='本地撤销当前源' WHERE business_id=$1`, id); e != nil {
					t.Fatal(e)
				}
			}
			if e = lock.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			e = <-result
			if change == "session" && !errors.Is(e, identity.ErrUnauthorized) {
				t.Fatal(e)
			}
			if change == "membership" && !errors.Is(e, businessconsole.ErrForbidden) {
				t.Fatal(e)
			}
			if change == "source" && e != nil {
				t.Fatal("source revoked should be unknown, not leaked/error", e)
			}
		})
	}
}
func TestBusinessKnowledgeNativeFinalSessionNaturalExpiry(t *testing.T) {
	for _, kind := range []string{"idle", "absolute"} {
		t.Run(kind, func(t *testing.T) {
			f, id := businessKnowledgeNative(t)
			a := f.who(0, id)
			snap, e := f.store.ReadOwnBusinessKnowledge(f.ctx, a)
			if e != nil {
				t.Fatal(e)
			}
			var deadline time.Time
			sql := `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',idle_expires_at=stamp.at+interval '250 milliseconds' FROM stamp WHERE token_sha256=$1 RETURNING idle_expires_at`
			if kind == "absolute" {
				sql = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',expires_at=stamp.at+interval '250 milliseconds',idle_expires_at=stamp.at+interval '250 milliseconds' FROM stamp WHERE token_sha256=$1 RETURNING expires_at`
			}
			if e = f.pool.QueryRow(f.ctx, sql, a.SessionDigest[:]).Scan(&deadline); e != nil {
				t.Fatal(e)
			}
			lock, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(f.ctx)
			if _, e = lock.Exec(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.SessionDigest[:]); e != nil {
				t.Fatal(e)
			}
			result := make(chan error, 1)
			go func() {
				ctx, cancel := context.WithTimeout(f.ctx, 6*time.Second)
				defer cancel()
				result <- f.store.ValidateOwnBusinessKnowledge(ctx, a, snap.SourceVersion)
			}()
			businessNativeWait(t, f, lock.Conn().PgConn().PID())
			for {
				var now time.Time
				if e = f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
					t.Fatal(e)
				}
				if !now.Before(deadline) {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if e = lock.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			if e = <-result; !errors.Is(e, identity.ErrUnauthorized) {
				t.Fatal("expired session revived", e)
			}
		})
	}
}

func TestBusinessKnowledgeNativeVenueCurrentOperator(t *testing.T) {
	f, id := businessKnowledgeNative(t)
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
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO organizations(account_id,organization_type,name) VALUES($1,'venue','合成场地运营者') RETURNING id`, principal).Scan(&organization); e != nil {
		t.Fatal(e)
	}
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label) VALUES(gen_random_uuid(),'aberdeen-gb','合成商家专属场地','sports','published','LOCAL_SYNTHETIC_FIXTURE','local:business-knowledge','合成维护者') RETURNING id`).Scan(&place); e != nil {
		t.Fatal(e)
	}
	if e := f.pool.QueryRow(f.ctx, `INSERT INTO venue_candidates(place_id,city_id,submitted_by,capacity,reservation_support,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at,operator_organization_id) VALUES($1,'aberdeen-gb',$2,24,'contact','https://example.invalid/venue','本地明确合成资料和审核，不是现实经营证明',clock_timestamp()+interval '1 hour','approved',$3,clock_timestamp(),$4) RETURNING id`, place, f.people[0], f.people[1], organization).Scan(&candidate); e != nil {
		t.Fatal(e)
	}
	f.exec(t, `INSERT INTO venues(place_id,city_id,capacity,reservation_support,source_candidate_id,source_url,reviewed_by,reviewed_at,expires_at,operator_organization_id) VALUES($1,'aberdeen-gb',24,'contact',$2,'https://example.invalid/venue',$3,clock_timestamp(),clock_timestamp()+interval '1 hour',$4)`, place, candidate, f.people[1], organization)
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	in := businessconsole.VenueInput{Facts: businessconsole.VenueFacts{Suitability: []string{"学习"}, BookingURL: "https://example.invalid/book", Note: "管理员私有备注不得输出"}, SourceURL: "https://example.invalid/operation", RightsNote: "合成经营权声明", ValidUntil: now.UTC().Add(time.Hour)}
	if _, e := f.store.PutBusinessVenueFacts(f.ctx, f.who(0, id), place, in); e != nil {
		t.Fatal(e)
	}
	if _, e := f.store.ReviewBusinessVenueFacts(f.ctx, f.who(1, id), place, businessconsole.ReviewInput{ExpectedVersion: 1, Decision: "approve", Note: "合成开发核验"}); e != nil {
		t.Fatal(e)
	}
	q := agentbusiness.Query{Query: "场地预约链接", PlaceID: place}
	answer, e := agentbusiness.NewService(f.store).Answer(f.ctx, f.who(0, id), q)
	if e != nil || answer.Status != "known" || len(answer.Sources) != 1 || answer.Sources[0].ID != place {
		t.Fatal(answer, e)
	}
	f.exec(t, `UPDATE organizations SET status='suspended' WHERE id=$1`, organization)
	answer, e = agentbusiness.NewService(f.store).Answer(f.ctx, f.who(0, id), q)
	if e != nil || answer.Status != "unknown" || len(answer.Sources) != 0 {
		t.Fatal("inactive Venue operator was quoted", answer, e)
	}
	f.exec(t, `UPDATE organizations SET status='active' WHERE id=$1`, organization)
	f.exec(t, `UPDATE places SET expires_at=clock_timestamp() WHERE id=$1`, place)
	answer, e = agentbusiness.NewService(f.store).Answer(f.ctx, f.who(0, id), q)
	if e != nil || answer.Status != "unknown" {
		t.Fatal("expired Place quoted", answer, e)
	}
}

func TestBusinessKnowledgeNativeFutureReviewAndRoleABA(t *testing.T) {
	f, id := businessKnowledgeNative(t)
	a := f.who(0, id)
	old, e := f.store.ReadOwnBusinessKnowledge(f.ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE business_memberships SET status='removed' WHERE business_id=$1 AND user_account_id=$2`, id, a.ActingPersonID)
	f.exec(t, `UPDATE business_memberships SET status='active' WHERE business_id=$1 AND user_account_id=$2`, id, a.ActingPersonID)
	if e = f.store.ValidateOwnBusinessKnowledge(f.ctx, a, old.SourceVersion); !errors.Is(e, agentbusiness.ErrChanged) {
		t.Fatal("role ABA retained former source token", e)
	}
	for _, timestamp := range []string{"clock_timestamp()+interval '1 day'", "'infinity'::timestamptz"} {
		f.exec(t, `UPDATE business_console_profiles SET version=version+1,reviewed_at=`+timestamp+` WHERE business_id=$1`, id)
		answer, e := agentbusiness.NewService(f.store).Answer(f.ctx, a, agentbusiness.Query{Query: "商家介绍"})
		if e != nil || answer.Status != "unknown" || len(answer.Sources) != 0 {
			t.Fatal("future/unbounded review quoted", answer, e)
		}
	}
}
