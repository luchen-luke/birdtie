package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMemoryCorrectionNativeReadRelationWaitTTL(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("root.expiry.probe")
	var observed time.Time
	if err := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
		t.Fatal(err)
	}
	input.ValidUntil = observed.Add(5 * time.Second).Truncate(time.Microsecond)
	created := mustPutAgentMemory(t, f, id, input)
	var before string
	if err := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	held, err := b.pool.Begin(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback(context.Background())
	var locker int
	if err = held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&locker); err != nil {
		t.Fatal(err)
	}
	if _, err = held.Exec(b.ctx, `LOCK TABLE agent_memories IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		records []agentmemory.Record
		err     error
	}
	done := make(chan result, 1)
	go func() { rows, e := b.store.ReadOwnMemories(b.ctx, f.owner); done <- result{rows, e} }()
	until := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		err = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%FROM agent_memories%' AND query NOT LIKE '%pg_stat_activity%')`, locker).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			t.Log("actual specific owned relation waiter observed")
			break
		}
		if time.Now().After(until) {
			t.Fatal("actual relation waiter not observed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		if err = b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&observed); err != nil {
			t.Fatal(err)
		}
		if !observed.Before(created.ValidUntil.Add(100 * time.Millisecond)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err = held.Commit(b.ctx); err != nil {
		t.Fatal(err)
	}
	var got result
	select {
	case got = <-done:
	case <-time.After(8 * time.Second):
		t.Fatal("read did not finish after actual locker release")
	}
	if got.err != nil {
		t.Fatalf("read failed instead of expired projection: %v", got.err)
	}
	var after string
	if err = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, id).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("read mutated native memory row or xmin")
	}
	if len(got.records) != 1 || got.records[0].ID != id || got.records[0].Version != created.Version {
		t.Fatal("read changed original native identity/version")
	}
	if got.records[0].Status != agentmemory.StatusExpired {
		t.Fatalf("EXPIRED required after actual relation wait crossed original TTL; returned status=%s; native row/xmin unchanged", got.records[0].Status)
	}
}

