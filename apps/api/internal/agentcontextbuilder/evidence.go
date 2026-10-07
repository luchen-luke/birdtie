package agentcontextbuilder

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycorrection"
)

const FieldEvidenceSchema = "agent-field-evidence-v1"
const MaxFieldClaims = 100
const PendingEvidenceConfirmation = "AWAITING_CONFIRMATION"
const fieldConflictExplanation = "这些声明不一致，等待本人确认；不会据此改写身份、权限或位置。"

// FieldEvidence is descriptive, not a grant, instruction or a second Memory.
// It deliberately excludes declaration contents, value hashes, EXIF and GPS.
// CollectedAt means this current native read, not the original media upload.
type FieldEvidence struct {
	ID                string                      `json:"id"`
	ItemKind          string                      `json:"itemKind"`
	ItemID            string                      `json:"itemId"`
	SelectionField    string                      `json:"selectionField,omitempty"`
	SubjectKind       string                      `json:"subjectKind"`
	SubjectID         string                      `json:"subjectId"`
	Field             string                      `json:"field"`
	ClaimantKind      string                      `json:"claimantKind"`
	ClaimantID        string                      `json:"claimantId,omitempty"`
	Source            Source                      `json:"source"`
	Nature            string                      `json:"nature"`
	Use               string                      `json:"use"`
	ObservedAt        time.Time                   `json:"observedAt"`
	CollectedAt       time.Time                   `json:"collectedAt"`
	CollectionEvent   string                      `json:"collectionEvent"`
	SourceUpdatedAt   *time.Time                  `json:"sourceUpdatedAt,omitempty"`
	DeclaredAt        *time.Time                  `json:"declaredAt,omitempty"`
	SourceCreatedAt   *time.Time                  `json:"sourceCreatedAt,omitempty"`
	ValidFrom         *time.Time                  `json:"validFrom,omitempty"`
	ValidUntil        *time.Time                  `json:"validUntil,omitempty"`
	CapturedAt        *time.Time                  `json:"capturedAt,omitempty"`
	CaptureTimeStatus string                      `json:"captureTimeStatus"`
	ValidityStatus    string                      `json:"validityStatus"`
	Confidence        *agentconfidence.Assessment `json:"confidence,omitempty"`
}
type FieldEvidenceConflict struct {
	SubjectKind        string   `json:"subjectKind"`
	SubjectID          string   `json:"subjectId"`
	Field              string   `json:"field"`
	ClaimIDs           []string `json:"claimIds"`
	Status             string   `json:"status"`
	OtherClaimsOmitted bool     `json:"otherClaimsOmitted"`
	Explanation        string   `json:"explanation"`
}
type FieldEvidenceSet struct {
	SchemaVersion   string                  `json:"schemaVersion"`
	Owner           actorref.PrincipalRef   `json:"owner"`
	ObservedAt      time.Time               `json:"observedAt"`
	ExpiresAt       time.Time               `json:"expiresAt"`
	Scope           string                  `json:"scope"`
	Claims          []FieldEvidence         `json:"claims"`
	Conflicts       []FieldEvidenceConflict `json:"conflicts"`
	ModelAccess     string                  `json:"modelAccess"`
	MediaAccess     string                  `json:"mediaAccess"`
	GrantsAuthority bool                    `json:"grantsAuthority"`
}
type FieldEvidenceSelector struct{ Kind, ID, Field string }

func timeCopy(t time.Time) *time.Time { v := t.UTC(); return &v }
func newFieldSet(owner actorref.PrincipalRef, observed, expires time.Time, scope string) FieldEvidenceSet {
	return FieldEvidenceSet{SchemaVersion: FieldEvidenceSchema, Owner: owner, ObservedAt: observed.UTC(), ExpiresAt: expires.UTC(), Scope: scope,
		Claims: []FieldEvidence{}, Conflicts: []FieldEvidenceConflict{}, ModelAccess: "UNAVAILABLE", MediaAccess: "UNAVAILABLE"}
}
func closedCategory(s string) bool {
	return hasKey([]string{"badminton", "basketball", "football", "sports", "culture", "hiking"}, s)
}

