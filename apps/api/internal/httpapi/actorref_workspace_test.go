package httpapi

import (
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
)

func TestWorkspaceTaskRequiresMatchingTypedPrincipal(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	task := agentworkspace.Task{PrincipalType: "person", PrincipalID: id}
	if !workspaceTaskMatches(task, "PERSON", id) {
		t.Fatal("person task should match its person principal")
	}
	for _, kind := range []string{"ORGANIZATION", "BUSINESS", "COMMUNITY"} {
		if workspaceTaskMatches(task, kind, id) {
			t.Fatalf("task leaked into %s namespace with the same UUID", kind)
		}
	}
	task.PrincipalType = "organization"
	if !workspaceTaskMatches(task, "organization", id) || workspaceTaskMatches(task, "person", id) {
		t.Fatal("organization task principal mismatch")
	}
	task.PrincipalID = "invalid"
	if workspaceTaskMatches(task, "organization", id) {
		t.Fatal("invalid stored principal matched")
	}
}

func TestExistingAgentWorkspacesUseSharedRolePolicy(t *testing.T) {
	personalType, personal := workspaceAgentPolicy(false)
	organizationType, organization := workspaceAgentPolicy(true)
	if personalType != "person" || string(personal.Role) != "PERSONAL" ||
		len(personal.PermissionStrings()) != 2 ||
		organizationType != "organization" || string(organization.Role) != "ORGANIZATION" ||
		len(organization.PermissionStrings()) != 2 {
		t.Fatalf("workspace policy changed: personal=%+v organization=%+v", personal, organization)
	}
}
