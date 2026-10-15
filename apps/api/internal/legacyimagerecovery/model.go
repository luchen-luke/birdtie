// Package legacyimagerecovery recovers authenticated static images whose old
// metadata or matcher MIME was wrong. It never revises the 157/159 import facts.
package legacyimagerecovery

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

const Protocol = "birdtie-civu-private-image-recovery-v1"
const JPEGUnknown = "JPEG_UNKNOWN_SOURCE_METADATA"
const HEICMisclassified = "HEIC_MISCLASSIFIED_FTYP"
const MaxInputBytes = legacymedia.MaxInputBytes

var ErrInvalid = errors.New("legacy_image_recovery_input_rejected")
var ErrUnavailable = errors.New("legacy_image_recovery_codec_unavailable")

type Counts struct {
	Images int `json:"images"`
	JPEG   int `json:"jpeg"`
	HEIC   int `json:"heic"`
	Notes  int `json:"notes"`
	Owners int `json:"owners"`
}
type Record struct {
	legacymedia.Record
	Format, RecoveryReason string
	SourceMIME             string
	SourceDeclaredBytes    *int64
}

func (Record) String() string { return "authenticated private image recovery selector" }

type Plan struct {
	RunID                                                                                              string         `json:"runId"`
	Counts                                                                                             Counts         `json:"counts"`
	Deferred                                                                                           map[string]int `json:"deferred"`
	ProjectionSHA256, SourceFileSHA256, ManifestFileSHA256, MatcherFileSHA256, AuthenticatedSealSHA256 string         `json:"-"`
	Records                                                                                            []Record       `json:"-"`
}

func (p Plan) String() string {
	return fmt.Sprintf("private image recovery: images=%d; NOT_IMPORTED", p.Counts.Images)
}

type selector struct {
	MediaID     string   `json:"sourceMediaId"`
	OwnerID     string   `json:"sourceOwnerId"`
	Role        string   `json:"role"`
	Status      string   `json:"sourceStatus"`
	StorageKey  string   `json:"sourceStorageKey"`
	Member      string   `json:"archiveMember"`
	SourceMIME  string   `json:"sourceMIME"`
	MIME        string   `json:"observedMIME"`
	SourceBytes *int64   `json:"sourceBytes"`
	Bytes       *int64   `json:"bytes"`
	SHA256      string   `json:"sha256"`
	Matched     bool     `json:"matched"`
	Reasons     []string `json:"reasons"`
}

