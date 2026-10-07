package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelrequestrun"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This receipt locates immutable configuration only. It contains neither a
// private Task query nor a reusable native handle, controller Ticket or answer.
type modelRunConfigurationReceipt struct {
	PID          int
	RunID        string
	TaskID       string
	BindingID    string
	Reference    modelconfiguration.RunReference
	Definition   modelconfiguration.Configuration
	PromptSHA256 string
	Policy       modelconfiguration.PolicyVersionReference
	Fingerprint  string
}

func readModelRunConfigurationReceipt(ctx context.Context, s *Store, access agentevent.Access, id string) (modelRunConfigurationReceipt, error) {
	var out modelRunConfigurationReceipt
	run, e := s.ReadOwnLocalModelRun(ctx, access, id)
	if e != nil {
		return out, e
	}
	binding, e := s.ReadPinnedModelTaskConfiguration(ctx, access, run.TaskID, run.BindingID, false)
	if e != nil {
		return out, e
	}
	config, e := s.ReadModelConfiguration(ctx, binding.Reference.ConfigurationVersion)
	if e != nil {
		return out, e
	}
	if run.ModelRunID == run.BindingID || binding.Reference.RunID != run.BindingID || binding.TaskID != run.TaskID {
		return out, modelconfiguration.ErrDenied
	}
	out = modelRunConfigurationReceipt{PID: os.Getpid(), RunID: run.ModelRunID, TaskID: run.TaskID, BindingID: run.BindingID, Reference: binding.Reference, Definition: config.Configuration, PromptSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(config.Prompt.Text))), Policy: config.Policy, Fingerprint: config.Fingerprint}
	return out, nil
}

func assertModelRunConfiguration(t *testing.T, f *egressFixture, id, binding string, index int, r modelRunConfigurationReceipt) {
	t.Helper()
	expected := f.f.configs[index]
	if r.RunID != id || r.BindingID != binding || r.TaskID != f.f.native.task.ID || r.Reference.RunID != binding || r.Reference.Agent.AgentID != f.f.native.private.base.personID {
		t.Fatal("Run/binding/Task/native Agent identity changed")
	}
	if !reflect.DeepEqual(r.Definition, expected.Configuration) || r.Fingerprint != expected.Fingerprint || r.Reference.ConfigurationFingerprint != expected.Fingerprint || r.Reference.ConfigurationVersion != expected.Configuration.Version || r.PromptSHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte(expected.Prompt.Text))) || r.Policy != expected.Policy {
		t.Fatal("historical central configuration/artifact mismatch")
	}
	c := expected.Configuration
	if r.Reference.PromptVersion != c.PromptVersion || r.Reference.InputSchemaVersion != c.InputSchemaVersion || r.Reference.OutputSchemaVersion != c.OutputSchemaVersion || r.Reference.PolicyVersion != c.PolicyVersion || r.Reference.TaskKind != c.TaskKind || r.Reference.OutputMode != c.OutputMode || !reflect.DeepEqual(r.Reference.ToolAllowlist, c.ToolAllowlist) || !reflect.DeepEqual(r.Reference.CapabilitiesRequired, c.CapabilitiesRequired) {
		t.Fatal("incomplete historical prompt/schema/policy/tools/capability pin")
	}
}

