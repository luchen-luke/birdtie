package postgres

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	maint "github.com/birdtie/birdtie/apps/api/internal/agentoutboxmaintenance"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Every top-level native chain creates an independently owned migration child.
// Fixtures use original human producers/approvals; no queue/receipt is forged.
type mixedMaintenanceFixture struct{ purpose *contextPurposeFixture }

func mixedMaintenanceNative(t *testing.T) *mixedMaintenanceFixture {
	t.Helper()
	f := contextPurposeNative(t)
	b := f.f.place.private.base
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	b.ctx = ctx // Natural 30s lease expiry is tested within this owned fixture.
	return &mixedMaintenanceFixture{f}
}
func (f *mixedMaintenanceFixture) id(t *testing.T) string {
	return (&memoryStormFixture{purpose: f.purpose}).id(t)
}
func (f *mixedMaintenanceFixture) run(t *testing.T, batch int) maint.Report {
	t.Helper()
	b := f.purpose.f.place.private.base
	r := maint.Run(b.ctx, b.store, maint.Options{Subject: b.person, WorkerID: f.id(t), Handler: agentoutbox.HandlerV1, Batch: batch, Timeout: 10 * time.Second, MixedHandlers: true})
	if r.Status != "FINISHED" || r.BusinessExecution != "UNAVAILABLE" {
		t.Fatal("actual mixed maintenance did not finish", r)
	}
	return r
}
func (f *mixedMaintenanceFixture) history(t *testing.T) string {
	t.Helper()
	b := f.purpose.f.place.private.base
	var raw string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('events',(SELECT jsonb_agg(to_jsonb(d)||jsonb_build_object('xmin',d.xmin::text) ORDER BY event_id) FROM agent_domain_outbox d WHERE subject_id=$1),'receipts',(SELECT jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY event_id,handler_version) FROM agent_consumer_inbox i WHERE subject_id=$1),'grants',(SELECT jsonb_agg(to_jsonb(g) ORDER BY id) FROM consent_grants g WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ'))::text`, b.person.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw // Private assertions only; never export native rows or grant data.
}
func (f *mixedMaintenanceFixture) claim(t *testing.T) (agentoutbox.Record, agentoutbox.Claim) {
	t.Helper()
	b := f.purpose.f.place.private.base
	for i := 0; i < 32; i++ {
		r, c, e := b.store.ClaimMixedAgentOutboxControlForSubject(b.ctx, b.person, f.id(t))
		if e == maint.ErrNativeControlRetired && r == (agentoutbox.Record{}) && c == (agentoutbox.Claim{}) {
			continue
		}
		if e != nil {
			t.Fatal("actual mixed claim", e)
		}
		return r, c
	}
	t.Fatal("bounded mixed retirement did not reach a current control")
	return agentoutbox.Record{}, agentoutbox.Claim{}
}
func (f *mixedMaintenanceFixture) memoryOnly() {
	s := &f.purpose.selection
	s.ProfileFields = nil
	s.PlaceIDs = nil
	s.ActivityIDs = nil
	s.RelationshipTieIDs = nil
	s.PolicyFamilies = nil
}
func TestMixedMaintenanceNativeActualCleanupAndReceipt(t *testing.T) {
	ownedMigrationDatabase(t)
	f := mixedMaintenanceNative(t)
	b := f.purpose.f.place.private.base
	f.run(t, 32) // Retire the genuine initial sources before concrete previews.
	f.memoryOnly()
	memoryPreview, memoryGrant := f.purpose.approve(t)
	memoryUnbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, f.purpose.selection)
	if e != nil {
		t.Fatal(e)
	}
	profileSelection := f.purpose.selection
	profileSelection.MemoryIDs = nil
	profileSelection.ProfileFields = []string{"availability"}
	profilePurpose := &contextPurposeFixture{f: f.purpose.f, selection: profileSelection}
	profilePreview, profileGrant := profilePurpose.approve(t)
	profileUnbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, profileSelection)
	if e != nil {
		t.Fatal(e)
	}
	oldMoment, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput())
	if e != nil {
		t.Fatal(e)
	}
	memoryUpdated := (&memoryStormFixture{purpose: f.purpose}).write(t, "mixed maintenance 原Memory修改正文不得进入控制输出")
	profileUpdated := (&preferenceNativeFixture{purpose: profilePurpose}).write(t, "mixed maintenance 原private来源不得进入控制输出")
	memoryRoot := (&memoryStormFixture{purpose: f.purpose}).row(t, memoryUpdated.Version, agentoutbox.MemoryUpdated)
	profileRoot := (&preferenceNativeFixture{purpose: profilePurpose}).row(t, profileUpdated.Profile.ProfileVersion, agentoutbox.PreferenceUpdated)
	// Preference changes the native metadata epoch, making the old Moment
	// genuinely stale without hand-editing its event/fingerprint/TTL.
	currentMoment, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput())
	if e != nil {
		t.Fatal(e)
	}
	report := f.run(t, 32)
	if report.Counts.RetiredControls < 1 || report.Counts.MaintenanceComplete != 4 || report.Counts.Unavailable != 1 || report.ConfirmedReceipts != 5 {
		t.Fatal("stale head prevented other original families' real cleanup", report)
	}
	for _, pair := range []struct {
		unbound, bound, grant string
		revision              int64
	}{{memoryUnbound.ID, memoryPreview.ID, memoryGrant.ID, memoryGrant.Revision}, {profileUnbound.ID, profilePreview.ID, profileGrant.ID, profileGrant.Revision}} {
		var removed, retained, revoked bool
		var revision int64
		if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1),EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$2),(SELECT revoked_at IS NOT NULL FROM consent_grants WHERE id=$3),(SELECT revision FROM consent_grants WHERE id=$3)`, pair.unbound, pair.bound, pair.grant).Scan(&removed, &retained, &revoked, &revision); e != nil || !removed || !retained || !revoked || revision != pair.revision+1 {
			t.Fatal("mixed mode did not execute original preview/grant CAS cleanup", removed, retained, revoked, revision, e)
		}
	}
	var stale, current string
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT delivery_state FROM agent_domain_outbox WHERE source_id=$1 AND schema_version='agent-outbox-v1'),(SELECT delivery_state FROM agent_domain_outbox WHERE source_id=$2 AND schema_version='agent-outbox-v1')`, oldMoment.ID, currentMoment.ID).Scan(&stale, &current); e != nil || stale != "INVALIDATED" || current != "UNAVAILABLE" {
		t.Fatal("Moment control was presented as cognition", stale, current, e)
	}
	var validChildren bool
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*)=2 AND bool_and(c.causation_id=p.event_id AND c.root_trace_id=p.root_trace_id AND c.subject_id=p.subject_id AND c.agent_id=p.agent_id AND c.expires_at=p.expires_at AND c.delivery_state='INVALIDATION_COMPLETE') FROM agent_domain_outbox c JOIN agent_domain_outbox p ON p.event_id=c.causation_id WHERE c.subject_id=$1 AND c.root_trace_id=ANY($2::uuid[]) AND c.causation_id IS NOT NULL`, b.person.ID, []string{memoryRoot.Event.RootTraceID, profileRoot.Event.RootTraceID}).Scan(&validChildren); e != nil || !validChildren {
		t.Fatal("mixed consumer bypassed original causal children/TTL", e)
	}
	before := f.history(t)
	again := f.run(t, 32)
	if again.ConfirmedReceipts != 0 || again.Counts.RetiredControls != 0 || f.history(t) != before {
		t.Fatal("mixed invocation rewrote completed history", again)
	}
	encoded, _ := json.Marshal(report)
	for _, secret := range []string{b.person.ID, oldMoment.Title, oldMoment.Body, "private来源", "原Memory修改"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatal("aggregate mixed report leaked a source")
		}
	}
	// Exercise the original fixed Moment entrance after extraction, including
	// its unchanged unavailable consumer result and genuine native receipt.
	legacyMoment, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, outboxMomentInput())
	if e != nil {
		t.Fatal(e)
	}
	legacyRecord, legacyClaim, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, b.person, f.id(t), agentoutbox.HandlerV1)
	if e != nil || legacyRecord.Event.Source.ID != legacyMoment.ID || legacyClaim.HandlerVersion != agentoutbox.HandlerV1 {
		t.Fatal("fixed Moment entrance lost native claim compatibility", e)
	}
	legacyReceipt, e := b.store.ConsumeAgentOutboxControl(b.ctx, legacyClaim)
	if e != agentoutbox.ErrUnavailable || legacyReceipt.State != agentoutbox.Unavailable || legacyReceipt.EventID != legacyClaim.EventID {
		t.Fatal("fixed Moment entrance changed consumer semantics", e)
	}
}

func TestMixedMaintenanceNativeCrossFamilyFairnessAndCapacity(t *testing.T) {
	ownedMigrationDatabase(t)
	fixtures := make([]*mixedMaintenanceFixture, 5)
	for i := range fixtures {
		fixtures[i] = mixedMaintenanceNative(t)
	}
	b := fixtures[0].purpose.f.place.private.base
	claims := make([]agentoutbox.Claim, 4)
	for i := 0; i < 4; i++ {
		_, claims[i] = fixtures[i].claim(t)
	}
	for _, i := range []int{0, 4} {
		f := fixtures[i]
		before := f.history(t)
		owner := f.purpose.f.place.private.base.person
		r, c, e := b.store.ClaimMixedAgentOutboxControlForSubject(b.ctx, owner, f.id(t))
		if e != agentoutbox.ErrDispatchBusy || r != (agentoutbox.Record{}) || c != (agentoutbox.Claim{}) || before != f.history(t) {
			t.Fatal("mixed shared global4/Person1 cap changed native history", i, e)
		}
	}
	// Existing fixed-handler workers participate in the exact same active count.
	if _, _, e := b.store.ClaimAgentOutboxControlForSubject(b.ctx, fixtures[4].purpose.f.place.private.base.person, fixtures[4].id(t), agentoutbox.MemoryHandler); e != agentoutbox.ErrDispatchBusy {
		t.Fatal("fixed handler bypassed mixed admission", e)
	}
	if _, e := b.store.ConsumeAgentOutboxControl(b.ctx, claims[0]); e != nil {
		t.Fatal(e)
	}
	// Global mixed nomination must favor the still unserved owner over the
	// first owner's remaining genuine backlog, across handler families.
	_, fifth, e := b.store.ClaimMixedAgentOutboxControl(b.ctx, fixtures[0].id(t))
	if e != nil || fifth.Subject != fixtures[4].purpose.f.place.private.base.person {
		t.Fatal("mixed global head starved an unserved Person", e)
	}
	for _, c := range append(claims[1:], fifth) {
		if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, c); e != nil {
			t.Fatal(e)
		}
	}
	first := fixtures[0]
	first.run(t, 32)
	// Many real Memory writes capture stale and current controls. A newly due
	// Preference family must be serviced before the Memory burst is exhausted.
	for i := 0; i < 12; i++ {
		(&memoryStormFixture{purpose: first.purpose}).write(t, fmt.Sprintf("明确混合突发%d", i))
	}
	(&preferenceNativeFixture{purpose: first.purpose}).write(t, "明确混合偏好刷新")
	seenPreference := false
	for i := 0; i < 4; i++ {
		r, c, e := b.store.ClaimMixedAgentOutboxControlForSubject(b.ctx, b.person, first.id(t))
		if e == maint.ErrNativeControlRetired {
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		if c.HandlerVersion == agentoutbox.PreferenceHandler {
			seenPreference = true
		}
		if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, c); e != nil {
			t.Fatal("actual mixed burst consumer", r.Event.SchemaVersion, e)
		}
	}
	if !seenPreference {
		t.Fatal("one family burst monopolized mixed maintenance")
	}
}

func TestMixedMaintenanceNativeOriginalSourceLeaseAndRootFences(t *testing.T) {
	t.Run("original-root6-depth1-with-real-remainder", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := mixedMaintenanceNative(t)
		b := f.purpose.f.place.private.base
		f.run(t, 32)
		f.memoryOnly()
		for i := 0; i < 501; i++ {
			f.purpose.approve(t)
		}
		saved := (&memoryStormFixture{purpose: f.purpose}).write(t, "原root预算必须保留未完成的真实旧许可")
		report := f.run(t, 32)
		if report.ConfirmedReceipts != 6 || report.Counts.DeadLetter != 1 || report.Counts.MaintenanceComplete != 1 {
			t.Fatal("mixed reset original root budget", report)
		}
		var attempts, revoked, pending, updated, memories int
		if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT sum(attempt) FROM agent_domain_outbox WHERE subject_id=$1 AND schema_version=$2 AND source_revision=$3),(SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NOT NULL),(SELECT count(*) FROM consent_grants WHERE owner_account_id=$1 AND purpose='TASK_CONTEXT_READ' AND revoked_at IS NULL),(SELECT count(*) FROM agent_domain_outbox WHERE subject_id=$1 AND event_type='MEMORY_UPDATED'),(SELECT count(*) FROM agent_memories WHERE owner_id=$1)`, b.person.ID, agentoutbox.MemorySchema, saved.Version).Scan(&attempts, &revoked, &pending, &updated, &memories); e != nil || attempts != 6 || revoked != 500 || pending != 1 || updated != 2 || memories != 1 {
			t.Fatal("mixed cleanup expanded root/Memory authority", attempts, revoked, pending, updated, memories, e)
		}
		child := (&memoryStormFixture{purpose: f.purpose}).row(t, saved.Version, agentoutbox.MemoryContextInvalidation)
		if child.Event.CausationID == nil || child.State != agentoutbox.DeadLetter {
			t.Fatal("original depth1 child absent")
		}
		before := f.history(t)
		_, e := b.pool.Exec(b.ctx, `INSERT INTO agent_domain_outbox(schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,causation_id,occurred_at,received_at,expires_at) SELECT schema_version,event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_type,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,event_id,occurred_at,received_at,expires_at FROM agent_domain_outbox WHERE event_id=$1`, child.Event.EventID)
		var native *pgconn.PgError
		if !errors.As(e, &native) || native.Code != "55000" || before != f.history(t) {
			t.Fatal("mixed bypassed native causal loop guard", e)
		}
	})
	t.Run("actual-source-ABA-and-current-exact-grant", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := mixedMaintenanceNative(t)
		b := f.purpose.f.place.private.base
		f.run(t, 32)
		f.memoryOnly()
		m := &memoryStormFixture{purpose: f.purpose}
		m.write(t, "明确原值A")
		m.write(t, "明确中间值B")
		current := m.write(t, "明确原值A")
		preview, grant := f.purpose.approve(t)
		result := f.run(t, 32)
		if result.Counts.RetiredControls != 2 || result.Counts.MaintenanceComplete != 2 {
			t.Fatal("mixed source ABA revived old revisions", result)
		}
		var present, unchanged, currentMemory bool
		if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1),EXISTS(SELECT 1 FROM consent_grants WHERE id=$2 AND revision=$3 AND revoked_at IS NULL),EXISTS(SELECT 1 FROM agent_memories WHERE id=$4 AND version=$5)`, preview.ID, grant.ID, grant.Revision, f.purpose.memory, current.Version).Scan(&present, &unchanged, &currentMemory); e != nil || !present || !unchanged || !currentMemory {
			t.Fatal("mixed stale source changed current concrete approval", e)
		}
		before := f.history(t)
		if _, _, e := b.store.ClaimMixedAgentOutboxControlForSubject(b.ctx, actorref.PrincipalRef{Type: actorref.Organization, ID: b.person.ID}, f.id(t)); e != agentoutbox.ErrInvalid || before != f.history(t) {
			t.Fatal("mixed subject selector accepted organization alias", e)
		}
	})
	t.Run("real-consumer-wait-past-original-lease", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := mixedMaintenanceNative(t)
		b := f.purpose.f.place.private.base
		f.run(t, 32)
		f.memoryOnly()
		_, grant := f.purpose.approve(t)
		(&memoryStormFixture{purpose: f.purpose}).write(t, "等待真实许可锁的明确原Memory修改")
		_, parent := f.claim(t)
		if _, e := b.store.ConsumeAgentOutboxControl(b.ctx, parent); e != nil {
			t.Fatal(e)
		}
		_, child := f.claim(t)
		if child.HandlerVersion != agentoutbox.MemoryHandler {
			t.Fatal("expected original Memory child")
		}
		blocker, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer blocker.Rollback(context.Background())
		if _, e = blocker.Exec(b.ctx, `SELECT id FROM consent_grants WHERE id=$1 FOR UPDATE`, grant.ID); e != nil {
			t.Fatal(e)
		}
		result := make(chan error, 1)
		consumer, _ := memoryStormPool(t, b.ctx, "mixed-maintenance-lease-wait")
		go func() { _, e := New(consumer, false).ConsumeAgentOutboxControl(b.ctx, child); result <- e }()
		preferenceWait(t, b.pool, b.ctx, "mixed-maintenance-lease-wait")
		memoryStormAwaitDeadline(t, b.pool, b.ctx, child.LeaseUntil)
		if e = blocker.Commit(b.ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-result:
			if e == nil {
				t.Fatal("mixed expired worker committed cleanup")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("original consumer did not leave lease-expired wait")
		}
		var unchanged bool
		if e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM consent_grants WHERE id=$1 AND revision=$2 AND revoked_at IS NULL)`, grant.ID, grant.Revision).Scan(&unchanged); e != nil || !unchanged {
			t.Fatal("mixed stale fence partially revoked original grant", e)
		}
		f.run(t, 32)
		if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, child); !errors.Is(e, agentoutbox.ErrConflict) {
			t.Fatal("old mixed lease survived original re-claim fence", e)
		}
	})
}

