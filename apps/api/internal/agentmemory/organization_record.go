package agentmemory

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"math"
	"regexp"
	"strings"
	"time"
)

type OrganizationCategory string

const (
	OrgPastActivity          OrganizationCategory = "PAST_ACTIVITY"
	OrgUpcomingActivity      OrganizationCategory = "UPCOMING_ACTIVITY"
	OrgVenue                 OrganizationCategory = "VENUE"
	OrgAnnouncement          OrganizationCategory = "ANNOUNCEMENT"
	OrgFAQ                   OrganizationCategory = "FAQ"
	OrgPartner               OrganizationCategory = "PARTNER"
	OrgCommunityRelationship OrganizationCategory = "COMMUNITY_RELATIONSHIP"
	OrgPolicy                OrganizationCategory = "POLICY"
)

var organizationKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:-]{0,59}$`)

func OrganizationCategories() []OrganizationCategory {
	return []OrganizationCategory{OrgPastActivity, OrgUpcomingActivity, OrgVenue, OrgAnnouncement, OrgFAQ, OrgPartner, OrgCommunityRelationship, OrgPolicy}
}
func OrganizationMemoryKey(category OrganizationCategory, key string) (MemoryType, string, error) {
	if !organizationKeyPattern.MatchString(key) {
		return "", "", ErrInvalid
	}
	kind := TypeOrganization
	switch category {
	case OrgPastActivity, OrgUpcomingActivity:
		kind = TypeActivity
	case OrgVenue:
		kind = TypePlace
	case OrgCommunityRelationship:
		kind = TypeCommunity
	case OrgAnnouncement, OrgFAQ, OrgPartner, OrgPolicy:
	default:
		return "", "", ErrInvalid
	}
	return kind, "org.v1." + strings.ToLower(string(category)) + "." + key, nil
}
func OrganizationCategoryForKey(kind MemoryType, key string) (OrganizationCategory, string, error) {
	for _, category := range OrganizationCategories() {
		prefix := "org.v1." + strings.ToLower(string(category)) + "."
		if strings.HasPrefix(key, prefix) {
			suffix := strings.TrimPrefix(key, prefix)
			t, k, e := OrganizationMemoryKey(category, suffix)
			if e == nil && t == kind && k == key {
				return category, suffix, nil
			}
		}
	}
	return "", "", ErrInvalid
}

// This validates an Org-owned declaration shape, not the acting human's role,
// external source, model purpose or retention grant. Personal APIs remain strict.
func ValidateOrganizationRecord(record Record) error {
	if record.OwnerType != actorref.Organization || record.SourceType != SourceExplicit || record.Confidence != 1 || record.LastReinforcedAt != nil {
		return ErrInvalid
	}
	if _, e := NormalizeMemoryID(record.OwnerID); e != nil {
		return ErrInvalid
	}
	if _, _, e := OrganizationCategoryForKey(record.MemoryType, record.MemoryKey); e != nil {
		return ErrInvalid
	}
	if _, e := NormalizeOrganizationStructuredValue(record.StructuredValue); e != nil {
		return ErrInvalid
	}
	return validateRecordShape(record)
}
func NormalizeOrganizationStructuredValue(raw json.RawMessage) (json.RawMessage, error) {
	value, e := NormalizeStructuredValue(raw)
	if e != nil {
		return nil, e
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(value, &fields) != nil {
		return nil, ErrInvalid
	}
	for key := range fields {
		if key != "note" && key != "tags" {
			return nil, ErrInvalid
		}
	}
	if note, ok := fields["note"]; ok {
		var text string
		if string(note) == "null" || json.Unmarshal(note, &text) != nil {
			return nil, ErrInvalid
		}
	}
	if tags, ok := fields["tags"]; ok {
		var list []string
		if json.Unmarshal(tags, &list) != nil || list == nil || len(list) > 16 {
			return nil, ErrInvalid
		}
		for _, tag := range list {
			if len(tag) == 0 || len(tag) > 80 {
				return nil, ErrInvalid
			}
		}
	}
	return value, nil
}
func NewOrganizationExplicit(id, agentID string, owner actorref.PrincipalRef, version int64, input PutInput, now, createdAt time.Time) (Record, error) {
	for _, value := range []string{id, agentID, owner.ID} {
		if _, e := NormalizeMemoryID(value); e != nil {
			return Record{}, ErrInvalid
		}
	}
	if owner.Type != actorref.Organization || input.ExpectedVersion == math.MaxInt64 || version != input.ExpectedVersion+1 || !validTime(createdAt) || createdAt.After(now) {
		return Record{}, ErrInvalid
	}
	normalized, e := NormalizePutInput(input, now)
	if e != nil {
		return Record{}, e
	}
	record := Record{SchemaVersion: SchemaV1, ID: id, AgentID: agentID, OwnerType: actorref.Organization, OwnerID: owner.ID, Version: version, MemoryType: normalized.MemoryType, MemoryKey: normalized.MemoryKey, Summary: normalized.Summary, StructuredValue: normalized.StructuredValue, Confidence: 1, SourceType: SourceExplicit, Visibility: normalized.Visibility, Status: StatusActive, ValidFrom: now.UTC(), ValidUntil: normalized.ValidUntil, CreatedAt: createdAt.UTC(), UpdatedAt: now.UTC()}
	if ValidateOrganizationRecord(record) != nil {
		return Record{}, ErrInvalid
	}
	return record, nil
}
