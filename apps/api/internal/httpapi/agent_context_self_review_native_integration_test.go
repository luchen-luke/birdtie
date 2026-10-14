package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const selfReviewNativeHidden = "SELF_REVIEW_UNSELECTED_PRIVATE_CANARY"

// The adapter never supplies an identity, authority, source version or context.
// Both reads and Resolve delegate to the actual Store. A second-read hook only
// changes this fixture's committed native records. In the registered handler,
// this read is reached after the entire private DTO has been JSON-encoded.
type selfReviewNativeCatalog struct {
	*postgres.Store
	calls, resolves int
	requests        []acb.Request
	first           acb.BuiltContext
	beforeSecond    func(context.Context, acb.Request) error
	hookErr         error
}

func (c *selfReviewNativeCatalog) ResolveOwnContextAgent(ctx context.Context, a agentprofile.PrivateAccess) (agentcognitive.AgentReference, error) {
	c.resolves++
	return c.Store.ResolveOwnContextAgent(ctx, a)
}

func (c *selfReviewNativeCatalog) BuildOwnAgentContext(ctx context.Context, r acb.Request) (acb.BuiltContext, error) {
	c.calls++
	c.requests = append(c.requests, r)
	if c.calls == 2 && c.beforeSecond != nil {
		if c.hookErr = c.beforeSecond(ctx, r); c.hookErr != nil {
			return acb.BuiltContext{}, acb.ErrUnavailable
		}
	}
	out, e := c.Store.BuildOwnAgentContext(ctx, r)
	if c.calls == 1 && e == nil {
		c.first = out // Observe only; never modify the original native result.
	}
	return out, e
}

type selfReviewNativeFixture struct {
	*privateProfileHTTPDBFixture
	catalog *selfReviewNativeCatalog
	access  agentprofile.PrivateAccess
	profile agentprofile.PrivateRecord
	memory  agentmemory.Record
	policy  agentpolicysettings.Bundle
}

func selfReviewNativeNew(t *testing.T) *selfReviewNativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires root-owned disposable backend-native PostgreSQL parent")
	}
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || cfg.ConnConfig.Host != "127.0.0.1" || cfg.ConnConfig.Port != 55432 ||
		!regexp.MustCompile(`^birdtie_backend_native_[0-9a-f]{12}$`).MatchString(cfg.ConnConfig.Database) ||
		cfg.ConnConfig.Database != os.Getenv("BIRDTIE_VISIBILITY_DATABASE") {
		t.Fatal("SelfReview refuses shared/phone/unregistered database")
	}
	// Registered before the original fixture: runs after its own cleanup and
	// closed pool, so Memory/policy/outbox residues are checked independently.
	var f *selfReviewNativeFixture
	t.Cleanup(func() {
		if f == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Error("cannot inspect SelfReview owned residue after original cleanup", err)
			return
		}
		defer pool.Close()
		var residue int
		err = pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM agent_memories WHERE owner_id=ANY($1::uuid[]))+
 (SELECT count(*) FROM agent_policy_settings WHERE owner_id=ANY($1::uuid[]))+
 (SELECT count(*) FROM agent_domain_outbox WHERE subject_id=ANY($1::uuid[]))+
 (SELECT count(*) FROM agent_consumer_inbox WHERE subject_id=ANY($1::uuid[]))`, f.accountIDs).Scan(&residue)
		if err != nil || residue != 0 {
			t.Errorf("SelfReview additional owned residue=%d err=%v", residue, err)
		} else {
			t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY SelfReview Memory/policy/outbox/inbox residue=0")
		}
	})
	f = &selfReviewNativeFixture{privateProfileHTTPDBFixture: privateProfileHTTPDBNew(t)}
	var parentOwned bool
	if e = f.pool.QueryRow(f.ctx, `SELECT datname=current_database() AND pg_get_userbyid(datdba)='birdtie'
 AND shobj_description(oid,'pg_database') LIKE 'birdtie-owned-backend-native-2026-10-10:%'
 FROM pg_database WHERE datname=current_database()`).Scan(&parentOwned); e != nil || !parentOwned {
		t.Fatal("root-owned backend-native database marker absent")
	}
	digest, e := identity.ParseBearer("Bearer " + f.tokens[0])
	if e != nil {
		t.Fatal(e)
	}
	f.access = agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}}
	current, e := f.store.ReadOwnAgentPrivateProfile(f.ctx, f.access)
	if e != nil {
		t.Fatal("native human Profile read", e)
	}
	f.profile, e = f.store.ReplaceOwnAgentPrivateProfile(f.ctx, f.access, agentprofile.ReplacePrivateInput{ExpectedVersion: current.Profile.ProfileVersion,
		Fields: agentprofile.PrivateFields{PreferredActivityTypes: []string{"badminton"}, AgentNotes: selfReviewNativeHidden, PrivateCityHistory: selfReviewNativeHidden}})
	if e != nil {
		t.Fatal("native explicit human Profile write", e)
	}
	var id string
	var now time.Time
	if e = f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text,clock_timestamp()`).Scan(&id, &now); e != nil {
		t.Fatal(e)
	}
	f.memory, e = f.store.PutOwnMemory(f.ctx, f.access, id, selfReviewNativeMemoryInput(0, now.Add(time.Hour)))
	if e != nil || f.memory.SourceType != agentmemory.SourceExplicit || f.memory.Visibility != agentmemory.VisibilityPrivate {
		t.Fatal("native explicit human Memory write", e)
	}
	f.policy, e = f.store.PutOwnPolicy(f.ctx, f.access, agentpolicysettings.Autonomy, agentpolicysettings.PutInput{Settings: json.RawMessage(`{"level":"LEVEL_0_OBSERVE"}`), ExpiresAt: now.Add(time.Hour)})
	if e != nil {
		t.Fatal("native explicit human policy write", e)
	}
	f.catalog = &selfReviewNativeCatalog{Store: f.store}
	f.handler = privateProfileHTTPNew(f.catalog, f.store)
	return f
}

