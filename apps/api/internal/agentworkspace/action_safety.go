package agentworkspace

import (
	"strings"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const (
	OpenNewPeople           = "OPEN_NEW_PEOPLE"
	OpenRelationshipContext = "OPEN_RELATIONSHIP_CONTEXT"
	OpenOrganizationConsole = "OPEN_ORGANIZATION_CONSOLE"
)

type organizationMenuAuthority struct {
	actorID               string
	organizationID        string
	organizationAccountID string
}

// AuthorizedOrganizationConsoleAction may be constructed only from a live
// server-resolved owner/admin menu. The account ID is distinct from the public
// organization ID. A wire Action cannot supply this internal evidence, and
// this navigation evidence never grants permission to publish an Activity.
func AuthorizedOrganizationConsoleAction(actorID, organizationID, organizationAccountID string) Action {
	return Action{Type: OpenOrganizationConsole, Label: "去创建活动",
		TargetType: "organization", TargetID: organizationID,
		organizationMenuAuthority: &organizationMenuAuthority{
			actorID: actorID, organizationID: organizationID,
			organizationAccountID: organizationAccountID,
		}}
}

func actionUUID(id string) bool {
	if id != strings.TrimSpace(id) || id == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	_, err := actorref.Parse("PERSON", id)
	return err == nil
}

// safeNavigationActions is a closed projection, not an action executor.
// Unknown and high-impact actions have no wire representation here, including
// actions whose generated labels claim that a person has already confirmed.
func safeNavigationActions(result Results, task Task) []Action {
	out := []Action{}
	principal, err := task.PrincipalRef()
	if err != nil || task.Status != TaskCompleted || !actionUUID(task.ID) ||
		!actionUUID(task.PrincipalID) || !actionUUID(task.ActingUserID) {
		return out
	}
	resultPrincipal, err := actorref.ParsePrincipal(result.PrincipalType, result.PrincipalID)
	if err != nil || !resultPrincipal.Equal(principal) {
		return out
	}
	switch principal.Type {
	case actorref.Person:
		if result.Workspace != "PERSONAL" || !strings.EqualFold(task.PrincipalID, task.ActingUserID) {
			return out
		}
	case actorref.Organization:
		if result.Workspace != "ORGANIZATION" {
			return out
		}
	default:
		return out
	}
	seen := map[string]bool{}
	for _, action := range result.Actions {
		var safe Action
		switch action.Type {
		case OpenNewPeople, OpenRelationshipContext:
			if principal.Type != actorref.Person || action.TargetType != "" ||
				action.TargetID != "" || action.organizationMenuAuthority != nil {
				continue
			}
			if action.Type == OpenNewPeople && task.Intent == FindNewPeople {
				safe = Action{Type: OpenNewPeople, Label: "找新朋友"}
			} else if action.Type == OpenRelationshipContext && task.Intent == PersonalRelationshipContext {
				safe = Action{Type: OpenRelationshipContext, Label: "查看关系信号"}
			} else {
				continue
			}
		case OpenOrganizationConsole:
			menu := action.organizationMenuAuthority
			if task.Intent != CreateActivity || action.TargetType != "organization" ||
				!actionUUID(action.TargetID) || menu == nil || !actionUUID(menu.actorID) ||
				!actionUUID(menu.organizationID) || !actionUUID(menu.organizationAccountID) ||
				!strings.EqualFold(action.TargetID, menu.organizationID) ||
				!strings.EqualFold(task.ActingUserID, menu.actorID) {
				continue
			}
			if principal.Type == actorref.Organization &&
				(!strings.EqualFold(task.PrincipalID, menu.organizationAccountID) ||
					(result.Role != "OWNER" && result.Role != "ADMIN")) {
				continue
			}
			safe = Action{Type: OpenOrganizationConsole, Label: "去创建活动",
				TargetType: "organization", TargetID: strings.ToLower(action.TargetID),
				organizationMenuAuthority: menu}
		default:
			continue
		}
		key := safe.Type + ":" + safe.TargetID
		if !seen[key] {
			out = append(out, safe)
			seen[key] = true
		}
	}
	return out
}

// SanitizeTaskForResponse copies the persisted task. Internal comparison IDs,
// explanations, inferred preferences and unknown keys cannot become response
// filters. Current query and viewport conditions remain available for restore.
func SanitizeTaskForResponse(task Task) Task {
	task.Filters = explicitResponseFilters(task.Filters)
	return task
}

func explicitResponseFilters(filters map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range filters {
		allowed := false
		switch key {
		case "category":
			allowed = oneOf(value, "", "badminton", "basketball", "football", "sports", "culture")
		case "timePreference":
			allowed = oneOf(value, "", "anytime", "today", "tonight", "tomorrow", "weekend")
		case "distancePreference":
			allowed = oneOf(value, "", "closer")
		case "locationPreference":
			allowed = oneOf(value, "", "city", "viewport")
		case "targetIntent":
			allowed = oneOf(value, "", FindActivity, FindOrganization, FindPlace, AreaDiscovery,
				RefineResults, CompareResults, CreateActivity, PersonalRelationshipContext, FindNewPeople)
		case "currentQuery", "searchTerm":
			// These are bounded, explicit query text, not generated private facts.
			allowed = len(value) <= 240
		}
		if allowed {
			out[key] = value
		}
	}
	if bounds, err := BoundsFromFilters(filters); err == nil && bounds != nil {
		for key, value := range bounds.Filters() {
			out[key] = value
		}
	}
	return out
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
