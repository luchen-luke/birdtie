// Package agentdecay projects temporal confidence metadata. It grants no
// permission, produces no inference and never mutates a native Memory row.
package agentdecay

import (
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"math"
	"time"
)

const SchemaV1 = "agent-memory-decay-v1"
const PolicyV1 = "inferred-grace30-half90-v1"
const Grace = 30 * 24 * time.Hour
const HalfLife = 90 * 24 * time.Hour

var (
	ErrInvalid     = errors.New("invalid memory decay metadata")
	ErrForbidden   = errors.New("memory decay forbidden")
	ErrNotFound    = errors.New("memory decay not found")
	ErrConflict    = errors.New("memory decay version conflict")
	ErrUnavailable = errors.New("memory decay unavailable")
)

// Metadata is not a Memory or provenance claim. updated_at is used only to
// validate stored timestamps; it never substitutes for reinforcement.
type Metadata struct {
	MemoryID, AgentID, OwnerID                  string
	Version                                     int64
	SourceType                                  agentmemory.SourceType
	Status                                      agentmemory.Status
	Baseline                                    float64
	CreatedAt, UpdatedAt, ValidFrom, ValidUntil time.Time
	LastReinforcedAt                            *time.Time
}
type View struct {
	SchemaVersion string                     `json:"schemaVersion"`
	PolicyVersion string                     `json:"policyVersion"`
	MemoryID      string                     `json:"memoryId"`
	MemoryVersion int64                      `json:"memoryVersion"`
	AgentID       string                     `json:"agentId"`
	OwnerID       string                     `json:"ownerId"`
	OwnerType     actorref.Type              `json:"ownerType"`
	SourceType    agentmemory.SourceType     `json:"sourceType"`
	Status        agentmemory.Status         `json:"status"`
	Baseline      float64                    `json:"baselineConfidence"`
	Assessment    agentconfidence.Assessment `json:"assessment"`
	AnchorAt      time.Time                  `json:"anchorAt"`
	ReadAt        time.Time                  `json:"readAt"`
	ValidUntil    time.Time                  `json:"validUntil"`
	Decayed       bool                       `json:"decayed"`
	Explanation   string                     `json:"explanation"`
	ModelAccess   string                     `json:"modelAccess"`
}

func validTime(t time.Time) bool { return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 }
func canonicalID(id string) bool {
	x, e := agentmemory.NormalizeMemoryID(id)
	return e == nil && x == id
}

// Project calculates from the original baseline, never a previous projected
// score. A supplied clock has no authorization value; native callers use PG.
func Project(m Metadata, now time.Time) (View, error) {
	if !canonicalID(m.MemoryID) || !canonicalID(m.AgentID) || !canonicalID(m.OwnerID) || m.Version <= 0 || !validTime(now) || !validTime(m.CreatedAt) || !validTime(m.UpdatedAt) || !validTime(m.ValidFrom) || !validTime(m.ValidUntil) || m.UpdatedAt.Before(m.CreatedAt) || m.CreatedAt.After(now) || m.UpdatedAt.After(now) || !m.ValidUntil.After(m.ValidFrom) || m.ValidUntil.Sub(m.ValidFrom) > agentmemory.MaxValidity || math.IsNaN(m.Baseline) || math.IsInf(m.Baseline, 0) || m.Baseline < 0 || m.Baseline > 1 {
		return View{}, ErrInvalid
	}
	if m.ValidFrom.After(now) || !m.ValidUntil.After(now) || m.Status == agentmemory.StatusExpired || m.Status == agentmemory.StatusDeleted {
		return View{}, ErrNotFound
	}
	anchor := m.CreatedAt.UTC()
	if m.LastReinforcedAt != nil {
		if !validTime(*m.LastReinforcedAt) || m.LastReinforcedAt.Before(m.CreatedAt) || m.LastReinforcedAt.After(m.UpdatedAt) || m.LastReinforcedAt.After(now) {
			return View{}, ErrInvalid
		}
		anchor = m.LastReinforcedAt.UTC()
	}
	v := View{SchemaVersion: SchemaV1, PolicyVersion: PolicyV1, MemoryID: m.MemoryID, MemoryVersion: m.Version, AgentID: m.AgentID, OwnerID: m.OwnerID, OwnerType: actorref.Person, SourceType: m.SourceType, Status: m.Status, Baseline: m.Baseline, AnchorAt: anchor, ReadAt: now.UTC(), ValidUntil: m.ValidUntil.UTC(), ModelAccess: "UNAVAILABLE"}
	switch m.SourceType {
	case agentmemory.SourceExplicit:
		if m.Status != agentmemory.StatusActive || m.Baseline != 1 || m.LastReinforcedAt != nil {
			return View{}, ErrInvalid
		}
		v.Assessment = agentconfidence.NewDirectDeclaration()
		v.Explanation = "本人明确填写，不自动衰减；数值1表示直接声明，不代表正确概率。"
	case agentmemory.SourceInferred:
		if m.Status != agentmemory.StatusPendingReview {
			return View{}, ErrInvalid
		}
		elapsed := float64(now.Unix()-anchor.Unix()) + float64(now.Nanosecond()-anchor.Nanosecond())/1e9 - Grace.Seconds()
		score := m.Baseline
		if elapsed > 0 {
			score = m.Baseline * math.Exp2(-elapsed/HalfLife.Seconds())
		}
		v.Decayed = score < m.Baseline
		v.Assessment, _ = agentconfidence.NewUncalibratedScore(score)
		v.Explanation = "待审阅推断；评分随未强化时间降低，未经概率校准，不会自动确认或授权。"
	default:
		return View{}, ErrInvalid
	}
	return v, nil
}
