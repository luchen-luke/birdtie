package content

import (
	"context"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const MapContentThreadSchema = "map-content-thread-v1"

// An unavailable position never carries its former selector, text or media.
type MapContentThreadEntry struct {
	Position int               `json:"position"`
	Status   string            `json:"status"`
	Detail   *MapContentMoment `json:"detail"`
}

type MapContentThread struct {
	SchemaVersion        string                     `json:"schemaVersion"`
	ViewerAccountID      string                     `json:"viewerAccountId"`
	SeedMomentID         string                     `json:"seedMomentId"`
	SeedRevision         int64                      `json:"seedRevision"`
	SeedVisibilitySource string                     `json:"seedVisibilitySource"`
	Status               string                     `json:"status"`
	ThreadID             string                     `json:"threadId"`
	Entries              []MapContentThreadEntry    `json:"entries"`
	ObservedAt           time.Time                  `json:"observedAt"`
	ValidUntil           time.Time                  `json:"validUntil"`
	SourceVersion        string                     `json:"sourceVersion"`
	ResponseProof        identity.HumanControlProof `json:"-"`
}

func (v MapContentThread) Valid(now time.Time) bool {
	if v.SchemaVersion != MapContentThreadSchema || !identity.HumanControlUUID(v.ViewerAccountID) || !identity.HumanControlUUID(v.SeedMomentID) || v.SeedRevision < 1 || !MapContentVisibility(v.SeedVisibilitySource) || v.SeedVisibilitySource == "CIVU_PUBLIC_CATALOG" || v.Entries == nil || !mapContentLease(v.ObservedAt, v.ValidUntil, now, v.SourceVersion) {
		return false
	}
	if v.Status == "NOT_A_THREAD" {
		return v.ThreadID == "" && len(v.Entries) == 0
	}
	if v.Status != "AVAILABLE" || !identity.HumanControlUUID(v.ThreadID) || len(v.Entries) < 1 || len(v.Entries) > MomentThreadMaxEntries {
		return false
	}
	seen, seed := map[string]bool{}, false
	for i, e := range v.Entries {
		if e.Position != i {
			return false
		}
		if e.Status == "UNAVAILABLE" {
			if e.Detail != nil {
				return false
			}
			continue
		}
		m := e.Detail
		if e.Status != "AVAILABLE" || m == nil || !m.Valid(now) || m.ViewerAccountID != v.ViewerAccountID || seen[m.ID] || m.ValidUntil.Before(v.ValidUntil) {
			return false
		}
		seen[m.ID] = true
		if m.ID == v.SeedMomentID {
			seed = m.Revision == v.SeedRevision && m.VisibilitySource == v.SeedVisibilitySource
		}
	}
	return seed
}

type HumanMapContentThreadStore interface {
	ReadHumanMapContentThread(context.Context, [32]byte, identity.Actor, string) (MapContentThread, error)
}
