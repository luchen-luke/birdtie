package modelrequestrun

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/modelresilience"
)

func fixtureID(n int) string { return fmt.Sprintf("11111111-1111-4111-8111-%012d", n) }
func fixtureControl() (Control, time.Time) {
	now := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	return Control{SchemaVersion: SchemaVersion, ModelRunID: fixtureID(1), Owner: actorref.PrincipalRef{Type: actorref.Person, ID: fixtureID(2)}, TaskID: fixtureID(3), RootTraceID: fixtureID(4), BindingID: fixtureID(5), State: RunPlanned, Revision: 1, Fence: 1, CreatedAt: now.Add(-time.Second), UpdatedAt: now, DeadlineAt: now.Add(time.Minute), LeaseUntil: now.Add(20 * time.Second), Steps: []Step{{Ordinal: 1, OperationID: fixtureID(6), PreviewID: fixtureID(7), PriceVersion: "synthetic-price-a", RequestDigest: strings.Repeat("a", 64), State: StepPlanned}, {Ordinal: 2, OperationID: fixtureID(8), PreviewID: fixtureID(9), PriceVersion: "synthetic-price-b", RequestDigest: strings.Repeat("b", 64), State: StepPlanned}}}, now
}

func TestModelRequestRunStagedClosedMetadataAndOriginalBinding(t *testing.T) {
	v, now := fixtureControl()
	if ValidateControl(v, now) != nil || ValidatePlan(v, now) != nil {
		t.Fatal("valid explicit metadata")
	}
	if v.BindingID == v.ModelRunID {
		t.Fatal("058 binding must remain distinct")
	}
	if len(v.Steps) != 2 || v.Steps[1].ReservationID != "" {
		t.Fatal("unused fallback is not reserved")
	}
	clone := Clone(v)
	clone.Steps[0].OperationID = fixtureID(77)
	if v.Steps[0].OperationID != fixtureID(6) {
		t.Fatal("mutable step alias")
	}
	for _, mutate := range []struct {
		name string
		f    func(*Control)
	}{
		{"schema", func(x *Control) { x.SchemaVersion = "other" }},
		{"organization", func(x *Control) { x.Owner.Type = actorref.Organization }},
		{"zeroOwner", func(x *Control) { x.Owner.ID = "00000000-0000-0000-0000-000000000000" }},
		{"trimmedID", func(x *Control) { x.TaskID = " " + x.TaskID }},
		{"wrongID", func(x *Control) { x.ModelRunID = "not-an-id" }},
		{"bindingNotRun", func(x *Control) { x.ModelRunID = x.BindingID }},
		{"openState", func(x *Control) { x.State = "MODEL_AVAILABLE" }},
		{"zeroRevision", func(x *Control) { x.Revision = 0 }},
		{"zeroFence", func(x *Control) { x.Fence = 0 }},
		{"deadlineMissing", func(x *Control) { x.DeadlineAt = time.Time{} }},
		{"deadlineBeyondWindow", func(x *Control) { x.DeadlineAt = x.CreatedAt.Add(3 * time.Minute) }},
		{"leaseBeyondDeadline", func(x *Control) { x.LeaseUntil = x.DeadlineAt.Add(time.Second) }},
		{"futureUpdated", func(x *Control) { x.UpdatedAt = now.Add(time.Second) }},
		{"noSteps", func(x *Control) { x.Steps = nil }},
		{"unboundedSteps", func(x *Control) {
			for len(x.Steps) <= modelresilience.MaxAttempts {
				x.Steps = append(x.Steps, x.Steps[0])
			}
		}},
		{"duplicateOperation", func(x *Control) { x.Steps[1].OperationID = x.Steps[0].OperationID }},
		{"ordinalGap", func(x *Control) { x.Steps[1].Ordinal = 3 }},
		{"opaqueDigest", func(x *Control) { x.Steps[0].RequestDigest = "private query" }},
		{"unknownStepState", func(x *Control) { x.Steps[0].State = "RETRY_ALLOWED" }},
		{"plannedNotReserved", func(x *Control) { x.Steps[0].ReservationID = x.Steps[0].OperationID }},
		{"reservedMissingOriginal", func(x *Control) { x.Steps[0].State = StepReserved }},
		{"reservedWrongOriginal", func(x *Control) { x.Steps[0].State = StepReserved; x.Steps[0].ReservationID = fixtureID(88) }},
		{"priceBody", func(x *Control) { x.Steps[0].PriceVersion = "url://private" }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			x := Clone(v)
			mutate.f(&x)
			if !errors.Is(ValidateControl(x, now), ErrInvalid) {
				t.Fatal("malformed control accepted")
			}
		})
	}
}