func correctionPreview(t *testing.T, f *agentPrivateFixture, target string, version int64, action, category string, replacement *agentmemory.PutInput) mc.Preview {
	t.Helper()
	p, e := f.base.store.PreviewOwnMemoryCorrection(f.base.ctx, f.owner, mc.Input{ID: agentMemoryID(t, f), TargetKind: "MEMORY", TargetID: target, ExpectedVersion: version, Action: action, Category: category, Replacement: replacement})
	if e != nil || mc.ValidatePreview(p) != nil {
		t.Fatal("native correction preview", e)
	}
	return p
}
func correctionConfirm(t *testing.T, f *agentPrivateFixture, p mc.Preview) mc.Receipt {
	t.Helper()
	r, e := f.base.store.ConfirmOwnMemoryCorrection(f.base.ctx, f.owner, p.ID, mc.ConfirmInput{PlanDigest: p.PlanDigest})
	if e != nil || mc.ValidateReceipt(r) != nil || r.State != "COMMITTED" {
		t.Fatal("native correction confirm", e)
	}
	return r
}
func TestMemoryCorrectionNativeLifecycleExactOnceAndMetadata(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, action := range []string{"EDIT", "DELETE", "REJECT", "REJECT_RESERVED"} {
		t.Run(action, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			id := agentMemoryID(t, f)
			input := agentMemoryInput("correction.personal")
			m := mustPutAgentMemory(t, f, id, input)
			kind := action
			if action == "REJECT_RESERVED" {
				kind = "REJECT"
				id = agentMemoryID(t, f)
				b.exec(`INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,confidence,source_type,status,visibility,valid_until) VALUES($1,$2,$3,'PREFERENCE','reserved.pending','合成待审形状', '{"shapeOnly":true}',0.25,'INFERRED','PENDING_REVIEW','AGENT_ONLY',clock_timestamp()+interval '1 day')`, id, b.personID, b.person.ID)
				m, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, id))
				if e != nil || m.SourceType != agentmemory.SourceInferred {
					t.Fatal(e)
				}
			}
			var replace *agentmemory.PutInput
			if action == "EDIT" {
				input.ExpectedVersion = 1
				input.Summary = "本人明确修订的合成声明"
				replace = &input
			}
			sourcesBefore := agentMemoryOwnedSourceSnapshot(t, f)
			p := correctionPreview(t, f, id, 1, kind, "", replace)
			r := correctionConfirm(t, f, p)
			if r.ResultMemoryID == nil || *r.ResultMemoryID != id || r.ResultMemoryVersion == nil || *r.ResultMemoryVersion != 2 || !r.CurrentResultMatches {
				t.Fatal("wrong actual result")
			}
			if kind == "EDIT" {
				got, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, id))
				if e != nil || got.Summary != input.Summary || got.ID != m.ID {
					t.Fatal(e)
				}
			} else {
				var status, summary, nature string
				var body []byte
				if e := b.pool.QueryRow(b.ctx, `SELECT status,summary,structured_value,source_type FROM agent_memories WHERE id=$1`, id).Scan(&status, &summary, &body, &nature); e != nil || status != "DELETED" || summary != "" || string(body) != "{}" || action == "REJECT_RESERVED" && nature != "INFERRED" {
					t.Fatal("discard did not use original tombstone", e)
				}
			}
			before := correctionPairs(t, b.pool, b.ctx)
			for i := 0; i < 100; i++ {
				retry, e := b.store.ConfirmOwnMemoryCorrection(b.ctx, f.owner, p.ID, mc.ConfirmInput{PlanDigest: p.PlanDigest})
				if e != nil || retry.State != "COMMITTED" || *retry.ResultMemoryVersion != 2 {
					t.Fatal("same UUID once replay", i, e)
				}
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("100 retries changed ledgers/row epochs")
			}
			var proposal string
			var count int
			if b.pool.QueryRow(b.ctx, `SELECT input::text FROM agent_memory_corrections WHERE id=$1`, p.ID).Scan(&proposal) != nil || proposal != "{}" {
				t.Fatal("committed private proposal not scrubbed")
			}
			if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM audit_events WHERE resource_type='agent_memory_correction' AND resource_id=$1`, p.ID).Scan(&count) != nil || count != 1 {
				t.Fatal("correction audit not once")
			}
			if sourcesBefore != agentMemoryOwnedSourceSnapshot(t, f) {
				t.Fatal("correction changed source or Profile")
			}
			_, digest, e := identity.NewToken()
			if e != nil {
				t.Fatal(e)
			}
			b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '1 hour')`, b.person.ID, digest[:])
			next := f.owner
			next.SessionDigest = digest
			afterSession := correctionPairs(t, b.pool, b.ctx)
			if _, e = b.store.ConfirmOwnMemoryCorrection(b.ctx, next, p.ID, mc.ConfirmInput{PlanDigest: p.PlanDigest}); !errors.Is(e, agentmemory.ErrForbidden) {
				t.Fatal("new session restored approval", e)
			}
			meta, e := b.store.ReadOwnMemoryCorrection(b.ctx, next, p.ID)
			if e != nil || meta.State != "COMMITTED" || meta.ResultMemoryID == nil || *meta.ResultMemoryID != id {
				t.Fatal("new session exact metadata", e)
			}
			if afterSession != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("metadata recovery changed business")
			}
			if kind == "EDIT" {
				input.ExpectedVersion = 2
				input.Summary = "后续独立本人声明"
				mustPutAgentMemory(t, f, id, input)
				meta, e = b.store.ReadOwnMemoryCorrection(b.ctx, f.owner, p.ID)
				if e != nil || meta.State != "COMMITTED" || meta.CurrentResultMatches {
					t.Fatal("history confused with current value", e)
				}
			}
		})
	}
}
func TestMemoryCorrectionNativeChangedAuthorityAndTargetZeroWrite(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"target_change", "target_ABA", "metadata_change", "metadata_ABA", "token_rotate", "revoked", "wrong_owner", "org", "wrong_digest", "preview_ID_reuse"} {
		t.Run(mode, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			id := agentMemoryID(t, f)
			input := agentMemoryInput("correction.guard")
			m := mustPutAgentMemory(t, f, id, input)
			p := correctionPreview(t, f, m.ID, m.Version, "DELETE", "", nil)
			a := f.owner
			digest := p.PlanDigest
			switch mode {
			case "target_change", "target_ABA":
				input.ExpectedVersion = 1
				input.Summary = "变更后的合成声明"
				mustPutAgentMemory(t, f, id, input)
				if mode == "target_ABA" {
					input.ExpectedVersion = 2
					input.Summary = m.Summary
					mustPutAgentMemory(t, f, id, input)
				}
			case "metadata_change", "metadata_ABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
				if mode == "metadata_ABA" {
					b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
				}
			case "token_rotate":
				b.exec(`UPDATE sessions SET token_sha256=sha256(convert_to(gen_random_uuid()::text,'UTF8')) WHERE id=$1`, f.ownerSession)
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession)
			case "wrong_owner":
				a = f.peer
			case "org":
				a = f.org
			case "wrong_digest":
				digest = strings.Repeat("b", 64)
			}
			before := correctionPairs(t, b.pool, b.ctx)
			if mode == "preview_ID_reuse" {
				in := p.Input
				in.Action = "REJECT"
				if _, e := b.store.PreviewOwnMemoryCorrection(b.ctx, a, in); e == nil {
					t.Fatal("operation ID changed original plan")
				}
			} else {
				r, e := b.store.ConfirmOwnMemoryCorrection(b.ctx, a, p.ID, mc.ConfirmInput{PlanDigest: digest})
				if e == nil || !reflect.DeepEqual(r, mc.Receipt{}) {
					t.Fatal("changed bound scope returned authority", e)
				}
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("rejected operation changed any public row/xmin")
			}
		})
	}
}
func TestMemoryCorrectionNativeNegativePersistsAcrossSourceAndMemoryEdits(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	a := f.base.private.owner
	c := memoryCandidateSave(t, f, s)
	oldHuman := memoryCandidatePreview(t, f, s, c)
	p, e := b.store.PreviewOwnMemoryCorrection(b.ctx, a, mc.Input{ID: agentMemoryID(t, f.base.private), TargetKind: "CANDIDATE", TargetID: c.ID, ExpectedVersion: c.Version, Action: "NEGATE", Category: c.Category})
	if e != nil {
		t.Fatal(e)
	}
	r, e := b.store.ConfirmOwnMemoryCorrection(b.ctx, a, p.ID, mc.ConfirmInput{PlanDigest: p.PlanDigest})
	if e != nil || !r.SuppressionActive || r.ResultMemoryID == nil {
		t.Fatal("concrete category correction", e)
	}
	if _, e = s.AcceptOwnCandidate(b.ctx, a, oldHuman); e == nil {
		t.Fatal("old specific human approval revived")
	}
	var gotSummary, nature, status string
	var confidence float64
	if e = b.pool.QueryRow(b.ctx, `SELECT summary,source_type,status,confidence FROM agent_memories WHERE id=$1`, r.ResultMemoryID).Scan(&gotSummary, &nature, &status, &confidence); e != nil || gotSummary != mc.NegativeStatement(c.Category) || nature != "EXPLICIT" || status != "ACTIVE" || confidence != 1 {
		t.Fatal("negative declaration is not exact human result", e)
	}
	d := memoryCandidateDraft(t, f)
	rejectSave := func() {
		t.Helper()
		before := correctionPairs(t, b.pool, b.ctx)
		if _, e := s.SaveOwnCandidate(b.ctx, a, agentMemoryID(t, f.base.private), d, "", 0); e == nil {
			t.Fatal("new source proposal bypassed permanent decision")
		}
		if before != correctionPairs(t, b.pool, b.ctx) {
			t.Fatal("suppressed manual producer wrote ledgers")
		}
	}
	rejectSave()
	source := f.sources[agentevent.MomentCreated]
	b.exec(`UPDATE moments SET revision=revision+1 WHERE id=$1`, source)
	var markers int
	if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_source_invalidations WHERE owner_id=$1 AND source_type='MOMENT' AND source_id=$2`, b.person.ID, source).Scan(&markers) != nil || markers < 1 {
		t.Fatal("source mutation omitted retained metadata")
	}
	rejectSave()
	if _, e = b.store.DeleteOwnMemory(b.ctx, a, *r.ResultMemoryID, *r.ResultMemoryVersion); e != nil {
		t.Fatal(e)
	}
	rejectSave()
	meta, e := b.store.ReadOwnMemoryCorrection(b.ctx, a, p.ID)
	if e != nil || !meta.SuppressionActive || meta.CurrentResultMatches {
		t.Fatal("deleted correction restored category or confused current state", e)
	}
	for _, q := range []string{`DELETE FROM agent_memory_suppressions WHERE owner_id=$1`, `UPDATE agent_memory_suppressions SET category='culture' WHERE owner_id=$1`, `DELETE FROM agent_memory_source_invalidations WHERE owner_id=$1`} {
		tx, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = tx.Exec(b.ctx, q, b.person.ID)
		tx.Rollback(b.ctx)
		if e == nil {
			t.Fatal("persistent controls mutable")
		}
	}
}
func TestMemoryCorrectionNativeCandidateRejectAndSourceChange(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"reject", "changed_source", "source_ABA"} {
		t.Run(mode, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			a := f.base.private.owner
			c := memoryCandidateSave(t, f, s)
			p, e := b.store.PreviewOwnMemoryCorrection(b.ctx, a, mc.Input{ID: agentMemoryID(t, f.base.private), TargetKind: "CANDIDATE", TargetID: c.ID, ExpectedVersion: 1, Action: "REJECT"})
			if e != nil {
				t.Fatal(e)
			}
			if mode != "reject" {
				source := f.sources[agentevent.MomentCreated]
				b.exec(`UPDATE moments SET revision=revision+1 WHERE id=$1`, source)
				if mode == "source_ABA" {
					b.exec(`UPDATE moments SET revision=revision+1 WHERE id=$1`, source)
				}
			}
			before := correctionPairs(t, b.pool, b.ctx)
			r, e := b.store.ConfirmOwnMemoryCorrection(b.ctx, a, p.ID, mc.ConfirmInput{PlanDigest: p.PlanDigest})
			if mode == "reject" {
				if e != nil || r.ResultMemoryID != nil || r.State != "COMMITTED" {
					t.Fatal(e)
				}
				got, e := s.ReadOwnCandidate(b.ctx, a, c.ID)
				if e != nil || got.Status != agentmemorycandidate.Rejected || len(got.Sources) != 0 {
					t.Fatal(e)
				}
			} else {
				if e == nil {
					t.Fatal("source changes reused old specific plan")
				}
				if before != correctionPairs(t, b.pool, b.ctx) {
					t.Fatal("source-changed reject leaked a side effect")
				}
			}
		})
	}
}
func TestMemoryCorrectionNativeGuardDisabledZeroWrites(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, relation := range []string{"agent_memory_corrections", "agent_memory_suppressions", "agent_memory_source_invalidations", "agent_memory_candidates", "moments", "activity_participations", "saved_items"} {
		t.Run(relation, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			id := agentMemoryID(t, f)
			m := mustPutAgentMemory(t, f, id, agentMemoryInput("guard.disabled"))
			b.exec(`ALTER TABLE ` + relation + ` DISABLE TRIGGER USER`)
			defer b.exec(`ALTER TABLE ` + relation + ` ENABLE TRIGGER USER`)
			before := correctionPairs(t, b.pool, b.ctx)
			r, e := b.store.PreviewOwnMemoryCorrection(b.ctx, f.owner, mc.Input{ID: agentMemoryID(t, f), TargetKind: "MEMORY", TargetID: id, ExpectedVersion: m.Version, Action: "DELETE"})
			if !errors.Is(e, agentmemory.ErrUnavailable) || !reflect.DeepEqual(r, mc.Preview{}) {
				t.Fatal("missing installed guard did not fail closed", e)
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("guard unavailable wrote domain")
			}
		})
	}
}
func TestMemoryCorrectionNativeDirectFalseReceiptRollback(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	m := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("direct.guard"))
	p := correctionPreview(t, f, m.ID, m.Version, "DELETE", "", nil)
	before := correctionPairs(t, b.pool, b.ctx)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, e = tx.Exec(b.ctx, `UPDATE agent_memory_corrections SET committed_at=clock_timestamp(),result_memory_id=$2,result_memory_version=2,input='{}' WHERE id=$1`, p.ID, m.ID)
	var pe *pgconn.PgError
	if !errors.As(e, &pe) || pe.Code != "23514" {
		t.Fatal("false receipt not rejected by exact actual result guard", e)
	}
	tx.Rollback(b.ctx)
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("false receipt changed full ledgers")
	}
}
func TestMemoryCorrectionNativeBoundaryNilAndCanceled(t *testing.T) {
	for _, s := range []*Store{nil, {}} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r, e := s.ReadOwnMemoryCorrection(ctx, agentprofile.PrivateAccess{}, "01100000-0000-4000-8000-000000000001")
		if !errors.Is(e, agentmemory.ErrUnavailable) || !reflect.DeepEqual(r, mc.Receipt{}) {
			t.Fatal("nil native boundary panicked or returned authority")
		}
	}
}

func TestMemoryCorrectionNativeWaitAcrossOriginalExpiryAndScrub(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	id := agentMemoryID(t, f)
	input := agentMemoryInput("correction.wait")
	var now time.Time
	if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
		t.Fatal("PG clock")
	}
	input.ValidUntil = now.Add(4 * time.Second).Truncate(time.Microsecond)
	m := mustPutAgentMemory(t, f, id, input)
	p := correctionPreview(t, f, m.ID, m.Version, "DELETE", "", nil)
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	var pid int
	if held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&pid) != nil {
		t.Fatal("actual locker")
	}
	if _, e = held.Exec(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, id); e != nil {
		t.Fatal(e)
	}
	before := correctionPairs(t, b.pool, b.ctx)
	done := make(chan error, 1)
	go func() {
		_, e := b.store.ConfirmOwnMemoryCorrection(b.ctx, f.owner, p.ID, mc.ConfirmInput{PlanDigest: p.PlanDigest})
		done <- e
	}()
	waitUntil := time.Now().Add(2 * time.Second)
	for {
		var waiter bool
		if b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%FROM agent_memories%' AND query NOT LIKE '%pg_stat_activity%')`, pid).Scan(&waiter) != nil {
			t.Fatal("actual waiter query")
		}
		if waiter {
			t.Log("actual owned correction memory row waiter observed")
			break
		}
		if time.Now().After(waitUntil) {
			t.Fatal("actual native waiter absent")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for {
		if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now) != nil {
			t.Fatal("late PG clock")
		}
		if !now.Before(p.ExpiresAt) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = held.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("expired specific preview approved after wait")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("native wait did not finish")
	}
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("rejected late confirmation changed ledgers/xmin")
	}
	r, e := b.store.ReadOwnMemoryCorrection(b.ctx, f.owner, p.ID)
	if e != nil || r.State != "EXPIRED" || r.CommittedAt != nil {
		t.Fatal("expired receipt state", e)
	}
	var body string
	if b.pool.QueryRow(b.ctx, `SELECT input::text FROM agent_memory_corrections WHERE id=$1`, p.ID).Scan(&body) != nil || body != "{}" {
		t.Fatal("expired private review not scrubbed")
	}
	if _, e = b.store.ConfirmOwnMemoryCorrection(b.ctx, f.owner, p.ID, mc.ConfirmInput{PlanDigest: p.PlanDigest}); e == nil {
		t.Fatal("scrubbed old approval restored")
	}
}

