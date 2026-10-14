package agenttool

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
)

// Metadata alone neither widens the read-only planner nor mints a Call. Only
// the separate current native activity-join approval adapter can execute it.
const ActivityJoin = "activity.join"

func init() {
	catalogue = append(catalogue, Descriptor{Tool: ActivityJoin, Version: "activity.join.v1", InputSchema: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["taskId","logicalOperationId","actionId","activityId","expectedTaskUpdatedAt","replyTurnDigest"],"properties":{"taskId":{"type":"string","format":"uuid"},"logicalOperationId":{"type":"string","format":"uuid"},"actionId":{"type":"string","format":"uuid"},"activityId":{"type":"string","format":"uuid"},"expectedTaskUpdatedAt":{"type":"string","format":"date-time"},"replyTurnDigest":{"type":"string","pattern":"^[0-9a-f]{64}$"}}}`), OutputSchema: json.RawMessage(`{"type":"object","required":["schemaVersion","owner","agentId","approvalId","dispatchId","status","committedAt"],"properties":{"schemaVersion":{"const":"air.activity_join.v1"},"status":{"enum":["DISPATCH_COMMITTED","IN_FLIGHT","UNKNOWN_OUTCOME","SUCCEEDED","NO_EFFECT"]}}}`), Kind: "WRITE", ResourceScope: "OWN_PERSON_PRINCIPAL_CURRENT_PUBLIC_ACTIVITY_JOIN", Risk: "CONFIRMATION", RequiredPermissions: []string{"CURRENT_PERSONAL_TASK_OWNER", "CURRENT_SESSION_AGENT_SOURCE", "ORIGINAL_ACTIVITY_JOIN_ACL", "EXACT_VERSION_ACTIVITY_JOIN_GRANT"}, Operation: agentautonomy.PrepareRegistration, Purpose: "OWN_ACTIVITY_JOIN_ACTION", Idempotency: "OWNER_LOGICAL_OPERATION_ACTION_ACTIVITY_JOIN", Reconciliation: "ORIGINAL_AGENT_ACTIVITY_JOIN_EFFECT_AND_PERMANENT_FENCE"})
}
