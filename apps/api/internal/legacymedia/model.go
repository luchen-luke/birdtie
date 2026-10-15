// Package legacymedia selects authenticated historical attachments for private
// native storage. It never activates owners, publishes, or imports raw media.
package legacymedia

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

	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
)

const Protocol = "birdtie-civu-private-images-v1"
const MediaCipherSHA256 = "4c4b7cd1e4b45d9cdc3a7aee3cc31e136702eb44b3c7b0bc5f91de21f365bc0d"
const MaxInputBytes = 16 * 1024 * 1024
const MaxMetadataBytes = 64 * 1024 * 1024

var ErrInvalid = errors.New("legacy_private_media_input_rejected")

type Counts struct {
	Images int `json:"images"`
	Notes  int `json:"notes"`
	Owners int `json:"owners"`
}

type Record struct {
	SourceNoteID, SourceMediaID, OwnerSourceID string
	SourceNoteSHA256, SourceMediaSHA256        string
	InputSHA256, ImageID                       string
	InputBytes                                 int64
	Position                                   int
}

func (Record) String() string { return "private historical image selector" }

// MarshalJSON exposes aggregates only. Private selectors never enter ordinary
// CLI diagnostics, failure receipts, a journal, or a public API response.
type Plan struct {
	RunID                   string         `json:"runId"`
	Counts                  Counts         `json:"counts"`
	Deferred                map[string]int `json:"deferred"`
	ProjectionSHA256        string         `json:"-"`
	SourceFileSHA256        string         `json:"-"`
	ManifestFileSHA256      string         `json:"-"`
	MatcherFileSHA256       string         `json:"-"`
	AuthenticatedSealSHA256 string         `json:"-"`
	Records                 []Record       `json:"-"`
}