// Full public rows+xmin are synthetic snapshots. Ordinary go test needs no
// artifact configuration; persistence is only an explicit local evidence opt-in.
func correctionPairs(t *testing.T, pool *pgxpool.Pool, ctx context.Context) string {
	t.Helper()
	pairs := initialCaptureSnapshot(t, pool, ctx, "memory-correction")
	raw, e := json.Marshal(pairs)
	if e != nil {
		t.Fatal(e)
	}
	if dir := os.Getenv("BIRDTIE_MEMORY_CORRECTION_ARTIFACT_DIR"); dir != "" {
		if e = os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
		owned, e := os.MkdirTemp(dir, "pairs-")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(owned, "all-public-pairs.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(owned, "test.txt"), []byte(t.Name()), 0600); e != nil {
			t.Fatal(e)
		}
	}
	return string(raw)
}

func correctionNegative(t *testing.T, f *agentPrivateFixture, category string) mc.Receipt {
	t.Helper()
	in := agentMemoryInput("activity_category:" + category)
	m := mustPutAgentMemory(t, f, agentMemoryID(t, f), in)
	return correctionConfirm(t, f, correctionPreview(t, f, m.ID, m.Version, "NEGATE", category, nil))
}
func TestMemoryCorrectionNativeSingleProducerOldApprovalAndNewSourceBlocked(t *testing.T) {
	ownedMigrationDatabase(t)
	f := pipelineNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	r := correctionNegative(t, f.f.f.place.private, p.Review.Proposal.Category)
	svc := NewCandidatePipeline(b.store, pipelineFlags(t, true))
	blocked := func(gid string) {
		t.Helper()
		before := correctionPairs(t, b.pool, b.ctx)
		v, e := svc.StageOwnMomentCandidate(b.ctx, a, gid)
		if e == nil || v.Committed || v.Candidate != nil {
			t.Fatal("single producer bypassed explicit category correction", e)
		}
		if before != correctionPairs(t, b.pool, b.ctx) {
			t.Fatal("single denied stage changed any public row/xmin")
		}
	}
	blocked(g.ID)
	m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.f.f.place.city, Title: "本人新来源", Body: "羽毛球新合成原生记录", TimePrecision: "unknown", LocationPrecision: "city"})
	if e != nil {
		t.Fatal(e)
	}
	f.f.selection.MomentID = m.ID
	f.f.selection.MomentRevision = m.Revision
	_, ag := f.f.approve(t)
	f.selection.AnalysisGrantID = ag.ID
	_, fresh := f.approve(t)
	if fresh.ID == g.ID {
		t.Fatal("new approval not independent")
	}
	blocked(fresh.ID)
	if row, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, r.ResultMemoryID)); e != nil || row.Status != agentmemory.StatusActive || row.SourceType != agentmemory.SourceExplicit || row.Summary != mc.NegativeStatement(p.Review.Proposal.Category) {
		t.Fatal("blocked producer damaged independent declaration", e)
	}
	b.exec(`ALTER TABLE agent_memory_suppressions DISABLE TRIGGER USER`)
	before := correctionPairs(t, b.pool, b.ctx)
	if _, e = svc.StageOwnMomentCandidate(b.ctx, a, fresh.ID); e == nil {
		t.Fatal("single missing guard allowed stage")
	}
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("missing single guard changed ledgers")
	}
	b.exec(`ALTER TABLE agent_memory_suppressions ENABLE TRIGGER USER`)
}
func TestMemoryCorrectionNativeMultiProducerOldApprovalAndNewSourcesBlocked(t *testing.T) {
	f := multiNative(t)
	b := f.f.f.place.private.base
	a := f.f.f.place.private.owner
	p, g := f.approve(t)
	r := correctionNegative(t, f.f.f.place.private, p.Review.Proposal.Category)
	svc := NewMultiCandidatePipeline(b.store, pipelineFlags(t, true))
	blocked := func(gid string) {
		t.Helper()
		before := correctionPairs(t, b.pool, b.ctx)
		v, e := svc.StageOwnMultiCandidate(b.ctx, a, gid)
		if e == nil || v.Committed || v.Candidate != nil {
			t.Fatal("multi producer bypassed explicit category correction", e)
		}
		if before != correctionPairs(t, b.pool, b.ctx) {
			t.Fatal("multi denied stage changed any public row/xmin")
		}
	}
	blocked(g.ID)
	freshIDs := []string{}
	for i := 0; i < 2; i++ {
		m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: f.f.f.place.city, Title: "新的独立原生来源", Body: "新的羽毛球合成记录", TimePrecision: "unknown", LocationPrecision: "city"})
		if e != nil {
			t.Fatal(e)
		}
		selection := f.f.selection
		selection.MomentID = m.ID
		selection.MomentRevision = m.Revision
		preview, e := b.store.PreviewOwnEnrichmentPurpose(b.ctx, a, selection)
		if e != nil {
			t.Fatal(e)
		}
		grant, e := b.store.ApproveOwnEnrichmentPurpose(b.ctx, a, preview.ID)
		if e != nil {
			t.Fatal(e)
		}
		freshIDs = append(freshIDs, grant.ID)
	}
	f.selection.AnalysisGrantIDs = freshIDs
	_, fresh := f.approve(t)
	blocked(fresh.ID)
	if row, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, r.ResultMemoryID)); e != nil || row.Status != agentmemory.StatusActive || row.SourceType != agentmemory.SourceExplicit {
		t.Fatal("multi changed independent declaration", e)
	}
	b.exec(`ALTER TABLE moments DISABLE TRIGGER memory_moment_invalidated`)
	before := correctionPairs(t, b.pool, b.ctx)
	if _, e := svc.StageOwnMultiCandidate(b.ctx, a, fresh.ID); e == nil {
		t.Fatal("multi missing source guard allowed stage")
	}
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("missing multi source guard changed ledgers")
	}
	b.exec(`ALTER TABLE moments ENABLE TRIGGER memory_moment_invalidated`)
}
func TestMemoryCorrectionNativeMomentDeleteOnlyEvidenceNoGhost(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"native_withdraw", "actual_delete"} {
		t.Run(mode, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			a := f.base.private.owner
			c := memoryCandidateSave(t, f, s)
			old := memoryCandidatePreview(t, f, s, c)
			source := f.sources[agentevent.MomentCreated]
			m := mustPutAgentMemory(t, f.base.private, agentMemoryID(t, f.base.private), agentMemoryInput("independent.human"))
			eid := agentMemoryID(t, f.base.private)
			evidence, e := b.store.PutOwnMemoryEvidence(b.ctx, a, m.ID, eid, agentmemory.EvidenceReferenceInput{ExpectedMemoryVersion: m.Version, SourceType: agentevent.MomentSource, SourceID: source})
			if e != nil || evidence.Source == nil {
				t.Fatal(e)
			}
			reinforce := NewMemoryReinforcementService(b.store, reinforcementFlags(t))
			supportPreview, e := reinforce.PreviewOwnReinforcement(b.ctx, a, m.ID, m.Version, 0, []string{eid})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = reinforce.ApproveOwnReinforcement(b.ctx, a, supportPreview); e != nil {
				t.Fatal(e)
			}
			var nativeMemory string
			if b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, m.ID).Scan(&nativeMemory) != nil {
				t.Fatal("full original Memory row")
			}
			if mode == "native_withdraw" {
				if e = b.store.WithdrawMoment(b.ctx, b.person.ID, source, c.Sources[0].Version.Revision); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e = b.pool.Exec(b.ctx, `DELETE FROM moments WHERE id=$1 AND author_account_id=$2`, source, b.person.ID); e != nil {
					t.Fatal("actual owned source delete", e)
				}
			}
			var markers int
			if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_source_invalidations WHERE owner_id=$1 AND source_type='MOMENT' AND source_id=$2`, b.person.ID, source).Scan(&markers) != nil || markers < 1 {
				t.Fatal("delete did not retain native invalidation")
			}
			before := correctionPairs(t, b.pool, b.ctx)
			if _, e = s.AcceptOwnCandidate(b.ctx, a, old); e == nil {
				t.Fatal("deleted source reused old human acceptance")
			}
			if _, e = reinforce.ApproveOwnReinforcement(b.ctx, a, supportPreview); e == nil {
				t.Fatal("deleted only support reused old approval")
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("late old approval changed full rows/xmin")
			}
			current, e := s.ReadOwnCandidate(b.ctx, a, c.ID)
			if e != nil || current.Status != agentmemorycandidate.Expired || len(current.Sources) != 0 || current.Category != "" {
				t.Fatal("deleted source left consumable ghost proposal", e)
			}
			provenance, e := b.store.ReadOwnMemoryProvenance(b.ctx, a, m.ID)
			if e != nil || len(provenance.Evidence) != 0 {
				t.Fatal("unique deleted evidence survived current read", e)
			}
			support, e := reinforce.ReadOwnReinforcement(b.ctx, a, m.ID, m.Version)
			if e != nil || support.EvidenceCount != 0 || support.LastSupportAt != nil {
				t.Fatal("deleted evidence left current support", e)
			}
			var residue int
			if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_evidence WHERE id=$1 AND(status<>'REMOVED' OR source_id IS NOT NULL OR source_token IS NOT NULL OR source_revision IS NOT NULL OR signal_type IS NOT NULL OR event_time IS NOT NULL OR weight<>0)`, eid).Scan(&residue) != nil || residue != 0 {
				t.Fatal("invalidated unique evidence payload not scrubbed")
			}
			var after string
			if b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text||m.xmin::text FROM agent_memories m WHERE id=$1`, m.ID).Scan(&after) != nil || nativeMemory != after {
				t.Fatal("source removal deleted or rewrote independent human declaration")
			}
			before = correctionPairs(t, b.pool, b.ctx)
			if _, e = s.SaveOwnCandidate(b.ctx, a, agentMemoryID(t, f.base.private), memoryCandidateDraft(t, f), "", 0); e == nil {
				t.Fatal("new candidate ID revived deleted old source")
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("deleted-source replay changed ledgers")
			}
		})
	}
}

func TestMemoryCorrectionNativeNegateReservedInference(t *testing.T) {
	ownedMigrationDatabase(t)
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	a := f.base.private.owner
	d := memoryCandidateDraft(t, f)
	d.Category = "hiking"
	candidate, e := s.SaveOwnCandidate(b.ctx, a, agentMemoryID(t, f.base.private), d, "", 0)
	if e != nil {
		t.Fatal(e)
	}
	oldHuman := memoryCandidatePreview(t, f, s, candidate)
	id := agentMemoryID(t, f.base.private)
	b.exec(`INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,confidence,source_type,status,visibility,valid_until) VALUES($1,$2,$3,'PREFERENCE','activity_category:hiking','待审推断预留形状', '{"activityCategory":"hiking"}',0.25,'INFERRED','PENDING_REVIEW','AGENT_ONLY',clock_timestamp()+interval '1 day')`, id, b.personID, b.person.ID)
	sourceBefore := agentMemoryOwnedSourceSnapshot(t, f.base.private)
	p := correctionPreview(t, f.base.private, id, 1, "NEGATE", "hiking", nil)
	r := correctionConfirm(t, f.base.private, p)
	if r.ResultMemoryID == nil || *r.ResultMemoryID == id || !r.SuppressionActive {
		t.Fatal("negative did not create separate human declaration")
	}
	old, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, id))
	if e != nil || old.Version != 2 || old.Status != agentmemory.StatusDeleted || old.Summary != "" || string(old.StructuredValue) != "{}" || old.SourceType != agentmemory.SourceInferred {
		t.Fatal("reserved inference not retired/scrubbed", e)
	}
	current, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, *r.ResultMemoryID))
	if e != nil || current.SourceType != agentmemory.SourceExplicit || current.Status != agentmemory.StatusActive || current.Summary != mc.NegativeStatement("hiking") || current.Confidence != 1 || current.LastReinforcedAt != nil {
		t.Fatal("new declaration is not explicit human result", e)
	}
	before := correctionPairs(t, b.pool, b.ctx)
	if _, e = s.AcceptOwnCandidate(b.ctx, a, oldHuman); e == nil {
		t.Fatal("old candidate approval revived")
	}
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("rejected old approval changed rows/xmin")
	}
	if sourceBefore != agentMemoryOwnedSourceSnapshot(t, f.base.private) {
		t.Fatal("correction changed original Moment/Profile")
	}
}