// Only the exact existing structured preference contracts are comparable.
// A free-form summary or private note is never parsed into a fact or authority.
func memoryPreference(m ReviewMemory) (string, string) {
	if m.MemoryType != agentmemory.TypePreference {
		return "", ""
	}
	var value struct {
		Category string `json:"activityCategory"`
		Nature   string `json:"nature"`
	}
	if json.Unmarshal(m.StructuredValue, &value) != nil || !closedCategory(value.Category) || m.MemoryKey != "activity_category:"+value.Category {
		return "", ""
	}
	switch value.Nature {
	case "human-declaration":
		if m.Summary == agentmemorycandidate.Statement(value.Category) {
			return value.Category, "true"
		}
	case "human-correction":
		if m.Summary == agentmemorycorrection.NegativeStatement(value.Category) {
			return value.Category, "false"
		}
	}
	return "", ""
}

func BuildFieldEvidenceSet(b Bundle) (*FieldEvidenceSet, error) {
	if b.Agent.Principal.Type != actorref.Person || !validID(b.Agent.Principal.ID) || !ValidTime(b.ObservedAt) || !b.ExpiresAt.After(b.ObservedAt) || b.ModelAccess != "UNAVAILABLE" || b.MemoryPromotionAllowed {
		return nil, ErrUnavailable
	}
	out := newFieldSet(b.Agent.Principal, b.ObservedAt, b.ExpiresAt, "COMPLETE_SELECTED_CONTEXT")
	sources := map[string]Source{}
	for _, s := range b.Sources {
		if s.ID == "" || !ValidTime(s.NativeTime) || s.NativeTime.After(b.ObservedAt) {
			return nil, ErrUnavailable
		}
		key := s.Kind + "\x00" + s.ID
		if _, found := sources[key]; found {
			return nil, ErrUnavailable
		}
		s.RowToken = ""
		sources[key] = s
	}
	values := map[string]string{}
	add := func(kind, id, selection, subjectKind, subjectID, field, sourceKind, nature, use, value string, m *ReviewMemory) error {
		s, ok := sources[sourceKind+"\x00"+id]
		if !ok {
			return ErrUnavailable
		}
		c := FieldEvidence{ID: sourceKind + ":" + id + ":" + field, ItemKind: kind, ItemID: id, SelectionField: selection, SubjectKind: subjectKind, SubjectID: subjectID, Field: field,
			Source: s, Nature: nature, Use: use, ObservedAt: out.ObservedAt, CollectedAt: out.ObservedAt, CollectionEvent: "CURRENT_NATIVE_READ", SourceUpdatedAt: timeCopy(s.NativeTime), CaptureTimeStatus: "UNKNOWN_NOT_COLLECTED", ValidityStatus: "SOURCE_INTERVAL_UNKNOWN"}
		if nature == "USER_DECLARATION" {
			c.ClaimantKind = "PERSON_DECLARATION"
			c.ClaimantID = out.Owner.ID
			c.DeclaredAt = timeCopy(s.NativeTime)
		} else {
			c.ClaimantKind = "NATIVE_DOMAIN_RECORD"
		}
		if m != nil {
			if m.ValidFrom != nil {
				c.ValidFrom = timeCopy(*m.ValidFrom)
			}
			if m.CreatedAt != nil {
				c.SourceCreatedAt = timeCopy(*m.CreatedAt)
			}
			c.ValidUntil = timeCopy(m.ValidUntil)
			c.ValidityStatus = "VALID_UNTIL_KNOWN"
			if c.ValidFrom != nil {
				c.ValidityStatus = "SOURCE_INTERVAL_KNOWN"
			}
			if m.Confidence != nil {
				a, e := agentconfidence.NormalizeAssessment(*m.Confidence)
				if e != nil || a.Semantics != agentconfidence.DirectDeclaration {
					return ErrUnavailable
				}
				c.Confidence = &a
			}
		}
		out.Claims = append(out.Claims, c)
		if value != "" {
			values[c.ID] = value
		}
		return nil
	}
	profileKind, memoryKind, policyKind := "HUMAN_PRIVATE_PROFILE", "HUMAN_EXPLICIT_MEMORY", "HUMAN_POLICY_SETTINGS"
	if b.Mode == MachineTaskContext {
		profileKind, memoryKind, policyKind = "PURPOSE_PRIVATE_PROFILE", "PURPOSE_EXPLICIT_MEMORY", "PURPOSE_POLICY_SETTINGS"
	}
	if b.Mode != RulesPublicQuery && b.Mode != HumanSelfReview && b.Mode != MachineTaskContext {
		return nil, ErrUnavailable
	}
	if b.Task != nil || b.TaskID != "" {
		if e := add("task", b.TaskID, "", "TASK", b.TaskID, "request.query", "CURRENT_TASK_REQUEST", "USER_DECLARATION", "REQUEST_DATA_ONLY", "", nil); e != nil {
			return nil, e
		}
	}
	if b.City != nil || b.CityID != "" {
		if e := add("city", b.CityID, "", "CITY", b.CityID, "city.context", "PUBLIC_CITY", "NATIVE_RECORD", "CURRENT_DOMAIN_RECORD_ONLY", "", nil); e != nil {
			return nil, e
		}
	}
	for _, key := range PrivateFieldKeys() {
		raw, ok := b.Profile[key]
		if !ok {
			continue
		}
		if e := add("profile", b.Agent.AgentID, key, "PERSON", out.Owner.ID, "profile."+key, profileKind, "USER_DECLARATION", "DECLARATION_ONLY", "", nil); e != nil {
			return nil, e
		}
		if key == "preferredActivityTypes" {
			var list []string
			if json.Unmarshal(raw, &list) != nil {
				continue
			}
			seen := map[string]bool{}
			for _, category := range list {
				if closedCategory(category) && !seen[category] {
					seen[category] = true
					if e := add("profile", b.Agent.AgentID, key, "PERSON", out.Owner.ID, "preference.activityCategory."+category, profileKind, "USER_DECLARATION", "DECLARATION_ONLY", "true", nil); e != nil {
						return nil, e
					}
				}
			}
		}
	}
	for i := range b.Memories {
		m := &b.Memories[i]
		use := "DECLARATION_ONLY"
		switch m.MemoryType {
		case agentmemory.TypeIdentity:
			use = "NO_IDENTITY_AUTHORITY"
		case agentmemory.TypePlace, agentmemory.TypeCity, agentmemory.TypeExperience, agentmemory.TypeHistory:
			use = "NO_VISIT_OR_CURRENT_LOCATION_AUTHORITY"
		}
		if e := add("memories", m.ID, "", "PERSON", out.Owner.ID, "memory.summary", memoryKind, "USER_DECLARATION", use, "", m); e != nil {
			return nil, e
		}
		if category, value := memoryPreference(*m); category != "" {
			if e := add("memories", m.ID, "", "PERSON", out.Owner.ID, "preference.activityCategory."+category, memoryKind, "USER_DECLARATION", "DECLARATION_ONLY", value, m); e != nil {
				return nil, e
			}
		}
	}
	for _, p := range b.Policies {
		if p.Configured {
			if e := add("policies", b.Agent.AgentID+":"+string(p.Family), "", "PERSON", out.Owner.ID, "policy."+string(p.Family)+".settings", policyKind, "USER_DECLARATION", "DECLARED_SETTINGS_NOT_AN_ACTION_GRANT", "", nil); e != nil {
				return nil, e
			}
			claim := &out.Claims[len(out.Claims)-1]
			if p.ValidFrom != nil {
				claim.ValidFrom = timeCopy(*p.ValidFrom)
			}
			if p.ExpiresAt != nil {
				claim.ValidUntil = timeCopy(*p.ExpiresAt)
				claim.ValidityStatus = "VALID_UNTIL_KNOWN"
			}
			if claim.ValidFrom != nil && claim.ValidUntil != nil {
				claim.ValidityStatus = "SOURCE_INTERVAL_KNOWN"
			}
		}
	}
	for _, p := range b.Places {
		for _, field := range []string{"name", "category"} {
			if e := add("places", p.ID, "", "PLACE", p.ID, "place."+field, "PUBLIC_PLACE", "NATIVE_RECORD", "CURRENT_DOMAIN_RECORD_ONLY", "", nil); e != nil {
				return nil, e
			}
		}
	}
	for _, a := range b.Activities {
		for _, field := range []string{"title", "category", "startsAt", "endsAt", "organizerType", "organizerId"} {
			if e := add("activities", a.ID, "", "ACTIVITY", a.ID, "activity."+field, "PUBLIC_ACTIVITY", "NATIVE_RECORD", "SCHEDULE_OR_RECORD_NOT_ATTENDANCE", "", nil); e != nil {
				return nil, e
			}
		}
	}
	for _, r := range b.Relationships {
		for _, field := range []string{"peerAccountId", "state"} {
			if e := add("relationships", r.ID, "", "RELATIONSHIP", r.ID, "relationship."+field, "PURPOSE_RELATIONSHIP_TIE", "NATIVE_RECORD", "CURRENT_RELATIONSHIP_NOT_MESSAGE_PERMISSION", "", nil); e != nil {
				return nil, e
			}
		}
	}
	addConflicts(&out, values)
	sortFieldSet(&out)
	if ValidateFieldEvidenceShape(out) != nil {
		return nil, ErrUnavailable
	}
	return &out, nil
}

