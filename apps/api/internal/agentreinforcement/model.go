// Package agentreinforcement models human-approved support for an existing
// declaration. Shapes and source clusters never confer analysis/model permission.
package agentreinforcement

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentconfidence"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

const Schema = "agent-memory-reinforcement-v1"
const MaxEntries = 100
const PreviewLease = 5 * time.Minute

var ErrServerOnly = errors.New("强化批准仅供服务端使用")

// Entry is retained metadata, not an evidence payload or verified permission.
// Fingerprint binds current native source epoch, links and exact Evidence.
type Entry struct {
	EvidenceID      string   `json:"evidenceId"`
	EvidenceVersion int64    `json:"evidenceVersion"`
	Fingerprint     string   `json:"fingerprint"`
	Anchors         []string `json:"anchors"`
}
type View struct {
	SchemaVersion   string                     `json:"schemaVersion"`
	MemoryID        string                     `json:"memoryId"`
	MemoryVersion   int64                      `json:"memoryVersion"`
	AgentID         string                     `json:"agentId"`
	Owner           actorref.PrincipalRef      `json:"owner"`
	Version         int64                      `json:"version"`
	EvidenceCount   int                        `json:"evidenceCount"`
	SupportClusters int                        `json:"supportClusters"`
	LastSupportAt   *time.Time                 `json:"lastSupportAt,omitempty"`
	Assessment      agentconfidence.Assessment `json:"assessment"`
	Explanation     string                     `json:"explanation"`
	ModelAccess     string                     `json:"modelAccess"`
}

// Review is inspectable metadata for a specific native preview. It is not an
// approval capability; decoding/copying this projection cannot apply anything.
type ReviewedEvidence struct {
	EvidenceID      string `json:"evidenceId"`
	EvidenceVersion int64  `json:"evidenceVersion"`
	SnapshotDigest  string `json:"snapshotDigest"`
}
type Review struct {
	Memory      View               `json:"memory"`
	Evidence    []ReviewedEvidence `json:"evidence"`
	PlanDigest  string             `json:"planDigest"`
	Purpose     string             `json:"purpose"`
	PreviewedAt time.Time          `json:"previewedAt"`
	ExpiresAt   time.Time          `json:"expiresAt"`
}

func canonicalID(id string) bool {
	n, e := agentmemory.NormalizeMemoryID(id)
	return e == nil && n == id
}
func digestValid(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func Digest(value any) (string, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return "", agentmemory.ErrInvalid
	}
	d := sha256.Sum256(b)
	return hex.EncodeToString(d[:]), nil
}
func NormalizeEntries(entries []Entry) ([]Entry, error) {
	if len(entries) > MaxEntries {
		return nil, agentmemory.ErrInvalid
	}
	out := make([]Entry, len(entries))
	seen := map[string]bool{}
	for i, e := range entries {
		if !canonicalID(e.EvidenceID) || e.EvidenceVersion <= 0 || !digestValid(e.Fingerprint) || seen[e.EvidenceID] || len(e.Anchors) == 0 || len(e.Anchors) > 100 {
			return nil, agentmemory.ErrInvalid
		}
		seen[e.EvidenceID] = true
		anchors := append([]string(nil), e.Anchors...)
		sort.Strings(anchors)
		for j, a := range anchors {
			p := strings.Split(a, ":")
			if len(p) != 2 || !canonicalID(p[1]) || (p[0] != "ACTIVITY" && p[0] != "MOMENT" && p[0] != "PLACE") || (j > 0 && a == anchors[j-1]) {
				return nil, agentmemory.ErrInvalid
			}
		}
		e.Anchors = anchors
		out[i] = e
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EvidenceID < out[j].EvidenceID })
	return out, nil
}

// ClusterCount conservatively joins intersecting anchor sets, including
// transitive multi-Activity overlap. Media count and Evidence IDs add no votes.
func ClusterCount(entries []Entry) (int, error) {
	es, e := NormalizeEntries(entries)
	if e != nil {
		return 0, e
	}
	parent := make([]int, len(es))
	for i := range parent {
		parent[i] = i
	}
	var root func(int) int
	root = func(i int) int {
		if parent[i] != i {
			parent[i] = root(parent[i])
		}
		return parent[i]
	}
	first := map[string]int{}
	for i, v := range es {
		for _, a := range v.Anchors {
			if j, ok := first[a]; ok {
				parent[root(i)] = root(j)
			} else {
				first[a] = i
			}
		}
	}
	roots := map[int]bool{}
	for i := range es {
		roots[root(i)] = true
	}
	return len(roots), nil
}
func BuildView(m agentmemory.Record, version int64, entries []Entry, last *time.Time, now time.Time) (View, error) {
	if agentmemory.ValidateRecord(m) != nil || m.SourceType != agentmemory.SourceExplicit || m.Status != agentmemory.StatusActive || m.ValidFrom.After(now) || !m.ValidUntil.After(now) || version < 0 || now.IsZero() || now.Year() < 1 || now.Year() > 9999 {
		return View{}, agentmemory.ErrUnavailable
	}
	es, e := NormalizeEntries(entries)
	if e != nil {
		return View{}, e
	}
	count, e := ClusterCount(es)
	if e != nil {
		return View{}, e
	}
	if (len(es) == 0) != (last == nil) || last != nil && (last.IsZero() || last.Year() < 1 || last.Year() > 9999 || last.After(now)) {
		return View{}, agentmemory.ErrUnavailable
	}
	return View{SchemaVersion: Schema, MemoryID: m.ID, MemoryVersion: m.Version, AgentID: m.AgentID, Owner: actorref.PrincipalRef{Type: m.OwnerType, ID: m.OwnerID}, Version: version, EvidenceCount: len(es), SupportClusters: count, LastSupportAt: last, Assessment: agentconfidence.NewDirectDeclaration(), Explanation: "本人明确填写；支持来源已按共同原生事件去重，支持数不表示事实概率、出席或到访", ModelAccess: "UNAVAILABLE"}, nil
}
