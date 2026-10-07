package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/modelconfiguration"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type configurationFixture struct {
	native   *agentEventFixture
	versions []string
	configs  []modelconfiguration.ResolvedConfiguration
	route    ModelConfigurationRoute
}

func configurationNativeFixture(t *testing.T) *configurationFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("set disposable PostgreSQL for native configuration verification")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("configuration verification requires disposable PostgreSQL")
	}
	f := &configurationFixture{native: eventIntegrationFixture(t)}
	b := f.native.private.base
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{
			`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`,
		} {
			if _, err := b.pool.Exec(ctx, q, b.accounts); err != nil {
				t.Errorf("owned native Task/binding cleanup: %v", err)
			}
		}
		for _, q := range []string{`DELETE FROM model_configuration_routes WHERE version=ANY($1::text[])`, `DELETE FROM model_configuration_versions WHERE version=ANY($1::text[])`, `DELETE FROM model_prompt_versions WHERE version=ANY($1::text[])`, `DELETE FROM model_configuration_policy_versions WHERE version=ANY($1::text[])`} {
			if _, err := b.pool.Exec(ctx, q, f.versions); err != nil {
				t.Errorf("owned unreferenced config cleanup: %v", err)
			}
		}
	})
	for _, suffix := range []string{"v1", "v2"} {
		v := "config_" + strings.ReplaceAll(b.person.ID, "-", "") + "_" + suffix
		f.versions = append(f.versions, v)
		c := modelconfiguration.Configuration{SchemaVersion: modelconfiguration.SchemaVersion, Version: v, TaskKind: modelgateway.ActivityQuery, PromptVersion: v, InputSchemaVersion: "air.messages.v1", OutputSchemaVersion: "air.answer.v1", OutputMode: modelgateway.Structured, ToolAllowlist: []string{}, PolicyVersion: v, CapabilitiesRequired: []string{"text", "structured_output_validatable"}}
		registered, err := b.store.RegisterModelConfiguration(b.ctx, c, modelconfiguration.PromptDefinition{Version: v, Text: "合成配置版本" + suffix + "：只回答本地测试明确数据。"}, modelconfiguration.PolicyVersionReference{Version: v, ArtifactSHA256: strings.Repeat("a", 64)})
		if err != nil {
			t.Fatal("native immutable registry registration", err)
		}
		f.configs = append(f.configs, registered)
	}
	return f
}

