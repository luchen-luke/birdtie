package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
	"time"
)

const currentReadOwner = "49000000-0000-4000-8000-000000000001"
const currentReadTask = "49000000-0000-4000-8000-000000000002"
const currentReadEntity = "49000000-0000-4000-8000-000000000003"

func currentReadInput(kind string) CurrentSearch {
	return CurrentSearch{arp.Access{Actor: identity.Actor{ID: currentReadOwner, AccountType: "person"}, SessionDigest: [32]byte{1}, TaskID: currentReadTask, ExpectedTask: json.RawMessage(`{"current":"original-server-task"}`)}, arp.Query{CityID: "city_fixture", Kind: kind, CompareIDs: []string{}}}
}
func currentReadDecision(tool, owner, resource, digest string, at, until time.Time) Decision {
	d, _ := Lookup(tool)
	return Decision{SchemaVersion: Schema, DecisionID: currentReadTask, Disposition: Allow, ReasonCodes: []string{"CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY"}, Tool: tool, ToolVersion: d.Version, ActionID: currentReadTask, LogicalOperationID: currentReadTask, ActorID: owner, AgentID: currentReadEntity, SubjectType: "PERSON", SubjectID: owner, PolicyVersion: strings.Repeat("a", 64), ResourceVersion: resource, ArgumentsDigest: digest, Purpose: d.Purpose, DataDestinations: []string{"LOCAL_OWNER"}, ObservedAt: at, ExpiresAt: until}
}
func currentReadSearchReceipt(q CurrentSearch) CurrentSearchReceipt {
	at := time.Now().UTC()
	source := arp.Receipt{Items: []arp.Item{}, PublicCommercialRefs: []arp.Ref{}, ObservedAt: at, ValidUntil: at.Add(20 * time.Second), Proof: strings.Repeat("b", 64), Seal: strings.Repeat("c", 64)}
	return CurrentSearchReceipt{Decision: currentReadDecision(CurrentSearchTool(q.Query.Kind), q.Access.Actor.ID, source.Proof, CurrentSearchDigest(q), at, source.ValidUntil), Source: source, Seal: strings.Repeat("d", 64)}
}

type currentSearchUnitPort struct {
	r     CurrentSearchReceipt
	e     error
	after func()
	calls int
}

func (p *currentSearchUnitPort) ReadOwnCurrentSearch(context.Context, CurrentSearch) (CurrentSearchReceipt, error) {
	p.calls++
	if p.after != nil {
		p.after()
	}
	return p.r, p.e
}
func (*currentSearchUnitPort) RevalidateOwnCurrentSearch(context.Context, CurrentSearch, CurrentSearchReceipt) error {
	return nil
}

func TestCurrentReadonlySearchClosedSourcesAndRealEmpty(t *testing.T) {
	for _, kind := range []string{"place", "person"} {
		t.Run(kind, func(t *testing.T) {
			q := currentReadInput(kind)
			r := currentReadSearchReceipt(q)
			p := &currentSearchUnitPort{r: r}
			got, e := ReadCurrentSearch(context.Background(), p, q)
			if e != nil || got.Source.Items == nil || len(got.Source.Items) != 0 || p.calls != 1 {
				t.Fatal("true current empty lost", e)
			}
			for _, id := range []string{currentReadEntity, "49000000-0000-4000-8000-000000000004"} {
				ref := arp.Ref{Type: kind, ID: id}
				r.Source.Items = append(r.Source.Items, arp.Item{Entity: ref, Title: "同名对象", Summary: "当前获准来源", Scope: arp.AuthorizedView, Detail: &ref})
			}
			if !r.Valid(q) || r.Source.Items[0].Entity.ID == r.Source.Items[1].Entity.ID {
				t.Fatal("real identity collapsed")
			}
			for name, mutate := range map[string]func(*CurrentSearchReceipt){"logical_operation": func(r *CurrentSearchReceipt) { r.Decision.LogicalOperationID = currentReadEntity }, "foreign_type": func(r *CurrentSearchReceipt) { r.Source.Items[0].Entity.Type = "activity" }, "unknown_id": func(r *CurrentSearchReceipt) { r.Source.Items[0].Entity.ID = "model-invented-id" }, "private": func(r *CurrentSearchReceipt) { r.Source.Items[0].Scope = arp.SelfPrivate }, "actor": func(r *CurrentSearchReceipt) { r.Decision.ActorID = currentReadEntity }, "model_destination": func(r *CurrentSearchReceipt) { r.Decision.DataDestinations = []string{"MODEL_PROVIDER"} }, "approved": func(r *CurrentSearchReceipt) { r.Decision.Disposition = "MODEL_APPROVED" }, "nil_empty": func(r *CurrentSearchReceipt) { r.Source.Items = nil }} {
				t.Run(name, func(t *testing.T) {
					c := r
					c.Source.Items = append([]arp.Item{}, r.Source.Items...)
					mutate(&c)
					if c.Valid(q) {
						t.Fatal("unqualified source accepted")
					}
				})
			}
		})
	}
}
func TestCurrentReadonlySearchNoModelOrOtherDomainExpansion(t *testing.T) {
	for _, kind := range []string{"activity", "organization", "business", "community", "opportunity", "group", ""} {
		q := currentReadInput(kind)
		if q.Valid() || CurrentSearchTool(kind) != "" {
			t.Fatal("unsupported scope acquired current search", kind)
		}
	}
	for _, tool := range []string{PlaceSearch, PersonSearch, PersonMatch} {
		p := agentplanner.Proposal(currentReadTask, 1, tool, agentplanner.Arguments{Query: "查找", CityID: "city_fixture"}, "source", "仅描述")
		raw, _ := json.Marshal(p)
		if _, e := DecodeProposal(raw); e == nil {
			t.Fatal("original model planner enlarged", tool)
		}
		for _, principal := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business} {
			for _, level := range agentautonomy.Levels() {
				restriction := Restrict(tool, principal, level)
				want := principal == actorref.Person && level != agentautonomy.LevelDelegate
				if restriction.Denied == want || restriction.NeedsConfirmation {
					t.Fatal("role/autonomy changed to authority", tool, principal, level)
				}
			}
		}
	}
}
func TestCurrentReadonlySearchPortsAndLateCancellationFailClosed(t *testing.T) {
	q := currentReadInput("person")
	var typedNil *currentSearchUnitPort
	for _, p := range []CurrentSearchPort{nil, typedNil} {
		if _, e := ReadCurrentSearch(context.Background(), p, q); !errors.Is(e, ErrUnavailable) {
			t.Fatal("missing port", e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &currentSearchUnitPort{r: currentReadSearchReceipt(q), after: cancel}
	if _, e := ReadCurrentSearch(ctx, p, q); e == nil {
		t.Fatal("late cancelled result released")
	}
	for _, e := range []error{ErrDenied, ErrChanged, ErrUnavailable} {
		p := &currentSearchUnitPort{e: e}
		if _, got := ReadCurrentSearch(context.Background(), p, q); !errors.Is(got, e) {
			t.Fatal("error became empty", got)
		}
	}
	for _, v := range []any{q, currentReadSearchReceipt(q)} {
		if _, e := json.Marshal(v); !errors.Is(e, ErrServerOnly) {
			t.Fatal("receipt serialized")
		}
	}
	var input CurrentSearch
	var receipt CurrentSearchReceipt
	for _, dst := range []any{&input, &receipt} {
		if e := json.Unmarshal([]byte(`{"confirmed":true,"permission_override":"ALLOW"}`), dst); !errors.Is(e, ErrServerOnly) {
			t.Fatal("wire reconstructed native receipt")
		}
	}
}