func decode(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if len(raw) < 2 || len(raw) > legacymedia.MaxMetadataBytes || d.Decode(v) != nil {
		return ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	return nil
}
func stableID(note, asset string) string {
	s := sha256.Sum256([]byte(Protocol + "\x00" + legacymigration.SourceSystem + "\x00" + note + "\x00" + asset))
	s[6] = (s[6] & 15) | 128
	s[8] = (s[8] & 63) | 128
	h := hex.EncodeToString(s[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// The original pinned authenticated matcher is unchanged. Only two closed
// metadata defects are recoverable; a decoder must independently prove pixels.
func Build(run string, source, manifest, matched, seal []byte, expectedSealSHA string) (Plan, error) {
	proof, e := legacymedia.Build(run, source, manifest, matched, seal, expectedSealSHA)
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
	var m struct {
		Selectors []selector `json:"selectors"`
	}
	if decode(source, &doc) != nil || decode(matched, &m) != nil {
		return Plan{}, ErrInvalid
	}
	p := Plan{RunID: run, SourceFileSHA256: proof.SourceFileSHA256, ManifestFileSHA256: proof.ManifestFileSHA256, MatcherFileSHA256: proof.MatcherFileSHA256, AuthenticatedSealSHA256: proof.AuthenticatedSealSHA256, Records: []Record{}, Deferred: map[string]int{}}
	if e = selectRecords(&p, base, doc.Tables, m.Selectors); e != nil {
		return Plan{}, e
	}
	raw, e := json.Marshal([]any{Protocol, run, p.SourceFileSHA256, p.ManifestFileSHA256, p.MatcherFileSHA256, p.AuthenticatedSealSHA256, p.Records, p.Counts, p.Deferred})
	if e != nil {
		return Plan{}, ErrInvalid
	}
	var canonical any
	if decode(raw, &canonical) != nil {
		return Plan{}, ErrInvalid
	}
	p.ProjectionSHA256, e = legacymigration.Digest(canonical)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	return p, nil
}
func reasonSet(v []string, wanted ...string) bool {
	if len(v) != len(wanted) {
		return false
	}
	seen := map[string]bool{}
	for _, s := range v {
		if seen[s] {
			return false
		}
		seen[s] = true
	}
	for _, s := range wanted {
		if !seen[s] {
			return false
		}
	}
	return true
}
func empty(v any) bool { return v == nil || v == "" }
func unknownSize(v any, declared *int64) bool {
	if v == nil {
		return declared == nil
	}
	n, ok := v.(json.Number)
	return ok && string(n) == "0" && declared != nil && *declared == 0
}
func selectRecords(p *Plan, base legacymigration.Plan, tables map[string][]map[string]any, selectors []selector) error {
	assets := map[string]map[string]any{}
	notes := map[string]legacymigration.NotePlan{}
	owners := map[string]bool{}
	for _, o := range base.Owners {
		owners[o.SourceID] = true
	}
	for _, n := range base.Notes {
		notes[n.SourceID] = n
	}
	for _, a := range tables["media_assets"] {
		id, _ := a["id"].(string)
		if assets[id] != nil {
			return ErrInvalid
		}
		assets[id] = a
	}
	byID := map[string]selector{}
	for _, s := range selectors {
		if s.Role != "original" {
			continue
		}
		if _, ok := byID[s.MediaID]; ok {
			return ErrInvalid
		}
		byID[s.MediaID] = s
	}
	seen := map[string]bool{}
	positions := map[string]bool{}
	noteSet := map[string]bool{}
	ownerSet := map[string]bool{}
	for _, rel := range tables["note_media"] {
		note, _ := rel["note_id"].(string)
		asset, _ := rel["media_id"].(string)
		n, ok := notes[note]
		if !ok {
			return ErrInvalid
		}
		if n.Deleted {
			p.Deferred["deleted_note_relations"]++
			continue
		}
		a, ok := assets[asset]
		s, found := byID[asset]
		if !ok || !found || a["owner_id"] != n.OwnerSourceID || s.OwnerID != n.OwnerSourceID || !owners[n.OwnerSourceID] {
			return ErrInvalid
		}
		if s.Matched {
			p.Deferred["original_matched_relations_unchanged"]++
			continue
		}
		format, reason := "", ""
		switch {
		case s.MIME == "image/jpeg" && s.SourceMIME == "" && empty(a["content_type"]) && unknownSize(a["size_bytes"], s.SourceBytes) && reasonSet(s.Reasons, "NOT_SCANNED", "RIGHTS_NOT_REVIEWED", "SOURCE_MIME_UNKNOWN", "SOURCE_SIZE_UNKNOWN"):
			format, reason = "image/jpeg", JPEGUnknown
		case s.MIME == "video/mp4" && s.SourceMIME == "image/heic" && a["content_type"] == "image/heic" && reasonSet(s.Reasons, "NOT_SCANNED", "RIGHTS_NOT_REVIEWED", "MIME_MISMATCH"):
			format, reason = "image/heic", HEICMisclassified
		default:
			p.Deferred["unrecoverable_metadata_family"]++
			continue
		}
		pos, posOK := rel["position"].(json.Number)
		position, pe := strconv.ParseInt(string(pos), 10, 32)
		if !posOK || pe != nil || position < 0 || s.Bytes == nil || *s.Bytes < 1 || *s.Bytes > MaxInputBytes || !legacymedia.ValidSHA(s.SHA256) || s.Status != "ready" || a["status"] != "ready" || a["kind"] != "image" || s.Member == "" || s.StorageKey == "" || a["storage_key"] != s.StorageKey {
			return ErrInvalid
		}
		if format == "image/heic" {
			size, sizeOK := a["size_bytes"].(json.Number)
			v, se := strconv.ParseInt(string(size), 10, 64)
			if !sizeOK || se != nil || s.SourceBytes == nil || *s.SourceBytes != *s.Bytes || v != *s.Bytes {
				return ErrInvalid
			}
		}
		key, pk := note+":"+asset, note+":"+string(pos)
		if seen[key] || positions[pk] {
			return ErrInvalid
		}
		seen[key], positions[pk] = true, true
		hash, e := legacymigration.Digest(a)
		if e != nil {
			return ErrInvalid
		}
		p.Records = append(p.Records, Record{Record: legacymedia.Record{SourceNoteID: note, SourceMediaID: asset, OwnerSourceID: n.OwnerSourceID, SourceNoteSHA256: n.SourceSHA256, SourceMediaSHA256: hash, InputSHA256: s.SHA256, ImageID: stableID(note, asset), InputBytes: *s.Bytes, Position: int(position)}, Format: format, RecoveryReason: reason, SourceMIME: s.SourceMIME, SourceDeclaredBytes: s.SourceBytes})
		if format == "image/jpeg" {
			p.Counts.JPEG++
		} else {
			p.Counts.HEIC++
		}
		noteSet[note], ownerSet[n.OwnerSourceID] = true, true
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
	perNote := map[string]int{}
	for _, r := range p.Records {
		perNote[r.SourceNoteID]++
		if perNote[r.SourceNoteID] > 12 {
			return ErrInvalid
		}
	}
	p.Counts.Images, p.Counts.Notes, p.Counts.Owners = len(p.Records), len(noteSet), len(ownerSet)
	return nil
}
