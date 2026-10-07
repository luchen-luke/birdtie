// Package agentorganizationmemory manages current administrators' explicit,
// private Organization-owned declarations in the one native Memory ledger.
// It implements no cognition reader, source grant or automatic learning.
package agentorganizationmemory

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"time"
)

const Origin = "CURRENT_ORGANIZATION_ADMIN_DECLARATION"
const MaxRecords = 128

var ErrLimit = errors.New("organization Memory record limit reached")

const Disclaimer = "组织管理员明确填写，未经独立事实核验；不代表公开发布、核验合作关系、到场证明或允许模型分析。"

type Access struct {
	SessionDigest  [32]byte `json:"-"`
	ActingPersonID string   `json:"-"`
	OrganizationID string   `json:"-"`
}

func (Access) MarshalJSON() ([]byte, error) { return nil, agentmemory.ErrForbidden }
func (a *Access) UnmarshalJSON([]byte) error {
	if a != nil {
		*a = Access{}
	}
	return agentmemory.ErrForbidden
}

func ValidateAccess(a Access) error {
	if a.SessionDigest == ([32]byte{}) {
		return agentmemory.ErrForbidden
	}
	for _, id := range []string{a.ActingPersonID, a.OrganizationID} {
		n, e := agentmemory.NormalizeMemoryID(id)
		if e != nil || n != id {
			return agentmemory.ErrForbidden
		}
	}
	return nil
}

type PutInput struct {
	ExpectedVersion int64                            `json:"expectedVersion"`
	Category        agentmemory.OrganizationCategory `json:"category"`
	Key             string                           `json:"key"`
	Summary         string                           `json:"summary"`
	StructuredValue json.RawMessage                  `json:"structuredValue"`
	Visibility      agentmemory.Visibility           `json:"visibility"`
	ValidUntil      time.Time                        `json:"validUntil"`
}

func (input PutInput) Native(now time.Time) (agentmemory.PutInput, error) {
	t, key, e := agentmemory.OrganizationMemoryKey(input.Category, input.Key)
	if e != nil {
		return agentmemory.PutInput{}, e
	}
	// Contents are bounded annotations. No IDs, grants or facts are interpreted
	// from structuredValue; wire rejects unsupported fields before jsonb.
	value, e := agentmemory.NormalizeOrganizationStructuredValue(input.StructuredValue)
	if e != nil {
		return agentmemory.PutInput{}, e
	}
	return agentmemory.NormalizePutInput(agentmemory.PutInput{ExpectedVersion: input.ExpectedVersion, MemoryType: t, MemoryKey: key, Summary: input.Summary, StructuredValue: value, Visibility: input.Visibility, ValidUntil: input.ValidUntil.UTC().Truncate(time.Microsecond)}, now)
}

type View struct {
	OrganizationID        string                           `json:"organizationId"`
	OrganizationAccountID string                           `json:"organizationAccountId"`
	Category              agentmemory.OrganizationCategory `json:"category"`
	Key                   string                           `json:"key"`
	StatementOrigin       string                           `json:"statementOrigin"`
	Disclaimer            string                           `json:"disclaimer"`
	Memory                agentmemory.Record               `json:"memory"`
}

func NewView(orgID string, r agentmemory.Record) (View, error) {
	c, key, e := agentmemory.OrganizationCategoryForKey(r.MemoryType, r.MemoryKey)
	if e != nil || agentmemory.ValidateOrganizationRecord(r) != nil {
		return View{}, agentmemory.ErrUnavailable
	}
	return View{OrganizationID: orgID, OrganizationAccountID: r.OwnerID, Category: c, Key: key, StatementOrigin: Origin, Disclaimer: Disclaimer, Memory: r}, nil
}
func ValidateView(v View, orgID string) error {
	if v.OrganizationID != orgID || v.OrganizationAccountID != v.Memory.OwnerID || v.Memory.OwnerType != actorref.Organization || v.StatementOrigin != Origin || v.Disclaimer != Disclaimer {
		return agentmemory.ErrUnavailable
	}
	c, key, e := agentmemory.OrganizationCategoryForKey(v.Memory.MemoryType, v.Memory.MemoryKey)
	if e != nil || c != v.Category || key != v.Key || agentmemory.ValidateOrganizationRecord(v.Memory) != nil {
		return agentmemory.ErrUnavailable
	}
	return nil
}

type Store interface {
	ValidateOrganizationMemoryAccess(context.Context, Access) (time.Time, error)
	ListOrganizationMemories(context.Context, Access) ([]View, error)
	PutOrganizationMemory(context.Context, Access, string, PutInput) (View, error)
	DeleteOrganizationMemory(context.Context, Access, string, int64) (View, error)
}