func addConflicts(out *FieldEvidenceSet, values map[string]string) {
	groups := map[string][]FieldEvidence{}
	for _, c := range out.Claims {
		if values[c.ID] != "" {
			key := c.SubjectKind + "\x00" + c.SubjectID + "\x00" + c.Field
			groups[key] = append(groups[key], c)
		}
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		first := values[group[0].ID]
		different := false
		for _, c := range group[1:] {
			different = different || values[c.ID] != first
		}
		if !different {
			continue
		}
		conflict := FieldEvidenceConflict{SubjectKind: group[0].SubjectKind, SubjectID: group[0].SubjectID, Field: group[0].Field, ClaimIDs: []string{}, Status: PendingEvidenceConfirmation, Explanation: fieldConflictExplanation}
		for _, c := range group {
			conflict.ClaimIDs = append(conflict.ClaimIDs, c.ID)
		}
		out.Conflicts = append(out.Conflicts, conflict)
	}
}
func sortFieldSet(s *FieldEvidenceSet) {
	sort.Slice(s.Claims, func(i, j int) bool { return s.Claims[i].ID < s.Claims[j].ID })
	for i := range s.Conflicts {
		sort.Strings(s.Conflicts[i].ClaimIDs)
	}
	sort.Slice(s.Conflicts, func(i, j int) bool {
		a, b := s.Conflicts[i], s.Conflicts[j]
		return a.SubjectKind+":"+a.SubjectID+":"+a.Field < b.SubjectKind+":"+b.SubjectID+":"+b.Field
	})
}
func ValidateFieldEvidenceShape(s FieldEvidenceSet) error {
	if s.SchemaVersion != FieldEvidenceSchema || s.Owner.Type != actorref.Person || !validID(s.Owner.ID) || !ValidTime(s.ObservedAt) || !s.ExpiresAt.After(s.ObservedAt) || s.ModelAccess != "UNAVAILABLE" || s.MediaAccess != "UNAVAILABLE" || s.GrantsAuthority || s.Claims == nil || s.Conflicts == nil || len(s.Claims) > MaxFieldClaims || len(s.Conflicts) > MaxFieldClaims || !hasKey([]string{"COMPLETE_SELECTED_CONTEXT", "FILTERED_CONTEXT", "BUDGETED_CONTEXT", "HUMAN_MEMORY_DETAIL"}, s.Scope) {
		return ErrUnavailable
	}
	claims := map[string]FieldEvidence{}
	for _, c := range s.Claims {
		if c.ID == "" || c.ID != c.Source.Kind+":"+c.Source.ID+":"+c.Field || c.Source.RowToken != "" || c.SubjectID == "" || c.SubjectKind == "" || c.ItemID != c.Source.ID || c.ItemKind == "" || c.Field == "" || !c.ObservedAt.Equal(s.ObservedAt) || !c.CollectedAt.Equal(s.ObservedAt) || c.CollectionEvent != "CURRENT_NATIVE_READ" || c.SourceUpdatedAt == nil || !c.SourceUpdatedAt.Equal(c.Source.NativeTime) || c.Source.NativeTime.After(s.ObservedAt) || !ValidTime(c.Source.NativeTime) || c.CapturedAt != nil || c.CaptureTimeStatus != "UNKNOWN_NOT_COLLECTED" {
			return ErrUnavailable
		}
		if _, ok := claims[c.ID]; ok {
			return ErrUnavailable
		}
		claims[c.ID] = c
		for _, t := range []*time.Time{c.DeclaredAt, c.SourceCreatedAt, c.ValidFrom} {
			if t != nil && (!ValidTime(*t) || t.After(s.ObservedAt)) {
				return ErrUnavailable
			}
		}
		if c.ValidUntil != nil && (!c.ValidUntil.After(s.ObservedAt) || (c.ValidFrom != nil && !c.ValidUntil.After(*c.ValidFrom))) {
			return ErrUnavailable
		}
		if c.Confidence != nil {
			a, e := agentconfidence.NormalizeAssessment(*c.Confidence)
			if e != nil || (a.Semantics != agentconfidence.DirectDeclaration && a.Semantics != agentconfidence.UncalibratedScore && a.Semantics != agentconfidence.Ordinal) {
				return ErrUnavailable
			}
		}
		if !hasKey([]string{"USER_DECLARATION", "NATIVE_RECORD", "MODEL_INFERENCE"}, c.Nature) {
			return ErrUnavailable
		}
		switch c.Nature {
		case "NATIVE_RECORD":
			if c.ClaimantKind != "NATIVE_DOMAIN_RECORD" || c.ClaimantID != "" || c.DeclaredAt != nil || c.Confidence != nil || !hasKey([]string{"CURRENT_DOMAIN_RECORD_ONLY", "SCHEDULE_OR_RECORD_NOT_ATTENDANCE", "CURRENT_RELATIONSHIP_NOT_MESSAGE_PERMISSION"}, c.Use) {
				return ErrUnavailable
			}
		case "MODEL_INFERENCE":
			if c.ClaimantKind != "UNVERIFIED_INFERENCE" || c.ClaimantID != "" || c.DeclaredAt != nil || c.Use != "CANDIDATE_ONLY" || c.Confidence == nil || c.Confidence.Semantics == agentconfidence.DirectDeclaration {
				return ErrUnavailable
			}
		case "USER_DECLARATION":
			if !hasKey([]string{"DECLARATION_ONLY", "NO_IDENTITY_AUTHORITY", "NO_VISIT_OR_CURRENT_LOCATION_AUTHORITY", "REQUEST_DATA_ONLY", "DECLARED_SETTINGS_NOT_AN_ACTION_GRANT"}, c.Use) {
				return ErrUnavailable
			}
		}
		if c.Nature == "USER_DECLARATION" && (c.ClaimantKind != "PERSON_DECLARATION" || c.ClaimantID != s.Owner.ID || c.DeclaredAt == nil) {
			return ErrUnavailable
		}
		switch c.Source.Kind {
		case "HUMAN_PRIVATE_PROFILE", "HUMAN_EXPLICIT_MEMORY", "HUMAN_POLICY_SETTINGS", "PURPOSE_PRIVATE_PROFILE", "PURPOSE_EXPLICIT_MEMORY", "PURPOSE_POLICY_SETTINGS", "HUMAN_MEMORY_DETAIL":
			if c.Source.Version.Kind != agentevent.RevisionVersion || c.Source.Version.Revision <= 0 || c.Source.Version.Token != "" {
				return ErrUnavailable
			}
		case "CURRENT_TASK_REQUEST", "PUBLIC_CITY", "PUBLIC_PLACE", "PUBLIC_ACTIVITY", "PURPOSE_RELATIONSHIP_TIE":
			if c.Source.Version.Kind != agentevent.UpdatedAtDigestVersion || c.Source.Version.Revision != 0 || len(c.Source.Version.Token) != 64 {
				return ErrUnavailable
			}
		default:
			return ErrUnavailable
		}
		if !hasKey([]string{"DECLARATION_ONLY", "NO_IDENTITY_AUTHORITY", "NO_VISIT_OR_CURRENT_LOCATION_AUTHORITY", "CURRENT_DOMAIN_RECORD_ONLY", "REQUEST_DATA_ONLY", "DECLARED_SETTINGS_NOT_AN_ACTION_GRANT", "SCHEDULE_OR_RECORD_NOT_ATTENDANCE", "CURRENT_RELATIONSHIP_NOT_MESSAGE_PERMISSION", "CANDIDATE_ONLY"}, c.Use) {
			return ErrUnavailable
		}
	}
	for _, c := range s.Conflicts {
		if c.Status != PendingEvidenceConfirmation || c.Explanation != fieldConflictExplanation || len(c.ClaimIDs) == 0 || (!c.OtherClaimsOmitted && len(c.ClaimIDs) < 2) {
			return ErrUnavailable
		}
		if c.SubjectKind != "PERSON" || c.SubjectID != s.Owner.ID || !strings.HasPrefix(c.Field, "preference.activityCategory.") || !closedCategory(strings.TrimPrefix(c.Field, "preference.activityCategory.")) {
			return ErrUnavailable
		}
		seen := map[string]bool{}
		for _, id := range c.ClaimIDs {
			claim, ok := claims[id]
			if !ok || seen[id] || claim.SubjectKind != c.SubjectKind || claim.SubjectID != c.SubjectID || claim.Field != c.Field {
				return ErrUnavailable
			}
			seen[id] = true
		}
	}
	return nil
}

