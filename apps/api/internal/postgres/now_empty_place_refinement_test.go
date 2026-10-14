package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

func emptyPlacePGInput() (agentworkspace.Task, arp.Access, arp.Query) {
	task, access, _ := currentReadonlyPGTaskInput()
	task.Intent, task.Status, task.ContextType = agentworkspace.FindPlace, agentworkspace.TaskActive, "CITY"
	task.Filters = map[string]string{"targetIntent": agentworkspace.FindPlace, "reason": agentworkspace.ExplicitPlaceRefinement, "currentQuery": "only cafes", "category": "cafe", "searchTerm": "", "locationPreference": "city", "timePreference": "anytime"}
	task.Conversation = []agentworkspace.Message{{Role: "user", Text: "only cafes"}}
	access.ExpectedTask, _ = json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
	query, _ := replyCurrentQuery(task)
	return task, access, query
}

func TestEmptyPlaceRefinementNativeEntryNeverTreatsMissingAuthorityAsEmpty(t *testing.T) {
	_, a, _ := emptyPlacePGInput()
	for name, change := range map[string]func(*arp.Access){
		"missing session":  func(a *arp.Access) { a.SessionDigest = [32]byte{} },
		"other actor":      func(a *arp.Access) { a.Actor.ID = currentReadonlyPGEntity },
		"organization":     func(a *arp.Access) { a.Actor.AccountType = "organization" },
		"wrong task":       func(a *arp.Access) { a.TaskID = currentReadonlyPGEntity },
		"missing expected": func(a *arp.Access) { a.ExpectedTask = nil },
	} {
		t.Run(name, func(t *testing.T) {
			copy := a
			change(&copy)
			for _, s := range []*Store{nil, {}} {
				task, read, handled, e := s.TryOwnEmptyPlaceRefinementReply(context.Background(), copy)
				if e == nil || handled || read != nil || task.ID != "" {
					t.Fatal("missing native authority became empty success", handled, e)
				}
			}
		})
	}
	// A structurally eligible snapshot does not create a native Store/session.
	if _, _, handled, e := (&Store{}).TryOwnEmptyPlaceRefinementReply(context.Background(), a); handled || e == nil {
		t.Fatal("DTO alone granted empty success")
	}
}

func TestEmptyPlaceRefinementNativeSourceDoesNotCollapseNonemptyOrBadFacts(t *testing.T) {
	_, _, q := emptyPlacePGInput()
	at := time.Now().UTC()
	source := arp.Receipt{Items: []arp.Item{}, ObservedAt: at, ValidUntil: at.Add(time.Second), Proof: strings.Repeat("a", 64), Seal: strings.Repeat("b", 64)}
	if e := requireEmptyPlaceRefinementSource(q, source); e != nil {
		t.Fatal(e)
	}
	for name, change := range map[string]func(*arp.Receipt){
		"place with no licensed facts": func(r *arp.Receipt) { r.Places = []foundation.Place{{ID: currentReadonlyPGEntity}} },
		"item with no places": func(r *arp.Receipt) {
			r.Items = []arp.Item{{Entity: arp.Ref{Type: "place", ID: currentReadonlyPGEntity}, Title: "UNIT matching venue", Summary: "", Scope: arp.AuthorizedView}}
		},
		"activity fixture": func(r *arp.Receipt) { r.Activities = []foundation.Activity{{ID: currentReadonlyPGEntity}} },
		"unbound refs":     func(r *arp.Receipt) { r.PublicCommercialRefs = []arp.Ref{{Type: "place", ID: currentReadonlyPGEntity}} },
		"missing source":   func(r *arp.Receipt) { r.Items = nil },
		"bad lease":        func(r *arp.Receipt) { r.ValidUntil = r.ObservedAt },
	} {
		t.Run(name, func(t *testing.T) {
			copy := source
			change(&copy)
			if e := requireEmptyPlaceRefinementSource(q, copy); e == nil {
				t.Fatal("bad/nonempty source accepted as empty")
			}
		})
	}
	q.Kind = "activity"
	if requireEmptyPlaceRefinementSource(q, source) == nil {
		t.Fatal("activity empty set became place result")
	}
}

