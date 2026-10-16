// Package legacymomentauthor projects only attribution already carried by
// readable Civu public moments. It never exposes a current private profile.
package legacymomentauthor

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/legacymedia"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"github.com/birdtie/birdtie/apps/api/internal/legacynative"
	rm "github.com/birdtie/birdtie/apps/api/internal/legacyreferencemedia"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const Protocol = "birdtie-civu-moment-author-v1"

var ErrInvalid = errors.New("legacy_moment_author_invalid")

type Counts struct {
	Moments int `json:"moments"`
	Authors int `json:"authors"`
	Avatars int `json:"avatars"`
}
type Author struct {
	SourceID, SourceSHA256, DisplayName string
	Version                             int64
	Avatar                              *rm.Asset
}
type Fact struct {
	MomentID, SourceNoteID, SourceNoteSHA256, SourceUserID string
	CreatedAt                                              *time.Time
}
type Plan struct {
	RunID                                                                                                 string         `json:"runId"`
	Counts                                                                                                Counts         `json:"counts"`
	Deferred                                                                                              map[string]int `json:"deferred"`
	Authors                                                                                               []Author       `json:"-"`
	Facts                                                                                                 []Fact         `json:"-"`
	SourceFileSHA256, ManifestFileSHA256, SnapshotSHA256, MatcherFileSHA256, SealSHA256, ProjectionSHA256 string         `json:"-"`
}

