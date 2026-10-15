// Package legacyvideo adapts only authenticated owned historical videos. It
// neither activates an owner nor grants publication or an upload approval.
package legacyvideo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

const Protocol = "birdtie-civu-private-videos-v1"
const Validation = "FULL_VIDEO_DECODE_CANONICAL_REENCODE_V1"
const MaxInputBytes = 64 * 1024 * 1024
const MaxContentBytes = 64 * 1024 * 1024
const MaxPosterBytes = 5 * 1024 * 1024
const MaxDurationMS = 120000

var ErrInvalid = errors.New("legacy_private_video_input_rejected")
var ErrUnavailable = errors.New("legacy_private_video_codec_unavailable")

type Counts struct {
	Videos int `json:"videos"`
	Notes  int `json:"notes"`
	Owners int `json:"owners"`
}
type Record struct {
	SourceNoteID, SourceMediaID, OwnerSourceID, SourceNoteSHA256, SourceMediaSHA256, InputSHA256, VideoID string
	InputBytes                                                                                            int64
	Position                                                                                              int
}

func (Record) String() string { return "private historical video selector" }

type Plan struct {
	RunID                                                                                              string         `json:"runId"`
	Counts                                                                                             Counts         `json:"counts"`
	Deferred                                                                                           map[string]int `json:"deferred"`
	ProjectionSHA256, SourceFileSHA256, ManifestFileSHA256, MatcherFileSHA256, AuthenticatedSealSHA256 string         `json:"-"`
	Records                                                                                            []Record       `json:"-"`
}

func (p Plan) String() string {
	return fmt.Sprintf("private video migration: videos=%d; NOT_IMPORTED", p.Counts.Videos)
}
func ValidSHA(s string) bool { return legacymedia.ValidSHA(s) }
func Hash(b []byte) string   { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func StableID(note, asset string) string {
	h := sha256.Sum256([]byte(Protocol + "\x00" + legacymigration.SourceSystem + "\x00" + note + "\x00" + asset))
	h[6] = (h[6] & 15) | 128
	h[8] = (h[8] & 63) | 128
	s := hex.EncodeToString(h[:16])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
func decode(b []byte, v any) error {
	if len(b) < 2 || len(b) > legacymedia.MaxMetadataBytes {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if d.Decode(v) != nil {
		return ErrInvalid
	}
	var x any
	if d.Decode(&x) != io.EOF {
		return ErrInvalid
	}
	return nil
}

// The immutable authenticated-match receipt is shared with157; its original
// externally pinned digest is mandatory, rather than trusting a new local seal.
func Build(run string, source, manifest, matched, seal []byte, pinned string) (Plan, error) {
	proof, e := legacymedia.Build(run, source, manifest, matched, seal, pinned)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	base, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	var match struct {
		Selectors []struct {
			ID          string `json:"sourceMediaId"`
			Owner       string `json:"sourceOwnerId"`
			Role        string `json:"role"`
			Status      string `json:"sourceStatus"`
			Key         string `json:"sourceStorageKey"`
			Member      string `json:"archiveMember"`
			SourceMIME  string `json:"sourceMIME"`
			MIME        string `json:"observedMIME"`
			SourceBytes *int64 `json:"sourceBytes"`
			Bytes       *int64 `json:"bytes"`
			SHA         string `json:"sha256"`
			Matched     bool   `json:"matched"`
		} `json:"selectors"`
	}
	if decode(source, &doc) != nil || decode(matched, &match) != nil {
		return Plan{}, ErrInvalid
	}
	p := Plan{RunID: run, Deferred: map[string]int{}, Records: []Record{}, SourceFileSHA256: proof.SourceFileSHA256, ManifestFileSHA256: proof.ManifestFileSHA256, MatcherFileSHA256: proof.MatcherFileSHA256, AuthenticatedSealSHA256: pinned}
	assets := map[string]map[string]any{}
	for _, a := range doc.Tables["media_assets"] {
		id, _ := a["id"].(string)
		assets[id] = a
	}
	selectors := map[string]int{}
	for i, s := range match.Selectors {
		if s.Role == "original" {
			selectors[s.ID] = i
		}
	}
	notes := map[string]legacymigration.NotePlan{}
	for _, n := range base.Notes {
		notes[n.SourceID] = n
	}
	owners, seen, positions, noteSet, ownerSet := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, o := range base.Owners {
		owners[o.SourceID] = true
	}
	for _, rel := range doc.Tables["note_media"] {
		note, _ := rel["note_id"].(string)
		asset, _ := rel["media_id"].(string)
		n, ok := notes[note]
		if !ok {
			return Plan{}, ErrInvalid
		}
		if n.Deleted {
			continue
		}
		i, found := selectors[asset]
		if !found {
			continue
		}
		s := match.Selectors[i]
		a, found := assets[asset]
		if !found || !s.Matched || s.MIME != "video/mp4" {
			continue
		}
		if a["owner_id"] != n.OwnerSourceID || s.Owner != n.OwnerSourceID || !owners[s.Owner] {
			p.Deferred["owner_unavailable"]++
			continue
		}
		size, se := strconv.ParseInt(fmt.Sprint(a["size_bytes"]), 10, 64)
		pos, pe := strconv.ParseInt(fmt.Sprint(rel["position"]), 10, 32)
		if se != nil || pe != nil || size < 1 || size > MaxInputBytes || pos < 0 || s.SourceBytes == nil || s.Bytes == nil || *s.SourceBytes != size || *s.Bytes != size || !legacymedia.ValidSHA(s.SHA) || s.Status != "ready" || a["status"] != "ready" || s.SourceMIME != "video/mp4" || a["content_type"] != "video/mp4" || s.Key == "" || s.Member == "" || a["storage_key"] != s.Key {
			return Plan{}, ErrInvalid
		}
		key := note + ":" + asset
		pk := note + ":" + strconv.FormatInt(pos, 10)
		if seen[key] || positions[pk] {
			return Plan{}, ErrInvalid
		}
		seen[key], positions[pk] = true, true
		ah, e := legacymigration.Digest(a)
		if e != nil {
			return Plan{}, ErrInvalid
		}
		p.Records = append(p.Records, Record{SourceNoteID: note, SourceMediaID: asset, OwnerSourceID: s.Owner, SourceNoteSHA256: n.SourceSHA256, SourceMediaSHA256: ah, InputSHA256: s.SHA, VideoID: StableID(note, asset), InputBytes: size, Position: int(pos)})
		noteSet[note], ownerSet[s.Owner] = true, true
	}
	sort.Slice(p.Records, func(i, j int) bool {
		a, b := p.Records[i], p.Records[j]
		if a.OwnerSourceID != b.OwnerSourceID {
			return a.OwnerSourceID < b.OwnerSourceID
		}
		if a.SourceNoteID != b.SourceNoteID {
			return a.SourceNoteID < b.SourceNoteID
		}
		return a.Position < b.Position
	})
	p.Counts = Counts{len(p.Records), len(noteSet), len(ownerSet)}
	raw, _ := json.Marshal(map[string]any{"protocol": Protocol, "run": run, "source": p.SourceFileSHA256, "manifest": p.ManifestFileSHA256, "matcher": p.MatcherFileSHA256, "seal": p.AuthenticatedSealSHA256, "records": p.Records, "counts": p.Counts, "deferred": p.Deferred})
	var value any
	if decode(raw, &value) != nil {
		return Plan{}, ErrInvalid
	}
	p.ProjectionSHA256, e = legacymigration.Digest(value)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	return p, nil
}
