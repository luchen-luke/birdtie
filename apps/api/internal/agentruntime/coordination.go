package agentruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
)

const (
	CoordinationVersion                    = "agent-coordination-v1"
	CoordinationPurposeAskActivityInterest = "ASK_ACTIVITY_INTEREST"
	CoordinationFieldNextStep              = "NEXT_STEP"
	CoordinationNextStepAskUser            = "ASK_USER"
	CoordinationMaxTTL                     = 15 * time.Minute
)

var (
	ErrInvalidCoordination   = errors.New("invalid coordination contract")
	ErrCoordinationForbidden = errors.New("coordination not authorized")
	ErrCoordinationFactsJSON = errors.New("coordination facts are server-only")
	coordinationUUID         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type CoordinationAgentRef struct {
	AgentID   string                `json:"agentId"`
	Principal actorref.PrincipalRef `json:"principal"`
}

type CoordinationResourceRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// The wire identifies a request only. It never supplies authority or consent.
type CoordinationRequest struct {
	Version   string                  `json:"version"`
	RequestID string                  `json:"requestId"`
	TaskID    string                  `json:"taskId"`
	Sender    CoordinationAgentRef    `json:"sender"`
	Recipient CoordinationAgentRef    `json:"recipient"`
	Purpose   string                  `json:"purpose"`
	Scope     Scope                   `json:"scope"`
	Resource  CoordinationResourceRef `json:"resource"`
	Fields    []string                `json:"fields"`
	ExpiresAt time.Time               `json:"expiresAt"`
}

// ASK_USER is a permission outcome, not delivery, interest or participation.
type CoordinationResponse struct {
	Version   string `json:"version"`
	RequestID string `json:"requestId"`
	NextStep  string `json:"nextStep"`
}

type CoordinationSessionFacts struct {
	Verified     bool
	Active       bool
	ActingUserID string
}

type CoordinationAgentFacts struct {
	Verified    bool
	Active      bool
	OwnerActive bool
	Role        Role
	AgentID     string
	Principal   actorref.PrincipalRef
}

type CoordinationTaskFacts struct {
	Verified           bool
	ID                 string
	OwnerPrincipal     actorref.PrincipalRef
	AgentID            string
	Status             string
	Intent             string
	ResourceAuthorized bool
}

type CoordinationResourceFacts struct {
	Verified         bool
	Reference        CoordinationResourceRef
	Published        bool
	Cancelled        bool
	StartsAt         time.Time
	EndsAt           time.Time
	ExpiresAt        *time.Time
	SenderVisible    bool
	RecipientVisible bool
}

type CoordinationTieFacts struct {
	Verified           bool
	ID                 string
	RequestID          string
	RequestSenderID    string
	RequestRecipientID string
	PersonAID          string
	PersonBID          string
	Active             bool
	RequestScope       string
	RequestState       string
	Blocked            bool
}

// Each owner grants only this request, task, pair, resource, purpose and field.
// These facts have no live resolver/store in AGA-001. Profile, disclosure and
// own-Agent consent flags are not substitutes for independent task grants.
type CoordinationGrant struct {
	Verified           bool
	ID                 string
	OwnerPrincipal     actorref.PrincipalRef
	RequestID          string
	TaskID             string
	SenderAgentID      string
	RecipientAgentID   string
	SenderPrincipal    actorref.PrincipalRef
	RecipientPrincipal actorref.PrincipalRef
	Purpose            string
	Scope              Scope
	Resource           CoordinationResourceRef
	Fields             []string
	HumanConfirmed     bool
	ConfirmedAt        time.Time
	ExpiresAt          time.Time
	RevokedAt          *time.Time
	Revision           int64
	CurrentRevision    int64
}

// CoordinationFacts must come from live server records at every authorization
// boundary. Never decode client JSON or Agent text into these fields. There is
// no transport/callable coordination tool or live task-grant adapter yet; absent
// verified evidence, use the zero value and fail closed.
type CoordinationFacts struct {
	Session        CoordinationSessionFacts
	Sender         CoordinationAgentFacts
	Recipient      CoordinationAgentFacts
	Task           CoordinationTaskFacts
	Resource       CoordinationResourceFacts
	Tie            CoordinationTieFacts
	SenderGrant    CoordinationGrant
	RecipientGrant CoordinationGrant
}

func (CoordinationFacts) MarshalJSON() ([]byte, error) {
	return nil, ErrCoordinationFactsJSON
}

func (facts *CoordinationFacts) UnmarshalJSON([]byte) error {
	*facts = CoordinationFacts{}
	return ErrCoordinationFactsJSON
}

// The token pass enforces exact key spelling and duplicates at every depth;
// encoding/json's usual case-insensitive fields/last-value wins are insufficient.
type coordinationJSONShape struct {
	kind   string
	fields map[string]coordinationJSONShape
}

var coordinationStringShape = coordinationJSONShape{kind: "string"}
var coordinationPrincipalShape = coordinationJSONShape{kind: "object", fields: map[string]coordinationJSONShape{
	"type": coordinationStringShape, "id": coordinationStringShape,
}}
var coordinationAgentShape = coordinationJSONShape{kind: "object", fields: map[string]coordinationJSONShape{
	"agentId": coordinationStringShape, "principal": coordinationPrincipalShape,
}}
var coordinationResourceShape = coordinationJSONShape{kind: "object", fields: map[string]coordinationJSONShape{
	"type": coordinationStringShape, "id": coordinationStringShape,
}}
var coordinationRequestShape = coordinationJSONShape{kind: "object", fields: map[string]coordinationJSONShape{
	"version": coordinationStringShape, "requestId": coordinationStringShape, "taskId": coordinationStringShape,
	"sender": coordinationAgentShape, "recipient": coordinationAgentShape, "purpose": coordinationStringShape,
	"scope": coordinationStringShape, "resource": coordinationResourceShape,
	"fields": {kind: "strings"}, "expiresAt": coordinationStringShape,
}}
var coordinationResponseShape = coordinationJSONShape{kind: "object", fields: map[string]coordinationJSONShape{
	"version": coordinationStringShape, "requestId": coordinationStringShape, "nextStep": coordinationStringShape,
}}

func scanCoordinationJSON(decoder *json.Decoder, shape coordinationJSONShape) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidCoordination
	}
	switch shape.kind {
	case "string":
		if _, ok := token.(string); !ok {
			return ErrInvalidCoordination
		}
	case "strings":
		if token != json.Delim('[') {
			return ErrInvalidCoordination
		}
		count := 0
		for decoder.More() {
			count++
			if count > 1 || scanCoordinationJSON(decoder, coordinationStringShape) != nil {
				return ErrInvalidCoordination
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') || count != 1 {
			return ErrInvalidCoordination
		}
	case "object":
		if token != json.Delim('{') {
			return ErrInvalidCoordination
		}
		seen := make(map[string]bool, len(shape.fields))
		for decoder.More() {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || seen[name] {
				return ErrInvalidCoordination
			}
			child, known := shape.fields[name]
			if !known || scanCoordinationJSON(decoder, child) != nil {
				return ErrInvalidCoordination
			}
			seen[name] = true
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') || len(seen) != len(shape.fields) {
			return ErrInvalidCoordination
		}
	default:
		return ErrInvalidCoordination
	}
	return nil
}