func selfReviewNativeMemoryInput(version int64, until time.Time) agentmemory.PutInput {
	return agentmemory.PutInput{ExpectedVersion: version, MemoryType: agentmemory.TypePreference, MemoryKey: "activity_category:badminton",
		Summary:         agentmemorycorrection.NegativeStatement("badminton"),
		StructuredValue: json.RawMessage(`{"activityCategory":"badminton","nature":"human-correction","privateDiagnostic":"` + selfReviewNativeHidden + `"}`),
		Visibility:      agentmemory.VisibilityPrivate, ValidUntil: until}
}

func (f *selfReviewNativeFixture) body(t *testing.T, memoryID string) string {
	t.Helper()
	raw, e := json.Marshal(acb.HumanSelfReviewSelection{ProfileFields: []string{"preferredActivityTypes"}, MemoryIDs: []string{memoryID}, PolicyFamilies: []agentpolicysettings.Family{agentpolicysettings.Autonomy}})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}

func (f *selfReviewNativeFixture) call(t *testing.T, body, token string, want int) *httptest.ResponseRecorder {
	t.Helper()
	f.requestCount++
	requestID := fmt.Sprintf("native_self_review_%04d", f.requestCount)
	r := privateProfileHTTPRequest(http.MethodPost, humanSelfReviewHTTPPath, body, token, "application/json")
	r.Header.Set("X-Request-ID", requestID)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	// Preserve the actual synthetic wire even on a failing security assertion.
	t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY SELF_REVIEW status=%d request_id=%s wire=%s", w.Code, requestID, w.Body.String())
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") != requestID {
		t.Fatal("native SelfReview lost no-store/request correlation")
	}
	if f.catalog.hookErr != nil {
		t.Fatal("owned native change failed; not a permission rejection", f.catalog.hookErr)
	}
	if want != http.StatusOK {
		for _, marker := range []string{`"data"`, `"profile"`, `"memories"`, `"fieldEvidenceSet"`, "badminton", agentmemorycorrection.NegativeStatement("badminton"), f.memory.ID, selfReviewNativeHidden} {
			if strings.Contains(w.Body.String(), marker) {
				t.Fatalf("native denied/expired response emitted old private body: status=%d marker=%q", w.Code, marker)
			}
		}
	}
	if w.Code != want {
		t.Fatalf("native SelfReview status=%d want=%d", w.Code, want)
	}
	return w
}