func TestModelRequestRunConfigurationNativeOSChild(t *testing.T) {
	if os.Getenv("BIRDTIE_RUN_CONFIG_CHILD") != "1" {
		return
	}
	var in struct {
		Digest [32]byte
		IDs    []string
	}
	if e := json.NewDecoder(os.Stdin).Decode(&in); e != nil {
		t.Fatal("child input", e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	out := make([]modelRunConfigurationReceipt, 0, len(in.IDs))
	for _, id := range in.IDs {
		r, e := readModelRunConfigurationReceipt(ctx, s, agentevent.Access{SessionDigest: in.Digest}, id)
		if e != nil {
			t.Fatal(e)
		}
		out = append(out, r)
	}
	wire, e := json.Marshal(out)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(os.Getenv("BIRDTIE_RUN_CONFIG_OUTPUT"), wire, 0600); e != nil {
		t.Fatal(e)
	}
}

func modelRunConfigurationOSRead(t *testing.T, f *egressFixture, ids []string) []modelRunConfigurationReceipt {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	output := filepath.Join(t.TempDir(), "configuration-metadata.json")
	input, e := json.Marshal(struct {
		Digest [32]byte
		IDs    []string
	}{f.f.native.access.SessionDigest, ids})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestModelRequestRunConfigurationNativeOSChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), "BIRDTIE_RUN_CONFIG_CHILD=1", "BIRDTIE_RUN_CONFIG_OUTPUT="+output)
	cmd.Stdin = bytes.NewReader(input)
	processOutput, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("actual OS metadata reader failed: %v %s", e, processOutput)
	}
	executableBytes, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	wire, e := os.ReadFile(output)
	if e != nil {
		t.Fatal(e)
	}
	var out []modelRunConfigurationReceipt
	if e = json.Unmarshal(wire, &out); e != nil || len(out) != len(ids) {
		t.Fatal("child receipt shape", e)
	}
	if out[0].PID == os.Getpid() {
		t.Fatal("same process is not OS restart")
	}
	t.Logf("actual OS metadata-only process PID=%d exit=0 runs=%d executableSHA256=%x rawProcessOutput=%q no dispatch or private answer recovery", out[0].PID, len(out), sha256.Sum256(executableBytes), processOutput)
	t.Logf("CONFIGURATION_METADATA_ONLY %s", wire)
	return out
}

func modelRunConfigurationNextBinding(t *testing.T, f *egressFixture, index int, route ModelConfigurationRoute) {
	t.Helper()
	b := f.f.native.private.base
	f.binding = egressID(t, f)
	request := f.f.request(t, index, f.binding)
	if _, e := b.store.BindModelTaskConfiguration(b.ctx, f.f.native.access, f.f.native.task.ID, route.Version, route.Revision, request); e != nil {
		t.Fatal("new explicit configuration binding", e)
	}
	f.root = egressID(t, f)
	var now time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	if e := b.store.CreateOwnModelBudgetRoot(b.ctx, f.f.native.access, modelegressbudget.RootInput{RootTraceID: f.root, TaskID: f.f.native.task.ID, BindingID: f.binding, Currency: "GBP", Limits: f.limits, ExpiresAt: now.Add(10 * time.Minute)}); e != nil {
		t.Fatal("new explicit original budget root", e)
	}
}

