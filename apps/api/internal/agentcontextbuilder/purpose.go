package agentcontextbuilder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

// TaskContextRead is a process-local, selected-source Task read. It does not
// authorize inference, model egress, forwarding, actions or Memory writes.
const TaskContextRead = "TASK_CONTEXT_READ"
const PurposePreviewTTL = 5 * time.Minute

type PurposeSelection struct {
	AgentID            string                       `json:"agentId"`
	TaskID             string                       `json:"taskId"`
	CityID             string                       `json:"cityId"`
	CurrentQuery       string                       `json:"currentQuery,omitempty"`
	QueryDigest        string                       `json:"queryDigest"`
	TaskUpdatedAt      time.Time                    `json:"taskUpdatedAt"`
	ProfileFields      []string                     `json:"profileFields"`
	MemoryIDs          []string                     `json:"memoryIds"`
	PlaceIDs           []string                     `json:"placeIds"`
	ActivityIDs        []string                     `json:"activityIds"`
	RelationshipTieIDs []string                     `json:"relationshipTieIds"`
	PolicyFamilies     []agentpolicysettings.Family `json:"policyFamilies"`
	DeadlineAt         time.Time                    `json:"deadlineAt"`
}

func PurposeQueryDigest(query string) string {
	h := sha256.Sum256([]byte(query))
	return hex.EncodeToString(h[:])
}

// NormalizePurposeSelection validates and canonicalizes selectors, not consent.
// A stored selection deliberately has no duplicated raw Task query.
func NormalizePurposeSelection(s PurposeSelection) (PurposeSelection, error) {
	if !validID(s.AgentID) || !validID(s.TaskID) || !validText(s.CityID, 100, false) || !ValidTime(s.TaskUpdatedAt) || !ValidTime(s.DeadlineAt) || !s.TaskUpdatedAt.Equal(s.TaskUpdatedAt.Truncate(time.Microsecond)) || !s.DeadlineAt.Equal(s.DeadlineAt.Truncate(time.Microsecond)) {
		return PurposeSelection{}, ErrInvalid
	}
	if s.CurrentQuery != "" {
		if !validText(s.CurrentQuery, 240, false) {
			return PurposeSelection{}, ErrInvalid
		}
		d := PurposeQueryDigest(s.CurrentQuery)
		if s.QueryDigest != "" && s.QueryDigest != d {
			return PurposeSelection{}, ErrInvalid
		}
		s.QueryDigest = d
	}
	if len(s.QueryDigest) != 64 {
		return PurposeSelection{}, ErrInvalid
	}
	if _, e := hex.DecodeString(s.QueryDigest); e != nil || strings.ToLower(s.QueryDigest) != s.QueryDigest {
		return PurposeSelection{}, ErrInvalid
	}
	if len(s.ProfileFields) > 3 || !uniqueIDs(s.MemoryIDs, 3) || !uniqueIDs(s.PlaceIDs, 5) || !uniqueIDs(s.ActivityIDs, 5) || !uniqueIDs(s.RelationshipTieIDs, 3) || len(s.PolicyFamilies) > 1 {
		return PurposeSelection{}, ErrInvalid
	}
	seen := map[string]bool{}
	for _, key := range s.ProfileFields {
		if !hasKey(PrivateFieldKeys(), key) || seen[key] {
			return PurposeSelection{}, ErrInvalid
		}
		seen[key] = true
	}
	for _, f := range s.PolicyFamilies {
		if !agentpolicysettings.ValidFamily(f) {
			return PurposeSelection{}, ErrInvalid
		}
	}
	if len(s.ProfileFields)+len(s.MemoryIDs)+len(s.PlaceIDs)+len(s.ActivityIDs)+len(s.RelationshipTieIDs)+len(s.PolicyFamilies) == 0 {
		return PurposeSelection{}, ErrInvalid
	}
	s.AgentID = strings.ToLower(s.AgentID)
	s.TaskID = strings.ToLower(s.TaskID)
	s.TaskUpdatedAt = s.TaskUpdatedAt.UTC()
	s.DeadlineAt = s.DeadlineAt.UTC()
	s.ProfileFields = append([]string{}, s.ProfileFields...)
	sort.Strings(s.ProfileFields)
	for _, ids := range []*[]string{&s.MemoryIDs, &s.PlaceIDs, &s.ActivityIDs, &s.RelationshipTieIDs} {
		*ids = append([]string{}, (*ids)...)
		sort.Strings(*ids)
	}
	s.PolicyFamilies = append([]agentpolicysettings.Family{}, s.PolicyFamilies...)
	return s, nil
}
func PurposeSelectionBytes(s PurposeSelection) ([]byte, error) {
	s, e := NormalizePurposeSelection(s)
	if e != nil {
		return nil, e
	}
	s.CurrentQuery = ""
	return json.Marshal(s)
}
func SamePurposeSelection(a, b PurposeSelection) bool {
	x, e := PurposeSelectionBytes(a)
	if e != nil {
		return false
	}
	y, e := PurposeSelectionBytes(b)
	return e == nil && string(x) == string(y)
}

