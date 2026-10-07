package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	mc "github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func apiMemoryDetail(t *testing.T, f *agentPrivateFixture, id string) agentmemory.DetailProjection {
	t.Helper()
	p, e := f.base.store.ReadOwnMemoryDetail(f.base.ctx, f.owner, id)
	if e != nil || agentmemory.ValidateDetail(p, p.ObservedAt) != nil {
		t.Fatal("native detail", e)
	}
	if _, e = agentmemory.DetailNativeProof(p, f.owner); e != nil {
		t.Fatal("native proof", e)
	}
	return p
}

// Reserved inference is only a synthetic native PENDING_REVIEW shape. It is
// inserted with its original nature, never converted from an EXPLICIT record.
func apiReservedMemory(t *testing.T, f *agentPrivateFixture, key string) agentmemory.Record {
	t.Helper()
	b := f.base
	id := agentMemoryID(t, f)
	b.exec(`INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,confidence,source_type,status,visibility,valid_until) VALUES($1,$2,$3,'PREFERENCE',$4,'合成待审预留形状', '{"shapeOnly":true}',0,'INFERRED','PENDING_REVIEW','AGENT_ONLY',clock_timestamp()+interval '1 day')`, id, b.personID, b.person.ID, key)
	m, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, id))
	if e != nil || agentmemory.ValidateRecord(m) != nil {
		t.Fatal("reserved native shape", e)
	}
	return m
}
func TestMemoryAPINativeDetailStableStatesAndZeroWrites(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"ACTIVE", "PENDING_REVIEW", "EXPIRED", "DELETED"} {
		t.Run(mode, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			var m agentmemory.Record
			if mode == "PENDING_REVIEW" {
				m = apiReservedMemory(t, f, "api.states")
			} else if mode == "EXPIRED" {
				id := agentMemoryID(t, f)
				b.exec(`INSERT INTO agent_memories(id,agent_id,owner_id,memory_type,memory_key,summary,structured_value,valid_from,valid_until) VALUES($1,$2,$3,'PREFERENCE','api.states','合成已到期的本人声明','{"human":true}',clock_timestamp()-interval '2 days',clock_timestamp()-interval '1 day')`, id, b.personID, b.person.ID)
				var e error
				m, e = scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, id))
				if e != nil {
					t.Fatal(e)
				}
			} else {
				m = mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.states"))
			}
			if mode == "DELETED" {
				if _, e := b.store.DeleteOwnMemory(b.ctx, f.owner, m.ID, m.Version); e != nil {
					t.Fatal(e)
				}
			}
			before := correctionPairs(t, b.pool, b.ctx)
			p := apiMemoryDetail(t, f, m.ID)
			if string(p.Target.Status) != mode || p.Target.ID != m.ID || p.Owner.ID != b.person.ID || p.AgentID != b.personID || p.ModelAccess {
				t.Fatal("native state/identity changed")
			}
			if mode == "EXPIRED" || mode == "DELETED" {
				if p.Memory != nil {
					t.Fatal("expired/deleted content")
				}
			} else if p.Memory == nil || p.Memory.Summary != m.Summary {
				t.Fatal("valid native original lost")
			}
			if e := b.store.RevalidateOwnMemoryDetail(b.ctx, f.owner, p); e != nil {
				t.Fatal(e)
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("GET detail/revalidate changed rows or xmin")
			}
		})
	}
}
func TestMemoryAPINativeDetailAuthorityABAAndNoPrivateResult(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"memoryABA", "metadataVersionTwice", "agentABA", "ownerABA", "token", "revoked", "foreign", "org", "guardABA", "changedwire"} {
		t.Run(mode, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			m := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.authority"))
			p := apiMemoryDetail(t, f, m.ID)
			a := f.owner
			switch mode {
			case "memoryABA":
				b.exec(`UPDATE agent_memories SET summary='短暂合成变化',version=version+1 WHERE id=$1`, m.ID)
				b.exec(`UPDATE agent_memories SET summary=$2,version=version+1 WHERE id=$1`, m.ID, m.Summary)
			case "metadataVersionTwice":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
			case "agentABA":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
				b.exec(`UPDATE agents SET status='active' WHERE id=$1`, b.personID)
			case "ownerABA":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID)
			case "token":
				_, d, e := identity.NewToken()
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE sessions SET token_sha256=$2 WHERE id=$1`, f.ownerSession, d[:])
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession)
			case "foreign":
				a = f.peer
			case "org":
				a = f.org
			case "guardABA":
				b.exec(`ALTER TABLE moments DISABLE TRIGGER memory_moment_invalidated`)
				b.exec(`ALTER TABLE moments ENABLE TRIGGER memory_moment_invalidated`)
			case "changedwire":
				p.Memory.Summary = "改变已编码材料"
			}
			before := correctionPairs(t, b.pool, b.ctx)
			if e := b.store.RevalidateOwnMemoryDetail(b.ctx, a, p); e == nil {
				t.Fatal("old sealed detail survived current change")
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("revalidation failure wrote ledgers")
			}
		})
	}
}
func TestMemoryAPINativeDetailSourceChangeRetainsIndependentDeclaration(t *testing.T) {
	ownedMigrationDatabase(t)
	f := memoryEvidenceNativeFixture(t)
	f.attach(t, agentevent.MomentSource)
	a := f.native.base.private
	b := a.base
	p := apiMemoryDetail(t, a, f.memory.ID)
	source := f.sources[agentevent.MomentSource]
	b.exec(`UPDATE moments SET revision=revision+1 WHERE id=$1`, source)
	before := correctionPairs(t, b.pool, b.ctx)
	if e := b.store.RevalidateOwnMemoryDetail(b.ctx, a.owner, p); e == nil {
		t.Fatal("old source proof survived revision")
	}
	current := apiMemoryDetail(t, a, f.memory.ID)
	if current.Memory == nil || current.Memory.Summary != f.memory.Summary || current.Target.Version != f.memory.Version {
		t.Fatal("independent human declaration removed")
	}
	raw, _ := json.Marshal(current)
	if strings.Contains(string(raw), source) {
		t.Fatal("stale source reference disclosed")
	}
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("detail source change wrote or scrubbed native ledgers")
	}
}
func TestMemoryAPINativeSessionABARejectsOriginalDetail(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"tokenABA", "revokeABA"} {
		t.Run(mode, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			m := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.session.aba"))
			p := apiMemoryDetail(t, f, m.ID)
			if mode == "tokenABA" {
				_, d, e := identity.NewToken()
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE sessions SET token_sha256=$2 WHERE id=$1`, f.ownerSession, d[:])
			} else {
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession)
			}
			before := correctionPairs(t, b.pool, b.ctx)
			if e := b.store.RevalidateOwnMemoryDetail(b.ctx, f.owner, p); e == nil || before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("changed Session admitted original detail or wrote rows", e)
			}
			if mode == "tokenABA" {
				b.exec(`UPDATE sessions SET token_sha256=$2 WHERE id=$1`, f.ownerSession, f.owner.SessionDigest[:])
			} else {
				b.exec(`UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.ownerSession)
			}
			before = correctionPairs(t, b.pool, b.ctx)
			e := b.store.RevalidateOwnMemoryDetail(b.ctx, f.owner, p)
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("restored Session verification wrote rows")
			}
			if e == nil {
				t.Fatal("original sealed detail survived actual same-UUID Session ABA")
			}
			current := apiMemoryDetail(t, f, m.ID)
			if current.Memory == nil || current.Memory.Summary != m.Summary {
				t.Fatal("fresh current Session detail unavailable")
			}
		})
	}
}
func TestMemoryAPINativeRejectExplicitReservedExactOnce(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, reserved := range []bool{false, true} {
		t.Run(map[bool]string{false: "EXPLICIT", true: "RESERVED"}[reserved], func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			var m agentmemory.Record
			if reserved {
				m = apiReservedMemory(t, f, "api.reject")
			} else {
				m = mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.reject"))
			}
			p := correctionPreview(t, f, m.ID, m.Version, "REJECT", "", nil)
			in := agentmemory.RejectInput{OperationID: p.ID, PlanDigest: p.PlanDigest}
			r, e := b.store.RejectOwnMemory(b.ctx, f.owner, m.ID, in)
			if e != nil || r.State != "COMMITTED" || r.Action != "REJECT" || r.ResultMemoryID == nil || *r.ResultMemoryID != m.ID || r.ResultMemoryVersion == nil || *r.ResultMemoryVersion != 2 {
				t.Fatal("actual path reject", e)
			}
			current, e := scanAgentMemory(b.pool.QueryRow(b.ctx, `SELECT `+agentMemoryColumns+` FROM agent_memories WHERE id=$1`, m.ID))
			if e != nil || current.Status != agentmemory.StatusDeleted || current.Summary != "" || string(current.StructuredValue) != "{}" || reserved && current.SourceType != agentmemory.SourceInferred {
				t.Fatal("original tombstone semantics", e)
			}
			before := correctionPairs(t, b.pool, b.ctx)
			for i := 0; i < 100; i++ {
				v, e := b.store.RejectOwnMemory(b.ctx, f.owner, m.ID, in)
				if e != nil || !reflect.DeepEqual(v.ResultMemoryVersion, r.ResultMemoryVersion) || v.State != "COMMITTED" {
					t.Fatal("same original operation retry", i, e)
				}
			}
			if before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("committed repeats changed row/xmin/audit")
			}
			var count int
			if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM audit_events WHERE resource_type='agent_memory_correction' AND resource_id=$1`, p.ID).Scan(&count) != nil || count != 1 {
				t.Fatal("audit not once")
			}
		})
	}
}
func TestMemoryAPINativeRejectWrongBindingsZeroConfirm(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, mode := range []string{"path", "digest", "action", "version", "foreign", "newsession", "revoked", "expired", "candidatekind"} {
		t.Run(mode, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			m := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.reject.negative"))
			action := "REJECT"
			if mode == "action" {
				action = "DELETE"
			}
			p := correctionPreview(t, f, m.ID, m.Version, action, "", nil)
			id := m.ID
			in := agentmemory.RejectInput{OperationID: p.ID, PlanDigest: p.PlanDigest}
			a := f.owner
			switch mode {
			case "path":
				id = agentMemoryID(t, f)
			case "digest":
				in.PlanDigest = strings.Repeat("a", 64)
			case "version":
				next := agentMemoryInput("api.reject.negative")
				next.ExpectedVersion = 1
				next.Summary = "本人已改版"
				mustPutAgentMemory(t, f, m.ID, next)
			case "foreign":
				a = f.peer
			case "newsession":
				_, d, e := identity.NewToken()
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, b.person.ID, d[:])
				a.SessionDigest = d
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession)
			case "expired":
				b.exec(`UPDATE sessions SET idle_expires_at=GREATEST(clock_timestamp()+interval '200 milliseconds',created_at+interval '1 second') WHERE id=$1`, f.ownerSession)
				for {
					var expired bool
					if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()>=idle_expires_at FROM sessions WHERE id=$1`, f.ownerSession).Scan(&expired) != nil {
						t.Fatal("owned idle expiry clock")
					}
					if expired {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			case "candidatekind":
				cf, cs := memoryCandidateFixture(t)
				cb := cf.base.private.base
				c := memoryCandidateSave(t, cf, cs)
				cp, e := cb.store.PreviewOwnMemoryCorrection(cb.ctx, cf.base.private.owner, mc.Input{ID: agentMemoryID(t, cf.base.private), TargetKind: "CANDIDATE", TargetID: c.ID, ExpectedVersion: 1, Action: "REJECT"})
				if e != nil {
					t.Fatal(e)
				}
				before := correctionPairs(t, cb.pool, cb.ctx)
				v, e := cb.store.RejectOwnMemory(cb.ctx, cf.base.private.owner, c.ID, agentmemory.RejectInput{OperationID: cp.ID, PlanDigest: cp.PlanDigest})
				if e == nil || !reflect.DeepEqual(v, mc.Receipt{}) || before != correctionPairs(t, cb.pool, cb.ctx) {
					t.Fatal("candidate ID aliased as Memory")
				}
				return
			}
			before := correctionPairs(t, b.pool, b.ctx)
			v, e := b.store.RejectOwnMemory(b.ctx, a, id, in)
			if e == nil || !reflect.DeepEqual(v, mc.Receipt{}) || before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("wrong binding caused confirmation/side effect", mode, e)
			}
		})
	}
}
func TestMemoryAPINativeGuardMissingNoWrites(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, relation := range []string{"agent_memory_corrections", "agent_memory_suppressions", "agent_memory_source_invalidations", "agent_memory_candidates", "moments", "activity_participations", "saved_items"} {
		t.Run(relation, func(t *testing.T) {
			f := agentMemoryTestFixture(t)
			b := f.base
			m := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.guard"))
			p := correctionPreview(t, f, m.ID, 1, "REJECT", "", nil)
			b.exec(`ALTER TABLE ` + relation + ` DISABLE TRIGGER USER`)
			defer b.exec(`ALTER TABLE ` + relation + ` ENABLE TRIGGER USER`)
			before := correctionPairs(t, b.pool, b.ctx)
			v, e := b.store.ReadOwnMemoryDetail(b.ctx, f.owner, m.ID)
			if !errors.Is(e, agentmemory.ErrUnavailable) || !reflect.DeepEqual(v, agentmemory.DetailProjection{}) {
				t.Fatal("detail missing guard", e)
			}
			r, e := b.store.RejectOwnMemory(b.ctx, f.owner, m.ID, agentmemory.RejectInput{OperationID: p.ID, PlanDigest: p.PlanDigest})
			if !errors.Is(e, agentmemory.ErrUnavailable) || !reflect.DeepEqual(r, mc.Receipt{}) || before != correctionPairs(t, b.pool, b.ctx) {
				t.Fatal("reject missing guard", e)
			}
		})
	}
}
func TestMemoryAPINativeRealRelationWaitAcrossOriginalTTL(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	in := agentMemoryInput("api.actual.wait")
	var at time.Time
	if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
		t.Fatal("clock")
	}
	in.ValidUntil = at.Add(3 * time.Second)
	m := mustPutAgentMemory(t, f, agentMemoryID(t, f), in)
	p := apiMemoryDetail(t, f, m.ID)
	before := correctionPairs(t, b.pool, b.ctx)
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	var pid int
	if held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&pid) != nil {
		t.Fatal("pid")
	}
	if _, e = held.Exec(b.ctx, `LOCK TABLE agent_memories IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- b.store.RevalidateOwnMemoryDetail(b.ctx, f.owner, p) }()
	deadline := time.Now().Add(2 * time.Second)
	observed := false
	for !observed {
		if b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%FROM agent_memories%' AND query NOT LIKE '%pg_stat_activity%')`, pid).Scan(&observed) != nil {
			t.Fatal("waiter")
		}
		if time.Now().After(deadline) {
			t.Fatal("actual specific waiter absent")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Log("MEMORY_API actual specific owned relation waiter observed")
	for {
		if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
			t.Fatal("clock")
		}
		if at.After(m.ValidUntil.Add(50 * time.Millisecond)) {
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
			t.Fatal("old detail survived wait across TTL")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not complete")
	}
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("wait changed native ledger")
	}
	current := apiMemoryDetail(t, f, m.ID)
	if current.Target.Status != agentmemory.StatusExpired || current.Memory != nil {
		t.Fatal("late metadata exposed expired body")
	}
}
func TestMemoryAPINativeShortestLeaseAndExpiredRejectZeroWrites(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	in := agentMemoryInput("api.original.deadline")
	var at time.Time
	if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
		t.Fatal("native clock")
	}
	in.ValidUntil = at.Add(2 * time.Second)
	m := mustPutAgentMemory(t, f, agentMemoryID(t, f), in)
	detail := apiMemoryDetail(t, f, m.ID)
	if !detail.ExpiresAt.Equal(m.ValidUntil) {
		t.Fatal("detail extended original Memory TTL")
	}
	p := correctionPreview(t, f, m.ID, m.Version, "REJECT", "", nil)
	for {
		if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
			t.Fatal("native clock")
		}
		if at.After(p.ExpiresAt) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	before := correctionPairs(t, b.pool, b.ctx)
	r, e := b.store.RejectOwnMemory(b.ctx, f.owner, m.ID, agentmemory.RejectInput{OperationID: p.ID, PlanDigest: p.PlanDigest})
	if !errors.Is(e, agentmemory.ErrConflict) || !reflect.DeepEqual(r, mc.Receipt{}) || before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("expired original approval confirmed or scrubbed native rows", e)
	}
	long := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.session.deadline"))
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '10 seconds' WHERE id=$1`, f.ownerSession)
	var idle time.Time
	if b.pool.QueryRow(b.ctx, `SELECT idle_expires_at FROM sessions WHERE id=$1`, f.ownerSession).Scan(&idle) != nil {
		t.Fatal("current session deadline")
	}
	before = correctionPairs(t, b.pool, b.ctx)
	detail = apiMemoryDetail(t, f, long.ID)
	if !detail.ExpiresAt.Equal(idle) || before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("detail extended current Session or wrote rows")
	}
}
func TestMemoryAPINativeRealRelationWaitAcrossSessionDeadline(t *testing.T) {
	ownedMigrationDatabase(t)
	f := agentMemoryTestFixture(t)
	b := f.base
	m := mustPutAgentMemory(t, f, agentMemoryID(t, f), agentMemoryInput("api.session.actual.wait"))
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '3 seconds' WHERE id=$1`, f.ownerSession)
	var end time.Time
	if b.pool.QueryRow(b.ctx, `SELECT idle_expires_at FROM sessions WHERE id=$1`, f.ownerSession).Scan(&end) != nil {
		t.Fatal("original Session deadline")
	}
	p := apiMemoryDetail(t, f, m.ID)
	if !p.ExpiresAt.Equal(end) {
		t.Fatal("read did not retain original shortest Session deadline")
	}
	before := correctionPairs(t, b.pool, b.ctx)
	held, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer held.Rollback(context.Background())
	var pid int
	if held.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&pid) != nil {
		t.Fatal("owned blocker pid")
	}
	if _, e = held.Exec(b.ctx, `LOCK TABLE agent_memories IN ACCESS EXCLUSIVE MODE`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- b.store.RevalidateOwnMemoryDetail(b.ctx, f.owner, p) }()
	deadline := time.Now().Add(2 * time.Second)
	observed := false
	for !observed {
		if b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%FROM agent_memories%' AND query NOT LIKE '%pg_stat_activity%')`, pid).Scan(&observed) != nil {
			t.Fatal("owned waiter query")
		}
		if time.Now().After(deadline) {
			t.Fatal("actual specific Session-bound relation waiter absent")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Log("MEMORY_API actual specific Session-bound owned relation waiter observed")
	for {
		var at time.Time
		if b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&at) != nil {
			t.Fatal("PG clock")
		}
		if at.After(end.Add(50 * time.Millisecond)) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if e = held.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, agentmemory.ErrForbidden) {
			t.Fatal("old proof survived original Session deadline", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owned waiter did not complete")
	}
	if before != correctionPairs(t, b.pool, b.ctx) {
		t.Fatal("expired Session read changed native rows/xmin")
	}
}
func TestMemoryAPINativeNilAndUnissuedFailClosed(t *testing.T) {
	for _, s := range []*Store{nil, {}} {
		v, e := s.ReadOwnMemoryDetail(context.Background(), agentprofile.PrivateAccess{}, "11111111-1111-4111-8111-111111111111")
		if e == nil || !reflect.DeepEqual(v, agentmemory.DetailProjection{}) {
			t.Fatal("nil authority")
		}
		if s.RevalidateOwnMemoryDetail(context.Background(), agentprofile.PrivateAccess{}, agentmemory.DetailProjection{}) == nil {
			t.Fatal("unissued accepted")
		}
	}
}