// Actual original native mapper and final current-query helper with pgx row
// doubles only. No DB, ACL SQL, locks, provider or live-source claim.
func TestEmptyPlaceRefinementPostWriteRechecksOriginalSourceAndRejectsNewMatch(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(fmt.Sprint(nonempty), func(t *testing.T) {
			task, a, q := emptyPlacePGInput()
			task.Status = agentworkspace.TaskCompleted
			tx := &currentReadonlySearchTx{task: task, current: true}
			if nonempty {
				tx.items, _ = json.Marshal([]arp.Item{{Entity: arp.Ref{Type: "place", ID: currentReadonlyPGEntity}, Title: "UNIT newly matching cafe", Summary: "", Scope: arp.AuthorizedView}})
				tx.actions = []byte(`{"place:` + currentReadonlyPGEntity + `":{}}`)
			}
			p := &nativeToolPolicy{owner: a.Actor.ID, agent: currentReadonlyPGEntity, token: strings.Repeat("b", 64), level: agentautonomy.LevelAssist, until: time.Now().Add(30 * time.Second)}
			read, e := (&Store{}).readEmptyPlaceRefinementTx(context.Background(), tx, a, task, q, p)
			if nonempty {
				if read != nil || !errors.Is(e, arp.ErrChanged) {
					t.Fatal("new matching source was ignored", e)
				}
				return
			}
			if e != nil || read == nil {
				t.Fatal("native empty post-write source", e)
			}
			if strings.Join(tx.calls, ",") != "relations,account,task,session,payload" || len(tx.payloadArgs) != 10 {
				t.Fatal("original source/session/policy read bypassed", tx.calls)
			}
			if !strings.Contains(tx.payloadSQL, "category_place.category_code=q->>'Category'") || !strings.Contains(tx.payloadSQL, "p.publication_status='published'") {
				t.Fatal("original category/public source predicate absent")
			}
			if string(read.read().query.Access.ExpectedTask) == string(a.ExpectedTask) {
				t.Fatal("post-write read reused pre-completion expected Task")
			}
		})
	}
}

func TestEmptyPlaceRefinementOriginalOwnedTaskGuardRejectsStaleAndOtherActor(t *testing.T) {
	for name, change := range map[string]func(*arp.Access){"stale": func(a *arp.Access) { a.ExpectedTask = json.RawMessage(`{}`) }, "other actor": func(a *arp.Access) { a.Actor.ID = currentReadonlyPGEntity }} {
		t.Run(name, func(t *testing.T) {
			task, a, _ := emptyPlacePGInput()
			change(&a)
			tx := &currentReadonlySearchTx{task: task, current: true}
			if _, e := ownReplyTaskTx(context.Background(), tx, a, true); e == nil {
				t.Fatal("original exact ownership guard bypassed")
			}
		})
	}
}

type emptyPlaceFinalUnitPort struct {
	calls int
	err   error
}

func (*emptyPlaceFinalUnitPort) ReadOwnCurrentSearch(context.Context, agenttool.CurrentSearch) (agenttool.CurrentSearchReceipt, error) {
	return agenttool.CurrentSearchReceipt{}, errors.New("unexpected renewal")
}
func (p *emptyPlaceFinalUnitPort) RevalidateOwnCurrentSearch(context.Context, agenttool.CurrentSearch, agenttool.CurrentSearchReceipt) error {
	p.calls++
	return p.err
}

func TestEmptyPlaceRefinementPrivateHandleFinalFenceAndNoWireAuthority(t *testing.T) {
	task, a, q := emptyPlacePGInput()
	task.Status = agentworkspace.TaskCompleted
	tx := &currentReadonlySearchTx{task: task, current: true}
	p := &nativeToolPolicy{owner: a.Actor.ID, agent: currentReadonlyPGEntity, token: strings.Repeat("b", 64), level: agentautonomy.LevelAssist, until: time.Now().Add(30 * time.Second)}
	h, e := (&Store{}).readEmptyPlaceRefinementTx(context.Background(), tx, a, task, q, p)
	if e != nil {
		t.Fatal(e)
	}
	port := &emptyPlaceFinalUnitPort{}
	h.read().port = port
	if e = h.Revalidate(context.Background()); e != nil || port.calls != 1 {
		t.Fatal("original final source fence omitted", e)
	}
	port.err = agenttool.ErrChanged
	if e = h.Revalidate(context.Background()); !errors.Is(e, agenttool.ErrChanged) {
		t.Fatal("late new source/stale permission was swallowed", e)
	}
	if _, e = json.Marshal(h); !errors.Is(e, agenttool.ErrServerOnly) {
		t.Fatal("wire authority exported", e)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%q"} {
		if strings.Contains(fmt.Sprintf(format, h), a.Actor.ID) {
			t.Fatal("private handle leaked authority")
		}
	}
	if e = h.UnmarshalJSON([]byte(`{}`)); !errors.Is(e, agenttool.ErrServerOnly) {
		t.Fatal(e)
	}
	if e = h.Revalidate(context.Background()); !errors.Is(e, arp.ErrDenied) {
		t.Fatal("wire reset retained read authority", e)
	}
}
