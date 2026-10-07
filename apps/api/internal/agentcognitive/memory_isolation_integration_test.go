package agentcognitive_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Observation wrapper delegates to the real native Store. It is not an
// authorization resolver. CurrentDomainStore does not expose this Memory method.
type isolationObservedStore struct {
	*countedFeatureStore
	memoryReads int
}

func (s *isolationObservedStore) ReadOwnMemories(ctx context.Context, access agentprofile.PrivateAccess) ([]agentmemory.Record, error) {
	s.memoryReads++
	return s.store.ReadOwnMemories(ctx, access)
}

func TestMemoryIsolationActualFeatureGatedDomainsWithNativeCanary(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires explicitly owned disposable PostgreSQL")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store := postgres.New(pool, false)
	accounts := []string{}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, sql := range []string{
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`,
			`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`,
			`DELETE FROM organizations WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			if _, e := pool.Exec(cleanup, sql, accounts); e != nil {
				t.Errorf("owned cognitive fixture cleanup: %v", e)
			}
		}
		var count int
		if e := pool.QueryRow(cleanup, `SELECT (SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[]))+(SELECT count(*) FROM audit_events WHERE actor_account_id=ANY($1::uuid[]))`, accounts).Scan(&count); e != nil || count != 0 {
			t.Errorf("owned cognitive fixture residue=%d error=%v", count, e)
		}
	})
	refs := []agentcognitive.AgentReference{}
	accesses := []agentprofile.PrivateAccess{}
	var orgID string
	for _, kind := range []string{"person", "person", "organization", "business"} {
		var id, agentID string
		if err = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),$1) RETURNING id`, kind).Scan(&id); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, id)
		principal, e := actorref.ParsePrincipal(kind, id)
		if e != nil {
			t.Fatal(e)
		}
		if kind == "organization" {
			if err = pool.QueryRow(ctx, `INSERT INTO organizations(account_id,organization_type,name) VALUES($1,'club','合成060组织') RETURNING id`, id).Scan(&orgID); err != nil {
				t.Fatal(err)
			}
		}
		agentKind, status := "personal", "active"
		if kind == "organization" {
			agentKind = "organization"
		}
		if kind == "business" {
			agentKind, status = "business", "suspended"
		}
		if err = pool.QueryRow(ctx, `INSERT INTO agents(agent_type,principal_account_id,status) VALUES($1,$2,$3) RETURNING id`, agentKind, id, status).Scan(&agentID); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, agentcognitive.AgentReference{AgentID: agentID, Principal: principal, Role: agentruntime.ForType(principal.Type).Role})
		_, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, err = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, id, digest[:]); err != nil {
			t.Fatal(err)
		}
		accesses = append(accesses, agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: principal})
	}
	if orgID == accounts[2] {
		t.Fatal("Organization entity/principal namespaces merged")
	}
	var memoryID, requestID, taskID string
	if err = pool.QueryRow(ctx, `SELECT gen_random_uuid(),gen_random_uuid(),gen_random_uuid()`).Scan(&memoryID, &requestID, &taskID); err != nil {
		t.Fatal(err)
	}
	// Task/request UUIDs are shape references only, not a fabricated persisted task
	// or current task authorization. The actual cognition service stays unavailable.
	const canary = "合成060真实持久Memory认知不得读取"
	memory, err := store.PutOwnMemory(ctx, accesses[0], memoryID, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "cognitive-isolation-060", Summary: canary, StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Truncate(time.Microsecond).Add(time.Hour)})
	if err != nil || memory.OwnerID != accounts[0] || memory.SourceType != agentmemory.SourceExplicit {
		t.Fatal("real native canary save failed")
	}
	var auditBefore string
	if err = pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb)::text FROM audit_events a WHERE actor_account_id=ANY($1::uuid[])`, accounts).Scan(&auditBefore); err != nil {
		t.Fatal(err)
	}
	var auditRows []map[string]any
	if json.Unmarshal([]byte(auditBefore), &auditRows) != nil || len(auditRows) != 1 {
		t.Fatal("explicit canary must have exactly one minimal audit")
	}
	a := auditRows[0]
	if a["actor_account_id"] != accounts[0] || a["action"] != "put" || a["resource_type"] != "agent_memory" || a["resource_id"] != memoryID || a["purpose"] != "human_explicit_memory_edit" || a["request_id"] != nil || a["decision"] != "allowed" || a["target_resource_id"] != nil {
		t.Fatal("explicit canary audit attribution wrong")
	}
	for _, v := range a {
		if v == canary {
			t.Fatal("private canary leaked in audit")
		}
	}
	observed := &isolationObservedStore{countedFeatureStore: &countedFeatureStore{store: store}}
	native, err := observed.ReadOwnMemories(ctx, accesses[0])
	if err != nil || len(native) != 1 || native[0].Summary != canary {
		t.Fatal("actual native owner positive control failed")
	}
	observed.memoryReads = 0
	for _, enabled := range []bool{false, true} {
		configBody, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion, "flags": map[string]bool{"agent_enrichment": enabled, "agent_memory": enabled, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false}, "pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
		config, e := agentfeature.ParseConfig(configBody)
		if e != nil {
			t.Fatal(e)
		}
		controller, e := agentfeature.NewController(config)
		if e != nil {
			t.Fatal(e)
		}
		domains, e := agentcognitive.NewFeatureGatedDomains(controller, observed)
		if e != nil {
			t.Fatal(e)
		}
		for i, name := range []string{"owner_person", "other_person", "active_organization", "reserved_suspended_business"} {
			mode := "OFF"
			if enabled {
				mode = "ON"
			}
			t.Run(mode+"/"+name, func(t *testing.T) {
				request := agentcognitive.ReadRequest{Version: agentcognitive.ContractVersion, RequestID: requestID, TaskID: taskID, Agent: refs[i], Purpose: agentcognitive.ReadMemory, Scope: agentruntime.Private, Source: agentcognitive.SourceReference{Type: agentcognitive.Memory, ID: memory.ID, Owner: refs[0].Principal, Version: memory.Version}, ExpiresAt: time.Now().UTC().Add(time.Minute)}
				view, readErr := domains.ReadMemory(ctx, request)
				if !errors.Is(readErr, agentcognitive.ErrUnavailable) || !reflect.DeepEqual(view, agentcognitive.KnowledgeView{}) || observed.memoryReads != 0 || observed.auth+observed.profiles+observed.contexts != 0 {
					t.Fatal("actual cognition service read native Memory or released private data")
				}
			})
		}
	}
	t.Run("offline_runtime_private_isolation_never_inherits_close_grant", func(t *testing.T) {
		// These server-fact booleans are explicitly a pure contract probe. They are
		// not current source/session/membership facts for the real native service.
		for i := 1; i < len(refs); i++ {
			policy := agentruntime.ForType(refs[i].Principal.Type)
			decision := policy.DecideContext(agentruntime.AccessRequest{Scope: agentruntime.Private, OwnerID: accounts[0], ViewerID: accounts[1], PrincipalID: refs[i].Principal.ID, AuthorityVerified: true, Released: true, Relationship: agentruntime.VerifiedClose, ExplicitGrant: true})
			if decision.Allowed {
				t.Fatal("OfflineContract close grant/location context granted PRIVATE Memory")
			}
		}
	})
	if native, err = store.ReadOwnMemories(ctx, accesses[0]); err != nil || len(native) != 1 || !reflect.DeepEqual(native[0], memory) {
		t.Fatal("actual gate matrix changed native Memory")
	}
	var auditAfter string
	if err = pool.QueryRow(ctx, `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]'::jsonb)::text FROM audit_events a WHERE actor_account_id=ANY($1::uuid[])`, accounts).Scan(&auditAfter); err != nil || auditBefore != auditAfter {
		t.Fatal("cognition/read matrix appended or changed audit")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY: native persistent canary exists; actual FeatureGatedDomains OFF/ON returns empty Unavailable without native Memory/current domain reads; no authorized cognition task/resolver manufactured")
}
