package contextgraph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"io"
	"time"
)

const CommunityInterestSchema = "human-community-interest-v1"
const CommunityInterestResource = "person_community_declaration"
const CommunityInterestPurpose = "HUMAN_COMMUNITY_INTEREST_DECLARATION"

var ErrInterestInvalid = errors.New("invalid community interest")
var ErrInterestDenied = errors.New("community interest denied")
var ErrInterestChanged = errors.New("community interest changed")
var ErrInterestUnavailable = errors.New("community interest unavailable")

type CommunityInterestOption struct {
	CommunityID string `json:"communityId"`
	Name        string `json:"name"`
}
type CommunityInterestRecord struct {
	CommunityID     string `json:"communityId"`
	ContextID       string `json:"contextId,omitempty"`
	Name            string `json:"name"`
	SourceAvailable bool   `json:"sourceAvailable"`
	Relation        string `json:"relation"`
	State           string `json:"state"`
}
type CommunityInterestView struct {
	Limit             int                       `json:"limit"`
	Truncated         bool                      `json:"truncated"`
	SchemaVersion     string                    `json:"schemaVersion"`
	OwnerID           string                    `json:"ownerId"`
	AgentID           string                    `json:"agentId"`
	ObservedAt        time.Time                 `json:"observedAt"`
	Records           []CommunityInterestRecord `json:"records"`
	Options           []CommunityInterestOption `json:"options"`
	ModelAccess       bool                      `json:"modelAccess"`
	SendAllowed       bool                      `json:"sendAllowed"`
	MembershipGranted bool                      `json:"membershipGranted"`
}
type CommunityInterestPreview struct {
	SchemaVersion string `json:"schemaVersion"`
	OwnerID       string `json:"ownerId"`
	AgentID       string `json:"agentId"`
	CommunityInterestRecord
	Operation         string    `json:"operation"`
	TargetState       string    `json:"targetState"`
	Preview           string    `json:"preview"`
	ObservedAt        time.Time `json:"observedAt"`
	ExpiresAt         time.Time `json:"expiresAt"`
	Consequence       string    `json:"consequence"`
	ModelAccess       bool      `json:"modelAccess"`
	SendAllowed       bool      `json:"sendAllowed"`
	MembershipGranted bool      `json:"membershipGranted"`
}
type CommunityInterestInput struct {
	CommunityID string `json:"communityId"`
	Operation   string `json:"operation"`
}
type CommunityInterestStore interface {
	ReadOwnCommunityInterests(context.Context, agentprofile.PrivateAccess, bool) (CommunityInterestView, error)
	PreviewOwnCommunityInterest(context.Context, agentprofile.PrivateAccess, CommunityInterestInput) (CommunityInterestPreview, error)
	ApproveOwnCommunityInterest(context.Context, agentprofile.PrivateAccess, string) (CommunityInterestView, error)
}

func ValidateCommunityInterestInput(v CommunityInterestInput) error {
	if !uuid.MatchString(v.CommunityID) || v.CommunityID == "00000000-0000-0000-0000-000000000000" || (v.Operation != "PUBLIC" && v.Operation != "PRIVATE" && v.Operation != "DELETE") {
		return ErrInterestInvalid
	}
	return nil
}

// The small domain decoder rejects duplicate/unknown keys, selectors, nulls and trailing data.
func DecodeCommunityInterestBody(raw []byte, approve bool) (CommunityInterestInput, string, error) {
	var v CommunityInterestInput
	if len(raw) == 0 || len(raw) > 32768 {
		return v, "", ErrInterestInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return v, "", ErrInterestInvalid
	}
	m := map[string]string{}
	for d.More() {
		k, e := d.Token()
		if e != nil {
			return v, "", ErrInterestInvalid
		}
		key, ok := k.(string)
		if !ok {
			return v, "", ErrInterestInvalid
		}
		if _, ok = m[key]; ok {
			return v, "", ErrInterestInvalid
		}
		var val string
		if e = d.Decode(&val); e != nil || val == "" {
			return v, "", ErrInterestInvalid
		}
		m[key] = val
	}
	if _, e = d.Token(); e != nil {
		return v, "", ErrInterestInvalid
	}
	if _, e = d.Token(); e != io.EOF {
		return v, "", ErrInterestInvalid
	}
	if approve {
		if len(m) != 1 || m["preview"] == "" || len(m["preview"]) > 24000 {
			return v, "", ErrInterestInvalid
		}
		return v, m["preview"], nil
	}
	if len(m) != 2 {
		return v, "", ErrInterestInvalid
	}
	v = CommunityInterestInput{CommunityID: m["communityId"], Operation: m["operation"]}
	return v, "", ValidateCommunityInterestInput(v)
}
