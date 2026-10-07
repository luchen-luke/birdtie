package agenttool

import (
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
)

// Restriction applies the original role/044 limits. Passing it is not ALLOW:
// only original current native domain/identity/source facts can mint a Call.
type Restriction struct {
	Denied            bool
	NeedsConfirmation bool
	Reason            string
}

func Restrict(tool string, principal actorref.Type, level agentautonomy.Level) Restriction {
	d, ok := Lookup(tool)
	if !ok {
		return Restriction{true, false, "UNKNOWN_TOOL"}
	}
	role := agentruntime.ForType(principal)
	if principal != actorref.Person || !role.Available || role.Role != agentruntime.PersonalAgent {
		return Restriction{true, false, "PERSONAL_TOOL_SCOPE_REQUIRED"}
	}
	op, ok := agentautonomy.Lookup(d.Operation)
	if !ok {
		return Restriction{true, false, "UNKNOWN_OPERATION"}
	}
	rank := func(l agentautonomy.Level) int {
		for i, x := range agentautonomy.Levels() {
			if l == x {
				return i
			}
		}
		return -1
	}
	if rank(level) < 0 || level == agentautonomy.LevelDelegate || rank(level) < rank(op.MinimumLevel) {
		return Restriction{true, false, "AUTONOMY_LIMIT_DENIED"}
	}
	// Registered public reads and own sandbox have no counterparty/social
	// purpose. A future social tool cannot bypass the original 041 resolver.
	if op.NeedsSocialPolicy {
		return Restriction{true, false, "SOCIAL_PURPOSE_UNAVAILABLE"}
	}
	return Restriction{false, d.Kind == "WRITE" || op.NeedsHumanConfirmation, "CURRENT_DOMAIN_AUTHORITY_STILL_REQUIRED"}
}