func TestModelRequestRunStagedHistoricalMetadataIsNotPlanPermission(t *testing.T) {
	v, now := fixtureControl()
	later := now.Add(5 * time.Minute)
	if ValidateControl(v, later) != nil {
		t.Fatal("expired metadata must remain readable")
	}
	if !errors.Is(ValidatePlan(v, later), ErrExpired) {
		t.Fatal("expired metadata is not a current plan")
	}
	v.State = RunCancelled
	v.Steps[0].State = StepInFlight
	v.Steps[0].ReservationID = v.Steps[0].OperationID
	if ValidateControl(v, later) != nil || !errors.Is(ValidatePlan(v, now), ErrChanged) {
		t.Fatal("cancelled control cannot start")
	}
	after := Clone(v)
	after.Revision++
	after.UpdatedAt = later
	after.Steps[0].State = StepSettled
	if ValidateObservation(v, after, later) != nil {
		t.Fatal("original accounting may settle after cancellation")
	}
	newReserved := Clone(v)
	newReserved.Revision++
	newReserved.Steps[1].State = StepReserved
	newReserved.Steps[1].ReservationID = newReserved.Steps[1].OperationID
	if !errors.Is(ValidateObservation(v, newReserved, later), ErrChanged) {
		t.Fatal("cancelled Run observed newly reserved unused fallback")
	}
	for _, state := range []StepState{StepInFlight, StepUnknown, StepSettled} {
		t.Run(string(state), func(t *testing.T) {
			before := Clone(v)
			before.Steps[0].State = state
			next := Clone(before)
			next.Revision++
			next.Steps[0].State = StepReserved
			if !errors.Is(ValidateObservation(before, next, later), ErrChanged) {
				t.Fatal("observed accounting used to revive dispatch")
			}
		})
	}
}

func TestModelRequestRunStagedObservationsPreserveIdentityAndShortBound(t *testing.T) {
	v, now := fixtureControl()
	next := Clone(v)
	next.Revision++
	next.State = RunRunning
	next.Steps[0].State = StepReserved
	next.Steps[0].ReservationID = next.Steps[0].OperationID
	if ValidateObservation(v, next, now) != nil {
		t.Fatal("normal original reservation observation")
	}
	for _, mutate := range []struct {
		name string
		f    func(*Control)
	}{
		{"owner", func(x *Control) { x.Owner.ID = fixtureID(90) }},
		{"run", func(x *Control) { x.ModelRunID = fixtureID(90) }},
		{"binding", func(x *Control) { x.BindingID = fixtureID(90) }},
		{"root", func(x *Control) { x.RootTraceID = fixtureID(90) }},
		{"task", func(x *Control) { x.TaskID = fixtureID(90) }},
		{"operation", func(x *Control) { x.Steps[1].OperationID = fixtureID(90) }},
		{"preview", func(x *Control) { x.Steps[1].PreviewID = fixtureID(90) }},
		{"digest", func(x *Control) { x.Steps[1].RequestDigest = strings.Repeat("c", 64) }},
		{"price", func(x *Control) { x.Steps[1].PriceVersion = "synthetic-price-c" }},
		{"renewDeadline", func(x *Control) { x.DeadlineAt = v.DeadlineAt.Add(time.Second) }},
		{"renewLease", func(x *Control) { x.LeaseUntil = v.LeaseUntil.Add(time.Second) }},
		{"oldFence", func(x *Control) { x.Fence = 0 }},
		{"oldRevision", func(x *Control) { x.Revision = v.Revision - 1 }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			x := Clone(next)
			mutate.f(&x)
			if ValidateObservation(v, x, now) == nil {
				t.Fatal("replacement/renewal accepted")
			}
		})
	}
}

func TestModelRequestRunStagedMonotonicDeadlineIsNotJSONAuthority(t *testing.T) {
	observed := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	start := time.Now()
	b, e := NewClockBound(observed, observed.Add(time.Second), start)
	if e != nil || b.Remaining(start) != time.Second {
		t.Fatal("native relative bound")
	}
	short := start.Add(300 * time.Millisecond)
	b, e = b.Tighten(short)
	if e != nil || b.Remaining(start) != 300*time.Millisecond {
		t.Fatal("did not tighten")
	}
	if _, e = b.Tighten(start.Add(time.Second)); !errors.Is(e, ErrChanged) {
		t.Fatal("renewed seen short deadline")
	}
	if b.Remaining(short) != 0 {
		t.Fatal("deadline equality valid")
	}
	if _, e = json.Marshal(b); !errors.Is(e, ErrServerOnly) {
		t.Fatal("persisted private clock bound")
	}
	if e = json.Unmarshal([]byte(`{}`), &b); !errors.Is(e, ErrServerOnly) || b.Remaining(start) != 0 {
		t.Fatal("reconstructed bound")
	}
	for _, x := range []struct{ observed, end, start time.Time }{{time.Time{}, observed.Add(time.Second), start}, {observed, observed, start}, {observed, observed.Add(time.Second), time.Time{}}} {
		if _, e := NewClockBound(x.observed, x.end, x.start); !errors.Is(e, ErrInvalid) {
			t.Fatal("invalid clock anchor")
		}
	}
}

func TestModelRequestRunStagedControlContainsOnlyMetadata(t *testing.T) {
	v, _ := fixtureControl()
	wire, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	for _, key := range []string{"query", "answer", "token", "secret", "providerBody", "budgetUsed", "permission", "encodedResult"} {
		if strings.Contains(string(wire), `"`+key+`"`) {
			t.Fatal("private content/authority in control", key)
		}
	}
	var copy Control
	if json.Unmarshal(wire, &copy) != nil || copy.BindingID != v.BindingID {
		t.Fatal("control metadata roundtrip")
	}
	if copy.Steps[1].ReservationID != "" {
		t.Fatal("JSON metadata charged unused fallback")
	}
}
