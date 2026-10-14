package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMemoryInvalidationNativeOriginalStoreTwoStages(t *testing.T) {
	ownedMigrationDatabase(t)
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	f.selection.ProfileFields = nil
	f.selection.PlaceIDs = nil
	f.selection.ActivityIDs = nil
	f.selection.RelationshipTieIDs = nil
	f.selection.PolicyFamilies = nil
	old, grant := f.approve(t)
	unbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.f.place.private.owner, f.selection)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := b.store.PutOwnMemory(b.ctx, f.f.place.private.owner, f.memory, agentmemory.PutInput{ExpectedVersion: 1, MemoryType: agentmemory.TypePreference, MemoryKey: "context.explicit.native", Summary: "本人修改的明确声明_NATIVE_MEMORY", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.f.now.Add(time.Hour)})
	if e != nil || saved.Version != 2 {
		t.Fatal("original human Memory writer", e)
	}
	var worker string
	if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&worker); e != nil {
		t.Fatal(e)
	}
	r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, worker, agentoutbox.MemoryHandler)
	if e == agentoutbox.ErrUnavailable && r == (agentoutbox.Record{}) && c == (agentoutbox.Claim{}) {
		var state string
		var attempt int64
		if check := b.pool.QueryRow(b.ctx, `SELECT delivery_state,attempt FROM agent_domain_outbox WHERE subject_id=$1 AND source_id=$2 AND source_revision=1 AND event_type='MEMORY_UPDATED'`, b.person.ID, f.memory).Scan(&state, &attempt); check != nil || state != "INVALIDATED" || attempt != 0 {
			t.Fatal("stale original Memory root did not commit terminal progress", check)
		}
		r, c, e = b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, worker, agentoutbox.MemoryHandler)
	}
	if e != nil || r.Event.Source.ID != f.memory || r.Event.Source.Revision != saved.Version {
		t.Fatal("original Memory Store claim", e)
	}
	p, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil {
		// Real Store failure remains the RED oracle. Diagnostic invokes the same
		// transaction function in a new rollback-only native transaction.
		tx, begin := b.pool.Begin(b.ctx)
		if begin == nil {
			defer tx.Rollback(context.Background())
			_, _ = consumeMemoryOutboxTx(b.ctx, preferenceTraceTx{tx, t}, c)
		}
		t.Fatal("original Memory Store parent consume", e)
	}
	if p.State != agentoutbox.MemoryComplete {
		t.Fatal("parent did not complete", p)
	}
	_, child, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, worker, agentoutbox.MemoryHandler)
	if e != nil {
		t.Fatal(e)
	}
	if p, e = b.store.ConsumeAgentOutboxControl(b.ctx, child); e != nil || p.State != agentoutbox.MemoryComplete {
		t.Fatal("original Memory Store child consume", e)
	}
	var absent, bound bool
	var revision int64
	var revoked *time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1),EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$2)`, unbound.ID, old.ID).Scan(&absent, &bound); e != nil || !absent || !bound {
		t.Fatal("original Memory preview cleanup/history", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, grant.ID).Scan(&revision, &revoked); e != nil || revision != grant.Revision+1 || revoked == nil {
		t.Fatal("original Memory sole grant CAS", e)
	}
}

// Original Memory101 producer and consumers, not Preference102 coverage. All
// contexts/grants below are the original current human preview/approve paths;
// no authority, source event, receipt, session refresh or trigger waiver is
// synthesized. The new tests use independently owned migration children.
type memoryStormFixture struct{ purpose *contextPurposeFixture }

func memoryStormDatabase(t *testing.T) {
	t.Helper()
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("Memory storm native verification requires explicit owned backend runner")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !regexp.MustCompile(`^birdtie_backend_native_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_VISIBILITY_DATABASE") || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
		t.Fatal("refuse shared/phone/remote Memory storm parent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	parent, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("owned Memory parent connection unavailable")
	}
	t.Cleanup(parent.Close)
	var parentName, parentOwner, parentRole string
	if e = parent.QueryRow(ctx, `SELECT current_database(),pg_get_userbyid(d.datdba),current_user FROM pg_database d WHERE d.datname=current_database()`).Scan(&parentName, &parentOwner, &parentRole); e != nil || parentName != cfg.ConnConfig.Database || parentOwner != parentRole {
		t.Fatal("owned Memory parent identity mismatch")
	}
	var name, owner, marker string
	var oid uint32
	t.Cleanup(func() {
		if name == "" {
			return
		}
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		var remaining int
		if e := parent.QueryRow(cleanup, `SELECT count(*) FROM pg_database WHERE datname=$1 OR oid=$2`, name, oid).Scan(&remaining); e != nil || remaining != 0 {
			t.Error("owned Memory migration child residue", name, oid, remaining)
		}
		t.Logf("OWNED_MEMORY_CHILD_CLEANUP name=%s oid=%d owner=%s marker=%s remaining=%d", name, oid, owner, marker, remaining)
	})
	ownedMigrationDatabase(t) // Existing success-only CREATE/DROP helper001..119.
	child, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal("owned Memory child connection unavailable")
	}
	t.Cleanup(child.Close)
	var role string
	if e = child.QueryRow(ctx, `SELECT current_database(),d.oid,pg_get_userbyid(d.datdba),current_user FROM pg_database d WHERE d.datname=current_database()`).Scan(&name, &oid, &owner, &role); e != nil || !regexp.MustCompile(`^birdtie_owned_migration_[0-9a-f]{24}$`).MatchString(name) || name == parentName || owner != role {
		t.Fatal("independent Memory child ownership mismatch")
	}
	if e = child.QueryRow(ctx, `SELECT 'AIR020-owned-memory-'||gen_random_uuid()::text`).Scan(&marker); e != nil {
		t.Fatal(e)
	}
	if _, e = child.Exec(ctx, `COMMENT ON DATABASE `+pgx.Identifier{name}.Sanitize()+` IS '`+marker+`'`); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		var actualOwner, actualMarker string
		if e := parent.QueryRow(context.Background(), `SELECT pg_get_userbyid(datdba),shobj_description(oid,'pg_database') FROM pg_database WHERE datname=$1 AND oid=$2`, name, oid).Scan(&actualOwner, &actualMarker); e != nil || actualOwner != owner || actualMarker != marker {
			t.Error("owned Memory child pre-drop identity mismatch")
		}
	})
	t.Setenv("BIRDTIE_MEMORY_NATIVE_CHILD_DATABASE", name)
	t.Setenv("BIRDTIE_MEMORY_NATIVE_CHILD_MARKER", marker)
	t.Logf("OWNED_MEMORY_CHILD name=%s oid=%d owner=%s marker=%s parent=%s", name, oid, owner, marker, parentName)
}
func memoryStormNative(t *testing.T) *memoryStormFixture {
	t.Helper()
	f := contextPurposeNative(t)
	f.selection.ProfileFields = nil
	f.selection.PlaceIDs = nil
	f.selection.ActivityIDs = nil
	f.selection.RelationshipTieIDs = nil
	f.selection.PolicyFamilies = nil
	return &memoryStormFixture{purpose: f}
}
func (f *memoryStormFixture) id(t *testing.T) string {
	t.Helper()
	b := f.purpose.f.place.private.base
	var id string
	if e := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}