// ValidateExactFieldEvidence binds the additive native metadata to exactly the
// same already-authorized sources and contents. Nil is legacy port compatibility,
// not evidence that a native reader implemented the new contract.
func ValidateExactFieldEvidence(b Bundle) error {
	if b.FieldEvidenceSet == nil {
		return nil
	}
	expected, e := BuildFieldEvidenceSet(b)
	if e != nil {
		return e
	}
	a, _ := json.Marshal(expected)
	actual, _ := json.Marshal(b.FieldEvidenceSet)
	if !bytes.Equal(a, actual) {
		return ErrUnavailable
	}
	return nil
}

// FilterFieldEvidence rebuilds the metadata from selected claims. It never
// carries omitted IDs, values or field names through the new envelope. A retained
// side of a known conflict stays pending even when its other side is omitted.
func FilterFieldEvidence(input *FieldEvidenceSet, selectors []FieldEvidenceSelector, scope string) (*FieldEvidenceSet, error) {
	if input == nil {
		return nil, nil
	}
	if ValidateFieldEvidenceShape(*input) != nil {
		return nil, ErrUnavailable
	}
	out := newFieldSet(input.Owner, input.ObservedAt, input.ExpiresAt, scope)
	ids := map[string]bool{}
	for _, c := range input.Claims {
		for _, sel := range selectors {
			if c.ItemKind == sel.Kind && c.ItemID == sel.ID && (sel.Field == "" || sel.Field == c.SelectionField) {
				out.Claims = append(out.Claims, c)
				ids[c.ID] = true
				break
			}
		}
	}
	for _, conflict := range input.Conflicts {
		c := conflict
		c.ClaimIDs = []string{}
		for _, id := range conflict.ClaimIDs {
			if ids[id] {
				c.ClaimIDs = append(c.ClaimIDs, id)
			} else {
				c.OtherClaimsOmitted = true
			}
		}
		if len(c.ClaimIDs) > 0 {
			out.Conflicts = append(out.Conflicts, c)
		}
	}
	sortFieldSet(&out)
	if ValidateFieldEvidenceShape(out) != nil {
		return nil, ErrUnavailable
	}
	// Round-trip only descriptive metadata to break every pointer/slice alias.
	raw, _ := json.Marshal(out)
	var cloned FieldEvidenceSet
	if json.Unmarshal(raw, &cloned) != nil {
		return nil, ErrUnavailable
	}
	return &cloned, nil
}
func BundleEvidenceSelectors(b Bundle) []FieldEvidenceSelector {
	out := []FieldEvidenceSelector{}
	if b.TaskID != "" {
		out = append(out, FieldEvidenceSelector{"task", b.TaskID, ""})
	}
	if b.CityID != "" {
		out = append(out, FieldEvidenceSelector{"city", b.CityID, ""})
	}
	for _, k := range PrivateFieldKeys() {
		if _, ok := b.Profile[k]; ok {
			out = append(out, FieldEvidenceSelector{"profile", b.Agent.AgentID, k})
		}
	}
	for _, m := range b.Memories {
		out = append(out, FieldEvidenceSelector{"memories", m.ID, ""})
	}
	for _, p := range b.Policies {
		if p.Configured {
			out = append(out, FieldEvidenceSelector{"policies", b.Agent.AgentID + ":" + string(p.Family), ""})
		}
	}
	for _, p := range b.Places {
		out = append(out, FieldEvidenceSelector{"places", p.ID, ""})
	}
	for _, a := range b.Activities {
		out = append(out, FieldEvidenceSelector{"activities", a.ID, ""})
	}
	for _, r := range b.Relationships {
		out = append(out, FieldEvidenceSelector{"relationships", r.ID, ""})
	}
	return out
}

