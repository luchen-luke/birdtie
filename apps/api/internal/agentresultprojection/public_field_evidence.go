package agentresultprojection

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	aca "github.com/birdtie/birdtie/apps/api/internal/agentcontextadapter"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
)

const PublicFieldEvidenceSchema = "public-query-field-evidence-v1"

// PublicFieldEvidence describes fields in this already-authorized native read.
// It is neither a Context permission nor an input for a model or media analysis.
// The private binding/integrity only prevents moving descriptive metadata into
// another response. The original NativeStore final check remains authoritative.
type PublicFieldEvidence struct {
	SchemaVersion    string                    `json:"schemaVersion"`
	TaskID           string                    `json:"taskId"`
	QueryKind        string                    `json:"queryKind"`
	Owner            actorref.PrincipalRef     `json:"owner"`
	ObservedAt       time.Time                 `json:"observedAt"`
	ValidUntil       time.Time                 `json:"validUntil"`
	Status           string                    `json:"status"`
	FieldEvidenceSet *acb.FieldEvidenceSet     `json:"fieldEvidenceSet"`
	Budget           PublicFieldEvidenceBudget `json:"budget"`
	ModelAccess      string                    `json:"modelAccess"`
	MediaAccess      string                    `json:"mediaAccess"`
	GrantsAuthority  bool                      `json:"grantsAuthority"`
	binding          [32]byte
	integrity        [32]byte
}
type PublicFieldEvidenceBudget struct {
	Unit    string         `json:"unit"`
	Limit   int            `json:"limit"`
	Used    int            `json:"used"`
	Omitted map[string]int `json:"omitted"` // entity counts, never omitted IDs/values
}

func publicResponseBinding(owner actorref.PrincipalRef, taskID string, task json.RawMessage, items []Item, refs []Ref) ([32]byte, error) {
	var canonical any
	if json.Unmarshal(task, &canonical) != nil {
		return [32]byte{}, ErrInvalid
	}
	raw, e := json.Marshal(struct {
		Owner      actorref.PrincipalRef
		TaskID     string
		Task       any
		Items      []Item
		PublicRefs []Ref
	}{owner, taskID, canonical, items, refs})
	if e != nil {
		return [32]byte{}, ErrInvalid
	}
	return sha256.Sum256(raw), nil
}

// ForResponse returns a deep copy only for the original task/owner/version and
// exact native refs. An old ResultSet or a JSON-supplied descriptor cannot be
// used to restore metadata. It intentionally does not grant resource access.
func (p *PublicFieldEvidence) ForResponse(owner actorref.PrincipalRef, taskID string, task json.RawMessage, items []Item, refs []Ref) *PublicFieldEvidence {
	if p == nil || p.integrity == ([32]byte{}) || p.Owner != owner || p.TaskID != taskID {
		return nil
	}
	bind, e := publicResponseBinding(owner, taskID, task, items, refs)
	if e != nil || bind != p.binding {
		return nil
	}
	raw, e := json.Marshal(p)
	if e != nil || sha256.Sum256(raw) != p.integrity || len(raw) > aca.DefaultBudget().MaxEncodedBytes {
		return nil
	}
	var copy PublicFieldEvidence
	if json.Unmarshal(raw, &copy) != nil {
		return nil
	}
	copy.binding, copy.integrity = p.binding, p.integrity
	return &copy
}