func (f *configurationFixture) request(t *testing.T, index int, id string) modelgateway.Request {
	t.Helper()
	b := f.native.private.base
	r := modelgateway.Request{SchemaVersion: modelgateway.RequestVersion, RunID: id, Agent: agentcognitive.AgentReference{AgentID: b.personID, Principal: b.person, Role: agentruntime.PersonalAgent}, ContextSnapshotRef: "76000000-0000-4000-8000-000000000014", DataPolicyRef: "76000000-0000-4000-8000-000000000015", BudgetRef: "76000000-0000-4000-8000-000000000016", Budget: modelgateway.Budget{MaxOutputTokens: 128}, Messages: []modelgateway.Message{{Role: "user", Content: "合成输入，不是真实模型上下文许可"}}, DeadlineAt: time.Now().Add(time.Minute)}
	prepared, _, err := modelconfiguration.PrepareRequest(f.configs[index], r, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func activateConfigurationFixture(t *testing.T, f *configurationFixture, index int, expected int64) ModelConfigurationRoute {
	t.Helper()
	route, err := f.native.private.base.store.ActivateModelConfiguration(f.native.private.base.ctx, f.configs[index].Configuration.Version, expected)
	if err != nil {
		t.Fatal("native version activation", err)
	}
	f.route = route
	return route
}

func TestModelConfigurationNativePersistencePinSwitchAndRollbackIntegration(t *testing.T) {
	f := configurationNativeFixture(t)
	b := f.native.private.base
	route := activateConfigurationFixture(t, f, 0, 0)
	r := f.request(t, 0, "76000000-0000-4000-8000-000000000021")
	old, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, r)
	if err != nil {
		t.Fatal(err)
	}
	if old.TaskID != f.native.task.ID || old.Reference.Agent != r.Agent || old.ExecutionStatus != "UNAVAILABLE" {
		t.Fatal("binding became fake run or changed owner")
	}
	pool, err := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	restarted := New(pool, false)
	persisted, err := restarted.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, r.RunID, true)
	if err != nil || persisted.Reference.ConfigurationFingerprint != old.Reference.ConfigurationFingerprint {
		t.Fatal("new process/store lost exact persisted configuration", err)
	}
	route = activateConfigurationFixture(t, f, 1, route.Revision)
	read, err := b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, r.RunID, false)
	if err != nil || read.Reference.PromptVersion != f.configs[0].Prompt.Version {
		t.Fatal("new prompt rewrote old native reference", err)
	}
	duplicate, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, old.Reference.ConfigurationVersion, 1, r)
	if err != nil || duplicate.Reference.ConfigurationFingerprint != old.Reference.ConfigurationFingerprint {
		t.Fatal("same binding retry changed version after activation", err)
	}
	bad := r
	bad.RunID = "76000000-0000-4000-8000-000000000022"
	if _, err = b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, old.Reference.ConfigurationVersion, 1, bad); !errors.Is(err, modelconfiguration.ErrConflict) {
		t.Fatal("inactive old prompt accepted new binding", err)
	}
	newRequest := f.request(t, 1, bad.RunID)
	current, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, newRequest)
	if err != nil || current.Reference.ConfigurationFingerprint == old.Reference.ConfigurationFingerprint {
		t.Fatal("new allowed binding not new version", err)
	}
	rollback := activateConfigurationFixture(t, f, 0, route.Revision)
	if rollback.Revision != 3 {
		t.Fatal("rollback did not use CAS new route revision")
	}
	newRead, err := b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, newRequest.RunID, false)
	if err != nil || newRead.Reference.ConfigurationVersion != f.configs[1].Configuration.Version {
		t.Fatal("rollback overwrote previous pinned configuration", err)
	}
	var stored []byte
	if err = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(bind) FROM agent_task_model_bindings bind WHERE binding_id=$1`, r.RunID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{f.native.task.Query, f.native.task.Conversation[0].Text, "合成输入", "token_sha256", "messages", "provider", "consent_epoch"} {
		if strings.Contains(string(stored), private) {
			t.Fatal("private task or permission body persisted in new binding")
		}
	}
}

func TestModelConfigurationImmutableArtifactsAndMissingVersionsIntegration(t *testing.T) {
	f := configurationNativeFixture(t)
	b := f.native.private.base
	c := f.configs[0]
	_, err := b.store.RegisterModelConfiguration(b.ctx, c.Configuration, c.Prompt, c.Policy)
	if err != nil {
		t.Fatal("same immutable registration should be idempotent", err)
	}
	changedPrompt := c.Prompt
	changedPrompt.Text = "同名换prompt不能覆盖旧配置"
	if _, err = b.store.RegisterModelConfiguration(b.ctx, c.Configuration, changedPrompt, c.Policy); !errors.Is(err, modelconfiguration.ErrConflict) {
		t.Fatal("prompt mutation permitted", err)
	}
	changedPolicy := c.Policy
	changedPolicy.ArtifactSHA256 = strings.Repeat("b", 64)
	if _, err = b.store.RegisterModelConfiguration(b.ctx, c.Configuration, c.Prompt, changedPolicy); !errors.Is(err, modelconfiguration.ErrConflict) {
		t.Fatal("policy mutation permitted", err)
	}
	changedConfig := c.Configuration
	changedConfig.ToolAllowlist = []string{"activity.detail"}
	if _, err = b.store.RegisterModelConfiguration(b.ctx, changedConfig, c.Prompt, c.Policy); !errors.Is(err, modelconfiguration.ErrConflict) {
		t.Fatal("configuration mutation permitted", err)
	}
	for _, q := range []string{`UPDATE model_prompt_versions SET prompt_text='changed' WHERE version=$1`, `UPDATE model_configuration_policy_versions SET artifact_sha256=repeat('b',64) WHERE version=$1`, `UPDATE model_configuration_versions SET fingerprint=repeat('b',64) WHERE version=$1`} {
		if _, err = b.pool.Exec(b.ctx, q, c.Configuration.Version); err == nil {
			t.Fatal("DB immutable version can update")
		}
	}
	if _, err = b.store.ReadModelConfiguration(b.ctx, "unregistered.v1"); !errors.Is(err, modelconfiguration.ErrMissing) {
		t.Fatal("unknown version not denied", err)
	}
	r := f.request(t, 0, "76000000-0000-4000-8000-000000000023")
	if _, err = b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, c.Configuration.Version, 1, r); !errors.Is(err, modelconfiguration.ErrMissing) {
		t.Fatal("missing activation started task", err)
	}
	route := activateConfigurationFixture(t, f, 0, 0)
	if _, err = b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, r); err != nil {
		t.Fatal(err)
	}
	if _, err = b.pool.Exec(b.ctx, `DELETE FROM model_configuration_versions WHERE version=$1`, route.Version); err == nil {
		t.Fatal("used configuration was deleted")
	}
	if _, err = b.pool.Exec(b.ctx, `DELETE FROM agent_task_model_bindings WHERE binding_id=$1`, r.RunID); err == nil {
		t.Fatal("binding manually deleted while source exists")
	}
	if _, err = b.pool.Exec(b.ctx, `UPDATE agent_task_model_bindings SET configuration_fingerprint=repeat('b',64) WHERE binding_id=$1`, r.RunID); err == nil {
		t.Fatal("binding altered old pinned version")
	}
}

func TestModelConfigurationCurrentSubjectSourceAndSessionIntegration(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*configurationFixture)
		access func(*configurationFixture) agentevent.Access
	}{
		{"anonymous", func(*configurationFixture) {}, func(*configurationFixture) agentevent.Access { return agentevent.Access{} }},
		{"other_person", func(*configurationFixture) {}, func(f *configurationFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.native.private.peer.SessionDigest}
		}},
		{"organization_session", func(*configurationFixture) {}, func(f *configurationFixture) agentevent.Access {
			return agentevent.Access{SessionDigest: f.native.private.org.SessionDigest}
		}},
		{"revoked", func(f *configurationFixture) {
			f.native.private.base.exec(`UPDATE sessions SET revoked_at=now() WHERE id=$1`, f.native.private.ownerSession)
		}, nil},
		{"expired", func(f *configurationFixture) {
			f.native.private.base.exec(`UPDATE sessions SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour',idle_expires_at=now()-interval '1 hour' WHERE id=$1`, f.native.private.ownerSession)
		}, nil},
		{"person_suspended", func(f *configurationFixture) {
			f.native.private.base.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.native.private.base.person.ID)
		}, nil},
		{"agent_retired", func(f *configurationFixture) {
			f.native.private.base.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.native.private.base.personID)
		}, nil},
		{"metadata_missing", func(f *configurationFixture) {
			f.native.private.base.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, f.native.private.base.personID)
		}, nil},
		{"source_failed", func(f *configurationFixture) {
			f.native.private.base.exec(`UPDATE agent_tasks SET status='FAILED' WHERE id=$1`, f.native.task.ID)
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := configurationNativeFixture(t)
			b := f.native.private.base
			route := activateConfigurationFixture(t, f, 0, 0)
			r := f.request(t, 0, "76000000-0000-4000-8000-000000000024")
			tc.mutate(f)
			a := f.native.access
			if tc.access != nil {
				a = tc.access(f)
			}
			out, err := b.store.BindModelTaskConfiguration(b.ctx, a, f.native.task.ID, route.Version, route.Revision, r)
			if !errors.Is(err, modelconfiguration.ErrDenied) || out.TaskID != "" {
				t.Fatal("current invalid identity/source bound", err)
			}
		})
	}
}

