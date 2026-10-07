package agenttool

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"strings"
	"testing"
	"time"
)

type currentMatchUnitPort struct {
	r     CurrentMatchReceipt
	e     error
	calls int
	after func()
}

func (p *currentMatchUnitPort) ReadOwnCurrentMatch(context.Context, CurrentMatch) (CurrentMatchReceipt, error) {
	p.calls++
	if p.after != nil {
		p.after()
	}
	return p.r, p.e
}
func (*currentMatchUnitPort) RevalidateOwnCurrentMatch(context.Context, CurrentMatch, CurrentMatchReceipt) error {
	return nil
}
func currentReadMatchInput() CurrentMatch {
	return CurrentMatch{identity.Actor{ID: currentReadOwner, AccountType: "person"}, [32]byte{1}, currentReadTask}
}
func currentReadMatchReceipt(q CurrentMatch) CurrentMatchReceipt {
	at := time.Now().UTC()
	s := newpeople.HumanReceipt{Response: newpeople.Response{Source: "RULE_BASED", RuleVersion: newpeople.RuleVersion, SourceIntentID: q.SourceIntentID, Candidates: []newpeople.Candidate{}}, Proof: strings.Repeat("b", 64), Seal: strings.Repeat("c", 64), ExpiresAt: at.Add(20 * time.Second)}
	d := currentReadDecision(PersonMatch, q.Actor.ID, s.Proof, CurrentMatchDigest(q), at, s.ExpiresAt)
	return CurrentMatchReceipt{d, s, at, strings.Repeat("d", 64)}
}
func TestCurrentReadonlyMatchSeparationAndOriginalCandidateIDs(t *testing.T) {
	q := currentReadMatchInput()
	r := currentReadMatchReceipt(q)
	p := &currentMatchUnitPort{r: r}
	got, e := ReadCurrentMatch(context.Background(), p, q)
	if e != nil || got.Source.Response.Candidates == nil || p.calls != 1 {
		t.Fatal("real empty", e)
	}
	c := newpeople.Candidate{SourceIntentID: q.SourceIntentID, CandidateIntentID: currentReadEntity, AccountID: currentReadEntity, DisplayName: "本人公开声明", Category: "badminton", Modality: "ONLINE", ReasonCodes: []string{"DECLARED_CATEGORY"}, Reasons: []string{"声明相容；不是已建立关系"}}
	r.Source.Response.Candidates = []newpeople.Candidate{c}
	if !r.Valid(q) {
		t.Fatal("original candidates rejected")
	}
	for name, m := range map[string]func(*CurrentMatchReceipt){"unknown_rules": func(r *CurrentMatchReceipt) { r.Source.Response.RuleVersion = "invented" }, "false_native_source": func(r *CurrentMatchReceipt) { r.Source.Response.Source = "MODEL_RESULT" }, "logical_operation": func(r *CurrentMatchReceipt) { r.Decision.LogicalOperationID = currentReadEntity }, "unknown_modality": func(r *CurrentMatchReceipt) { r.Source.Response.Candidates[0].Modality = "GUESSED" }, "duplicate_person": func(r *CurrentMatchReceipt) {
		c := r.Source.Response.Candidates[0]
		c.CandidateIntentID = "49000000-0000-4000-8000-000000000004"
		r.Source.Response.Candidates = append(r.Source.Response.Candidates, c)
	}, "source": func(r *CurrentMatchReceipt) { r.Source.Response.SourceIntentID = currentReadEntity }, "foreign_candidate_source": func(r *CurrentMatchReceipt) { r.Source.Response.Candidates[0].SourceIntentID = currentReadEntity }, "fabricated": func(r *CurrentMatchReceipt) { r.Source.Response.Candidates[0].AccountID = "guess" }, "self": func(r *CurrentMatchReceipt) { r.Source.Response.Candidates[0].AccountID = q.Actor.ID }, "nil": func(r *CurrentMatchReceipt) { r.Source.Response.Candidates = nil }, "public_person_purpose": func(r *CurrentMatchReceipt) { r.Decision.Purpose = "HUMAN_READ_CURRENT_PUBLIC_PERSON" }, "expired_shape": func(r *CurrentMatchReceipt) { r.Source.ExpiresAt = r.ObservedAt }} {
		t.Run(name, func(t *testing.T) {
			copy := r
			copy.Source.Response.Candidates = append([]newpeople.Candidate{}, r.Source.Response.Candidates...)
			m(&copy)
			if copy.Valid(q) {
				t.Fatal("incorrect matched source accepted")
			}
		})
	}
}
func TestCurrentReadonlyMatchUnavailableAndNoWireAuthority(t *testing.T) {
	q := currentReadMatchInput()
	var missing *currentMatchUnitPort
	for _, p := range []CurrentMatchPort{nil, missing} {
		if _, e := ReadCurrentMatch(context.Background(), p, q); !errors.Is(e, ErrUnavailable) {
			t.Fatal("port bypass", e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &currentMatchUnitPort{r: currentReadMatchReceipt(q), after: cancel}
	if _, e := ReadCurrentMatch(ctx, p, q); e == nil {
		t.Fatal("late match released")
	}
	for _, actor := range []identity.Actor{{ID: q.Actor.ID, AccountType: "organization"}, {ID: q.Actor.ID, AccountType: "business"}, {}} {
		x := q
		x.Actor = actor
		if x.Valid() {
			t.Fatal("nonpersonal scope")
		}
	}
	for _, v := range []any{q, currentReadMatchReceipt(q)} {
		if _, e := json.Marshal(v); !errors.Is(e, ErrServerOnly) {
			t.Fatal("serialized control")
		}
	}
	var i CurrentMatch
	var r CurrentMatchReceipt
	for _, dst := range []any{&i, &r} {
		if e := json.Unmarshal([]byte(`{"confirmed":true}`), dst); !errors.Is(e, ErrServerOnly) {
			t.Fatal("model/JSON granted match")
		}
	}
}