// BuildPublicFieldEvidence uses only the final selected receipt. PUBLIC is the
// intersection of its typed ref, native public-ref set and same-ID domain row;
// AUTHORIZED_VIEW alone also includes legal invitations and is not PUBLIC.
func BuildPublicFieldEvidence(a Access, q Query, r Receipt) (*PublicFieldEvidence, error) {
	if q.Kind != "activity" && q.Kind != "place" {
		return nil, nil
	}
	if !a.Valid() || !q.Valid() || !r.Valid() {
		return nil, ErrInvalid
	}
	owner, e := actorref.ParsePrincipal(a.Actor.AccountType, a.Actor.ID)
	if e != nil || owner.Type != actorref.Person {
		return nil, ErrDenied
	}
	var task struct {
		ID            string `json:"id"`
		PrincipalType string `json:"principalType"`
		PrincipalID   string `json:"principalId"`
	}
	if json.Unmarshal(a.ExpectedTask, &task) != nil || task.ID != a.TaskID {
		return nil, ErrDenied
	}
	principal, e := actorref.ParsePrincipal(task.PrincipalType, task.PrincipalID)
	if e != nil || principal != owner {
		return nil, ErrDenied
	}
	bind, e := publicResponseBinding(owner, a.TaskID, a.ExpectedTask, r.Items, r.PublicCommercialRefs)
	if e != nil {
		return nil, e
	}
	p := &PublicFieldEvidence{SchemaVersion: PublicFieldEvidenceSchema, TaskID: a.TaskID, QueryKind: q.Kind, Owner: owner, ObservedAt: r.ObservedAt.UTC(), ValidUntil: r.ValidUntil.UTC(), Status: "NO_PUBLIC_FIELDS", ModelAccess: "UNAVAILABLE", MediaAccess: "UNAVAILABLE", binding: bind,
		FieldEvidenceSet: &acb.FieldEvidenceSet{SchemaVersion: acb.FieldEvidenceSchema, Owner: owner, ObservedAt: r.ObservedAt.UTC(), ExpiresAt: r.ValidUntil.UTC(), Scope: "FILTERED_CONTEXT", Claims: []acb.FieldEvidence{}, Conflicts: []acb.FieldEvidenceConflict{}, ModelAccess: "UNAVAILABLE", MediaAccess: "UNAVAILABLE"},
		Budget:           PublicFieldEvidenceBudget{Unit: aca.BudgetUnit, Limit: aca.DefaultBudget().MaxEncodedBytes, Omitted: map[string]int{"OMITTED_BUDGET": 0, "UNKNOWN_SOURCE_TIME": 0, "EXPIRED_SOURCE": 0, "SOURCE_METADATA_UNAVAILABLE": 0}}}
	public := map[Ref]bool{}
	for _, ref := range r.PublicCommercialRefs {
		public[ref] = true
	}
	groups := []int{}
	eligible := 0
	for _, item := range r.Items {
		if item.Entity.Type != q.Kind || !public[item.Entity] || item.Scope != AuthorizedView {
			continue
		}
		eligible++
		b := acb.Bundle{Agent: agentcognitive.AgentReference{Principal: owner}, Mode: acb.RulesPublicQuery, ObservedAt: p.ObservedAt, ExpiresAt: p.ValidUntil, ModelAccess: "UNAVAILABLE"}
		var source foundation.Source
		kind := ""
		matches := 0
		if q.Kind == "activity" {
			for _, v := range r.Activities {
				if v.ID == item.Entity.ID && v.Visibility == "public" {
					matches++
					source = v.Source
					kind = "PUBLIC_ACTIVITY"
					b.Activities = []acb.PublicActivity{{ID: v.ID, Title: v.Title, Category: v.CategoryCode, StartsAt: v.StartsAt, EndsAt: v.EndsAt, OrganizerType: v.Organizer.Type, OrganizerID: v.Organizer.ID}}
				}
			}
		} else {
			for _, v := range r.Places {
				if v.ID == item.Entity.ID {
					matches++
					source = v.Source
					kind = "PUBLIC_PLACE"
					b.Places = []acb.PublicPlace{{ID: v.ID, Name: v.Name, Category: v.CategoryCode}}
				}
			}
		}
		if matches != 1 {
			p.Budget.Omitted["SOURCE_METADATA_UNAVAILABLE"]++
			continue
		}
		if !acb.ValidTime(source.UpdatedAt) || source.UpdatedAt.After(p.ObservedAt) {
			p.Budget.Omitted["UNKNOWN_SOURCE_TIME"]++
			continue
		}
		if source.ExpiresAt != nil && (!acb.ValidTime(*source.ExpiresAt) || !source.ExpiresAt.After(p.ObservedAt)) {
			p.Budget.Omitted["EXPIRED_SOURCE"]++
			continue
		}
		// Fingerprint only this selected public projection and opaque final proof.
		// No private closure, coordinates, task text, EXIF or internal row token.
		projection, err := json.Marshal(struct {
			Activities      []acb.PublicActivity
			Places          []acb.PublicPlace
			SourceUpdatedAt time.Time
			SourceExpiresAt *time.Time
			CurrentProof    string
		}{b.Activities, b.Places, source.UpdatedAt, source.ExpiresAt, r.Proof})
		if err != nil {
			p.Budget.Omitted["SOURCE_METADATA_UNAVAILABLE"]++
			continue
		}
		version, err := acb.PublicVersion(kind, source.UpdatedAt, projection)
		if err != nil {
			p.Budget.Omitted["SOURCE_METADATA_UNAVAILABLE"]++
			continue
		}
		b.Sources = []acb.Source{{Kind: kind, ID: item.Entity.ID, Version: version, NativeTime: source.UpdatedAt.UTC()}}
		set, err := acb.BuildFieldEvidenceSet(b)
		if err != nil {
			p.Budget.Omitted["SOURCE_METADATA_UNAVAILABLE"]++
			continue
		}
		for i := range set.Claims {
			if source.ExpiresAt != nil {
				v := source.ExpiresAt.UTC()
				set.Claims[i].ValidUntil = &v
				set.Claims[i].ValidityStatus = "VALID_UNTIL_KNOWN"
			}
		}
		if acb.ValidateFieldEvidenceShape(*set) != nil {
			p.Budget.Omitted["SOURCE_METADATA_UNAVAILABLE"]++
			continue
		}
		if len(p.FieldEvidenceSet.Claims)+len(set.Claims) > acb.MaxFieldClaims {
			p.Budget.Omitted["OMITTED_BUDGET"]++
			continue
		}
		p.FieldEvidenceSet.Claims = append(p.FieldEvidenceSet.Claims, set.Claims...)
		groups = append(groups, len(set.Claims))
	}
	// Account for the entire additive wrapper, including its byte counter and
	// omission summary. Remove whole entity groups; never delete domain results.
	for {
		omitted := 0
		for _, n := range p.Budget.Omitted {
			omitted += n
		}
		switch {
		case eligible == 0:
			p.Status = "NO_PUBLIC_FIELDS"
		case len(p.FieldEvidenceSet.Claims) == 0:
			p.Status = "UNAVAILABLE"
		case omitted > 0:
			p.Status = "PARTIAL"
		default:
			p.Status = "AVAILABLE"
		}
		if p.Budget.Omitted["OMITTED_BUDGET"] > 0 {
			p.FieldEvidenceSet.Scope = "BUDGETED_CONTEXT"
		}
		raw, err := publicEvidenceSizedJSON(p)
		if err != nil {
			return nil, ErrUnavailable
		}
		if len(raw) <= p.Budget.Limit {
			if acb.ValidateFieldEvidenceShape(*p.FieldEvidenceSet) != nil {
				return nil, ErrUnavailable
			}
			p.integrity = sha256.Sum256(raw)
			return p, nil
		}
		if len(groups) == 0 {
			return nil, ErrUnavailable
		}
		last := groups[len(groups)-1]
		groups = groups[:len(groups)-1]
		p.FieldEvidenceSet.Claims = p.FieldEvidenceSet.Claims[:len(p.FieldEvidenceSet.Claims)-last]
		p.Budget.Omitted["OMITTED_BUDGET"]++
	}
}

func publicEvidenceSizedJSON(p *PublicFieldEvidence) ([]byte, error) {
	for i := 0; i < 8; i++ {
		raw, e := json.Marshal(p)
		if e != nil {
			return nil, e
		}
		if len(raw) == p.Budget.Used {
			return raw, nil
		}
		p.Budget.Used = len(raw)
	}
	return nil, ErrUnavailable
}