func (Plan) String() string { return "private immutable original attribution plan" }
func hash(v any) string {
	b, e := json.Marshal(v)
	if e != nil {
		return ""
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var x any
	if d.Decode(&x) != nil {
		return ""
	}
	h, _ := legacymigration.Digest(x)
	return h
}
func text(v any) string { s, _ := v.(string); return s }

// A raw URL is never fetched. Only the known Civu media-id route can bind an
// avatar to the same owner's exact, authenticated original archive object.
func avatarID(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.Host != "47.110.154.133" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, "/media/") {
		return ""
	}
	id := strings.TrimPrefix(u.Path, "/media/")
	if !legacymigration.ValidRunID(id) {
		return ""
	}
	return id
}
func Build(run string, source, manifest, matched, seal []byte, pin string) (Plan, error) {
	base, e := legacymigration.BuildPlan(run, source, manifest)
	if e != nil {
		return Plan{}, ErrInvalid
	}
	if _, e = legacymedia.Build(run, source, manifest, matched, seal, pin); e != nil {
		return Plan{}, ErrInvalid
	}
	var doc struct {
		Tables map[string][]map[string]any `json:"tables"`
	}
	d := json.NewDecoder(bytes.NewReader(source))
	d.UseNumber()
	if d.Decode(&doc) != nil {
		return Plan{}, ErrInvalid
	}
	var matcher struct {
		Selectors []struct {
			ID          string `json:"sourceMediaId"`
			Owner       string `json:"sourceOwnerId"`
			Role        string `json:"role"`
			Status      string `json:"sourceStatus"`
			Key         string `json:"sourceStorageKey"`
			Member      string `json:"archiveMember"`
			MIME        string `json:"observedMIME"`
			SourceMIME  string `json:"sourceMIME"`
			SHA         string `json:"sha256"`
			Bytes       *int64 `json:"bytes"`
			SourceBytes *int64 `json:"sourceBytes"`
			Matched     bool   `json:"matched"`
		} `json:"selectors"`
	}
	if json.Unmarshal(matched, &matcher) != nil {
		return Plan{}, ErrInvalid
	}
	selectors := map[string]int{}
	for i, s := range matcher.Selectors {
		if s.Role == "original" {
			if _, ok := selectors[s.ID]; ok {
				return Plan{}, ErrInvalid
			}
			selectors[s.ID] = i
		}
	}
	users := map[string]map[string]any{}
	media := map[string]map[string]any{}
	dates := map[string]*time.Time{}
	for _, v := range doc.Tables["users"] {
		users[text(v["id"])] = v
	}
	for _, v := range doc.Tables["media_assets"] {
		media[text(v["id"])] = v
	}
	for _, v := range doc.Tables["notes"] {
		if s := text(v["created_at"]); s != "" {
			at, e := time.Parse(time.RFC3339Nano, s)
			if e != nil {
				return Plan{}, ErrInvalid
			}
			dates[text(v["id"])] = &at
		}
	}
	p := Plan{RunID: run, Deferred: map[string]int{}, Authors: []Author{}, Facts: []Fact{}, SourceFileSHA256: base.Run.SourceFileSHA256, ManifestFileSHA256: base.Run.ManifestFileSHA256, SnapshotSHA256: base.Run.SnapshotSHA256, MatcherFileSHA256: legacymedia.Hash(matched), SealSHA256: legacymedia.Hash(seal)}
	owners := map[string]legacymigration.OwnerPlan{}
	used := map[string]bool{}
	for _, o := range base.Owners {
		owners[o.SourceID] = o
	}
	for _, n := range base.Notes {
		o := owners[n.OwnerSourceID]
		if n.Deleted || n.SourceVisibility != "public" || n.SourceModerationStatus != "ok" || o.SourceStatus != "active" {
			continue
		}
		if strings.TrimSpace(o.DisplayName) == "" || !utf8.ValidString(o.DisplayName) || len(o.DisplayName) > 16000 {
			p.Deferred["missing_original_name"]++
			continue
		}
		p.Facts = append(p.Facts, Fact{legacynative.StableTargetID("note", n.OwnerSourceID, n.SourceID), n.SourceID, n.SourceSHA256, n.OwnerSourceID, dates[n.SourceID]})
		used[o.SourceID] = true
	}
	for id := range used {
		o := owners[id]
		a := Author{SourceID: id, SourceSHA256: o.SourceSHA256, DisplayName: o.DisplayName, Version: o.SourceVersion}
		raw := text(users[id]["avatar_url"])
		mid := avatarID(raw)
		if raw != "" {
			m, ok := media[mid]
			i, matchedOK := selectors[mid]
			if mid == "" || !ok || !matchedOK {
				p.Deferred["avatar_not_bound_to_preserved_media"]++
			} else {
				s := matcher.Selectors[i]
				u, err := url.Parse(text(m["public_url"]))
				size, _ := m["size_bytes"].(json.Number)
				n, _ := size.Int64()
				if err != nil || u.Path != "/media/"+mid || text(m["owner_id"]) != id || m["kind"] != "image" || m["status"] != "ready" || s.Owner != id || s.Status != "ready" || s.Key == "" || s.Key != m["storage_key"] || s.Member == "" || !s.Matched || s.Bytes == nil || s.SourceBytes == nil || *s.Bytes != *s.SourceBytes || *s.Bytes != n || n < 1 || n > legacymedia.MaxInputBytes || !legacymedia.ValidSHA(s.SHA) || s.MIME != s.SourceMIME || s.MIME != text(m["content_type"]) || (s.MIME != "image/jpeg" && s.MIME != "image/png") {
					p.Deferred["avatar_source_proof_incomplete"]++
				} else {
					a.Avatar = &rm.Asset{SourceID: mid, SourceSHA256: hash(m), OwnerSourceID: id, InputSHA256: s.SHA, InputBytes: n, MIME: s.MIME}
					p.Counts.Avatars++
				}
			}
		}
		p.Authors = append(p.Authors, a)
	}
	sort.Slice(p.Authors, func(i, j int) bool { return p.Authors[i].SourceID < p.Authors[j].SourceID })
	sort.Slice(p.Facts, func(i, j int) bool { return p.Facts[i].MomentID < p.Facts[j].MomentID })
	p.Counts.Moments = len(p.Facts)
	p.Counts.Authors = len(p.Authors)
	p.ProjectionSHA256 = hash([]any{Protocol, p.SourceFileSHA256, p.ManifestFileSHA256, p.SnapshotSHA256, p.MatcherFileSHA256, p.SealSHA256, p.Authors, p.Facts, p.Counts, p.Deferred})
	if p.ProjectionSHA256 == "" {
		return Plan{}, ErrInvalid
	}
	return p, nil
}