func TestModelRequestRunConfigurationNativeVersionSwitchOSRestartRollback(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newEgressFixture(t, 8)
	b := f.f.native.private.base
	ids := []string{}
	bindingIDs := []string{}
	create := func(index int) {
		p := f.preview(t, f.f.native.task.ID, true)
		id := modelRunID(t, f)
		_, run := modelRunPlan(t, f, id, modelRunBindings(t, f, p, 1))
		if run.BindingID != f.binding || run.State != modelrequestrun.RunPlanned {
			t.Fatal("actual model run creation missing original binding")
		}
		ids = append(ids, id)
		bindingIDs = append(bindingIDs, f.binding)
		r, e := readModelRunConfigurationReceipt(b.ctx, b.store, f.f.native.access, id)
		if e != nil {
			t.Fatal(e)
		}
		assertModelRunConfiguration(t, f, id, f.binding, index, r)
	}
	create(0)
	firstBefore := modelRunConfigurationOriginalRows(t, f, ids[0])
	route := activateConfigurationFixture(t, f.f, 1, f.f.route.Revision)
	modelRunConfigurationNextBinding(t, f, 1, route)
	create(1)
	if modelRunConfigurationOriginalRows(t, f, ids[0]) != firstBefore {
		t.Fatal("activation/new Run changed old Run or original budget rows")
	}
	priorChildPID := 0
	for round := 0; round < 2; round++ {
		receipts := modelRunConfigurationOSRead(t, f, ids)
		if receipts[0].PID == priorChildPID {
			t.Fatal("OS readers did not have distinct process IDs")
		}
		priorChildPID = receipts[0].PID
		for i, r := range receipts {
			assertModelRunConfiguration(t, f, ids[i], bindingIDs[i], i, r)
		}
	}
	route = activateConfigurationFixture(t, f.f, 0, route.Revision)
	modelRunConfigurationNextBinding(t, f, 0, route)
	create(0)
	for i, id := range ids {
		index := 0
		if i == 1 {
			index = 1
		}
		r, e := readModelRunConfigurationReceipt(b.ctx, b.store, f.f.native.access, id)
		if e != nil {
			t.Fatal(e)
		}
		assertModelRunConfiguration(t, f, id, bindingIDs[i], index, r)
	}
	retryAssertOriginalBudgets(t, f, 0)
	for _, id := range ids {
		var n int
		if e := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM model_budget_reservations WHERE operation_id IN (SELECT operation_id FROM model_request_run_steps WHERE run_id=$1)`, id).Scan(&n); e != nil || n != 0 {
			t.Fatal("metadata created model dispatch/reservation", e, n)
		}
	}
	t.Log("R1=v1 R2=v2 two actual OS metadata readers rollback R3=v1; immutable reference preserved; adapter calls=0")
}

func TestModelRequestRunConfigurationNativeHistoryCannotAuthorizeChangedTask(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	bindings := modelRunBindings(t, f, p, 1)
	id := modelRunID(t, f)
	h, _ := modelRunPlan(t, f, id, bindings)
	before := modelRunAccountingSnapshot(t, f, id)
	b.exec(`UPDATE agent_tasks SET query=query||' 合成源已经改变',updated_at=clock_timestamp() WHERE id=$1`, f.f.native.task.ID)
	r, e := readModelRunConfigurationReceipt(b.ctx, b.store, f.f.native.access, id)
	if e != nil {
		t.Fatal("current owner lost retained metadata", e)
	}
	assertModelRunConfiguration(t, f, id, f.binding, 0, r)
	if _, e = b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.f.native.access, f.f.native.task.ID, f.binding, true); !errors.Is(e, modelconfiguration.ErrDenied) {
		t.Fatal("history restored current source authority", e)
	}
	if _, e = h.ReserveOwnModelAttempt(b.ctx, f.f.native.access, bindings[0].Input, f.gate); !errors.Is(e, modelegressbudget.ErrDenied) {
		t.Fatal("configuration metadata authorized stale model execution", e)
	}
	if modelRunAccountingSnapshot(t, f, id) != before {
		t.Fatal("historical read/stale execution changed original accounting")
	}
	wire, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{f.f.native.task.Query, "合成源已经改变", "token_sha256", `"messages":`, `"answer":`, `"ticket":`} {
		if strings.Contains(string(wire), secret) {
			t.Fatal("historical configuration receipt contains private query/result/claim")
		}
	}
	if _, e = json.Marshal(h); !errors.Is(e, modelegressbudget.ErrServerOnly) {
		t.Fatal("original native handle became recoverable JSON", e)
	}
}

func TestModelRequestRunConfigurationNativeMissingVersionsAndImmutableReferences(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newEgressFixture(t, 4)
	b := f.f.native.private.base
	p := f.preview(t, f.f.native.task.ID, true)
	bindings := modelRunBindings(t, f, p, 1)
	id := modelRunID(t, f)
	modelRunPlan(t, f, id, bindings)
	before := modelRunAccountingSnapshot(t, f, id)
	for _, kind := range []string{"unknownVersion", "unsupportedCapability", "unknownInputSchema", "unknownOutputSchema", "missingPrompt", "missingPolicy"} {
		t.Run(kind, func(t *testing.T) {
			c := f.f.configs[0].Configuration
			c.Version = "invalid_" + strings.ReplaceAll(egressID(t, f), "-", "")
			prompt := f.f.configs[0].Prompt
			policy := f.f.configs[0].Policy
			switch kind {
			case "unknownVersion":
				request := f.f.request(t, 0, egressID(t, f))
				if _, e := b.store.BindModelTaskConfiguration(b.ctx, f.f.native.access, f.f.native.task.ID, c.Version, f.f.route.Revision, request); e == nil {
					t.Fatal("unknown configuration bound")
				}
				return
			case "unsupportedCapability":
				c.CapabilitiesRequired = []string{"unknown_capability"}
			case "unknownInputSchema":
				c.InputSchemaVersion = "unknown.input.v1"
			case "unknownOutputSchema":
				c.OutputSchemaVersion = "unknown.output.v1"
			case "missingPrompt":
				prompt.Version = "missing_prompt"
				prompt.Text = ""
			case "missingPolicy":
				policy.Version = "missing_policy"
				policy.ArtifactSHA256 = ""
			}
			if _, e := b.store.RegisterModelConfiguration(b.ctx, c, prompt, policy); e == nil {
				t.Fatal("missing/unsupported artifact accepted")
			}
		})
	}
	if modelRunAccountingSnapshot(t, f, id) != before {
		t.Fatal("invalid registry operation changed original Run/Step/reservation/four budgets/audit")
	}
	ticket, e := f.gate.Capture(agentfeature.Enrichment)
	if e != nil {
		t.Fatal(e)
	}
	bad := append([]modelegressbudget.LocalRetryBinding(nil), bindings...)
	bad[0].Input.OperationID = egressID(t, f)
	bad[0].Input.PreviewID = egressID(t, f)
	if h, c, e := b.store.CreateOwnLocalModelRun(b.ctx, f.f.native.access, egressID(t, f), bad, f.gate, ticket); e == nil || h != nil || c.ModelRunID != "" {
		t.Fatal("unknown unapproved preview created callable Run", e)
	}
	if modelRunAccountingSnapshot(t, f, id) != before {
		t.Fatal("invalid actual Run creation wrote accounting")
	}
	for _, q := range []string{
		`UPDATE model_prompt_versions SET prompt_text=prompt_text||'changed' WHERE version=$1`,
		`UPDATE model_configuration_policy_versions SET artifact_sha256=repeat('b',64) WHERE version=$1`,
		`UPDATE model_configuration_versions SET definition=definition||'{"prompt_version":"changed"}'::jsonb WHERE version=$1`,
		`DELETE FROM model_configuration_versions WHERE version=$1`,
		`DELETE FROM model_prompt_versions WHERE version=$1`,
		`DELETE FROM model_configuration_policy_versions WHERE version=$1`,
	} {
		_, e := b.pool.Exec(b.ctx, q, f.f.configs[0].Configuration.Version)
		var pg *pgconn.PgError
		if !errors.As(e, &pg) || (pg.Code != "P0001" && pg.Code != "23503") {
			t.Fatal("referenced configuration mutation/delete did not fail closed", e)
		}
	}
	for _, q := range []string{`UPDATE agent_task_model_bindings SET configuration_version=configuration_version WHERE binding_id=$1`, `DELETE FROM agent_task_model_bindings WHERE binding_id=$1`} {
		if _, e := b.pool.Exec(b.ctx, q, f.binding); e == nil {
			t.Fatal("immutable original binding changed/deleted")
		}
	}
	r, e := readModelRunConfigurationReceipt(b.ctx, b.store, f.f.native.access, id)
	if e != nil {
		t.Fatal(e)
	}
	assertModelRunConfiguration(t, f, id, f.binding, 0, r)
	if modelRunAccountingSnapshot(t, f, id) != before {
		t.Fatal("failed direct SQL changed original accounting")
	}
}

func TestModelRequestRunConfigurationNativeOwnerSessionBoundary(t *testing.T) {
	for _, kind := range []string{"anonymous", "foreignOwner", "revoked", "absoluteExpiry", "idleExpiry"} {
		t.Run(kind, func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			id := modelRunID(t, f)
			modelRunPlan(t, f, id, modelRunBindings(t, f, p, 1))
			before := modelRunAccountingSnapshot(t, f, id)
			a := f.f.native.access
			switch kind {
			case "anonymous":
				a.SessionDigest = [32]byte{}
			case "foreignOwner":
				a.SessionDigest = f.f.native.private.peer.SessionDigest
			case "revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.f.native.private.ownerSession)
			case "absoluteExpiry":
				b.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp()-interval '1 second' x) UPDATE sessions SET created_at=stamp.x-interval '1 hour',expires_at=stamp.x,idle_expires_at=stamp.x FROM stamp WHERE id=$1`, f.f.native.private.ownerSession)
			case "idleExpiry":
				b.exec(`WITH stamp AS MATERIALIZED(SELECT clock_timestamp()-interval '1 second' x) UPDATE sessions SET created_at=stamp.x-interval '1 hour',idle_expires_at=stamp.x FROM stamp WHERE id=$1`, f.f.native.private.ownerSession)
			}
			if r, e := readModelRunConfigurationReceipt(b.ctx, b.store, a, id); e == nil || r.RunID != "" {
				t.Fatal("wrong current actor received configuration history", e)
			}
			if modelRunAccountingSnapshot(t, f, id) != before {
				t.Fatal("rejected history changed Run/Step/accounting")
			}
		})
	}
}

