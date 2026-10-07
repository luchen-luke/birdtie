package agentaction

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"time"
)

func ValidBinding(b Binding, p agenttool.SandboxProposal) bool {
	for _, id := range []string{b.ApprovalID, b.TenantID, b.ActorID, b.SubjectID, b.AgentID, b.SessionID, b.TaskID, b.LogicalOperationID, b.ActionID, b.TargetID, b.GrantID} {
		if !agentplanner.ValidID(id) {
			return false
		}
	}
	return p.Valid() && b.Schema == Schema && b.TenantID == b.ActorID && b.ActorID == b.SubjectID && b.SubjectType == "PERSON" && b.TargetID == b.SubjectID && b.Tool == agenttool.SandboxWrite && b.ToolVersion == ToolVersion && b.LogicalOperationID == p.LogicalOperationID && b.ActionID == p.ActionID && b.TargetID == p.TargetID && b.SourceVersion == p.ResourceVersion && b.PayloadDigest == agenttool.Digest(p) && hex64(b.SourceVersion) && hex64(b.AuthorityVersion) && hex64(b.PolicyVersion) && b.SourceGeneration != "" && b.ConsentPurpose == Purpose && b.GrantRevision == 1 && b.MembershipVersion == "NONE_PERSON_ONLY" && !b.ObservedAt.IsZero() && b.ExpiresAt.After(b.ObservedAt) && b.ExpiresAt.Sub(b.ObservedAt) <= 30*time.Second
}
func hex64(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}
func Canonical(b Binding, p agenttool.SandboxProposal) (string, string, error) {
	if !ValidBinding(b, p) {
		return "", "", ErrInvalid
	}
	raw, e := json.Marshal(b)
	if e != nil {
		return "", "", ErrInvalid
	}
	return string(raw), Digest(string(raw)), nil
}
func Digest(canonical string) string {
	h := sha256.Sum256([]byte("birdtie.sandbox-approval.v1:" + canonical))
	return hex.EncodeToString(h[:])
}

// Handler/tool versions and content digests are intentionally not in this key.
// They are approval bindings. Deliberate new operations retain separate effects.
func EffectKey(b Binding) string {
	raw, _ := json.Marshal([]string{b.TenantID, b.LogicalOperationID, b.ActionID, EffectKind})
	h := sha256.Sum256(append([]byte("birdtie.sandbox-effect.v1:"), raw...))
	return hex.EncodeToString(h[:])
}