func selfReviewNativeProjection(t *testing.T, f *selfReviewNativeFixture, w *httptest.ResponseRecorder) acb.HumanSelfReviewProjection {
	t.Helper()
	var wire struct {
		Data acb.HumanSelfReviewProjection `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &wire); e != nil {
		t.Fatal(e)
	}
	v := wire.Data
	if v.SchemaVersion != acb.HumanSelfReviewSchema || v.Owner != f.access.WorkspacePrincipal || v.AgentID != f.agentIDs[0] || v.ComparisonScope != acb.HumanSelfReviewComparison ||
		v.ModelAccess != "UNAVAILABLE" || v.MediaAccess != "UNAVAILABLE" || v.GrantsAuthority || v.MemoryPromotionAllowed ||
		len(v.Profile) != 1 || len(v.Memories) != 1 || len(v.Policies) != 1 || v.FieldEvidenceSet == nil {
		t.Fatal("native same-snapshot human projection incomplete or gained authority")
	}
	if string(v.Profile["preferredActivityTypes"]) != `["badminton"]` || v.Memories[0].ID != f.memory.ID || v.Memories[0].Version != f.memory.Version || v.Memories[0].Summary != f.memory.Summary ||
		v.Memories[0].CreatedAt == nil || !v.Memories[0].CreatedAt.Equal(f.memory.CreatedAt) || v.Memories[0].ValidFrom == nil || !v.Memories[0].ValidFrom.Equal(f.memory.ValidFrom) ||
		!v.Memories[0].ValidUntil.Equal(f.memory.ValidUntil) || v.Memories[0].Confidence == nil || v.Memories[0].Confidence.Semantics != agentconfidence.DirectDeclaration ||
		v.Memories[0].Confidence.Value == nil || *v.Memories[0].Confidence.Value != 1 {
		t.Fatal("selected declarations/native interval/direct confidence changed")
	}
	if !v.ObservedAt.Equal(f.catalog.first.Bundle.ObservedAt) || !v.ExpiresAt.Equal(f.catalog.first.Bundle.ExpiresAt) ||
		!v.FieldEvidenceSet.ObservedAt.Equal(v.ObservedAt) || !v.FieldEvidenceSet.ExpiresAt.Equal(v.ExpiresAt) || acb.ValidateFieldEvidenceShape(*v.FieldEvidenceSet) != nil {
		t.Fatal("DTO/evidence clocks no longer belong to the original native snapshot")
	}
	for _, marker := range []string{selfReviewNativeHidden, "structuredValue", "memoryKey", "RowToken", "rowToken", "SessionDigest", "sessionDigest", "authority", `"seal"`, `"bundle"`} {
		if strings.Contains(w.Body.String(), marker) {
			t.Fatal("internal/unselected metadata leaked", marker)
		}
	}
	if f.catalog.calls != 2 || f.catalog.resolves != 1 || len(f.catalog.requests) != 2 || !f.catalog.requests[1].DeadlineAt.Equal(v.ExpiresAt) {
		t.Fatal("registered handler skipped its same-Service final native read or renewed original expiry")
	}
	for _, r := range f.catalog.requests {
		if r.Access != f.access || r.Agent.Principal != f.access.WorkspacePrincipal || r.Mode != acb.HumanSelfReview || r.Selection != acb.ExactSelfReview ||
			r.TaskID != "" || r.CityID != "" || r.PurposeGrantID != "" || len(r.ActivityIDs)+len(r.PlaceIDs)+len(r.RelationshipTieIDs) != 0 ||
			!reflect.DeepEqual(r.MemoryIDs, []string{f.memory.ID}) || !reflect.DeepEqual(r.ProfileFields, []string{"preferredActivityTypes"}) {
			t.Fatal("native human read inherited another scope/task/model purpose")
		}
	}
	claims := map[string]acb.FieldEvidence{}
	for _, c := range v.FieldEvidenceSet.Claims {
		claims[c.ID] = c
		if c.SubjectID != f.accountIDs[0] || c.SubjectKind != "PERSON" || c.CapturedAt != nil || c.CaptureTimeStatus != "UNKNOWN_NOT_COLLECTED" ||
			!c.ObservedAt.Equal(v.ObservedAt) || !c.CollectedAt.Equal(v.ObservedAt) || c.CollectionEvent != "CURRENT_NATIVE_READ" || c.Source.RowToken != "" {
			t.Fatal("source chronology/subject was replaced by attendance/media/identity authority")
		}
		matched := false
		for _, native := range f.catalog.first.Bundle.Sources {
			if native.Kind == c.Source.Kind && native.ID == c.Source.ID {
				matched = native.Version == c.Source.Version && native.NativeTime.Equal(c.Source.NativeTime) && native.RowToken != ""
			}
		}
		if !matched {
			t.Fatal("claim source/version not read from the same original PostgreSQL snapshot")
		}
	}
	if len(v.FieldEvidenceSet.Conflicts) != 1 {
		t.Fatal("native opposite explicit declarations did not retain one bounded conflict")
	}
	conflict := v.FieldEvidenceSet.Conflicts[0]
	if conflict.Status != acb.PendingEvidenceConfirmation || conflict.SubjectID != f.accountIDs[0] || conflict.Field != "preference.activityCategory.badminton" || len(conflict.ClaimIDs) != 2 {
		t.Fatal("native conflict lost both original selected claim IDs or confirmation state")
	}
	sourceKinds := map[string]bool{}
	for _, id := range conflict.ClaimIDs {
		c, ok := claims[id]
		if !ok || c.Field != conflict.Field || c.Use != "DECLARATION_ONLY" {
			t.Fatal("conflict points to missing/different-field claims")
		}
		sourceKinds[c.Source.Kind] = true
	}
	if !sourceKinds["HUMAN_PRIVATE_PROFILE"] || !sourceKinds["HUMAN_EXPLICIT_MEMORY"] || v.Policies[0].NativeRevision != f.policy.Autonomy.NativeRevision {
		t.Fatal("native conflict collapsed one declaration or changed selected policy")
	}
	return v
}

func (f *selfReviewNativeFixture) sourceState(t *testing.T) []byte {
	t.Helper()
	var raw []byte
	e := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object(
 'private',coalesce((SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id) FROM agent_private_profiles p WHERE owner_id=ANY($1::uuid[])),'[]'),
 'memory',coalesce((SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM agent_memories m WHERE owner_id=ANY($1::uuid[])),'[]'),
 'policy',coalesce((SELECT jsonb_agg(to_jsonb(p) ORDER BY agent_id,family) FROM agent_policy_settings p WHERE owner_id=ANY($1::uuid[])),'[]'),
 'outbox',coalesce((SELECT jsonb_agg(to_jsonb(o) ORDER BY event_id) FROM agent_domain_outbox o WHERE subject_id=ANY($1::uuid[])),'[]'),
 'inbox',coalesce((SELECT jsonb_agg(to_jsonb(i) ORDER BY event_id,handler_version) FROM agent_consumer_inbox i WHERE subject_id=ANY($1::uuid[])),'[]'))`, f.accountIDs).Scan(&raw)
	if e != nil {
		t.Fatal("cannot inspect native read effects", e)
	}
	return raw
}

