package agenttool

import (
	"context"
	"encoding/json"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
)

const PlaceSearch = "place.search"
const PersonSearch = "person.search"

// CurrentSearch is constructed from an existing human Task, not decoded from a
// model ActionProposal. It does not enlarge the Activity-only ModelRun schema.
type CurrentSearch struct {
	Access arp.Access
	Query  arp.Query
}

func CurrentSearchTool(kind string) string {
	switch kind {
	case "place":
		return PlaceSearch
	case "person":
		return PersonSearch
	}
	return ""
}
func (q CurrentSearch) Valid() bool {
	return q.Access.Valid() && agentplanner.ValidID(q.Access.Actor.ID) && agentplanner.ValidID(q.Access.TaskID) && q.Query.Valid() && CurrentSearchTool(q.Query.Kind) != ""
}
func (CurrentSearch) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (q *CurrentSearch) UnmarshalJSON([]byte) error { *q = CurrentSearch{}; return ErrServerOnly }

// These native receipts cannot be supplied via the wire. Decision is descriptive;
// the original source seal and the private compound seal are still checked by
// the actual Store at the final response boundary.
type CurrentSearchReceipt struct {
	Decision Decision
	Source   arp.Receipt
	Seal     string
}

func (CurrentSearchReceipt) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (q *CurrentSearchReceipt) UnmarshalJSON([]byte) error {
	*q = CurrentSearchReceipt{}
	return ErrServerOnly
}

type CurrentSearchPort interface {
	ReadOwnCurrentSearch(context.Context, CurrentSearch) (CurrentSearchReceipt, error)
	RevalidateOwnCurrentSearch(context.Context, CurrentSearch, CurrentSearchReceipt) error
}

func currentPortMissing(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func currentDecisionShape(d Decision, tool, owner, resource, arguments string, observed, until time.Time) bool {
	meta, ok := Lookup(tool)
	return ok && meta.Kind == "READ" && d.SchemaVersion == Schema && d.Disposition == Allow &&
		agentplanner.ValidID(d.DecisionID) && agentplanner.ValidID(d.ActionID) && agentplanner.ValidID(d.LogicalOperationID) &&
		d.Tool == tool && d.ToolVersion == meta.Version && d.ActorID == owner && d.SubjectType == "PERSON" && d.SubjectID == owner && agentplanner.ValidID(d.AgentID) &&
		len(d.PolicyVersion) == 64 && d.ResourceVersion == resource && d.ArgumentsDigest == arguments && d.Purpose == meta.Purpose &&
		len(d.DataDestinations) == 1 && d.DataDestinations[0] == "LOCAL_OWNER" && len(d.ReasonCodes) == 1 && d.ReasonCodes[0] == "CURRENT_NATIVE_HUMAN_READ_NO_MACHINE_AUTHORITY" &&
		d.ObservedAt.Equal(observed) && d.ExpiresAt.Equal(until) && !observed.IsZero() && until.After(observed) && until.Sub(observed) <= 30*time.Second
}
func (r CurrentSearchReceipt) Valid(q CurrentSearch) bool {
	if !q.Valid() || !r.Source.Valid() || len(r.Seal) != 64 || r.Decision.LogicalOperationID != q.Access.TaskID || !currentDecisionShape(r.Decision, CurrentSearchTool(q.Query.Kind), q.Access.Actor.ID, r.Source.Proof, CurrentSearchDigest(q), r.Source.ObservedAt, r.Source.ValidUntil) {
		return false
	}
	for _, i := range r.Source.Items {
		if i.Entity.Type != q.Query.Kind || i.Scope != arp.AuthorizedView || !agentplanner.ValidID(i.Entity.ID) {
			return false
		}
	}
	if len(r.Source.Activities) != 0 {
		return false
	}
	if q.Query.Kind == "person" && (len(r.Source.Places) != 0 || len(r.Source.PublicCommercialRefs) != 0) {
		return false
	}
	return true
}

func ReadCurrentSearch(ctx context.Context, port CurrentSearchPort, q CurrentSearch) (CurrentSearchReceipt, error) {
	if ctx == nil || !q.Valid() {
		return CurrentSearchReceipt{}, ErrInvalid
	}
	if e := ctx.Err(); e != nil {
		return CurrentSearchReceipt{}, e
	}
	if currentPortMissing(port) {
		return CurrentSearchReceipt{}, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	started := time.Now()
	r, e := port.ReadOwnCurrentSearch(bounded, q)
	if e != nil {
		return CurrentSearchReceipt{}, e
	}
	if bounded.Err() != nil || !r.Valid(q) || time.Since(started) >= r.Source.ValidUntil.Sub(r.Source.ObservedAt) {
		return CurrentSearchReceipt{}, ErrChanged
	}
	return r, nil
}

// Digest is based on the exact server-only values, not MarshalJSON (which is
// deliberately forbidden for the CurrentSearch control type).
func (q CurrentSearch) digestValue() any {
	return struct {
		Access arp.Access
		Query  arp.Query
	}{q.Access, q.Query}
}
func CurrentSearchDigest(q CurrentSearch) string { return Digest(q.digestValue()) }

// Ensure the exported receipt fields never accidentally become a JSON grant.
var _ json.Marshaler = CurrentSearchReceipt{}
