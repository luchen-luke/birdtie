package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	pp "github.com/birdtie/birdtie/apps/api/internal/placeprofile"
)

type placeSemanticFixture struct {
	place                  *placeMemoryFixture
	editor, reviewer, peer pp.Access
	now                    time.Time
}

func semanticFixture(t *testing.T) *placeSemanticFixture {
	t.Helper()
	f := &placeSemanticFixture{place: placeMemoryNativeFixture(t)}
	b := f.place.private.base
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&f.now); e != nil {
		t.Fatal(e)
	}
	var installed bool
	if e := b.pool.QueryRow(b.ctx, `SELECT to_regclass('place_semantic_profiles') IS NOT NULL`).Scan(&installed); e != nil || !installed {
		t.Fatal("fresh067 required", e)
	}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM place_semantic_profiles WHERE city_id=$1`, `DELETE FROM place_semantic_candidates WHERE city_id=$1`, `DELETE FROM city_editor_memberships WHERE city_id=$1`} {
			if _, e := b.pool.Exec(context.Background(), q, f.place.city); e != nil {
				t.Error("owned067 cleanup", e)
			}
		}
	})
	for i, id := range []string{b.person.ID, b.other.ID} {
		_, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, id, digest[:])
		a := pp.Access{ActorID: id, AccountType: "person", SessionDigest: digest}
		if i == 0 {
			f.editor = a
		} else {
			f.reviewer = a
		}
		b.exec(`INSERT INTO city_editor_memberships(city_id,account_id,role,state) VALUES($1,$2,'reviewer','active')`, f.place.city, id)
	}
	f.peer = f.editor
	f.peer.SessionDigest = [32]byte{1}
	return f
}
func (f *placeSemanticFixture) input(t *testing.T) pp.SubmitInput {
	return pp.SubmitInput{OperationID: agentMemoryID(t, f.place.private), Facts: pp.Facts{Vibe: []string{"quiet"}, GoodFor: []string{"badminton"}, GroupSize: &pp.GroupSize{Min: 2, Max: 12}, Reservation: &pp.Reservation{Support: "contact"}}, Source: pp.SourceInput{Label: "合成本地067资料", URL: "https://example.invalid/local-semantic", RightsNote: "仅供隔离测试的明确虚构来源，无真实运营授权", ObservedAt: f.now.Add(-time.Minute), ExpiresAt: f.now.Add(time.Hour)}, Confidence: pp.Assessment{Kind: "EDITOR_ASSESSMENT_UNCALIBRATED", Level: "MEDIUM"}}
}
func (f *placeSemanticFixture) published(t *testing.T) pp.Candidate {
	t.Helper()
	b := f.place.private.base
	in := f.input(t)
	c, created, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, in)
	if e != nil || !created {
		t.Fatal("real submit", created, e)
	}
	c, e = b.store.ReviewPlaceSemanticCandidate(b.ctx, f.reviewer, c.ID, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "独立审核本次合成资料的具体版本"})
	if e != nil {
		t.Fatal("real approve", e)
	}
	return c
}
func TestPlaceSemanticNativeLifecycle(t *testing.T) {
	f := semanticFixture(t)
	b := f.place.private.base
	in := f.input(t)
	c, created, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, in)
	if e != nil || !created || c.Status != "pending" {
		t.Fatal(c, created, e)
	}
	if _, e = b.store.GetPublicPlaceSemanticProfile(b.ctx, f.place.place); !errors.Is(e, pp.ErrNotFound) {
		t.Fatal("pending published", e)
	}
	again, created, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, in)
	if e != nil || created || again.ID != c.ID {
		t.Fatal("idempotency", e, created)
	}
	changed := in
	changed.Confidence.Level = "HIGH"
	if _, _, e = b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, changed); !errors.Is(e, pp.ErrConflict) {
		t.Fatal("operation conflict", e)
	}
	items, e := b.store.ListPlaceSemanticCandidates(b.ctx, f.reviewer, f.place.city)
	if e != nil || len(items) != 1 {
		t.Fatal("review list", items, e)
	}
	_, e = b.store.ReviewPlaceSemanticCandidate(b.ctx, f.editor, c.ID, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "审核自身资料仍必须拒绝执行"})
	if !errors.Is(e, pp.ErrConflict) {
		t.Fatal("self review", e)
	}
	c, e = b.store.ReviewPlaceSemanticCandidate(b.ctx, f.reviewer, c.ID, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "独立审核这份本地合成资料"})
	if e != nil || c.Status != "approved" {
		t.Fatal("review", e)
	}
	p, e := b.store.GetPublicPlaceSemanticProfile(b.ctx, f.place.place)
	if e != nil || p.Version != 1 || p.CheckedAt.IsZero() || pp.ValidatePublic(p, p.CheckedAt) != nil {
		t.Fatal("current native public", p, e)
	}
	for _, secret := range []string{"rightsNote", "submittedBy", "reviewedBy", "latitude", "longitude", "token"} {
		raw, _ := json.Marshal(p)
		if strings.Contains(string(raw), secret) {
			t.Fatal("public private field", secret)
		}
	}
	restarted := New(b.pool, false)
	got, e := restarted.GetPublicPlaceSemanticProfile(b.ctx, f.place.place)
	if e != nil || got.Version != p.Version || !reflect.DeepEqual(got.Facts, p.Facts) {
		t.Fatal("restart", e)
	}
	if _, e = b.store.ReviewPlaceSemanticCandidate(b.ctx, f.reviewer, c.ID, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "重复旧批准不得再增加资料版本"}); !errors.Is(e, pp.ErrConflict) {
		t.Fatal("duplicate approval", e)
	}
	if _, e = b.store.WithdrawPlaceSemanticProfile(b.ctx, f.reviewer, f.place.place, pp.WithdrawInput{ExpectedVersion: 2, Note: "错误旧版本应拒绝撤回资料"}); !errors.Is(e, pp.ErrConflict) {
		t.Fatal("withdraw CAS", e)
	}
	w, e := b.store.WithdrawPlaceSemanticProfile(b.ctx, f.reviewer, f.place.place, pp.WithdrawInput{ExpectedVersion: 1, Note: "本次明确撤回这份公开合成资料"})
	if e != nil || w.Version != 2 || w.State != "withdrawn" {
		t.Fatal("withdraw", w, e)
	}
	if _, e = b.store.GetPublicPlaceSemanticProfile(b.ctx, f.place.place); !errors.Is(e, pp.ErrNotFound) {
		t.Fatal("withdraw still visible", e)
	}
}
func TestPlaceSemanticNativeCurrentOriginGuards(t *testing.T) {
	for _, mode := range []string{"place_hidden", "place_changed", "city_hidden", "city_expired", "submitter_suspended", "editor_revoked", "editor_restored", "account_restored"} {
		t.Run(mode, func(t *testing.T) {
			f := semanticFixture(t)
			b := f.place.private.base
			f.published(t)
			switch mode {
			case "place_hidden":
				b.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, f.place.place)
			case "place_changed":
				b.exec(`UPDATE places SET name='另一当前地点版本' WHERE id=$1`, f.place.place)
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='draft' WHERE id=$1`, f.place.city)
			case "city_expired":
				b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.place.city)
			case "submitter_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.editor.ActorID)
			case "editor_revoked", "editor_restored":
				b.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, f.place.city, f.editor.ActorID)
				if mode == "editor_restored" {
					b.exec(`UPDATE city_editor_memberships SET state='active' WHERE city_id=$1 AND account_id=$2`, f.place.city, f.editor.ActorID)
				}
			case "account_restored":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.editor.ActorID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.editor.ActorID)
			}
			p, e := b.store.GetPublicPlaceSemanticProfile(b.ctx, f.place.place)
			if !errors.Is(e, pp.ErrNotFound) || !reflect.DeepEqual(p, pp.Public{}) {
				t.Fatal("origin resurrected/hidden", mode, p, e)
			}
		})
	}
}
func TestPlaceSemanticNativeIdentityAndReviewGuards(t *testing.T) {
	for _, mode := range []string{"wrong_session", "wrong_type", "anonymous", "revoked", "expired", "nonmember", "contributor_review", "selfreview", "wrong_version", "wrong_city", "future_source"} {
		t.Run(mode, func(t *testing.T) {
			f := semanticFixture(t)
			b := f.place.private.base
			a := f.editor
			in := f.input(t)
			cityID := f.place.city
			switch mode {
			case "wrong_session":
				a.SessionDigest = f.reviewer.SessionDigest
			case "wrong_type":
				a.AccountType = "organization"
			case "anonymous":
				a.SessionDigest = [32]byte{}
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
			case "expired":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',idle_expires_at=clock_timestamp()-interval '1 second' WHERE token_sha256=$1`, a.SessionDigest[:])
			case "nonmember":
				b.exec(`UPDATE city_editor_memberships SET state='revoked' WHERE city_id=$1 AND account_id=$2`, f.place.city, a.ActorID)
			case "contributor_review", "selfreview", "wrong_version":
				c, _, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, a, cityID, f.place.place, in)
				if e != nil {
					t.Fatal(e)
				}
				r := f.reviewer
				version := int64(0)
				if mode == "contributor_review" {
					b.exec(`UPDATE city_editor_memberships SET role='contributor' WHERE city_id=$1 AND account_id=$2`, f.place.city, r.ActorID)
				}
				if mode == "selfreview" {
					r = a
				}
				if mode == "wrong_version" {
					version = 1
				}
				out, e := b.store.ReviewPlaceSemanticCandidate(b.ctx, r, c.ID, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: version, Note: "明确审核这个合成资料版本"})
				if e == nil || !reflect.DeepEqual(out, pp.Candidate{}) {
					t.Fatal("invalid review", out, e)
				}
				return
			case "wrong_city":
				cityID = "not-the-source-city"
			case "future_source":
				in.Source.ObservedAt = f.now.Add(time.Hour)
			}
			c, _, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, a, cityID, f.place.place, in)
			if e == nil || !reflect.DeepEqual(c, pp.Candidate{}) {
				t.Fatal("invalid write", c, e)
			}
		})
	}
}
func TestPlaceSemanticNativeImmutableSourceAndSQLShape(t *testing.T) {
	f := semanticFixture(t)
	b := f.place.private.base
	c := f.published(t)
	if _, e := b.pool.Exec(b.ctx, `UPDATE place_semantic_candidates SET source_label='伪造新的审核来源' WHERE id=$1`, c.ID); e == nil {
		t.Fatal("source mutated")
	}
	for _, raw := range []string{`1`, `null`, `[]`, `{}`, `{"vibe":null,"good_for":null,"price":null,"accessibility":null,"group_size":null,"reservation":null,"suitability":null}`, `{"vibe":["quiet","quiet"],"good_for":null,"price":null,"accessibility":null,"group_size":null,"reservation":null,"suitability":null}`} {
		t.Run(raw, func(t *testing.T) {
			var valid bool
			e := b.pool.QueryRow(b.ctx, `SELECT birdtie_place_semantic_facts_valid($1::jsonb)`, raw).Scan(&valid)
			if e != nil || valid {
				t.Fatal("invalid SQL shape", raw, valid, e)
			}
		})
	}
	if _, e := b.pool.Exec(b.ctx, `UPDATE place_semantic_profiles SET version=version+2 WHERE place_id=$1`, f.place.place); e == nil {
		t.Fatal("SQL CAS bypass")
	}
}

