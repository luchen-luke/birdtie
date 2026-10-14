package agenttool

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"reflect"
	"testing"
)

func TestAgentActivityJoinToolMetadataNeverWidensReadonlyPlanner(t *testing.T) {
	d, ok := Lookup(ActivityJoin)
	if !ok || d.Kind != "WRITE" || d.Purpose != "OWN_ACTIVITY_JOIN_ACTION" || d.Reconciliation == "NO_EFFECT" {
		t.Fatal("closed descriptor missing")
	}
	if r := Restrict(ActivityJoin, actorref.Person, agentautonomy.LevelPrepare); r.Denied || !r.NeedsConfirmation {
		t.Fatal(r)
	}
	for _, kind := range []actorref.Type{actorref.Organization, actorref.Business, actorref.Community} {
		if !Restrict(ActivityJoin, kind, agentautonomy.LevelPrepare).Denied {
			t.Fatal("cross principal allowed")
		}
	}
	if agentplanner.Proposal("25000000-0000-4000-8000-000000000001", 0, ActivityJoin, agentplanner.Arguments{ActivityID: "25000000-0000-4000-8000-000000000002"}, "native-source", "inspect").Valid() {
		t.Fatal("readonly model planner accepted write")
	}
	var schema struct {
		Type       string                       `json:"type"`
		Additional bool                         `json:"additionalProperties"`
		Required   []string                     `json:"required"`
		Properties map[string]map[string]string `json:"properties"`
	}
	if json.Unmarshal(d.InputSchema, &schema) != nil || schema.Type != "object" || schema.Additional || !reflect.DeepEqual(schema.Required, []string{"taskId", "logicalOperationId", "actionId", "activityId", "expectedTaskUpdatedAt", "replyTurnDigest"}) || len(schema.Properties) != 6 || schema.Properties["expectedTaskUpdatedAt"]["format"] != "date-time" || schema.Properties["replyTurnDigest"]["pattern"] != "^[0-9a-f]{64}$" {
		t.Fatal("tool metadata lost exact displayed native source selectors")
	}
}