func TestHumanSelfReviewHTTPNativeSameSnapshotConflictAndRestart(t *testing.T) {
	f := selfReviewNativeNew(t)
	before, sources := f.effects(t), f.sourceState(t)
	first := selfReviewNativeProjection(t, f, f.call(t, f.body(t, f.memory.ID), f.tokens[0], http.StatusOK))
	// New real Store/service/registered handler: persistent source contents and
	// versions survive, while each request gets a fresh native observation.
	store := postgres.New(f.pool, false)
	f.catalog = &selfReviewNativeCatalog{Store: store}
	f.handler = privateProfileHTTPNew(f.catalog, store)
	second := selfReviewNativeProjection(t, f, f.call(t, f.body(t, f.memory.ID), f.tokens[0], http.StatusOK))
	if !reflect.DeepEqual(first.Profile, second.Profile) || !reflect.DeepEqual(first.Memories, second.Memories) ||
		!reflect.DeepEqual(first.FieldEvidenceSet.Conflicts, second.FieldEvidenceSet.Conflicts) || second.ObservedAt.Before(first.ObservedAt) {
		t.Fatal("new registered handler lost retained sources/conflict or reused stale observation")
	}
	f.assertEffectsUnchanged(t, before)
	if !bytes.Equal(sources, f.sourceState(t)) {
		t.Fatal("human self-review promoted/corrected a Memory, policy, private field, outbox or consumer receipt")
	}
}