func TestPlaceSemanticNativeActualClockExpiry(t *testing.T) {
	f := semanticFixture(t)
	b := f.place.private.base
	in := f.input(t)
	var at time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		t.Fatal(e)
	}
	in.Source.ExpiresAt = at.Add(1500 * time.Millisecond)
	c, _, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.ReviewPlaceSemanticCandidate(b.ctx, f.reviewer, c.ID, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "明确审核短期限的合成资料"}); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.GetPublicPlaceSemanticProfile(b.ctx, f.place.place); e != nil {
		t.Fatal("live before expiry", e)
	}
	if _, e = b.pool.Exec(b.ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(epoch FROM ($1::timestamptz-clock_timestamp())))+0.02)`, in.Source.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	p, e := b.store.GetPublicPlaceSemanticProfile(b.ctx, f.place.place)
	if !errors.Is(e, pp.ErrNotFound) || !reflect.DeepEqual(p, pp.Public{}) {
		t.Fatal("actual expired source", p, e)
	}
}

func semanticWaitCurrentLock(t *testing.T, f *placeSemanticFixture, fragment string) {
	t.Helper()
	b := f.place.private.base
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		var count int
		e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1`, "%"+fragment+"%").Scan(&count)
		if e != nil {
			t.Fatal(e)
		}
		if count > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("real native lock wait was not observed")
}
func TestPlaceSemanticNativeAccountWaitLateAuthority(t *testing.T) {
	for _, mode := range []string{"session_revoked", "session_expired", "account_suspended", "context_cancelled"} {
		t.Run(mode, func(t *testing.T) {
			f := semanticFixture(t)
			b := f.place.private.base
			in := f.input(t)
			ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
			defer cancel()
			block, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer block.Rollback(context.Background())
			if _, e = block.Exec(b.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, f.editor.ActorID); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				c, _, e := b.store.SubmitPlaceSemanticCandidate(ctx, f.editor, f.place.city, f.place.place, in)
				if e == nil || !reflect.DeepEqual(c, pp.Candidate{}) {
					done <- errors.New("late authorization wrote semantic data")
					return
				}
				done <- nil
			}()
			semanticWaitCurrentLock(t, f, "SELECT id,xmin::text FROM accounts")
			switch mode {
			case "session_revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.editor.SessionDigest[:])
			case "session_expired":
				b.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour',idle_expires_at=clock_timestamp()-interval '1 second' WHERE token_sha256=$1`, f.editor.SessionDigest[:])
			case "account_suspended":
				if _, e = block.Exec(b.ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.editor.ActorID); e != nil {
					t.Fatal(e)
				}
			case "context_cancelled":
				cancel()
			}
			if e = block.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				if e != nil {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("late guard did not finish")
			}
			var count int
			if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM place_semantic_candidates WHERE city_id=$1)+(SELECT count(*) FROM place_semantic_profiles WHERE city_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$2 AND purpose='public_place_semantics')`, f.place.city, f.editor.ActorID).Scan(&count); e != nil || count != 0 {
				t.Fatal("late effects survived", count, e)
			}
		})
	}
}
func TestPlaceSemanticNativePlaceWaitFinalClockRollsBack(t *testing.T) {
	f := semanticFixture(t)
	b := f.place.private.base
	in := f.input(t)
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 second' WHERE token_sha256=$1`, f.editor.SessionDigest[:])
	block, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(b.ctx, `SELECT id FROM places WHERE id=$1 FOR UPDATE`, f.place.place); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		c, _, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, in)
		if e == nil || !reflect.DeepEqual(c, pp.Candidate{}) {
			done <- errors.New("source wait renewed expired session")
			return
		}
		done <- nil
	}()
	semanticWaitCurrentLock(t, f, "SELECT xmin::text FROM places")
	if _, e = block.Exec(b.ctx, `SELECT pg_sleep(1.05)`); e != nil {
		t.Fatal(e)
	}
	if e = block.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e := <-done:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("source final clock did not finish")
	}
	var count int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM place_semantic_candidates WHERE city_id=$1)+(SELECT count(*) FROM audit_events WHERE actor_account_id=$2 AND purpose='public_place_semantics')`, f.place.city, f.editor.ActorID).Scan(&count); e != nil || count != 0 {
		t.Fatal("final guard did not rollback", count, e)
	}
}
func TestPlaceSemanticNativeConcurrentIdempotencyAndPublishCAS(t *testing.T) {
	f := semanticFixture(t)
	b := f.place.private.base
	in := f.input(t)
	type result struct {
		c       pp.Candidate
		created bool
		e       error
	}
	done := make(chan result, 2)
	for n := 0; n < 2; n++ {
		go func() {
			c, new, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, in)
			done <- result{c, new, e}
		}()
	}
	one, two := <-done, <-done
	if one.e != nil || two.e != nil || one.c.ID != two.c.ID || one.created == two.created {
		t.Fatal("concurrent operation", one, two)
	}
	other := in
	other.OperationID = agentMemoryID(t, f.place.private)
	c, _, e := b.store.SubmitPlaceSemanticCandidate(b.ctx, f.editor, f.place.city, f.place.place, other)
	if e != nil {
		t.Fatal(e)
	}
	review := make(chan error, 2)
	for _, id := range []string{c.ID, one.c.ID} {
		go func(id string) {
			_, e := b.store.ReviewPlaceSemanticCandidate(b.ctx, f.reviewer, id, pp.ReviewInput{Decision: "approve", CandidateVersion: 1, ExpectedVersion: 0, Note: "独立审核同一目标的并发来源版本"})
			review <- e
		}(id)
	}
	a, d := <-review, <-review
	if (a == nil) == (d == nil) || (a != nil && !errors.Is(a, pp.ErrConflict)) || (d != nil && !errors.Is(d, pp.ErrConflict)) {
		t.Fatal("publish CAS", a, d)
	}
	p, e := b.store.GetPublicPlaceSemanticProfile(b.ctx, f.place.place)
	if e != nil || p.Version != 1 {
		t.Fatal("concurrent publish", p, e)
	}
}
func TestPlaceSemanticNativeValidPoolNilContext(t *testing.T) {
	f := semanticFixture(t)
	b := f.place.private.base
	in := f.input(t)
	if _, _, e := b.store.SubmitPlaceSemanticCandidate(nil, f.editor, f.place.city, f.place.place, in); !errors.Is(e, pp.ErrUnavailable) {
		t.Fatal(e)
	}
	if _, e := b.store.GetPublicPlaceSemanticProfile(nil, f.place.place); !errors.Is(e, pp.ErrUnavailable) {
		t.Fatal(e)
	}
}
