package agenttool

import (
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentplanner"
	"testing"
)

func TestAgentToolDenyFirstOriginalRoleAndAutonomyMatrix(t *testing.T) {
	for _, principal := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business, actorref.Type("CITY"), actorref.Type("")} {
		for _, level := range append(agentautonomy.Levels(), agentautonomy.Level("MODEL_APPROVED")) {
			for _, tool := range []string{agentplanner.ActivitySearch, agentplanner.ActivityDetail, SandboxWrite, "message.send", "profile.update", "shell.execute"} {
				t.Run(string(principal)+"/"+string(level)+"/"+tool, func(t *testing.T) {
					r := Restrict(tool, principal, level)
					knownRead := tool == agentplanner.ActivitySearch || tool == agentplanner.ActivityDetail
					validLevel := level == agentautonomy.LevelObserve || level == agentautonomy.LevelAssist || level == agentautonomy.LevelPrepare
					permittedLimit := principal == actorref.Person && validLevel && (knownRead || (tool == SandboxWrite && level == agentautonomy.LevelPrepare))
					if r.Denied == permittedLimit {
						t.Fatal("deny-first matrix mismatch", r)
					}
					if !r.Denied && r.NeedsConfirmation != (tool == SandboxWrite) {
						t.Fatal("write confirmation lost")
					}
				})
			}
		}
	}
}