func TestModelConfigurationSourceFingerprintAndTimeZoneIntegration(t *testing.T) {
	f := configurationNativeFixture(t)
	b := f.native.private.base
	route := activateConfigurationFixture(t, f, 0, 0)
	r := f.request(t, 0, "76000000-0000-4000-8000-000000000025")
	first, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, r)
	if err != nil {
		t.Fatal(err)
	}
	for _, zone := range []string{"Pacific/Honolulu", "Asia/Shanghai", "Europe/London"} {
		t.Run(zone, func(t *testing.T) {
			config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			config.ConnConfig.RuntimeParams["TimeZone"] = zone
			pool, err := pgxpool.NewWithConfig(b.ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			read, err := New(pool, false).ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, r.RunID, true)
			if err != nil || read.SourceVersion != first.SourceVersion {
				t.Fatal("session timezone changed actual source token", err)
			}
		})
	}
	b.exec(`UPDATE agent_tasks SET conversation=conversation||'[{"role":"user","text":"合成同clock新源内容"}]'::jsonb WHERE id=$1`, f.native.task.ID)
	if _, err = b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, r.RunID, true); !errors.Is(err, modelconfiguration.ErrDenied) {
		t.Fatal("changed same timestamp native source accepted", err)
	}
	if history, err := b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, r.RunID, false); err != nil || history.Reference.ConfigurationVersion != route.Version {
		t.Fatal("historical exact config lost when source changed", err)
	}
	b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, b.personID)
	if _, err = b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, r.RunID, false); !errors.Is(err, modelconfiguration.ErrDenied) {
		t.Fatal("metadata absence restored old binding", err)
	}
}

