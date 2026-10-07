package agentmemory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

const DetailSchema = "agent-memory-detail-v1"
const MaxDetailLease = 2 * time.Minute
const DetailExplanation = "本人管理记忆；过期或已丢弃内容不在详情中返回。待审推断仅为预留形状，不代表已确认事实或模型读取许可。"

type DetailTarget struct {
	ID      string `json:"id"`
	Version int64  `json:"version"`
	Status  Status `json:"status"`
}

// DetailProjection is a human read, not an approval or cognition grant. Its
// private native proof cannot be reconstructed from JSON or renewed by a client.
type DetailProjection struct {
	SchemaVersion string                `json:"schemaVersion"`
	Owner         actorref.PrincipalRef `json:"owner"`
	AgentID       string                `json:"agentId"`
	Target        DetailTarget          `json:"target"`
	Memory        *Record               `json:"memory"`
	ObservedAt    time.Time             `json:"observedAt"`
	ExpiresAt     time.Time             `json:"expiresAt"`
	Explanation   string                `json:"explanation"`
	ModelAccess   bool                  `json:"modelAccess"`
	proof         *detailProof
}
type detailProof struct {
	wire    [32]byte
	native  string
	session [32]byte
}

// Existing Store implementations need not claim this current native port.
type CurrentHumanStore interface {
	ReadOwnMemoryDetail(context.Context, agentprofile.PrivateAccess, string) (DetailProjection, error)
	RevalidateOwnMemoryDetail(context.Context, agentprofile.PrivateAccess, DetailProjection) error
}

func ValidateDetail(p DetailProjection, now time.Time) error {
	owner, e := actorref.ParsePrincipal(string(p.Owner.Type), p.Owner.ID)
	id, iderr := NormalizeMemoryID(p.Target.ID)
	agent, agerr := NormalizeMemoryID(p.AgentID)
	if e != nil || owner.Type != actorref.Person || !owner.Equal(p.Owner) || iderr != nil || id != p.Target.ID || agerr != nil || agent != p.AgentID || p.Target.Version <= 0 ||
		p.SchemaVersion != DetailSchema || p.ModelAccess || p.Explanation != DetailExplanation || !validTime(p.ObservedAt) || !validTime(p.ExpiresAt) || !validTime(now) ||
		now.Before(p.ObservedAt) || !p.ExpiresAt.After(now) || p.ExpiresAt.After(p.ObservedAt.Add(MaxDetailLease)) {
		return ErrInvalid
	}
	switch p.Target.Status {
	case StatusActive, StatusPendingReview:
		if p.Memory == nil || ValidateRecord(*p.Memory) != nil || p.Memory.ID != p.Target.ID || p.Memory.Version != p.Target.Version || p.Memory.Status != p.Target.Status ||
			p.Memory.OwnerType != p.Owner.Type || p.Memory.OwnerID != p.Owner.ID || p.Memory.AgentID != p.AgentID || p.Memory.ValidFrom.After(p.ObservedAt) ||
			p.Memory.CreatedAt.After(p.ObservedAt) || p.Memory.UpdatedAt.After(p.ObservedAt) || !p.Memory.ValidUntil.After(now) || p.ExpiresAt.After(p.Memory.ValidUntil) {
			return ErrInvalid
		}
	case StatusExpired, StatusDeleted:
		if p.Memory != nil {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// IssueDetail validates only shape. The native Store must capture, bind and
// revalidate its actual fingerprint; calling this pure helper grants no access.
func IssueDetail(p DetailProjection, a agentprofile.PrivateAccess, native string) (DetailProjection, error) {
	if ValidateDetail(p, p.ObservedAt) != nil || agentprofile.ValidatePrivateAccess(a) != nil || !a.WorkspacePrincipal.Equal(p.Owner) {
		return DetailProjection{}, ErrInvalid
	}
	b, e := hex.DecodeString(native)
	if e != nil || len(b) != sha256.Size || hex.EncodeToString(b) != native {
		return DetailProjection{}, ErrInvalid
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return DetailProjection{}, ErrInvalid
	}
	p.proof = &detailProof{wire: sha256.Sum256(raw), native: native, session: a.SessionDigest}
	return p, nil
}
func DetailNativeProof(p DetailProjection, a agentprofile.PrivateAccess) (string, error) {
	if p.proof == nil || ValidateDetail(p, p.ObservedAt) != nil || agentprofile.ValidatePrivateAccess(a) != nil || !a.WorkspacePrincipal.Equal(p.Owner) || a.SessionDigest != p.proof.session {
		return "", ErrInvalid
	}
	raw, e := json.Marshal(p)
	if e != nil || sha256.Sum256(raw) != p.proof.wire {
		return "", ErrInvalid
	}
	return p.proof.native, nil
}
func (p *DetailProjection) UnmarshalJSON([]byte) error {
	if p != nil {
		*p = DetailProjection{}
	}
	return ErrInvalid
}

type RejectInput struct {
	OperationID string `json:"operationId"`
	PlanDigest  string `json:"planDigest"`
}

func ValidateRejectInput(in RejectInput) error {
	id, e := NormalizeMemoryID(in.OperationID)
	b, d := hex.DecodeString(in.PlanDigest)
	if e != nil || id != in.OperationID || d != nil || len(b) != sha256.Size || hex.EncodeToString(b) != in.PlanDigest {
		return ErrInvalid
	}
	return nil
}
func DecodeRejectInput(raw []byte) (RejectInput, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes {
		return RejectInput{}, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return RejectInput{}, ErrInvalid
	}
	seen := map[string]bool{}
	in := RejectInput{}
	for d.More() {
		t, e = d.Token()
		key, ok := t.(string)
		if e != nil || !ok || seen[key] || (key != "operationId" && key != "planDigest") {
			return RejectInput{}, ErrInvalid
		}
		seen[key] = true
		var value string
		if d.Decode(&value) != nil {
			return RejectInput{}, ErrInvalid
		}
		if key == "operationId" {
			in.OperationID = value
		} else {
			in.PlanDigest = value
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(seen) != 2 {
		return RejectInput{}, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF || ValidateRejectInput(in) != nil {
		return RejectInput{}, ErrInvalid
	}
	return in, nil
}