// This connection drops only the actual PostgreSQL COMMIT success reply after
// reading its CommandComplete and ReadyForQuery. No private query/body is logged.
// The application gets EOF; a separate observer verifies the committed effect.
type mixedCommitReplyLossConn struct {
	net.Conn
	armed   bool
	pending []byte
	seen    *atomic.Bool
}

func (c *mixedCommitReplyLossConn) Write(raw []byte) (int, error) {
	if len(raw) >= 6 && raw[0] == 'Q' && int(binary.BigEndian.Uint32(raw[1:5]))+1 == len(raw) && strings.EqualFold(strings.TrimSuffix(string(raw[5:]), "\x00"), "commit") {
		c.armed = true
	}
	return c.Conn.Write(raw)
}
func (c *mixedCommitReplyLossConn) Read(dst []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(dst, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	if !c.armed {
		return c.Conn.Read(dst)
	}
	c.armed = false
	var reply []byte
	committed := false
	for {
		header := make([]byte, 5)
		if _, e := io.ReadFull(c.Conn, header); e != nil {
			return 0, e
		}
		size := int(binary.BigEndian.Uint32(header[1:]))
		if size < 4 || size > 4096 {
			return 0, errors.New("invalid local COMMIT reply")
		}
		body := make([]byte, size-4)
		if _, e := io.ReadFull(c.Conn, body); e != nil {
			return 0, e
		}
		reply = append(reply, header...)
		reply = append(reply, body...)
		if header[0] == 'C' && string(body) == "COMMIT\x00" {
			committed = true
		}
		if header[0] == 'Z' {
			if committed {
				c.seen.Store(true)
				_ = c.Conn.Close()
				return 0, io.EOF
			}
			n := copy(dst, reply)
			c.pending = reply[n:]
			return n, nil
		}
	}
}

type mixedNativeLostReceiptStore struct {
	normal, faulty   *Store
	record           agentoutbox.Record
	claim            agentoutbox.Claim
	claims, consumes int
}

func (s *mixedNativeLostReceiptStore) ClaimAgentOutboxControlForSubject(c context.Context, p actorref.PrincipalRef, w string, h agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
	return s.normal.ClaimAgentOutboxControlForSubject(c, p, w, h)
}
func (s *mixedNativeLostReceiptStore) ClaimMixedAgentOutboxControlForSubject(c context.Context, p actorref.PrincipalRef, w string) (agentoutbox.Record, agentoutbox.Claim, error) {
	s.claims++
	r, claim, e := s.normal.ClaimMixedAgentOutboxControlForSubject(c, p, w)
	s.record, s.claim = r, claim
	return r, claim, e
}
func (s *mixedNativeLostReceiptStore) ConsumeAgentOutboxControl(c context.Context, claim agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
	s.consumes++
	return s.faulty.ConsumeAgentOutboxControl(c, claim)
}

func mixedMaintenanceCommand(t *testing.T, subject string) maint.Report {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, maintenanceBinary(t), "--local-development-only", "--subject", subject, "--mixed-handlers", "--batch", "32", "--timeout", "10s")
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if e := cmd.Run(); e != nil || errOut.Len() != 0 {
		t.Fatal("actual mixed CLI failed", e)
	}
	var report maint.Report
	decoder := json.NewDecoder(&out)
	decoder.DisallowUnknownFields()
	if e := decoder.Decode(&report); e != nil || report.Status != "FINISHED" || report.BusinessExecution != "UNAVAILABLE" {
		t.Fatal("actual mixed CLI did not emit closed control report", e)
	}
	return report
}
func mixedMaintenanceChildClaim() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" || !regexp.MustCompile(`^birdtie_owned_migration_[0-9a-f]{24}$`).MatchString(cfg.ConnConfig.Database) || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_MIXED_CHILD_DATABASE") || cfg.ConnConfig.Host != "127.0.0.1" {
		os.Exit(2)
	}
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		os.Exit(3)
	}
	defer pool.Close()
	var owned bool
	if e = pool.QueryRow(ctx, `SELECT d.oid::text=$1 AND pg_get_userbyid(d.datdba)=current_user FROM pg_database d WHERE d.datname=current_database()`, os.Getenv("BIRDTIE_MIXED_CHILD_OID")).Scan(&owned); e != nil || !owned {
		os.Exit(4)
	}
	subject, e := actorref.ParsePrincipal("PERSON", os.Getenv("BIRDTIE_MIXED_CHILD_SUBJECT"))
	if e != nil {
		os.Exit(5)
	}
	_, _, e = New(pool, false).ClaimMixedAgentOutboxControlForSubject(ctx, subject, os.Getenv("BIRDTIE_MIXED_CHILD_WORKER"))
	if e != nil {
		os.Exit(6)
	}
	fmt.Print("CLAIMED")
	os.Exit(0)
}
func TestMixedMaintenanceNativeProcessRecoveryAndReceiptLoss(t *testing.T) {
	if os.Getenv("BIRDTIE_MIXED_NATIVE_HELPER") == "claim" {
		mixedMaintenanceChildClaim()
		return
	}
	t.Run("real-OS-exit-and-same-native-lease", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := mixedMaintenanceNative(t)
		b := f.purpose.f.place.private.base
		f.run(t, 32)
		(&memoryStormFixture{purpose: f.purpose}).write(t, "真实mixed进程退出前的明确Memory")
		worker := f.id(t)
		var database string
		var oid uint32
		if e := b.pool.QueryRow(b.ctx, `SELECT current_database(),oid FROM pg_database WHERE datname=current_database()`).Scan(&database, &oid); e != nil {
			t.Fatal(e)
		}
		cmd := exec.CommandContext(b.ctx, os.Args[0], "-test.run=^TestMixedMaintenanceNativeProcessRecoveryAndReceiptLoss$")
		cmd.Env = append(os.Environ(), "BIRDTIE_MIXED_NATIVE_HELPER=claim", "BIRDTIE_MIXED_CHILD_DATABASE="+database, "BIRDTIE_MIXED_CHILD_OID="+strconv.FormatUint(uint64(oid), 10), "BIRDTIE_MIXED_CHILD_SUBJECT="+b.person.ID, "BIRDTIE_MIXED_CHILD_WORKER="+worker)
		out, e := cmd.CombinedOutput()
		if e != nil || string(out) != "CLAIMED" {
			t.Fatal("owned actual OS child did not leave committed mixed claim", e)
		}
		old, e := scanAgentOutbox(b.pool.QueryRow(b.ctx, `SELECT `+outboxColumns+` FROM agent_domain_outbox d WHERE d.subject_id=$1 AND d.lease_owner=$2`, b.person.ID, worker))
		if e != nil || old.LeaseUntil == nil || old.State != agentoutbox.Leased {
			t.Fatal("child did not retain original native lease", e)
		}
		claim := agentoutbox.Claim{EventID: old.Event.EventID, Subject: b.person, AgentID: old.Event.AgentID, HandlerVersion: agentoutbox.MemoryHandler, WorkerID: worker, Fence: old.Fence, LeaseUntil: *old.LeaseUntil}
		memoryStormAwaitDeadline(t, b.pool, b.ctx, claim.LeaseUntil)
		report := mixedMaintenanceCommand(t, b.person.ID)
		if report.Counts.MaintenanceComplete != 2 || report.ConfirmedReceipts != 2 {
			t.Fatal("actual new CLI did not recover original parent and child", report)
		}
		if _, e = b.store.ConsumeAgentOutboxControl(b.ctx, claim); !errors.Is(e, agentoutbox.ErrConflict) {
			t.Fatal("old process mixed fence accepted after recovery", e)
		}
		before := f.history(t)
		if again := mixedMaintenanceCommand(t, b.person.ID); again.ConfirmedReceipts != 0 || f.history(t) != before {
			t.Fatal("new mixed process repeated completed history", again)
		}
	})
	t.Run("real-PostgreSQL-COMMIT-reply-loss-no-automatic-retry", func(t *testing.T) {
		ownedMigrationDatabase(t)
		f := mixedMaintenanceNative(t)
		b := f.purpose.f.place.private.base
		f.run(t, 32)
		f.memoryOnly()
		unbound, e := b.store.PreviewOwnContextPurpose(b.ctx, f.purpose.f.place.private.owner, f.purpose.selection)
		if e != nil {
			t.Fatal(e)
		}
		(&memoryStormFixture{purpose: f.purpose}).write(t, "真实提交成功但COMMIT回包丢失的明确Memory")
		cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.TLSConfig != nil {
			t.Fatal("owned COMMIT fault test requires literal loopback non-TLS")
		}
		cfg.MaxConns = 1
		cfg.MinConns = 0
		var seen atomic.Bool
		cfg.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
			host, _, e := net.SplitHostPort(address)
			if e != nil || network != "tcp" || host != "127.0.0.1" {
				return nil, errors.New("owned local dial required")
			}
			conn, e := (&net.Dialer{}).DialContext(ctx, network, address)
			if e != nil {
				return nil, e
			}
			return &mixedCommitReplyLossConn{Conn: conn, seen: &seen}, nil
		}
		pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
		if e != nil {
			t.Fatal("owned fault connection unavailable")
		}
		defer pool.Close()
		store := &mixedNativeLostReceiptStore{normal: b.store, faulty: New(pool, false)}
		report := maint.Run(b.ctx, store, maint.Options{Subject: b.person, WorkerID: f.id(t), Handler: agentoutbox.HandlerV1, Batch: 32, Timeout: 10 * time.Second, MixedHandlers: true})
		if !seen.Load() || report.Status != "STOPPED" || report.Reason != "CONSUME_UNCONFIRMED" || report.ConfirmedReceipts != 0 || report.Counts.RetiredControls != 0 || store.claims != 1 || store.consumes != 1 {
			t.Fatal("lost COMMIT reply counted success or retried", report, seen.Load(), store.claims, store.consumes)
		}
		var removed, committed bool
		if e = b.pool.QueryRow(b.ctx, `SELECT NOT EXISTS(SELECT 1 FROM agent_context_purpose_previews WHERE id=$1),EXISTS(SELECT 1 FROM agent_consumer_inbox WHERE event_id=$2 AND handler_version=$3 AND control_state='INVALIDATION_COMPLETE')`, unbound.ID, store.claim.EventID, store.claim.HandlerVersion).Scan(&removed, &committed); e != nil || !removed || !committed {
			t.Fatal("observer did not verify actual committed original cleanup", e)
		}
		before := f.history(t)
		receipt, e := b.store.ConsumeAgentOutboxControl(b.ctx, store.claim)
		if e != nil || receipt.EventID != store.claim.EventID || receipt.State != agentoutbox.MemoryComplete || f.history(t) != before {
			t.Fatal("same original claim receipt lookup repeated cleanup", e)
		}
		// The original child is independent remaining work, explicitly handled
		// by a later invocation; the unknown parent is never re-executed.
		f.run(t, 32)
	})
}