type PurposePreview struct {
	ID              string           `json:"id"`
	Purpose         string           `json:"purpose"`
	Selection       PurposeSelection `json:"selection"`
	Sources         []Source         `json:"sources"`
	ObservedAt      time.Time        `json:"observedAt"`
	ExpiresAt       time.Time        `json:"expiresAt"`
	ConsumedGrantID string           `json:"consumedGrantId,omitempty"`
	Review          PurposeReview    `json:"review"`
}

// PurposeReview is the human preview of exactly the captured versions. It is
// not a permission or machine Context Bundle and is never persisted here.
type PurposeReview struct {
	Profile       map[string]json.RawMessage   `json:"profile,omitempty"`
	Memories      []ReviewMemory               `json:"memories,omitempty"`
	Policies      []agentpolicysettings.Record `json:"policies,omitempty"`
	City          *ContextCity                 `json:"city,omitempty"`
	Task          *ContextTask                 `json:"task,omitempty"`
	Places        []PublicPlace                `json:"places,omitempty"`
	Activities    []PublicActivity             `json:"activities,omitempty"`
	Relationships []ContextTie                 `json:"relationships,omitempty"`
}
type PurposeGrant struct {
	ID        string           `json:"id"`
	Revision  int64            `json:"revision"`
	Purpose   string           `json:"purpose"`
	Selection PurposeSelection `json:"selection"`
	Sources   []Source         `json:"sources"`
	CreatedAt time.Time        `json:"createdAt"`
	ExpiresAt time.Time        `json:"expiresAt"`
	RevokedAt *time.Time       `json:"revokedAt,omitempty"`
}
type PurposeCapture struct {
	BoundSelection        PurposeSelection
	Sources               []Source
	Authority             string
	ObservedAt, ExpiresAt time.Time
}
type PurposeResolution struct {
	GrantID               string
	GrantRevision         int64
	BoundSelection        PurposeSelection
	Sources               []Source
	Authority             string
	ObservedAt, ExpiresAt time.Time
}

func (PurposeCapture) MarshalJSON() ([]byte, error)    { return nil, ErrServerOnly }
func (v *PurposeCapture) UnmarshalJSON([]byte) error   { *v = PurposeCapture{}; return ErrServerOnly }
func (PurposeResolution) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (v *PurposeResolution) UnmarshalJSON([]byte) error {
	*v = PurposeResolution{}
	return ErrServerOnly
}

type PurposeStore interface {
	PreviewOwnContextPurpose(context.Context, agentprofile.PrivateAccess, PurposeSelection) (PurposePreview, error)
	ApproveOwnContextPurpose(context.Context, agentprofile.PrivateAccess, string) (PurposeGrant, error)
	RevokeOwnContextPurpose(context.Context, agentprofile.PrivateAccess, string, int64) (PurposeGrant, error)
	ReadOwnContextPurpose(context.Context, agentprofile.PrivateAccess, string) (PurposeGrant, error)
	ResolveOwnContextPurpose(context.Context, agentprofile.PrivateAccess, string, PurposeSelection) (PurposeResolution, error)
}
