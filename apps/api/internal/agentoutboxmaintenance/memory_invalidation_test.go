package agentoutboxmaintenance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
)

func TestMemoryInvalidationMaintenanceUnitActualRunnerProgressAndUnknown(t *testing.T) {
	for _, tc := range []struct {
		name      string
		state     agentoutbox.State
		reason    agentoutbox.ReasonCode
		err       error
		confirmed bool
	}{
		{"complete", agentoutbox.MemoryComplete, agentoutbox.ReasonCleanupComplete, nil, true},
		{"more", agentoutbox.Pending, agentoutbox.ReasonCleanupMore, nil, true},
		{"spent_root_unfinished", agentoutbox.DeadLetter, agentoutbox.ReasonRootExhausted, nil, true},
		{"ambiguous_commit", agentoutbox.MemoryComplete, agentoutbox.ReasonCleanupComplete, errors.New("UNIT_SECRET_UNKNOWN"), false},
		{"old_unavailable_not_receipt", agentoutbox.MemoryComplete, agentoutbox.ReasonCleanupComplete, agentoutbox.ErrUnavailable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, c, p := records()
			r.Event.SchemaVersion = agentoutbox.MemorySchema
			r.Event.EventType = agentoutbox.MemoryUpdated
			r.Event.Source.Type = agentoutbox.MemorySource
			r.Event.Source.Status = agentoutbox.MemoryActive
			r.Event.Source.Revision = 2
			r.Event.Source.Fingerprint = strings.Repeat("a", 64)
			r.Event.LogicalOperationID = agentoutbox.MemoryOperationID(r.Event.Source.ID, r.Event.Source.Revision, r.Event.EventType)
			r.Event.RootTraceID = r.Event.LogicalOperationID
			r.Event.EventID = agentoutbox.StableEventID(r.Event)
			c.EventID = r.Event.EventID
			c.HandlerVersion = agentoutbox.MemoryHandler
			p.EventID = c.EventID
			p.HandlerVersion = c.HandlerVersion
			p.State = tc.state
			p.Reason = tc.reason
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, nil
			}, consume: func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) { return p, tc.err }}
			o := options()
			o.Handler = agentoutbox.MemoryHandler
			result := Run(context.Background(), s, o)
			if (result.ConfirmedReceipts == 1) != tc.confirmed || s.claims != 1 || s.consumes != 1 || result.BusinessExecution != "UNAVAILABLE" {
				t.Fatal(result, s.claims, s.consumes)
			}
			if tc.confirmed && result.Status != "FINISHED" || !tc.confirmed && result.Status != "STOPPED" {
				t.Fatal(result)
			}
			if tc.state == agentoutbox.Pending && tc.confirmed && result.Counts.MaintenanceProgress != 1 {
				t.Fatal("progress concealed as done", result)
			}
		})
	}
}