func (f *memoryStormFixture) input(version int64, text string) agentmemory.PutInput {
	return agentmemory.PutInput{ExpectedVersion: version, MemoryType: agentmemory.TypePreference, MemoryKey: "context.explicit.native", Summary: text, StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: f.purpose.f.now.Add(time.Hour)}
}
func (f *memoryStormFixture) write(t *testing.T, text string) agentmemory.Record {
	t.Helper()
	b := f.purpose.f.place.private.base
	var version int64
	if e := b.pool.QueryRow(b.ctx, `SELECT version FROM agent_memories WHERE id=$1 AND owner_id=$2`, f.purpose.memory, b.person.ID).Scan(&version); e != nil {
		t.Fatal(e)
	}
	r, e := b.store.PutOwnMemory(b.ctx, f.purpose.f.place.private.owner, f.purpose.memory, f.input(version, text))
	if e != nil || r.Version != version+1 {
		t.Fatal("original Memory producer did not advance exact source revision", e)
	}
	return r
}
func (f *memoryStormFixture) row(t *testing.T, version int64, kind agentoutbox.EventType) agentoutbox.Record {
	t.Helper()
	b := f.purpose.f.place.private.base
	r, e := scanAgentOutbox(b.pool.QueryRow(b.ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE subject_id=$1 AND source_id=$2 AND schema_version=$3 AND source_revision=$4 AND event_type=$5`, b.person.ID, f.purpose.memory, agentoutbox.MemorySchema, version, kind))
	if e != nil {
		t.Fatal("original captured Memory event unavailable", e)
	}
	return r
}
func (f *memoryStormFixture) history(t *testing.T) string {
	t.Helper()
	b := f.purpose.f.place.private.base
	var raw string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('events',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('xmin',d.xmin::text) ORDER BY event_id) FROM agent_domain_outbox d WHERE subject_id=$1 AND schema_version=$2),'receipts',(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY event_id) FROM agent_consumer_inbox i WHERE subject_id=$1 AND handler_version=$3))::text`, b.person.ID, agentoutbox.MemorySchema, agentoutbox.MemoryHandler).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw // Private assertion only. Never logged or exported.
}
func (f *memoryStormFixture) claim(t *testing.T) (agentoutbox.Record, agentoutbox.Claim) {
	t.Helper()
	b := f.purpose.f.place.private.base
	for i := 0; i < 16; i++ {
		var candidate string
		if e := b.pool.QueryRow(b.ctx, `SELECT event_id::text FROM agent_domain_outbox WHERE subject_id=$1 AND schema_version=$2 AND delivery_state='PENDING' ORDER BY occurred_at,event_id LIMIT 1`, b.person.ID, agentoutbox.MemorySchema).Scan(&candidate); e != nil {
			t.Fatal("actual pending Memory source absent", e)
		}
		r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.MemoryHandler)
		if e == nil {
			return r, c
		}
		var state string
		var attempt int64
		if e == agentoutbox.ErrUnavailable && r == (agentoutbox.Record{}) && c == (agentoutbox.Claim{}) && b.pool.QueryRow(b.ctx, `SELECT delivery_state,attempt FROM agent_domain_outbox WHERE event_id=$1`, candidate).Scan(&state, &attempt) == nil && state == string(agentoutbox.Invalidated) && attempt == 0 {
			continue // Exactly observed native terminal transition, never unknown retry.
		}
		t.Fatal("original Memory claim failed without known stale transition", e)
	}
	t.Fatal("bounded original Memory stale progress did not reach current source")
	return agentoutbox.Record{}, agentoutbox.Claim{}
}
func (f *memoryStormFixture) consume(t *testing.T, c agentoutbox.Claim) agentoutbox.ConsumerRecord {
	t.Helper()
	b := f.purpose.f.place.private.base
	r, e := b.store.ConsumeAgentOutboxControl(b.ctx, c)
	if e != nil {
		t.Fatal("original Memory consumer", e)
	}
	return r
}
func (f *memoryStormFixture) capture(t *testing.T, label string) {
	t.Helper()
	dir := os.Getenv("BIRDTIE_MEMORY_NATIVE_EVIDENCE_DIR")
	if dir == "" {
		return
	}
	abs, e := filepath.Abs(dir)
	allowed := filepath.Clean(`D:\Project\birdtie\docs\testing\evidence\unfinished-completion-2026-10-10\backend-native-checkpoint\memory`)
	if e != nil || !strings.HasPrefix(strings.ToLower(abs), strings.ToLower(allowed)+string(filepath.Separator)) || !regexp.MustCompile(`^[a-z0-9-]+$`).MatchString(label) {
		t.Fatal("refuse unowned Memory metadata output")
	}
	b := f.purpose.f.place.private.base
	var raw []byte
	e = b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('scope','LOCAL_OWNED_SYNTHETIC_MEMORY101','subjectId',$1::uuid,'memoryId',$4::uuid,'schema',$2::text,'handler',$3::text,'events',COALESCE((SELECT jsonb_agg(jsonb_build_object('eventId',d.event_id,'eventType',d.event_type,'sourceRevision',d.source_revision,'sourceStatus',d.source_status,'rootTraceId',d.root_trace_id,'causationId',d.causation_id,'occurredAt',d.occurred_at,'receivedAt',d.received_at,'expiresAt',d.expires_at,'state',d.delivery_state,'attempt',d.attempt,'fence',d.fence) ORDER BY event_id) FROM agent_domain_outbox d WHERE d.subject_id=$1 AND d.schema_version=$2),'[]'::jsonb),'receipts',COALESCE((SELECT jsonb_agg(jsonb_build_object('eventId',i.event_id,'state',i.control_state,'reason',i.reason_code,'attempt',i.attempt,'fence',i.fence) ORDER BY event_id) FROM agent_consumer_inbox i WHERE i.subject_id=$1 AND i.handler_version=$3),'[]'::jsonb))`, b.person.ID, agentoutbox.MemorySchema, agentoutbox.MemoryHandler, f.purpose.memory).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(abs, 0700); e != nil {
		t.Fatal(e)
	}
	file, e := os.OpenFile(filepath.Join(abs, label+"-"+b.person.ID+".roots.metadata.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal("immutable Memory metadata output", e)
	}
	_, e = file.Write(raw)
	closed := file.Close()
	if e != nil || closed != nil {
		t.Fatal("Memory metadata output failed")
	}
}

func TestMemoryInvalidationNativeSourceOrderABAAndHistoricalReplay(t *testing.T) {
	memoryStormDatabase(t)
	f := memoryStormNative(t)
	b := f.purpose.f.place.private.base
	first := f.write(t, "A本人明确声明")
	_, old := f.claim(t)
	f.write(t, "B本人明确声明")
	last := f.write(t, "A本人明确声明")
	if last.Version != first.Version+2 {
		t.Fatal("actual Memory ABA did not advance source revision twice")
	}
	current, grant := f.purpose.approve(t)
	receipt := f.consume(t, old)
	if receipt.State != agentoutbox.Invalidated || receipt.Reason != agentoutbox.ReasonSourceInvalidated {
		t.Fatal("old Memory source remained executable after ABA")
	}
	var revision int64
	var revoked *time.Time
	var present bool
	if e := b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, grant.ID).Scan(&revision, &revoked); e != nil || revision != grant.Revision || revoked != nil {
		t.Fatal("old source changed a current exact grant", e)
	}
	if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1)`, current.ID).Scan(&present); e != nil || !present {
		t.Fatal("old source removed current original preview", e)
	}
	before := f.history(t)
	for _, kind := range []string{"foreign-subject", "invented-fence", "old-producer-version", "terminal-replay"} {
		t.Run(kind, func(t *testing.T) {
			bad := old
			switch kind {
			case "foreign-subject":
				bad.Subject = b.other
				if p, e := b.store.ConsumeAgentOutboxControl(b.ctx, bad); e == nil || p != (agentoutbox.ConsumerRecord{}) {
					t.Fatal("another owner's native claim returned a receipt")
				}
			case "invented-fence":
				bad.Fence++
				if p, e := b.store.ConsumeAgentOutboxControl(b.ctx, bad); !errors.Is(e, agentoutbox.ErrConflict) || p != (agentoutbox.ConsumerRecord{}) {
					t.Fatal("invented Memory fence returned a receipt", e)
				}
			case "old-producer-version":
				if _, e := b.store.PutOwnMemory(b.ctx, f.purpose.f.place.private.owner, f.purpose.memory, f.input(first.Version, "旧批准不能覆盖当前Memory")); !errors.Is(e, agentmemory.ErrConflict) {
					t.Fatal("late original producer version accepted", e)
				}
			case "terminal-replay":
				if p, e := b.store.ConsumeAgentOutboxControl(b.ctx, old); e != nil || !reflect.DeepEqual(p, receipt) {
					t.Fatal("same native terminal receipt did not replay immutable history", e)
				}
			}
			if f.history(t) != before {
				t.Fatal("invalid/replayed operation changed native event or receipt xmin")
			}
		})
	}
	f.capture(t, "source-aba")
}

