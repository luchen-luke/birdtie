package agentautonomy

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

const ownerID = "44000000-0000-4000-8000-000000000001"
const agentID = "44000000-0000-4000-8000-000000000002"
const peerID = "44000000-0000-4000-8000-000000000003"
const otherAgentID = "44000000-0000-4000-8000-000000000004"
const sourceID = "44000000-0000-4000-8000-000000000005"
const requestID = "44000000-0000-4000-8000-000000000006"
const operationID = "44000000-0000-4000-8000-000000000007"

func agentFixture() agentcognitive.AgentReference {
	return agentcognitive.AgentReference{AgentID: agentID, Principal: actorref.PrincipalRef{Type: actorref.Person, ID: ownerID}, Role: agentruntime.PersonalAgent}
}
func storeFixture(t *testing.T, level Level, now time.Time) *Store {
	t.Helper()
	store, err := NewStore(agentFixture(), now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if level != LevelObserve {
		if err = store.Replace(1, Specification{level, now.Add(-time.Minute), now.Add(time.Hour)}, now); err != nil {
			t.Fatal(err)
		}
	}
	return store
}
func snapshotFixture(t *testing.T, store *Store) Snapshot {
	t.Helper()
	value, err := store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAutonomyClosedCatalogueAndChineseDescriptors(t *testing.T) {
	if len(Levels()) != 4 || len(Operations()) != 12 {
		t.Fatal("original closed catalogue changed")
	}
	counts := map[Level]int{}
	for _, descriptor := range Operations() {
		t.Run(string(descriptor.Operation), func(t *testing.T) {
			value, ok := Lookup(descriptor.Operation)
			if !ok || value != descriptor || !strings.ContainsAny(value.Explanation, "观察理解整理总结建议排列提醒草稿自主") {
				t.Fatalf("bad descriptor: %#v", value)
			}
			if value.NeedsHumanConfirmation != (value.MinimumLevel == LevelPrepare) {
				t.Fatal("L2 confirmation contract changed")
			}
			if data, err := json.Marshal(value); err != nil || !strings.Contains(string(data), `"minimumLevel"`) {
				t.Fatal("non-authority descriptor cannot be displayed")
			}
		})
		counts[descriptor.MinimumLevel]++
	}
	if counts[LevelObserve] != 3 || counts[LevelAssist] != 4 || counts[LevelPrepare] != 4 || counts[LevelDelegate] != 1 {
		t.Fatal(counts)
	}
	if _, ok := Lookup("send_message"); ok {
		t.Fatal("executor inferred from source operation")
	}
	levels := Levels()
	levels[0] = "BYPASS"
	operations := Operations()
	operations[0].Explanation = "mutated"
	if Levels()[0] != LevelObserve || Operations()[0].Explanation == "mutated" {
		t.Fatal("caller mutated catalogue")
	}
}

func TestAutonomyDefaultFiniteObserveAndIdentityIsolation(t *testing.T) {
	now := time.Date(2026, 10, 3, 8, 0, 0, 123456789, time.FixedZone("China", 8*3600))
	store, err := NewStore(agentFixture(), now)
	if err != nil {
		t.Fatal(err)
	}
	old := snapshotFixture(t, store)
	if old.Revision() != 1 || old.Revoked() || old.Specification().Level != LevelObserve || old.Specification().ExpiresAt.Sub(old.Specification().ValidFrom) != MaxSettingsTTL || old.Specification().ValidFrom.Location() != time.UTC {
		t.Fatal("unsafe default")
	}
	if !store.Current(old, now) || store.Current(old, old.spec.ExpiresAt) || store.Current(old, old.spec.ValidFrom.Add(-time.Nanosecond)) {
		t.Fatal("time boundary failed")
	}
	copySpec := old.Specification()
	copySpec.Level = LevelPrepare
	if snapshotFixture(t, store).Specification().Level != LevelObserve {
		t.Fatal("snapshot editable")
	}
	other, err := NewStore(agentFixture(), now)
	if err != nil {
		t.Fatal(err)
	}
	if other.Current(old, now) || store.Current(Snapshot{}, now) {
		t.Fatal("snapshot transferable across store instances")
	}
	for name, change := range map[string]func(*agentcognitive.AgentReference){
		"zeroAgent":   func(a *agentcognitive.AgentReference) { a.AgentID = "00000000-0000-0000-0000-000000000000" },
		"badAgent":    func(a *agentcognitive.AgentReference) { a.AgentID = "model-generated" },
		"unknownRole": func(a *agentcognitive.AgentReference) { a.Role = "AUTONOMOUS" },
		"organization": func(a *agentcognitive.AgentReference) {
			a.Principal.Type = actorref.Organization
			a.Role = agentruntime.OrganizationAgent
		},
		"business": func(a *agentcognitive.AgentReference) {
			a.Principal.Type = actorref.Business
			a.Role = agentruntime.BusinessAgent
		},
		"community":    func(a *agentcognitive.AgentReference) { a.Principal.Type = "COMMUNITY" },
		"city":         func(a *agentcognitive.AgentReference) { a.Principal.Type = "CITY" },
		"ownerUnknown": func(a *agentcognitive.AgentReference) { a.Principal.ID = "?" },
	} {
		t.Run(name, func(t *testing.T) {
			a := agentFixture()
			change(&a)
			if value, e := NewStore(a, now); value != nil || !errors.Is(e, ErrInvalid) {
				t.Fatalf("%v %v", value, e)
			}
		})
	}
}

func TestAutonomySpecificationBoundsAtomicCASAndRevocation(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	store := storeFixture(t, LevelObserve, now)
	base := Specification{LevelAssist, now, now.Add(MaxSettingsTTL)}
	for name, modify := range map[string]func(*Specification){
		"unknownLevel": func(s *Specification) { s.Level = "LEVEL_4" },
		"emptyLevel":   func(s *Specification) { s.Level = "" },
		"zeroStart":    func(s *Specification) { s.ValidFrom = time.Time{} },
		"zeroEnd":      func(s *Specification) { s.ExpiresAt = time.Time{} },
		"futureStart":  func(s *Specification) { s.ValidFrom = now.Add(time.Nanosecond) },
		"atDeadline":   func(s *Specification) { s.ExpiresAt = now },
		"reversed":     func(s *Specification) { s.ExpiresAt = s.ValidFrom.Add(-time.Second) },
		"over30Days":   func(s *Specification) { s.ExpiresAt = s.ExpiresAt.Add(time.Nanosecond) },
		"year10000":    func(s *Specification) { s.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
	} {
		t.Run(name, func(t *testing.T) {
			spec := base
			modify(&spec)
			if e := store.Replace(1, spec, now); !errors.Is(e, ErrInvalid) {
				t.Fatal(e)
			}
			if snapshotFixture(t, store).Revision() != 1 {
				t.Fatal("invalid update changed version")
			}
		})
	}
	if e := store.Replace(0, base, now); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	old := snapshotFixture(t, store)
	if e := store.Replace(1, base, now); e != nil {
		t.Fatal(e)
	}
	if store.Current(old, now) || old.Specification().Level != LevelObserve {
		t.Fatal("old immutable snapshot reused")
	}
	if e := store.Replace(1, base, now); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	current := snapshotFixture(t, store)
	if e := store.Revoke(current.Revision()); e != nil {
		t.Fatal(e)
	}
	if store.Current(current, now) || !snapshotFixture(t, store).Revoked() {
		t.Fatal("revocation did not invalidate")
	}
	if e := store.Revoke(current.Revision()); !errors.Is(e, ErrConflict) {
		t.Fatal(e)
	}
	if e := store.Replace(3, base, now); e != nil || snapshotFixture(t, store).Revoked() {
		t.Fatalf("trusted fresh setting replacement: %v", e)
	}
	store.current.revision = ^uint64(0)
	if e := store.Replace(^uint64(0), base, now); !errors.Is(e, ErrConflict) {
		t.Fatal("version wrapped")
	}
	if e := store.Revoke(^uint64(0)); !errors.Is(e, ErrConflict) {
		t.Fatal("revoke wrapped")
	}
	if value, e := NewStore(agentFixture(), time.Date(9999, 12, 30, 0, 0, 0, 0, time.UTC)); value != nil || !errors.Is(e, ErrInvalid) {
		t.Fatal("default expiry outside JSON time accepted")
	}
	if value, e := NewStore(agentFixture(), time.Time{}); value != nil || !errors.Is(e, ErrInvalid) {
		t.Fatal("zero trusted clock accepted")
	}
}

func TestAutonomyEightConcurrentCASHasOneWinner(t *testing.T) {
	now := time.Now()
	store := storeFixture(t, LevelObserve, now)
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results <- store.Replace(1, Specification{LevelAssist, now, now.Add(time.Duration(i+1) * time.Hour)}, now)
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	winners, conflicts := 0, 0
	for e := range results {
		if e == nil {
			winners++
		} else if errors.Is(e, ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if winners != 1 || conflicts != 7 || snapshotFixture(t, store).Revision() != 2 {
		t.Fatalf("winner%d conflict%d", winners, conflicts)
	}
}

func TestAutonomyDelegateDefinitionCannotBeConfigured(t *testing.T) {
	now := time.Now()
	store := storeFixture(t, LevelObserve, now)
	if agentfeature.Limits().AutonomousAction {
		t.Fatal("pilot brake unexpectedly lifted")
	}
	if err := store.Replace(1, Specification{LevelDelegate, now, now.Add(time.Hour)}, now); !errors.Is(err, ErrUnavailable) || snapshotFixture(t, store).Revision() != 1 {
		t.Fatalf("delegate settings bypassed brake: %v", err)
	}
	for _, body := range []string{
		`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":false,"agent_attention_policy":false,"agent_social_policy":true,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":true,"sensitiveInference":false}}`,
		`{"level":"LEVEL_3_DELEGATE","confirmed":true,"grant":"all"}`,
	} {
		var spec Specification
		if err := json.Unmarshal([]byte(body), &spec); !errors.Is(err, ErrServerOnly) || spec != (Specification{}) {
			t.Fatal("legacy JSON supplied settings authority")
		}
	}
}

func TestAutonomyNilAndJSONServerOnlyObjects(t *testing.T) {
	var missing *Store
	if _, e := missing.Snapshot(); !errors.Is(e, ErrUnavailable) || missing.Current(Snapshot{}, time.Now()) || !errors.Is(missing.Replace(1, Specification{}, time.Now()), ErrUnavailable) || !errors.Is(missing.Revoke(1), ErrUnavailable) {
		t.Fatal("nil store unsafe")
	}
	now := time.Now()
	store := storeFixture(t, LevelObserve, now)
	snapshot := snapshotFixture(t, store)
	for _, value := range []any{Specification{}, snapshot, Request{}, OfflineView{}, Assessment{}, store, &Service{}} {
		if _, err := json.Marshal(value); !errors.Is(err, ErrServerOnly) {
			t.Fatalf("server object encoded %T %v", value, err)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"verified":true,"confirmed":true}`, `{"ownerId":"` + peerID + `","level":"LEVEL_3_DELEGATE"}`, `{"revision":1,"sources":[{"version":1}]}`} {
		t.Run(body, func(t *testing.T) {
			values := []any{&Specification{}, &Snapshot{}, &Request{}, &OfflineView{}, &Assessment{}, store, &Service{}}
			for _, value := range values {
				if err := json.Unmarshal([]byte(body), value); !errors.Is(err, ErrServerOnly) {
					t.Fatalf("server object decoded %T %v", value, err)
				}
			}
			if snapshotFixture(t, store) != snapshot {
				t.Fatal("JSON changed live settings")
			}
		})
	}
	var spec *Specification
	var snap *Snapshot
	var request *Request
	var view *OfflineView
	var assessment *Assessment
	for _, value := range []interface{ UnmarshalJSON([]byte) error }{spec, snap, request, view, assessment} {
		if !errors.Is(value.UnmarshalJSON([]byte(`{}`)), ErrServerOnly) {
			t.Fatal("nil receiver authority accepted")
		}
	}
}
