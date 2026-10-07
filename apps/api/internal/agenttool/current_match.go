package agenttool

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"time"
)

const PersonMatch = "person.match"

type CurrentMatch struct {
	Actor          identity.Actor
	SessionDigest  [32]byte
	SourceIntentID string
}

func (q CurrentMatch) Valid() bool {
	return q.Actor.AccountType == "person" && agentplanner.ValidID(q.Actor.ID) && q.SessionDigest != ([32]byte{}) && agentplanner.ValidID(q.SourceIntentID)
}
func (CurrentMatch) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (q *CurrentMatch) UnmarshalJSON([]byte) error { *q = CurrentMatch{}; return ErrServerOnly }
func CurrentMatchDigest(q CurrentMatch) string {
	return Digest(struct {
		Actor          identity.Actor
		SessionDigest  [32]byte
		SourceIntentID string
	}{q.Actor, q.SessionDigest, q.SourceIntentID})
}

type CurrentMatchReceipt struct {
	Decision   Decision
	Source     newpeople.HumanReceipt
	ObservedAt time.Time
	Seal       string
}

func (CurrentMatchReceipt) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (q *CurrentMatchReceipt) UnmarshalJSON([]byte) error {
	*q = CurrentMatchReceipt{}
	return ErrServerOnly
}

type CurrentMatchPort interface {
	ReadOwnCurrentMatch(context.Context, CurrentMatch) (CurrentMatchReceipt, error)
	RevalidateOwnCurrentMatch(context.Context, CurrentMatch, CurrentMatchReceipt) error
}

func (r CurrentMatchReceipt) Valid(q CurrentMatch) bool {
	if !q.Valid() || len(r.Seal) != 64 || len(r.Source.Seal) != 64 || len(r.Source.Proof) != 64 || r.Decision.LogicalOperationID != q.SourceIntentID || r.Source.Response.Source != "RULE_BASED" || r.Source.Response.RuleVersion != newpeople.RuleVersion || r.Source.Response.SourceIntentID != q.SourceIntentID || r.Source.Response.Candidates == nil || len(r.Source.Response.Candidates) > 50 || !currentDecisionShape(r.Decision, PersonMatch, q.Actor.ID, r.Source.Proof, CurrentMatchDigest(q), r.ObservedAt, r.Source.ExpiresAt) {
		return false
	}
	seen := map[string]bool{}
	people := map[string]bool{}
	for _, c := range r.Source.Response.Candidates {
		if c.SourceIntentID != q.SourceIntentID || !agentplanner.ValidID(c.CandidateIntentID) || !agentplanner.ValidID(c.AccountID) || c.AccountID == q.Actor.ID || seen[c.CandidateIntentID] || people[c.AccountID] || c.Reasons == nil || c.ReasonCodes == nil || (c.Modality != "ONLINE" && c.Modality != "IN_PERSON" && c.Modality != "HYBRID") {
			return false
		}
		seen[c.CandidateIntentID] = true
		people[c.AccountID] = true
	}
	return true
}
func ReadCurrentMatch(ctx context.Context, port CurrentMatchPort, q CurrentMatch) (CurrentMatchReceipt, error) {
	if ctx == nil || !q.Valid() {
		return CurrentMatchReceipt{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return CurrentMatchReceipt{}, e
	}
	if currentPortMissing(port) {
		return CurrentMatchReceipt{}, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	r, e := port.ReadOwnCurrentMatch(bounded, q)
	if e != nil {
		return CurrentMatchReceipt{}, e
	}
	if bounded.Err() != nil || !r.Valid(q) || time.Since(started) >= r.Source.ExpiresAt.Sub(r.ObservedAt) {
		return CurrentMatchReceipt{}, ErrChanged
	}
	return r, nil
}