func (p Plan) String() string {
	return fmt.Sprintf("private image migration: images=%d; NOT_IMPORTED", p.Counts.Images)
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
type matchDocument struct {
	Schema       string          `json:"schemaVersion"`
	Source       json.RawMessage `json:"source"`
	SourceSHA    string          `json:"sourceFileSha256"`
	PlainSHA     string          `json:"archivePlaintextSHA256"`
	ArchiveBytes int64           `json:"archiveBytes"`
	Selectors    []selector      `json:"selectors"`
}
type sealDocument struct {
	Format            string `json:"format"`
	Authenticated     bool   `json:"aesGcmAuthenticated"`
	CipherSHA         string `json:"ciphertextSHA256"`
	PlainSHA          string `json:"plaintextSHA256"`
	MatcherSHA        string `json:"matcherFileSHA256"`
	ReaderExit        int    `json:"readerExit"`
	ExtractedFiles    int    `json:"extractedFiles"`
	DatabaseWrites    int    `json:"databaseWrites"`
	SourceWrites      int    `json:"sourceWrites"`
	PublicationRights string `json:"publicationRights"`
	Scanner           string `json:"scanner"`
	EXIF              string `json:"exifCleaning"`
}

func Hash(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
func ValidSHA(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && hex.EncodeToString(b) == s
}

func decode(raw []byte, target any) error {
	if len(raw) < 2 || len(raw) > MaxMetadataBytes {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(target) != nil {
		return ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrInvalid
	}
	return nil
}

func stableImageID(note, asset string) string {
	s := sha256.Sum256([]byte(Protocol + "\x00" + legacymigration.SourceSystem + "\x00" + note + "\x00" + asset))
	s[6] = (s[6] & 15) | 128
	s[8] = (s[8] & 63) | 128
	h := hex.EncodeToString(s[:16])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

// The expected seal digest must come from the separately retained controlled
// receipt, never be auto-derived from the untrusted supplied seal file.
func Build(run string, source, manifest, matched, seal []byte, expectedSealSHA string) (Plan, error) {
	base, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	if !ValidSHA(expectedSealSHA) || Hash(seal) != expectedSealSHA {
		return Plan{}, ErrInvalid
	}
	var m matchDocument
	var auth sealDocument
	if decode(matched, &m) != nil || decode(seal, &auth) != nil {
		return Plan{}, ErrInvalid
	}
	if m.Schema != "birdtie-civu-media-match-v1" || m.SourceSHA != base.Run.SourceFileSHA256 || !ValidSHA(m.PlainSHA) || m.ArchiveBytes < 1 || len(m.Selectors) > 100000 || auth.Format != "birdtie-civu-media-authenticated-match-v1" || !auth.Authenticated || auth.CipherSHA != MediaCipherSHA256 || auth.PlainSHA != m.PlainSHA || auth.MatcherSHA != Hash(matched) || auth.ReaderExit != 0 || auth.ExtractedFiles != 0 || auth.DatabaseWrites != 0 || auth.SourceWrites != 0 || auth.PublicationRights != "NOT_AUTHORIZED" || auth.Scanner != "NOT_RUN" || auth.EXIF != "NOT_RUN" {
		return Plan{}, ErrInvalid
	}
	var header any
	var original any
	if decode(m.Source, &header) != nil || decode(base.Run.Header, &original) != nil {
		return Plan{}, ErrInvalid
	}
	h, _ := legacymigration.Digest(header)
	o, _ := legacymigration.Digest(original)
	if h != o {
		return Plan{}, ErrInvalid
	}
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	if decode(source, &doc) != nil {
		return Plan{}, ErrInvalid
	}
	p := Plan{RunID: run, Records: []Record{}, Deferred: map[string]int{}, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, MatcherFileSHA256: Hash(matched), AuthenticatedSealSHA256: expectedSealSHA}
	if e = selectRecords(&p, base, doc.Tables, m.Selectors); e != nil {
		return Plan{}, e
	}
	// Explicit private projection: Plan's public JSON deliberately omits it.
	projected, e := json.Marshal(map[string]any{"protocol": Protocol, "run": run, "source": p.SourceFileSHA256, "manifest": p.ManifestFileSHA256, "matcher": p.MatcherFileSHA256, "seal": expectedSealSHA, "records": p.Records, "counts": p.Counts, "deferred": p.Deferred})
	if e != nil {
		return Plan{}, ErrInvalid
	}
	var normalized any
	if decode(projected, &normalized) != nil {
		return Plan{}, ErrInvalid
	}
	p.ProjectionSHA256, e = legacymigration.Digest(normalized)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	return p, nil
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
		assets[id] = a
	}
	byID := map[string]selector{}
	for _, s := range selectors {
		if s.Role != "original" {
			continue
		}
		if _, exists := byID[s.MediaID]; exists {
			return ErrInvalid
		}
		byID[s.MediaID] = s
	}
	seen := map[string]bool{}
	positions := map[string]bool{}
	noteSet := map[string]bool{}
	ownerSet := map[string]bool{}
	for _, r := range tables["note_media"] {
		noteID, _ := r["note_id"].(string)
		mediaID, _ := r["media_id"].(string)
		n, exists := notes[noteID]
		if !exists {
			return ErrInvalid
		}
		if n.Deleted {
			p.Deferred["deleted_note_relations"]++
			continue
		}
		a, exists := assets[mediaID]
		s, found := byID[mediaID]
		if !exists || !found || a["owner_id"] != n.OwnerSourceID || s.OwnerID != n.OwnerSourceID || !owners[n.OwnerSourceID] {
			p.Deferred["owner_or_selector_unavailable"]++
			continue
		}
		if !s.Matched {
			p.Deferred["unmatched_live_note_relations"]++
			continue
		}
		if s.MIME != "image/jpeg" {
			if s.MIME == "video/mp4" {
				p.Deferred["private_videos_not_adapted"]++
			} else {
				p.Deferred["unsupported_live_note_mime"]++
			}
			continue
		}
		inputSize, ok := a["size_bytes"].(json.Number)
		size, e := strconv.ParseInt(string(inputSize), 10, 64)
		pos, okPos := r["position"].(json.Number)
		position, pe := strconv.ParseInt(string(pos), 10, 32)
		if !ok || e != nil || size < 1 || size > MaxInputBytes || !okPos || pe != nil || position < 0 || s.Bytes == nil || s.SourceBytes == nil || *s.Bytes != size || *s.SourceBytes != size || !ValidSHA(s.SHA256) || s.Status != "ready" || a["status"] != "ready" || s.SourceMIME != "image/jpeg" || a["content_type"] != "image/jpeg" || s.StorageKey == "" || s.Member == "" || a["storage_key"] != s.StorageKey {
			return ErrInvalid
		}
		key := noteID + ":" + mediaID
		pk := noteID + ":" + string(pos)
		if seen[key] || positions[pk] {
			return ErrInvalid
		}
		seen[key], positions[pk] = true, true
		rowHash, e := legacymigration.Digest(a)
		if e != nil {
			return ErrInvalid
		}
		p.Records = append(p.Records, Record{SourceNoteID: noteID, SourceMediaID: mediaID, OwnerSourceID: n.OwnerSourceID, SourceNoteSHA256: n.SourceSHA256, SourceMediaSHA256: rowHash, InputSHA256: s.SHA256, ImageID: stableImageID(noteID, mediaID), InputBytes: size, Position: int(position)})
		noteSet[noteID], ownerSet[n.OwnerSourceID] = true, true
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
	for note := range noteSet {
		count := 0
		for _, r := range p.Records {
			if r.SourceNoteID == note {
				count++
			}
		}
		if count > 12 {
			return ErrInvalid
		}
	}
	p.Counts = Counts{len(p.Records), len(noteSet), len(ownerSet)}
	return nil
}