func modelRunConfigurationOriginalRows(t *testing.T, f *egressFixture, id string) string {
	t.Helper()
	b := f.f.native.private.base
	var out string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('run',to_jsonb(r)||jsonb_build_object('_xmin',r.xmin::text),'steps',(SELECT jsonb_agg(to_jsonb(s)||jsonb_build_object('_xmin',s.xmin::text) ORDER BY ordinal) FROM model_request_run_steps s WHERE run_id=r.id),'root',(SELECT to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text) FROM model_budget_roots x WHERE root_trace_id=r.root_trace_id),'task',(SELECT jsonb_agg(to_jsonb(x)||jsonb_build_object('_xmin',x.xmin::text)) FROM model_budget_tasks x WHERE root_trace_id=r.root_trace_id))::text FROM model_request_runs r WHERE r.id=$1`, id).Scan(&out)
	if e != nil {
		t.Fatal(e)
	}
	return out
}

func TestModelRequestRunConfigurationNativeWaitedHistoryExpiryAndRevoke(t *testing.T) {
	for _, kind := range []string{"expiry", "revoke"} {
		t.Run(kind, func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := newEgressFixture(t, 4)
			b := f.f.native.private.base
			p := f.preview(t, f.f.native.task.ID, true)
			id := modelRunID(t, f)
			modelRunPlan(t, f, id, modelRunBindings(t, f, p, 1))
			before := modelRunAccountingSnapshot(t, f, id)
			ctx, cancel := context.WithTimeout(b.ctx, 12*time.Second)
			defer cancel()
			config, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			application := "air027_run_wait_" + strings.ReplaceAll(id, "-", "")
			config.ConnConfig.RuntimeParams["application_name"] = application
			config.MaxConns = 1
			pool, e := pgxpool.NewWithConfig(ctx, config)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			if e = pool.Ping(ctx); e != nil {
				t.Fatal(e)
			}
			var cutoff time.Time
			if kind == "expiry" {
				if e = b.pool.QueryRow(ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING idle_expires_at`, f.f.native.private.ownerSession).Scan(&cutoff); e != nil {
					t.Fatal(e)
				}
			}
			hold, e := b.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer hold.Rollback(context.Background())
			if kind == "revoke" {
				if _, e = hold.Exec(ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.f.native.private.ownerSession); e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e = hold.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "birdtie.model-budget.owner:"+b.person.ID); e != nil {
					t.Fatal(e)
				}
			}
			var holder int
			if e = hold.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			consumed := false
			defer func() {
				cancel()
				hold.Rollback(context.Background())
				if !consumed {
					select {
					case <-done:
					case <-time.After(time.Second):
					}
				}
			}()
			go func() {
				r, e := readModelRunConfigurationReceipt(ctx, New(pool, false), f.f.native.access, id)
				if e == nil || r.RunID != "" {
					e = errors.New("waiting history released payload")
				}
				done <- e
			}()
			for {
				var blocked bool
				e = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND $2=ANY(pg_blocking_pids(pid)) AND (($3='expiry' AND query LIKE '%pg_advisory_xact_lock%') OR ($3='revoke' AND query LIKE '%FROM sessions ses%')))`, application, holder, kind).Scan(&blocked)
				if e != nil {
					t.Fatal(e)
				}
				if blocked {
					break
				}
				select {
				case e := <-done:
					consumed = true
					t.Fatal("did not reach exact native barrier", e)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(5 * time.Millisecond):
				}
			}
			if kind == "revoke" {
				if _, e = hold.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.f.native.private.ownerSession); e != nil {
					t.Fatal(e)
				}
				if e = hold.Commit(ctx); e != nil {
					t.Fatal(e)
				}
			} else {
				for {
					var now time.Time
					if e = b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
						t.Fatal(e)
					}
					if !now.Before(cutoff) {
						break
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-time.After(5 * time.Millisecond):
					}
				}
				if e = hold.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
			}
			select {
			case e = <-done:
				consumed = true
				if !errors.Is(e, modelegressbudget.ErrDenied) {
					t.Fatal("current Session after actual wait did not reject", e)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if modelRunAccountingSnapshot(t, f, id) != before {
				t.Fatal("late history denial changed accounting")
			}
			t.Logf("exact reader application=%s blockerPID=%d current %s rejected after real native wait", application, holder, kind)
		})
	}
}