func MemoryDetailFieldEvidence(p agentmemory.DetailProjection) (*FieldEvidenceSet, error) {
	if agentmemory.ValidateDetail(p, p.ObservedAt) != nil {
		return nil, ErrUnavailable
	}
	out := newFieldSet(p.Owner, p.ObservedAt, p.ExpiresAt, "HUMAN_MEMORY_DETAIL")
	if p.Memory == nil {
		return &out, nil
	}
	m := p.Memory
	s := Source{Kind: "HUMAN_MEMORY_DETAIL", ID: m.ID, Version: agentevent.SourceVersion{Kind: agentevent.RevisionVersion, Revision: m.Version}, NativeTime: m.UpdatedAt.UTC()}
	rm := ReviewMemory{ID: m.ID, Version: m.Version, MemoryType: m.MemoryType, MemoryKey: m.MemoryKey, Summary: m.Summary, StructuredValue: m.StructuredValue, ValidFrom: timeCopy(m.ValidFrom), CreatedAt: timeCopy(m.CreatedAt), ValidUntil: m.ValidUntil}
	a, e := agentconfidence.NormalizeAssessment(agentconfidence.Assessment{Semantics: agentconfidence.DirectDeclaration, Value: &m.Confidence})
	if m.SourceType == agentmemory.SourceInferred {
		a, e = agentconfidence.NewUncalibratedScore(m.Confidence)
	}
	if e != nil {
		return nil, ErrUnavailable
	}
	use := "DECLARATION_ONLY"
	switch m.MemoryType {
	case agentmemory.TypeIdentity:
		use = "NO_IDENTITY_AUTHORITY"
	case agentmemory.TypePlace, agentmemory.TypeCity, agentmemory.TypeHistory, agentmemory.TypeExperience:
		use = "NO_VISIT_OR_CURRENT_LOCATION_AUTHORITY"
	}
	c := FieldEvidence{ID: s.Kind + ":" + m.ID + ":memory.summary", ItemKind: "memories", ItemID: m.ID, SubjectKind: "PERSON", SubjectID: m.OwnerID, Field: "memory.summary", ClaimantKind: "PERSON_DECLARATION", ClaimantID: m.OwnerID, Source: s, Nature: "USER_DECLARATION", Use: use, ObservedAt: p.ObservedAt, CollectedAt: p.ObservedAt, CollectionEvent: "CURRENT_NATIVE_READ", SourceUpdatedAt: timeCopy(m.UpdatedAt), DeclaredAt: timeCopy(m.UpdatedAt), SourceCreatedAt: rm.CreatedAt, ValidFrom: rm.ValidFrom, ValidUntil: timeCopy(m.ValidUntil), CaptureTimeStatus: "UNKNOWN_NOT_COLLECTED", ValidityStatus: "SOURCE_INTERVAL_KNOWN", Confidence: &a}
	if m.SourceType == agentmemory.SourceInferred {
		c.Nature = "MODEL_INFERENCE"
		c.ClaimantKind = "UNVERIFIED_INFERENCE"
		c.ClaimantID = ""
		c.DeclaredAt = nil
		c.Use = "CANDIDATE_ONLY"
	}
	out.Claims = append(out.Claims, c)
	if ValidateFieldEvidenceShape(out) != nil {
		return nil, ErrUnavailable
	}
	return &out, nil
}

