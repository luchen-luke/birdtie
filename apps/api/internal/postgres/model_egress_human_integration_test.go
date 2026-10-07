package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"reflect"
	"testing"
	"time"
)

func TestModelEgressHumanNativeBoundedHistoryStillReadsOriginalID(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	a := f.f.native.access
	first := f.preview(t, f.f.native.task.ID, false)
	for range 50 {
		f.preview(t, f.f.native.task.ID, false)
	}
	items, e := b.store.ListOwnModelEgressReceipts(b.ctx, a)
	if e != nil || len(items) != 50 {
		t.Fatal("bounded recent receipts", e, len(items))
	}
	for _, item := range items {
		if item.PreviewID == first.ID {
			t.Fatal("old first unexpectedly in latest 50")
		}
	}
	r, e := b.store.ReadOwnModelEgressReceipt(b.ctx, a, first.ID)
	if e != nil || r.PreviewID != first.ID || !r.Reviewable {
		t.Fatal("old original ID cannot reconcile", e, r)
	}
}
func TestModelEgressHumanNativeMissingPriceCannotCreateDefaults(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	a := f.f.native.access
	b.exec(`DELETE FROM model_local_price_versions WHERE version=$1`, f.price.Version)
	opts, e := b.store.ListOwnModelEgressOptions(b.ctx, a)
	if e != nil || len(opts.Options) != 0 {
		t.Fatal("no price became free default", e, opts)
	}
	var count int
	b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_local_price_versions WHERE version=$1`, f.price.Version).Scan(&count)
	if count != 0 {
		t.Fatal("GET registered price")
	}
}
func TestModelEgressHumanNativeRealOwnerWaitExpiresCurrentSession(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	a := f.f.native.access
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(b.ctx, `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.model-budget.owner:'||$1,0))`, b.person.ID); e != nil {
		t.Fatal(e)
	}
	b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET expires_at=n.at+interval '450 milliseconds',idle_expires_at=n.at+interval '450 milliseconds' FROM n WHERE token_sha256=$1`, a.SessionDigest[:])
	done := make(chan error, 1)
	go func() { _, err := b.store.ListOwnModelEgressOptions(b.ctx, a); done <- err }()
	reached := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%pg_advisory_xact_lock%')`, int(blocker.Conn().PgConn().PID())).Scan(&reached); e != nil {
			t.Fatal(e)
		}
		if reached {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !reached {
		t.Fatal("did not reach actual owner advisory wait")
	}
	time.Sleep(550 * time.Millisecond)
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case err := <-done:
		if !errors.Is(err, modelegressbudget.ErrDenied) {
			t.Fatal("expired wait released choices", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("read wait did not finish")
	}
}
func TestModelEgressHumanNativeReceiptRelationWaitKeepsExpiredMetadataOnly(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	a := f.f.native.access
	p, e := b.store.PreviewOwnModelEgress(b.ctx, a, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, PriceVersion: f.price.Version, MaxOutputTokens: 64, DeadlineAt: time.Now().UTC().Add(400 * time.Millisecond)})
	if e != nil {
		t.Fatal(e)
	}
	blocker, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	if _, e = blocker.Exec(b.ctx, `LOCK TABLE model_egress_previews IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	type answer struct {
		r modelegressbudget.HumanReceipt
		e error
	}
	done := make(chan answer, 1)
	go func() { r, err := b.store.ReadOwnModelEgressReceipt(b.ctx, a, p.ID); done <- answer{r, err} }()
	reached := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%LOCK TABLE%')`, int(blocker.Conn().PgConn().PID())).Scan(&reached); e != nil {
			t.Fatal(e)
		}
		if reached {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !reached {
		t.Fatal("did not reach real relation wait")
	}
	time.Sleep(500 * time.Millisecond)
	if e = blocker.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case v := <-done:
		if v.e != nil || v.r.Preview != nil || v.r.Reviewable || v.r.Approvable || !v.r.Revocable || !v.r.ExpiresAt.Equal(p.ExpiresAt) {
			t.Fatal("expired original preview revived after wait", v.e, v.r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("relation wait did not finish")
	}
}

func TestModelEgressHumanNativeOptionsReceiptsAndReadsHaveNoEffects(t *testing.T) {
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	a := f.f.native.access
	opts, e := b.store.ListOwnModelEgressOptions(b.ctx, a)
	if e != nil || len(opts.Options) == 0 || opts.OwnerID != b.person.ID {
		t.Fatal("native existing choices", e, opts)
	}
	c := opts.Options[0]
	if c.RootTraceID != f.root || c.TaskID != f.f.native.task.ID || c.PriceVersion != f.price.Version || c.Evidence != "LOCAL_SYNTHETIC" || opts.ModelAccess != "UNAVAILABLE" || c.TaskQuery != f.f.native.task.Query {
		t.Fatal("false native selector", c)
	}
	p := f.preview(t, f.f.native.task.ID, false)
	before, e := b.store.ReadOwnModelBudget(b.ctx, a, f.root, p.TaskID)
	if e != nil {
		t.Fatal(e)
	}
	var previewCount, auditCount int
	b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM model_egress_previews WHERE owner_id=$1),(SELECT count(*) FROM model_budget_audit WHERE owner_id=$1)`, b.person.ID).Scan(&previewCount, &auditCount)
	for range 2 {
		list, err := b.store.ListOwnModelEgressReceipts(b.ctx, a)
		if err != nil || len(list) != 1 || list[0].PreviewID != p.ID || list[0].Preview != nil {
			t.Fatal("history returned source or wrong native ID", err, list)
		}
		r, err := b.store.ReadOwnModelEgressReceipt(b.ctx, a, p.ID)
		if err != nil || !r.Reviewable || !r.Approvable || !r.Revocable || r.Preview == nil || r.Preview.RequestDigest != p.RequestDigest || !r.ExpiresAt.Equal(p.ExpiresAt) {
			t.Fatal("exact native review", err, r)
		}
	}
	after, e := b.store.ReadOwnModelBudget(b.ctx, a, f.root, p.TaskID)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("GET changed quota", e)
	}
	var pc, ac, rc int
	b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM model_egress_previews WHERE owner_id=$1),(SELECT count(*) FROM model_budget_audit WHERE owner_id=$1),(SELECT count(*) FROM model_budget_reservations WHERE owner_id=$1)`, b.person.ID).Scan(&pc, &ac, &rc)
	if pc != previewCount || ac != auditCount || rc != 0 {
		t.Fatal("GET created data", pc, ac, rc)
	}
	if e = b.store.ApproveOwnModelEgress(b.ctx, a, p.ID, p.RequestDigest); e != nil {
		t.Fatal(e)
	}
	r, e := b.store.ReadOwnModelEgressReceipt(b.ctx, a, p.ID)
	if e != nil || r.Status != "APPROVED" || r.Revision != 2 || r.Approvable || !r.Reviewable {
		t.Fatal("authority receipt", e, r)
	}
	if e = b.store.RevokeOwnModelEgress(b.ctx, a, p.ID); e != nil {
		t.Fatal(e)
	}
	r, e = b.store.ReadOwnModelEgressReceipt(b.ctx, a, p.ID)
	if e != nil || r.Status != "REVOKED" || r.Reviewable || r.Approvable || r.Revocable || r.Preview != nil {
		t.Fatal("revoked receipt resurrected", e, r)
	}
}
func TestModelEgressHumanNativeChangedSourceAndMetadataKeepOnlyHistory(t *testing.T) {
	for _, kind := range []string{"task", "agentABA", "metadataABA"} {
		t.Run(kind, func(t *testing.T) {
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			a := f.f.native.access
			p := f.preview(t, f.f.native.task.ID, false)
			switch kind {
			case "task":
				b.exec(`UPDATE agent_tasks SET query=query||'变更',updated_at=clock_timestamp() WHERE id=$1`, p.TaskID)
			case "agentABA":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "metadataABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1,updated_at=clock_timestamp() WHERE agent_id=$1`, b.personID)
			}
			r, e := b.store.ReadOwnModelEgressReceipt(b.ctx, a, p.ID)
			if e != nil || r.Reviewable || r.Approvable || r.Preview != nil || !r.Revocable {
				t.Fatal("stale source exposed body/grant", e, r)
			}
			opts, e := b.store.ListOwnModelEgressOptions(b.ctx, a)
			if e != nil || len(opts.Options) != 0 {
				t.Fatal("old root revived", e, opts)
			}
			if e = b.store.ApproveOwnModelEgress(b.ctx, a, p.ID, p.RequestDigest); !errors.Is(e, modelegressbudget.ErrDenied) {
				t.Fatal("stale approval", e)
			}
			if e = b.store.RevokeOwnModelEgress(b.ctx, a, p.ID); e != nil {
				t.Fatal("current owner cannot revoke old source", e)
			}
		})
	}
}
func TestModelEgressHumanNativeOriginalSessionCannotTransfer(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	a := f.f.native.access
	p := f.preview(t, f.f.native.task.ID, false)
	// A real replacement session for the same owner has a different digest and ID.
	var digest []byte
	e := b.pool.QueryRow(b.ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) SELECT account_id,sha256(convert_to(gen_random_uuid()::text,'UTF8')),authentication_method,expires_at,idle_expires_at FROM sessions WHERE token_sha256=$1 RETURNING token_sha256`, a.SessionDigest[:]).Scan(&digest)
	if e != nil {
		t.Fatal(e)
	}
	var replacement agentevent.Access
	copy(replacement.SessionDigest[:], digest)
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
	r, e := b.store.ReadOwnModelEgressReceipt(b.ctx, replacement, p.ID)
	if e != nil || r.Preview != nil || r.Approvable || r.Reviewable || !r.Revocable {
		t.Fatal("new session inherited old approval", e, r)
	}
	if e = b.store.ApproveOwnModelEgress(b.ctx, replacement, p.ID, p.RequestDigest); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("transfer approved", e)
	}
	if e = b.store.RevokeOwnModelEgress(b.ctx, replacement, p.ID); e != nil {
		t.Fatal("same current owner cannot revoke", e)
	}
	if _, e = b.store.ReadOwnModelEgressReceipt(b.ctx, a, p.ID); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("revoked session received history", e)
	}
	b.exec(`DELETE FROM sessions WHERE token_sha256=$1`, digest)
}
func TestModelEgressHumanNativeExpiredPreviewMetadataAndNoRenewal(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	a := f.f.native.access
	p, e := b.store.PreviewOwnModelEgress(b.ctx, a, modelegressbudget.PreviewInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, PriceVersion: f.price.Version, MaxOutputTokens: 64, DeadlineAt: time.Now().UTC().Add(150 * time.Millisecond)})
	if e != nil {
		t.Fatal(e)
	}
	time.Sleep(180 * time.Millisecond)
	r, e := b.store.ReadOwnModelEgressReceipt(b.ctx, a, p.ID)
	if e != nil || r.Preview != nil || r.Approvable || r.Reviewable || !r.Revocable || !r.ExpiresAt.Equal(p.ExpiresAt) {
		t.Fatal("expired preview renewed", e, r)
	}
	if e = b.store.RevokeOwnModelEgress(b.ctx, a, p.ID); e != nil {
		t.Fatal(e)
	}
}
func TestModelEgressHumanNativeUnconfiguredEmptyAndForeignDenied(t *testing.T) {
	f := newEgressFixture(t, 3)
	b := f.f.native.private.base
	a := f.f.native.access
	p := f.preview(t, f.f.native.task.ID, false)
	foreign := agentevent.Access{SessionDigest: f.f.native.private.peer.SessionDigest}
	opts, e := b.store.ListOwnModelEgressOptions(b.ctx, foreign)
	if e != nil || len(opts.Options) != 0 {
		t.Fatal("foreign choices", e, opts)
	}
	if _, e = b.store.ReadOwnModelEgressReceipt(b.ctx, foreign, p.ID); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("cross-owner receipt", e)
	}
	if _, e = b.store.ListOwnModelEgressOptions(b.ctx, agentevent.Access{}); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("anonymous read", e)
	}
	b.exec(`DELETE FROM model_local_price_versions WHERE version=$1`, "not-an-existing-price")
	// Delete actual owned roots only after dependent native preview, keeping guards.
	b.exec(`DELETE FROM model_egress_previews WHERE owner_id=$1`, b.person.ID)
	b.exec(`DELETE FROM model_budget_tasks WHERE owner_id=$1`, b.person.ID)
	b.exec(`DELETE FROM model_budget_roots WHERE owner_id=$1`, b.person.ID)
	opts, e = b.store.ListOwnModelEgressOptions(b.ctx, a)
	if e != nil || len(opts.Options) != 0 {
		t.Fatal("missing root became fake config", e, opts)
	}
}