func TestMemoryInvalidationNativeTenantFairnessAndCapacity(t *testing.T) {
	t.Run("actual-global-four-owner-one-five-owners", func(t *testing.T) {
		memoryStormDatabase(t)
		fixtures := make([]*memoryStormFixture, 5)
		claims := make([]agentoutbox.Claim, 4)
		for i := range fixtures {
			fixtures[i] = memoryStormNative(t)
		}
		b := fixtures[0].purpose.f.place.private.base
		_, claims[0] = fixtures[0].claim(t)
		fixtures[0].write(t, "原领取仍有效时另有真实待处理声明")
		beforeOwner := fixtures[0].history(t)
		r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, fixtures[0].id(t), agentoutbox.MemoryHandler)
		if e != agentoutbox.ErrDispatchBusy || r != (agentoutbox.Record{}) || c != (agentoutbox.Claim{}) || beforeOwner != fixtures[0].history(t) {
			t.Fatal("one-owner cap failed with real eligible backlog and free global slots", e)
		}
		for i := 1; i < 4; i++ {
			_, claims[i] = fixtures[i].claim(t)
		}
		for _, i := range []int{0, 4} {
			f := fixtures[i]
			before := f.history(t)
			r, c, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, f.purpose.f.place.private.base.person, f.id(t), agentoutbox.MemoryHandler)
			if e != agentoutbox.ErrDispatchBusy || r != (agentoutbox.Record{}) || c != (agentoutbox.Claim{}) || before != f.history(t) {
				t.Fatal("global native cap mutated a denied tenant", i, e)
			}
		}
		var count int
		if e := b.pool.QueryRow(b.ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n) SELECT count(*) FROM agent_domain_outbox d CROSS JOIN stamp WHERE `+outboxControlActiveSQL).Scan(&count); e != nil || count != 4 {
			t.Fatal("native committed active count is not four", count, e)
		}
		fixtures[0].consume(t, claims[0]) // New source invalidates the old leased root.
		_, fifth := fixtures[4].claim(t)
		if fifth.Subject != fixtures[4].purpose.f.place.private.base.person {
			t.Fatal("fifth original owner did not regain the released capacity")
		}
		for _, f := range fixtures {
			f.capture(t, "quota-five-owners")
		}
	})
	t.Run("real-burst-does-not-monopolize-unserved-owners", func(t *testing.T) {
		memoryStormDatabase(t)
		a := memoryStormNative(t)
		for i := 0; i < 6; i++ {
			_, c := a.claim(t)
			a.consume(t, c)
			a.write(t, fmt.Sprintf("明确突发声明%d", i))
		}
		b, c := memoryStormNative(t), memoryStormNative(t)
		base := a.purpose.f.place.private.base
		seen := map[string]bool{}
		for i := 0; i < 2; i++ {
			_, claim, e := base.store.ClaimAgentOutboxControl(base.ctx, a.id(t), agentoutbox.MemoryHandler)
			if e != nil || claim.Subject == base.person {
				t.Fatal("served burst monopolized an unserved tenant head", e)
			}
			seen[claim.Subject.ID] = true
		}
		if !seen[b.purpose.f.place.private.base.person.ID] || !seen[c.purpose.f.place.private.base.person.ID] {
			t.Fatal("both distinct unserved original owners were not admitted")
		}
		for _, f := range []*memoryStormFixture{a, b, c} {
			f.capture(t, "burst-fairness")
		}
	})
}

func TestMemoryInvalidationNativeRootBudgetDepthAndNoGeneratedLoop(t *testing.T) {
	memoryStormDatabase(t)
	f := memoryStormNative(t)
	b := f.purpose.f.place.private.base
	for i := 0; i < 501; i++ {
		f.purpose.approve(t)
	}
	saved := f.write(t, "501份真实旧许可应受根预算限制")
	root, claim := f.claim(t)
	f.consume(t, claim)
	var last agentoutbox.ConsumerRecord
	for i := 0; i < 5; i++ {
		_, child := f.claim(t)
		last = f.consume(t, child)
	}
	if last.State != agentoutbox.DeadLetter || last.Reason != agentoutbox.ReasonRootExhausted {
		t.Fatal("original Memory root attempt budget did not terminate bounded work")
	}
	var attempts, revoked, pending, memories, updated int
	var version int64
	if e := b.pool.QueryRow(b.ctx, `SELECT
	 (SELECT sum(attempt) FROM agent_domain_outbox WHERE subject_id=$1 AND root_trace_id=$2),
	 (SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NOT NULL),
	 (SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NULL),
	 (SELECT count(*) FROM agent_memories WHERE owner_id=$1),
	 (SELECT version FROM agent_memories WHERE id=$3),
	 (SELECT count(*) FROM agent_domain_outbox WHERE subject_id=$1 AND schema_version=$4 AND event_type='MEMORY_UPDATED')`, b.person.ID, root.Event.RootTraceID, f.purpose.memory, agentoutbox.MemorySchema).Scan(&attempts, &revoked, &pending, &memories, &version, &updated); e != nil || attempts != 6 || revoked != 500 || pending != 1 || memories != 1 || version != saved.Version || updated != 2 {
		t.Fatal("native root6 left wrong source/cleanup/remainder or generated MemoryUpdated loop", attempts, revoked, pending, memories, version, updated, e)
	}
	child := f.row(t, saved.Version, agentoutbox.MemoryContextInvalidation)
	if child.Event.CausationID == nil || *child.Event.CausationID != root.Event.EventID || child.Event.RootTraceID != root.Event.RootTraceID {
		t.Fatal("actual depth1 child is not bound to original native root")
	}
	before := f.history(t)
	for _, pair := range []struct{ id, cause string }{{root.Event.EventID, root.Event.EventID}, {child.Event.EventID, child.Event.EventID}, {child.Event.EventID, root.Event.EventID}} {
		_, e := b.pool.Exec(b.ctx, `INSERT INTO agent_domain_outbox(schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,causation_id,occurred_at,received_at,expires_at)
		 SELECT schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,$2::uuid,occurred_at,received_at,expires_at FROM agent_domain_outbox WHERE event_id=$1`, pair.id, pair.cause)
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "55000" {
			t.Fatal("native causal guard did not reject self/depth overflow/recreated child", e)
		}
	}
	if before != f.history(t) {
		t.Fatal("rejected causal insertion changed real event/receipt history")
	}
	f.capture(t, "root-six-depth-one")
}

func memoryStormWait(t *testing.T, observer *pgxpool.Pool, ctx context.Context, waiting, blocker int, label string) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		var observed bool
		if e := observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid=$1 AND wait_event_type='Lock' AND $2=ANY(pg_blocking_pids(pid)))`, waiting, blocker).Scan(&observed); e != nil {
			t.Fatal(e)
		}
		if observed {
			t.Logf("NATIVE_MEMORY_LOCK_OBSERVED label=%s waitingPID=%d exactBlockerPID=%d", label, waiting, blocker)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("exact native wait/blocker positive control absent", label)
}
func memoryStormPool(t *testing.T, ctx context.Context, label string) (*pgxpool.Pool, int) {
	t.Helper()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.MaxConns, cfg.MinConns = 1, 0
	cfg.ConnConfig.RuntimeParams["application_name"] = label
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	var pid int
	if e = pool.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	return pool, pid
}
func memoryStormAwaitDeadline(t *testing.T, pool *pgxpool.Pool, ctx context.Context, deadline time.Time) {
	t.Helper()
	for {
		var expired bool
		if e := pool.QueryRow(ctx, `SELECT clock_timestamp()>$1`, deadline).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("native deadline did not elapse before fixture timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func TestMemoryInvalidationNativeOriginalProducerWaitWithdrawal(t *testing.T) {
	memoryStormDatabase(t)
	f := memoryStormNative(t)
	b := f.purpose.f.place.private.base
	_, grant := f.purpose.approve(t)
	_, old := f.claim(t)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var blocker int
	if e = tx.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&blocker); e != nil {
		t.Fatal(e)
	}
	binding, e := lockOwnAgentPrivateBinding(b.ctx, tx, f.purpose.f.place.private.owner, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = lockAgentPrivateMetadata(b.ctx, tx, binding, true); e != nil {
		t.Fatal(e)
	}
	deleted, e := deleteOwnMemoryInTx(b.ctx, tx, binding, f.purpose.memory, 1)
	if e != nil || deleted.Status != agentmemory.StatusDeleted {
		t.Fatal("original human delete transaction did not withdraw exact source", e)
	}
	consumer, pid := memoryStormPool(t, b.ctx, "memory-withdrawal-"+b.person.ID)
	type result struct {
		p agentoutbox.ConsumerRecord
		e error
	}
	done := make(chan result, 1)
	go func() { p, e := New(consumer, false).ConsumeAgentOutboxControl(b.ctx, old); done <- result{p, e} }()
	memoryStormWait(t, b.pool, b.ctx, pid, blocker, "original-human-source-withdrawal")
	if e = recheckAgentPrivateSession(b.ctx, tx, binding); e != nil {
		t.Fatal("original producer session after actual wait", e)
	}
	if e = tx.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case r := <-done:
		if r.e != nil || r.p.State != agentoutbox.Invalidated || r.p.Reason != agentoutbox.ReasonSourceInvalidated {
			t.Fatal("waiting consumer accepted source withdrawn by original transaction", r.e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("original producer wait did not resolve")
	}
	var revision int64
	var revoked *time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, grant.ID).Scan(&revision, &revoked); e != nil || revision != grant.Revision || revoked != nil {
		t.Fatal("stale claimed source changed original grant after lock wait", e)
	}
	f.capture(t, "original-withdrawal-wait")
}
func TestMemoryInvalidationNativeRealWaitFinalLeaseFence(t *testing.T) {
	memoryStormDatabase(t)
	f := memoryStormNative(t)
	b := f.purpose.f.place.private.base
	_, grant := f.purpose.approve(t)
	saved := f.write(t, "真实等待后必须重验原租期")
	_, root := f.claim(t)
	f.consume(t, root)
	_, child := f.claim(t)
	tx, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	var blocker int
	var id string
	if e = tx.QueryRow(b.ctx, `SELECT pg_backend_pid()`).Scan(&blocker); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(b.ctx, `SELECT id::text FROM consent_grants WHERE id=$1 FOR UPDATE`, grant.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	consumer, pid := memoryStormPool(t, b.ctx, "memory-expiring-consumer-"+b.person.ID)
	type result struct {
		p agentoutbox.ConsumerRecord
		e error
	}
	done := make(chan result, 1)
	go func() { p, e := New(consumer, false).ConsumeAgentOutboxControl(b.ctx, child); done <- result{p, e} }()
	memoryStormWait(t, b.pool, b.ctx, pid, blocker, "grant-cleanup")
	human, hpid := memoryStormPool(t, b.ctx, "memory-waiting-human-"+b.person.ID)
	hctx, cancel := context.WithTimeout(b.ctx, 2*time.Second)
	defer cancel()
	hdone := make(chan error, 1)
	go func() {
		_, e := New(human, false).PutOwnMemory(hctx, f.purpose.f.place.private.owner, f.purpose.memory, f.input(saved.Version, "未获执行的等待声明"))
		hdone <- e
	}()
	memoryStormWait(t, b.pool, b.ctx, hpid, pid, "human-metadata-order")
	if e = <-hdone; e == nil {
		t.Fatal("waiting producer bypassed actual metadata lock")
	}
	before := f.history(t)
	memoryStormAwaitDeadline(t, b.pool, b.ctx, child.LeaseUntil)
	if e = tx.Rollback(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case r := <-done:
		if !errors.Is(r.e, agentoutbox.ErrExpired) || r.p != (agentoutbox.ConsumerRecord{}) {
			t.Fatal("expired waiting consumer returned committed receipt", r.e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native consumer lease wait unresolved")
	}
	var revision, version int64
	var revoked *time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT revision,revoked_at FROM consent_grants WHERE id=$1`, grant.ID).Scan(&revision, &revoked); e != nil || revision != grant.Revision || revoked != nil {
		t.Fatal("expired cleanup committed partial grant revocation", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT version FROM agent_memories WHERE id=$1`, f.purpose.memory).Scan(&version); e != nil || version != saved.Version {
		t.Fatal("cancelled waiting producer changed original source", e)
	}
	r := f.row(t, saved.Version, agentoutbox.MemoryContextInvalidation)
	if r.State != agentoutbox.Leased || r.Fence != child.Fence || before != f.history(t) {
		t.Fatal("late consumer changed native fence/receipt despite rollback")
	}
	t.Logf("NATIVE_MEMORY_ORIGINAL_LEASE_ELAPSED eventId=%s leaseUntil=%s fence=%d noRenewal=true", child.EventID, child.LeaseUntil.UTC().Format(time.RFC3339Nano), child.Fence)
	f.capture(t, "natural-lease-expiry")
}
func TestMemoryInvalidationNativeProcessRestartAndConfirmedReceiptLoss(t *testing.T) {
	if os.Getenv("BIRDTIE_MEMORY_NATIVE_HELPER") == "claim" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_MEMORY_NATIVE_CHILD_DATABASE") || !regexp.MustCompile(`^birdtie_owned_migration_[0-9a-f]{24}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 {
			os.Exit(2)
		}
		pool, e := pgxpool.NewWithConfig(ctx, cfg)
		if e != nil {
			os.Exit(3)
		}
		defer pool.Close()
		var marked bool
		if e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname=current_database() AND pg_get_userbyid(datdba)=current_user AND shobj_description(oid,'pg_database')=$1)`, os.Getenv("BIRDTIE_MEMORY_NATIVE_CHILD_MARKER")).Scan(&marked); e != nil || !marked {
			os.Exit(4)
		}
		subject, e := actorref.ParsePrincipal("PERSON", os.Getenv("BIRDTIE_MEMORY_NATIVE_SUBJECT"))
		if e != nil {
			os.Exit(5)
		}
		_, _, e = New(pool, false).ClaimAgentOutboxControlForSubject(ctx, subject, os.Getenv("BIRDTIE_MEMORY_NATIVE_WORKER"), agentoutbox.MemoryHandler)
		if e != nil {
			os.Exit(6)
		}
		fmt.Print("CLAIMED")
		return
	}
	memoryStormDatabase(t)
	f := memoryStormNative(t)
	b := f.purpose.f.place.private.base
	binary := maintenanceBinary(t)
	worker := f.id(t)
	ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMemoryInvalidationNativeProcessRestartAndConfirmedReceiptLoss$")
	cmd.Env = append(os.Environ(), "BIRDTIE_MEMORY_NATIVE_HELPER=claim", "BIRDTIE_MEMORY_NATIVE_SUBJECT="+b.person.ID, "BIRDTIE_MEMORY_NATIVE_WORKER="+worker)
	out, e := cmd.CombinedOutput()
	// Go's child harness emits its PASS footer after the helper returns. It
	// never serializes Claim authority; native rows are inspected by the parent.
	if e != nil || !strings.HasPrefix(string(out), "CLAIMED") || !strings.Contains(string(out), "PASS") {
		t.Fatal("real OS child claim failed", e)
	}
	old := f.row(t, 1, agentoutbox.MemoryUpdated)
	if old.State != agentoutbox.Leased || old.LeaseOwner != worker || old.LeaseUntil == nil {
		t.Fatal("child process did not leave its native lease")
	}
	claim := agentoutbox.Claim{EventID: old.Event.EventID, Subject: old.Event.Subject, AgentID: old.Event.AgentID, HandlerVersion: agentoutbox.MemoryHandler, WorkerID: worker, Fence: old.Fence, LeaseUntil: *old.LeaseUntil}
	memoryStormAwaitDeadline(t, b.pool, b.ctx, claim.LeaseUntil)
	report, code, e := maintenanceCommand(binary, b.person.ID, agentoutbox.MemoryHandler)
	if e != nil || code != 0 || report.ConfirmedReceipts != 2 || report.Counts.MaintenanceComplete != 2 {
		t.Fatal("original OS maintenance parent/child recovery failed", report, code, e)
	}
	if p, e := b.store.ConsumeAgentOutboxControl(b.ctx, claim); !errors.Is(e, agentoutbox.ErrConflict) || p != (agentoutbox.ConsumerRecord{}) {
		t.Fatal("old process fence accepted after restart", e)
	}
	current := f.row(t, 1, agentoutbox.MemoryUpdated)
	if current.Fence != old.Fence+1 || current.State != agentoutbox.MemoryComplete {
		t.Fatal("original restart lost monotonic lease fence")
	}
	// Obtain a genuine new Claim from the original dispatcher, then deliberately
	// discard its successful native Consume result. Reconciliation reuses that
	// exact Claim; it never invents the CLI worker's post-completion authority.
	// This is confirmed receipt loss at the caller, not network commit ambiguity.
	f.write(t, "原生已确认回执丢弃后核实历史")
	_, completed := f.claim(t)
	first := f.consume(t, completed)
	if first.State != agentoutbox.MemoryComplete {
		t.Fatal("actual native confirmed receipt unavailable")
	}
	before := f.history(t)
	second, e := b.store.ConsumeAgentOutboxControl(b.ctx, completed)
	if e != nil || !reflect.DeepEqual(first, second) || before != f.history(t) {
		t.Fatal("confirmed receipt replay reran cleanup or changed native history", e)
	}
	_, pendingChild := f.claim(t)
	f.consume(t, pendingChild)
	before = f.history(t)
	report, code, e = maintenanceCommand(binary, b.person.ID, agentoutbox.MemoryHandler)
	if e != nil || code != 0 || report.ConfirmedReceipts != 0 || before != f.history(t) {
		t.Fatal("new original CLI repeated completed history", report, code, e)
	}
	h := sha256.Sum256([]byte(before))
	t.Logf("NATIVE_MEMORY_OS_RESTART eventId=%s oldFence=%d recoveredFence=%d historicalSHA256=%x realUnknownCommitNetwork=NOT_RUN", current.Event.EventID, old.Fence, current.Fence, h)
	f.capture(t, "os-restart-confirmed-history")
}