func TestModelConfigurationConcurrentRetryAndActivationCASIntegration(t *testing.T) {
	f := configurationNativeFixture(t)
	b := f.native.private.base
	route := activateConfigurationFixture(t, f, 0, 0)
	r := f.request(t, 0, "76000000-0000-4000-8000-000000000026")
	var wg sync.WaitGroup
	issues := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			binding, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, r)
			if err != nil {
				issues <- err
			} else if binding.Reference.ConfigurationFingerprint != f.configs[0].Fingerprint {
				issues <- errors.New("concurrent bind changed fixed version")
			}
		}()
	}
	wg.Wait()
	close(issues)
	for err := range issues {
		t.Error(err)
	}
	var count int
	if err := b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_task_model_bindings WHERE task_id=$1`, f.native.task.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate binding persisted", err)
	}
	updated, err := b.store.ActivateModelConfiguration(b.ctx, f.configs[1].Configuration.Version, 1)
	if err != nil || updated.Revision != 2 {
		t.Fatal(err)
	}
	if _, err = b.store.ActivateModelConfiguration(b.ctx, f.configs[0].Configuration.Version, 1); !errors.Is(err, modelconfiguration.ErrConflict) {
		t.Fatal("stale activation overwrote route", err)
	}
	r.Agent.Principal.Type = actorref.Organization
	r.Agent.Role = agentruntime.OrganizationAgent
	if _, err = b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, updated.Version, updated.Revision, r); !errors.Is(err, modelconfiguration.ErrUnavailable) {
		t.Fatal("unsupported org source made up permissions", err)
	}
}

func TestModelConfigurationWaitedLockRechecksCurrentTimeIntegration(t *testing.T) {
	for _, kind := range []string{"session_absolute", "session_idle", "request_deadline"} {
		t.Run(kind, func(t *testing.T) {
			f := configurationNativeFixture(t)
			b := f.native.private.base
			route := activateConfigurationFixture(t, f, 0, 0)
			ctx, cancel := context.WithTimeout(b.ctx, 10*time.Second)
			defer cancel()
			config, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if err != nil {
				t.Fatal(err)
			}
			application := "air027_wait_" + strings.ReplaceAll(b.person.ID, "-", "")
			config.ConnConfig.RuntimeParams["application_name"] = application
			config.MaxConns = 1
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if err = pool.Ping(ctx); err != nil {
				t.Fatal(err)
			}
			r := f.request(t, 0, "76000000-0000-4000-8000-000000000031")
			cutoff := time.Now().Add(1200 * time.Millisecond)
			if kind == "request_deadline" {
				r.DeadlineAt = cutoff
			} else if kind == "session_absolute" {
				b.exec(`UPDATE sessions SET expires_at=$2,idle_expires_at=$2 WHERE id=$1`, f.native.private.ownerSession, cutoff)
			} else {
				b.exec(`UPDATE sessions SET idle_expires_at=$2 WHERE id=$1`, f.native.private.ownerSession, cutoff)
			}
			blocker, err := b.pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer blocker.Rollback(context.Background())
			if _, err = blocker.Exec(ctx, `SELECT id FROM agent_tasks WHERE id=$1 FOR UPDATE`, f.native.task.ID); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				out, e := New(pool, false).BindModelTaskConfiguration(ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, r)
				if e == nil || out.TaskID != "" {
					done <- errors.New("late time-bound binding succeeded")
				} else {
					done <- e
				}
			}()
			for {
				var blocked bool
				if err = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock')`, application).Scan(&blocked); err != nil {
					t.Fatal(err)
				}
				if blocked {
					break
				}
				select {
				case e := <-done:
					t.Fatal("binding did not actually wait for the source lock", e)
				case <-ctx.Done():
					t.Fatal("wait observation timed out")
				case <-time.After(10 * time.Millisecond):
				}
			}
			for {
				var now time.Time
				if err = b.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
					t.Fatal(err)
				}
				if now.After(cutoff.Add(25 * time.Millisecond)) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if err = blocker.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			e := <-done
			expected := modelconfiguration.ErrDenied
			if kind == "request_deadline" {
				expected = modelconfiguration.ErrInvalid
			}
			if !errors.Is(e, expected) {
				t.Fatal("late boundary was not enforced", e, expected)
			}
			var count int
			if err = b.pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_model_bindings WHERE task_id=$1`, f.native.task.ID).Scan(&count); err != nil || count != 0 {
				t.Fatal("late invalid binding persisted", err, count)
			}
		})
	}
}

func TestModelConfigurationCurrentBindingCannotGrantOrRestoreIntegration(t *testing.T) {
	f := configurationNativeFixture(t)
	b := f.native.private.base
	route := activateConfigurationFixture(t, f, 0, 0)
	r := f.request(t, 0, "76000000-0000-4000-8000-000000000032")
	wrong := r
	wrong.Agent.AgentID = "76000000-0000-4000-8000-000000000039"
	if _, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, wrong); !errors.Is(err, modelconfiguration.ErrDenied) {
		t.Fatal("wrong exact Agent accepted", err)
	}
	if _, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, "76000000-0000-4000-8000-000000000033", route.Version, route.Revision, r); !errors.Is(err, modelconfiguration.ErrDenied) {
		t.Fatal("missing source invented", err)
	}
	if _, err := b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, route.Version, route.Revision, r); err != nil {
		t.Fatal(err)
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.native.private.ownerSession)
	for _, validate := range []bool{false, true} {
		if _, err := b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, r.RunID, validate); !errors.Is(err, modelconfiguration.ErrDenied) {
			t.Fatal("revoked session regained history/current metadata", err)
		}
	}
}

func TestModelConfigurationStorageClosedShapeAndRouteRevisionIntegration(t *testing.T) {
	f := configurationNativeFixture(t)
	b := f.native.private.base
	c := f.configs[0]
	raw, _ := json.Marshal(c.Configuration)
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"null", `null`}, {"array", `[]`}, {"scalar", `1`}, {"unknown", strings.Replace(string(raw), `"version":`, `"confirmed":true,"version":`, 1)},
		{"null_tools", strings.Replace(string(raw), `"tool_allowlist":[]`, `"tool_allowlist":null`, 1)},
		{"bad_kind", strings.Replace(string(raw), `ACTIVITY_QUERY`, `VISION`, 1)},
		{"unknown_tool", strings.Replace(string(raw), `"tool_allowlist":[]`, `"tool_allowlist":["send.email"]`, 1)},
		{"bad_caps", strings.Replace(string(raw), `structured_output_validatable`, `vision`, 1)},
		{"bool_version", strings.Replace(string(raw), `"version":"`+c.Configuration.Version+`"`, `"version":true`, 1)},
		{"bool_policy", strings.Replace(string(raw), `"policy_version":"`+c.Configuration.PolicyVersion+`"`, `"policy_version":true`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := b.pool.Begin(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(b.ctx)
			var valid bool
			err = tx.QueryRow(b.ctx, `SELECT birdtie_model_configuration_valid($1::jsonb)`, tc.raw).Scan(&valid)
			if err == nil && valid {
				t.Fatal("DB accepted invalid closed JSON", tc.raw)
			}
		})
	}
	if _, err := b.pool.Exec(b.ctx, `INSERT INTO model_configuration_routes(task_kind,output_mode,version,revision) VALUES($1,$2,$3,7)`, c.Configuration.TaskKind, c.Configuration.OutputMode, c.Configuration.Version); err == nil {
		t.Fatal("initial route arbitrary revision")
	}
	route := activateConfigurationFixture(t, f, 0, 0)
	for _, q := range []string{`UPDATE model_configuration_routes SET revision=revision+1 WHERE version=$1 AND $2::text IS NOT NULL`, `UPDATE model_configuration_routes SET version=$2 WHERE version=$1`, `UPDATE model_configuration_routes SET version=$2,revision=revision+2 WHERE version=$1`} {
		_, err := b.pool.Exec(b.ctx, q, route.Version, f.configs[1].Configuration.Version)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "P0001" {
			t.Fatal("route did not actually reject by revision trigger", q, err)
		}
	}
}

func TestModelConfigurationNonemptyDownProtectsNativeBindingsIntegration(t *testing.T) {
	f := configurationNativeFixture(t)
	b := f.native.private.base
	down, err := os.ReadFile("../../migrations/058_model_configuration_registry.down.sql")
	if err != nil {
		t.Fatal("read actual production rollback migration", err)
	}
	for _, state := range []string{"artifacts", "active_route", "native_binding"} {
		t.Run(state, func(t *testing.T) {
			if state == "active_route" {
				f.route = activateConfigurationFixture(t, f, 0, 0)
			}
			if state == "native_binding" {
				r := f.request(t, 0, "76000000-0000-4000-8000-000000000040")
				if _, err = b.store.BindModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, f.route.Version, f.route.Revision, r); err != nil {
					t.Fatal(err)
				}
			}
			conn, err := b.pool.Acquire(b.ctx)
			if err != nil {
				t.Fatal(err)
			}
			_, err = conn.Exec(b.ctx, string(down))
			var pgerr *pgconn.PgError
			guarded := errors.As(err, &pgerr) && pgerr.Code == "P0001"
			_, rollbackErr := conn.Exec(b.ctx, `ROLLBACK`)
			conn.Release()
			if !guarded || rollbackErr != nil {
				t.Fatal("actual rollback discarded nonempty registry", err, rollbackErr)
			}
			if _, err = b.store.ReadModelConfiguration(b.ctx, f.configs[0].Configuration.Version); err != nil {
				t.Fatal("denied rollback removed existing artifact", err)
			}
			if state == "native_binding" {
				if _, err = b.store.ReadPinnedModelTaskConfiguration(b.ctx, f.native.access, f.native.task.ID, "76000000-0000-4000-8000-000000000040", true); err != nil {
					t.Fatal("denied rollback lost native binding", err)
				}
			}
			t.Logf("actual production down refused with SQLSTATE=%s; existing %s preserved", pgerr.Code, state)
		})
	}
}