func strictCoordinationJSON(raw []byte, maxBytes int, shape coordinationJSONShape, output any) error {
	if len(raw) == 0 || len(raw) > maxBytes || !utf8.Valid(raw) {
		return ErrInvalidCoordination
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if scanCoordinationJSON(decoder, shape) != nil {
		return ErrInvalidCoordination
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidCoordination
	}
	if json.Unmarshal(raw, output) != nil {
		return ErrInvalidCoordination
	}
	return nil
}

func coordinationID(id string) bool {
	return coordinationUUID.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

func equalCoordinationID(left, right string) bool {
	return coordinationID(left) && coordinationID(right) && strings.EqualFold(left, right)
}

func sameCoordinationPrincipal(left, right actorref.PrincipalRef) bool {
	return left.Type == actorref.Person && right.Type == actorref.Person && equalCoordinationID(left.ID, right.ID)
}

func sameCoordinationResource(left, right CoordinationResourceRef) bool {
	return left.Type == "ACTIVITY" && right.Type == "ACTIVITY" && equalCoordinationID(left.ID, right.ID)
}

func coordinationFields(fields []string) bool {
	return len(fields) == 1 && fields[0] == CoordinationFieldNextStep
}

func normalizedCoordinationRequest(request CoordinationRequest) (CoordinationRequest, bool) {
	if request.Version != CoordinationVersion || !coordinationID(request.RequestID) || !coordinationID(request.TaskID) ||
		request.Purpose != CoordinationPurposeAskActivityInterest || request.Scope != Connection ||
		request.Resource.Type != "ACTIVITY" || !coordinationID(request.Resource.ID) || !coordinationFields(request.Fields) ||
		request.ExpiresAt.IsZero() ||
		!coordinationID(request.Sender.AgentID) || !coordinationID(request.Recipient.AgentID) ||
		request.Sender.Principal.Type != actorref.Person || request.Recipient.Principal.Type != actorref.Person ||
		!coordinationID(request.Sender.Principal.ID) || !coordinationID(request.Recipient.Principal.ID) ||
		equalCoordinationID(request.Sender.AgentID, request.Recipient.AgentID) ||
		equalCoordinationID(request.Sender.Principal.ID, request.Recipient.Principal.ID) {
		return CoordinationRequest{}, false
	}
	request.RequestID, request.TaskID = strings.ToLower(request.RequestID), strings.ToLower(request.TaskID)
	request.Sender.AgentID, request.Recipient.AgentID = strings.ToLower(request.Sender.AgentID), strings.ToLower(request.Recipient.AgentID)
	request.Sender.Principal.ID, request.Recipient.Principal.ID = strings.ToLower(request.Sender.Principal.ID), strings.ToLower(request.Recipient.Principal.ID)
	request.Resource.ID = strings.ToLower(request.Resource.ID)
	request.Fields = []string{CoordinationFieldNextStep}
	request.ExpiresAt = request.ExpiresAt.UTC()
	return request, true
}

func DecodeCoordinationRequest(raw []byte) (CoordinationRequest, error) {
	var request CoordinationRequest
	if strictCoordinationJSON(raw, 2048, coordinationRequestShape, &request) != nil {
		return CoordinationRequest{}, ErrInvalidCoordination
	}
	request, valid := normalizedCoordinationRequest(request)
	if !valid {
		return CoordinationRequest{}, ErrInvalidCoordination
	}
	return request, nil
}

func DecodeCoordinationResponse(raw []byte, expectedRequestID string) (CoordinationResponse, error) {
	var response CoordinationResponse
	if strictCoordinationJSON(raw, 512, coordinationResponseShape, &response) != nil ||
		response.Version != CoordinationVersion || response.NextStep != CoordinationNextStepAskUser ||
		!equalCoordinationID(response.RequestID, expectedRequestID) {
		return CoordinationResponse{}, ErrInvalidCoordination
	}
	response.RequestID = strings.ToLower(response.RequestID)
	return response, nil
}

func validCoordinationAgent(facts CoordinationAgentFacts, reference CoordinationAgentRef) bool {
	return facts.Verified && facts.Active && facts.OwnerActive && facts.Role == PersonalAgent &&
		equalCoordinationID(facts.AgentID, reference.AgentID) && sameCoordinationPrincipal(facts.Principal, reference.Principal)
}

func validCoordinationGrant(now time.Time, request CoordinationRequest, owner actorref.PrincipalRef, grant CoordinationGrant) bool {
	return grant.Verified && coordinationID(grant.ID) && sameCoordinationPrincipal(grant.OwnerPrincipal, owner) &&
		equalCoordinationID(grant.RequestID, request.RequestID) && equalCoordinationID(grant.TaskID, request.TaskID) &&
		equalCoordinationID(grant.SenderAgentID, request.Sender.AgentID) && equalCoordinationID(grant.RecipientAgentID, request.Recipient.AgentID) &&
		sameCoordinationPrincipal(grant.SenderPrincipal, request.Sender.Principal) && sameCoordinationPrincipal(grant.RecipientPrincipal, request.Recipient.Principal) &&
		grant.Purpose == request.Purpose && grant.Scope == request.Scope && sameCoordinationResource(grant.Resource, request.Resource) &&
		coordinationFields(grant.Fields) && grant.HumanConfirmed && !grant.ConfirmedAt.IsZero() && !grant.ConfirmedAt.After(now) &&
		grant.ConfirmedAt.Before(grant.ExpiresAt) && grant.ExpiresAt.After(now) && !grant.ExpiresAt.Before(request.ExpiresAt) &&
		grant.RevokedAt == nil && grant.Revision > 0 && grant.Revision == grant.CurrentRevision
}

func DecideCoordination(now time.Time, request CoordinationRequest, facts CoordinationFacts) AccessDecision {
	request, valid := normalizedCoordinationRequest(request)
	if !valid || now.IsZero() || !request.ExpiresAt.After(now) || request.ExpiresAt.After(now.Add(CoordinationMaxTTL)) {
		return deny("invalid_coordination_request")
	}
	if !facts.Session.Verified || !facts.Session.Active || !equalCoordinationID(facts.Session.ActingUserID, request.Sender.Principal.ID) ||
		!validCoordinationAgent(facts.Sender, request.Sender) || !validCoordinationAgent(facts.Recipient, request.Recipient) {
		return deny("coordination_authority_required")
	}
	task := facts.Task
	if !task.Verified || !equalCoordinationID(task.ID, request.TaskID) ||
		!sameCoordinationPrincipal(task.OwnerPrincipal, request.Sender.Principal) || !equalCoordinationID(task.AgentID, request.Sender.AgentID) ||
		task.Status != "COMPLETED" || task.Intent != "FIND_ACTIVITY" || !task.ResourceAuthorized {
		return deny("coordination_task_required")
	}
	resource := facts.Resource
	if !resource.Verified || !sameCoordinationResource(resource.Reference, request.Resource) || !resource.Published || resource.Cancelled ||
		resource.StartsAt.IsZero() || !resource.EndsAt.After(resource.StartsAt) || !resource.EndsAt.After(now) ||
		(resource.ExpiresAt != nil && !resource.ExpiresAt.After(now)) || !resource.SenderVisible || !resource.RecipientVisible {
		return deny("coordination_resource_required")
	}
	tie := facts.Tie
	first, second := request.Sender.Principal.ID, request.Recipient.Principal.ID
	if first > second {
		first, second = second, first
	}
	if !tie.Verified || !coordinationID(tie.ID) || !coordinationID(tie.RequestID) || !tie.Active || tie.RequestScope != "friend" ||
		tie.RequestState != "accepted" || tie.Blocked || !equalCoordinationID(tie.PersonAID, first) || !equalCoordinationID(tie.PersonBID, second) {
		return deny("coordination_connection_required")
	}
	if !((equalCoordinationID(tie.RequestSenderID, first) && equalCoordinationID(tie.RequestRecipientID, second)) ||
		(equalCoordinationID(tie.RequestSenderID, second) && equalCoordinationID(tie.RequestRecipientID, first))) {
		return deny("coordination_connection_required")
	}
	if !validCoordinationGrant(now, request, request.Sender.Principal, facts.SenderGrant) ||
		!validCoordinationGrant(now, request, request.Recipient.Principal, facts.RecipientGrant) ||
		equalCoordinationID(facts.SenderGrant.ID, facts.RecipientGrant.ID) {
		return deny("coordination_independent_consent_required")
	}
	return allow()
}

// Re-evaluate current facts; a cached AccessDecision cannot construct a response.
func BuildCoordinationResponse(now time.Time, request CoordinationRequest, facts CoordinationFacts) (CoordinationResponse, error) {
	if !DecideCoordination(now, request, facts).Allowed {
		return CoordinationResponse{}, ErrCoordinationForbidden
	}
	return CoordinationResponse{Version: CoordinationVersion, RequestID: strings.ToLower(request.RequestID), NextStep: CoordinationNextStepAskUser}, nil
}