func TestHumanSelfReviewHTTPNativeCurrentIdentityAndClosedSelection(t *testing.T) {
	for _, name := range []string{"otherOwner", "organization", "business", "unknownMemory", "revokedSession", "expiredSession", "clientConfirmed"} {
		t.Run(name, func(t *testing.T) {
			f := selfReviewNativeNew(t)
			body, token, want := f.body(t, f.memory.ID), f.tokens[0], http.StatusForbidden
			switch name {
			case "otherOwner":
				token = f.tokens[1]
			case "organization":
				token = f.tokens[2]
			case "business":
				token = f.tokens[3]
			case "unknownMemory":
				var unknown string
				if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&unknown); e != nil {
					t.Fatal(e)
				}
				body = f.body(t, unknown)
			case "revokedSession":
				token, want = f.newSession(f.accountIDs[0], false, true), http.StatusUnauthorized
			case "expiredSession":
				token, want = f.newSession(f.accountIDs[0], true, false), http.StatusUnauthorized
			case "clientConfirmed":
				body, want = strings.TrimSuffix(body, "}")+`,"confirmed":true}`, http.StatusBadRequest
			}
			before, sources := f.effects(t), f.sourceState(t)
			f.call(t, body, token, want)
			if (name == "otherOwner" || name == "unknownMemory") && (f.catalog.resolves != 1 || f.catalog.calls != 1) {
				t.Fatal("owner/unknown selection did not reach original native denied read")
			}
			f.assertEffectsUnchanged(t, before)
			if !bytes.Equal(sources, f.sourceState(t)) {
				t.Fatal("denied human read wrote source data")
			}
		})
	}
}