// MediaMetadataDisposition is a disabled-media contract check, not a media
// reader, permitted analysis, native Evidence or permission to store a photo.
// No current production caller installs a MetadataExtractor. Even this shape
// never turns a timestamp/location tag into a personal visit or current city.
func MediaMetadataDisposition(captured, collected, observed time.Time, hasGPS bool, assessment agentconfidence.Assessment) (string, string, error) {
	if !ValidTime(observed) || !ValidTime(collected) || collected.After(observed) || !ValidTime(captured) || captured.After(observed) {
		return "", "", ErrUnavailable
	}
	a, e := agentconfidence.NormalizeAssessment(assessment)
	if e != nil || a.Semantics == agentconfidence.DirectDeclaration {
		return "", "", ErrUnavailable
	}
	use := "CANDIDATE_ONLY"
	if captured.Before(collected) {
		use = "HISTORICAL_CANDIDATE_ONLY"
	}
	origin := "media_metadata"
	if !hasGPS {
		origin = "media_metadata_location_unknown"
	}
	return use, origin, nil
}

// Source equality excludes xmin from public metadata; the original native
// Sources and current proof/seal continue retaining and checking it internally.
func FieldEvidenceMatchesBundle(b Bundle) error {
	if b.FieldEvidenceSet == nil {
		return nil
	}
	if ValidateFieldEvidenceShape(*b.FieldEvidenceSet) != nil {
		return ErrUnavailable
	}
	expected, e := BuildFieldEvidenceSet(b)
	if e != nil {
		return e
	}
	a, _ := json.Marshal(expected.Claims)
	actual, _ := json.Marshal(b.FieldEvidenceSet.Claims)
	if !bytes.Equal(a, actual) {
		return ErrUnavailable
	}
	if !b.FieldEvidenceSet.Owner.Equal(b.Agent.Principal) || !b.FieldEvidenceSet.ObservedAt.Equal(b.ObservedAt) || !b.FieldEvidenceSet.ExpiresAt.Equal(b.ExpiresAt) {
		return ErrUnavailable
	}
	if b.FieldEvidenceSet.Scope == "COMPLETE_SELECTED_CONTEXT" {
		return ValidateExactFieldEvidence(b)
	}
	for _, required := range expected.Conflicts {
		found := false
		for _, actual := range b.FieldEvidenceSet.Conflicts {
			if actual.SubjectKind != required.SubjectKind || actual.SubjectID != required.SubjectID || actual.Field != required.Field {
				continue
			}
			x, _ := json.Marshal(actual.ClaimIDs)
			y, _ := json.Marshal(required.ClaimIDs)
			if bytes.Equal(x, y) {
				found = true
			}
		}
		if !found {
			return ErrUnavailable
		}
	}
	return nil
}
