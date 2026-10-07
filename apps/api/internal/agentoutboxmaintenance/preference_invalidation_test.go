package agentoutboxmaintenance

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentoutbox"
	"testing"
)

func TestPreferenceInvalidationRunnerClosedReceipt(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "metadata_complete", true: "unknown_commit_stops"}[unknown], func(t *testing.T) {
			r, c, p := records()
			e := &r.Event
			e.SchemaVersion = agentoutbox.PreferenceSchema
			e.EventType = agentoutbox.PreferenceUpdated
			e.Source.Type = agentoutbox.PreferenceSource
			e.Source.ID = e.AgentID
			e.Source.Revision = 2
			e.Source.Status = agentoutbox.PreferenceConfigured
			e.LogicalOperationID = agentoutbox.PreferenceOperationID(e.Source.ID, e.Source.Revision, e.EventType)
			e.RootTraceID = e.LogicalOperationID
			e.EventID = agentoutbox.StableEventID(*e)
			c.EventID = e.EventID
			c.HandlerVersion = agentoutbox.PreferenceHandler
			p.EventID = e.EventID
			p.HandlerVersion = c.HandlerVersion
			p.State = agentoutbox.MemoryComplete
			p.Reason = agentoutbox.ReasonCleanupComplete
			s := &testStore{claim: func(context.Context, actorref.PrincipalRef, string, agentoutbox.HandlerVersion) (agentoutbox.Record, agentoutbox.Claim, error) {
				return r, c, nil
			}, consume: func(context.Context, agentoutbox.Claim) (agentoutbox.ConsumerRecord, error) {
				if unknown {
					return agentoutbox.ConsumerRecord{}, errors.New("UNIT_UNKNOWN_COMMIT")
				}
				return p, nil
			}}
			o := options()
			o.Handler = agentoutbox.PreferenceHandler
			report := Run(context.Background(), s, o)
			if s.claims != 1 || s.consumes != 1 || (unknown && (report.Status != "STOPPED" || report.ConfirmedReceipts != 0)) || (!unknown && (report.Counts.MaintenanceComplete != 1 || report.ConfirmedReceipts != 1)) {
				t.Fatal(report)
			}
		})
	}
}