func TestHumanSelfReviewHTTPNativeAfterEncodingCurrentSourceSessionAndExpiry(t *testing.T) {
	for _, name := range []string{"profileClear", "profileABA", "memoryWithdraw", "memoryABA", "ownerABA", "agentABA", "sessionRevoke", "sessionABA", "policyABA", "memoryLeaseExpiry", "sessionLeaseExpiry", "liveIdleRenewal", "normalAuthenticateRenewal"} {
		t.Run(name, func(t *testing.T) {
			f := selfReviewNativeNew(t)
			if name == "memoryLeaseExpiry" {
				var now time.Time
				if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
					t.Fatal(e)
				}
				var e error
				f.memory, e = f.store.PutOwnMemory(f.ctx, f.access, f.memory.ID, selfReviewNativeMemoryInput(f.memory.Version, now.Add(time.Second)))
				if e != nil {
					t.Fatal(e)
				}
			}
			if name == "sessionLeaseExpiry" {
				f.exec(`WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS at)
 UPDATE sessions SET expires_at=clock.at+interval '1 second',idle_expires_at=clock.at+interval '1 second'
 FROM clock WHERE token_sha256=$1`, f.access.SessionDigest[:])
			}
			f.catalog.beforeSecond = func(ctx context.Context, r acb.Request) error {
				if f.catalog.first.Bundle.FieldEvidenceSet == nil || len(f.catalog.first.Bundle.FieldEvidenceSet.Conflicts) != 1 || !r.DeadlineAt.Equal(f.catalog.first.Bundle.ExpiresAt) {
					return fmt.Errorf("second native read did not retain first complete conflicting snapshot/lease")
				}
				switch name {
				case "profileClear", "profileABA":
					changed, e := f.store.ReplaceOwnAgentPrivateProfile(ctx, f.access, agentprofile.ReplacePrivateInput{ExpectedVersion: f.profile.Profile.ProfileVersion, Fields: agentprofile.PrivateFields{}})
					if e != nil || name == "profileClear" {
						return e
					}
					_, e = f.store.ReplaceOwnAgentPrivateProfile(ctx, f.access, agentprofile.ReplacePrivateInput{ExpectedVersion: changed.Profile.ProfileVersion, Fields: f.profile.Fields})
					return e
				case "memoryWithdraw":
					_, e := f.store.DeleteOwnMemory(ctx, f.access, f.memory.ID, f.memory.Version)
					return e
				case "memoryABA":
					input := selfReviewNativeMemoryInput(f.memory.Version, f.memory.ValidUntil)
					input.Summary = "合成临时不同声明，随后恢复旧文字"
					changed, e := f.store.PutOwnMemory(ctx, f.access, f.memory.ID, input)
					if e != nil {
						return e
					}
					_, e = f.store.PutOwnMemory(ctx, f.access, f.memory.ID, selfReviewNativeMemoryInput(changed.Version, f.memory.ValidUntil))
					return e
				case "ownerABA":
					for _, status := range []string{"suspended", "active"} {
						if _, e := f.pool.Exec(ctx, `UPDATE accounts SET status=$2 WHERE id=$1`, f.accountIDs[0], status); e != nil {
							return e
						}
					}
				case "agentABA":
					for _, status := range []string{"retired", "active"} {
						if _, e := f.pool.Exec(ctx, `UPDATE agents SET status=$2 WHERE id=$1`, f.agentIDs[0], status); e != nil {
							return e
						}
					}
				case "sessionRevoke":
					return f.store.RevokeSession(ctx, f.access.SessionDigest)
				case "sessionABA":
					var before, after int64
					if e := f.pool.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE token_sha256=$1`, f.access.SessionDigest[:]).Scan(&before); e != nil {
						return e
					}
					// Privileged diagnostic only. No production session-restore API
					// is introduced or treated as authority by this test.
					for _, sql := range []string{`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, `UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`} {
						if _, e := f.pool.Exec(ctx, sql, f.access.SessionDigest[:]); e != nil {
							return e
						}
					}
					if e := f.pool.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE token_sha256=$1`, f.access.SessionDigest[:]).Scan(&after); e != nil || after != before+2 {
						return fmt.Errorf("native session revoke/restore generation not advanced: before=%d after=%d err=%v", before, after, e)
					}
				case "policyABA":
					changed, e := f.store.PutOwnPolicy(ctx, f.access, agentpolicysettings.Autonomy, agentpolicysettings.PutInput{ExpectedVersion: f.policy.Autonomy.NativeRevision, Settings: json.RawMessage(`{"level":"LEVEL_1_ASSIST"}`), ExpiresAt: *f.policy.Autonomy.ExpiresAt})
					if e != nil {
						return e
					}
					_, e = f.store.PutOwnPolicy(ctx, f.access, agentpolicysettings.Autonomy, agentpolicysettings.PutInput{ExpectedVersion: changed.Autonomy.NativeRevision, Settings: f.policy.Autonomy.Settings, ExpiresAt: *f.policy.Autonomy.ExpiresAt})
					return e
				case "memoryLeaseExpiry", "sessionLeaseExpiry":
					if f.catalog.first.Bundle.ExpiresAt.Sub(f.catalog.first.Bundle.ObservedAt) > time.Second || !f.catalog.first.Bundle.ExpiresAt.Before(f.catalog.requests[0].DeadlineAt) {
						return fmt.Errorf("first native lease was not clipped to actual source/session expiry")
					}
					return selfReviewNativeWaitPast(ctx, f.pool, f.catalog.first.Bundle.ExpiresAt)
				case "liveIdleRenewal", "normalAuthenticateRenewal":
					var before, after int64
					var oldIdle, newIdle time.Time
					if e := f.pool.QueryRow(ctx, `SELECT authorization_generation,idle_expires_at FROM sessions WHERE token_sha256=$1`, f.access.SessionDigest[:]).Scan(&before, &oldIdle); e != nil {
						return e
					}
					if name == "normalAuthenticateRenewal" {
						actor, e := f.store.Authenticate(ctx, f.access.SessionDigest)
						if e != nil || actor.ID != f.accountIDs[0] || actor.AccountType != "person" {
							return fmt.Errorf("original native Authenticate renewal failed: %v", e)
						}
					} else if _, e := f.pool.Exec(ctx, `UPDATE sessions SET idle_expires_at=least(expires_at,clock_timestamp()+interval '90 minutes') WHERE token_sha256=$1`, f.access.SessionDigest[:]); e != nil {
						return e
					}
					if e := f.pool.QueryRow(ctx, `SELECT authorization_generation,idle_expires_at FROM sessions WHERE token_sha256=$1`, f.access.SessionDigest[:]).Scan(&after, &newIdle); e != nil || after != before || !newIdle.After(oldIdle) {
						return fmt.Errorf("live idle renewal changed native security generation: %d/%d err=%v", before, after, e)
					}
				}
				return nil
			}
			want := http.StatusForbidden
			if name == "memoryLeaseExpiry" {
				want = http.StatusConflict
			}
			if name == "liveIdleRenewal" || name == "normalAuthenticateRenewal" {
				want = http.StatusOK
			}
			w := f.call(t, f.body(t, f.memory.ID), f.tokens[0], want)
			if f.catalog.calls != 2 || f.catalog.resolves != 1 {
				t.Fatal("native final mutation did not execute at the registered after-encoding boundary")
			}
			if want == http.StatusOK {
				selfReviewNativeProjection(t, f, w)
			}
		})
	}
}

func selfReviewNativeWaitPast(ctx context.Context, pool *pgxpool.Pool, expires time.Time) error {
	if _, e := pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.025)`, expires); e != nil {
		return e
	}
	var at time.Time
	if e := pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return e
	} else if !at.After(expires) {
		return fmt.Errorf("native wall clock did not cross original clipped deadline")
	}
	return nil
}

func TestHumanSelfReviewHTTPNativeAfterEncodingSourceLockWaitExpiresOriginalLease(t *testing.T) {
	f := selfReviewNativeNew(t)
	var now time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	var e error
	f.memory, e = f.store.PutOwnMemory(f.ctx, f.access, f.memory.ID, selfReviewNativeMemoryInput(f.memory.Version, now.Add(2*time.Second)))
	if e != nil {
		t.Fatal(e)
	}
	cfg := f.pool.Config().Copy()
	app := "self-review-source-wait-" + f.accountIDs[0]
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	store := postgres.New(pool, false)
	f.catalog = &selfReviewNativeCatalog{Store: store}
	f.handler = privateProfileHTTPNew(f.catalog, store)
	var lock pgx.Tx
	var blocker int
	entered := make(chan error, 1)
	f.catalog.beforeSecond = func(ctx context.Context, _ acb.Request) error {
		lock, e = f.pool.Begin(ctx)
		if e == nil {
			e = lock.QueryRow(ctx, `SELECT pg_backend_pid() FROM agent_memories WHERE id=$1 FOR UPDATE`, f.memory.ID).Scan(&blocker)
		}
		entered <- e
		return e
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	body := f.body(t, f.memory.ID)
	go func() {
		r := privateProfileHTTPRequest(http.MethodPost, humanSelfReviewHTTPPath, body, f.tokens[0], "application/json")
		r.Header.Set("X-Request-ID", "native_self_review_source_wait")
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		done <- w
	}()
	select {
	case e = <-entered:
		if e != nil {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("registered SelfReview did not reach after-encoding second-read hook")
	}
	defer lock.Rollback(context.Background())
	waiting := false
	for until := time.Now().Add(time.Second); time.Now().Before(until); {
		if e = f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND $2::int=ANY(pg_blocking_pids(pid)))`, app, blocker).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("no actual selected Memory lock wait/blocking PID positive control")
	}
	t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY actual after-encoding selected Memory lock wait confirmed by pg_stat_activity/pg_blocking_pids")
	if e = selfReviewNativeWaitPast(f.ctx, f.pool, f.catalog.first.Bundle.ExpiresAt); e != nil {
		t.Fatal(e)
	}
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case w := <-done:
		t.Logf("LOCAL_DISPOSABLE_SYNTHETIC_ONLY SELF_REVIEW_SOURCE_WAIT status=%d wire=%s", w.Code, w.Body.String())
		if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), `"data"`) || strings.Contains(w.Body.String(), f.memory.ID) || strings.Contains(w.Body.String(), "badminton") || strings.Contains(w.Body.String(), selfReviewNativeHidden) ||
			f.catalog.calls != 2 || f.catalog.hookErr != nil || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Request-ID") != "native_self_review_source_wait" {
			t.Fatal("source wait renewed original lease, emitted old private body or skipped final native clock")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("native source wait did not finish after owned lock release")
	}
}
