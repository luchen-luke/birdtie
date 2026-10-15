package legacymigration

import (
	"bytes"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"sort"
	"strings"
	"time"
)

const DevEmailProtocol = "birdtie-legacy-dev-email-binding-v1"

type DevEmailBinding struct {
	SourceOwnerID                           string
	SourceOwnerVersion                      int64
	SourceOwnerSHA256, SourceIdentitySHA256 string
	EmailDigest                             [32]byte
}
type DevEmailCounts struct {
	Owners          int `json:"owners"`
	EmailIdentities int `json:"emailIdentities"`
	EmailOnlyOwners int `json:"emailOnlyOwners"`
	BindableOwners  int `json:"bindableOwners"`
}
type DevEmailPlan struct {
	Original         Plan              `json:"-"`
	Bindings         []DevEmailBinding `json:"-"`
	ProjectionSHA256 string            `json:"-"`
	Counts           DevEmailCounts    `json:"counts"`
}

func (DevEmailBinding) String() string { return "private exact test email binding" }
func (DevEmailPlan) String() string    { return "private fixed-email binding plan; NOT_IMPORTED" }
func BuildDevEmailPlan(run string, source, manifest []byte) (DevEmailPlan, error) {
	original, e := BuildPlan(run, source, manifest)
	if e != nil {
		return DevEmailPlan{}, e
	}
	value, e := decode(source)
	if e != nil {
		return DevEmailPlan{}, ErrInvalid
	}
	tables := value.(map[string]any)["tables"].(map[string]any)
	p := DevEmailPlan{Original: original, Bindings: []DevEmailBinding{}, Counts: DevEmailCounts{Owners: len(original.Owners)}}
	if e = selectDevEmails(&p, tables); e != nil {
		return DevEmailPlan{}, e
	}
	raw, e := json.Marshal([]any{DevEmailProtocol, run, original.Run.SourceFileSHA256, original.Run.ManifestFileSHA256, original.Run.SnapshotSHA256, p.Bindings, p.Counts})
	if e != nil {
		return DevEmailPlan{}, ErrInvalid
	}
	normalized, e := decode(raw)
	if e != nil {
		return DevEmailPlan{}, ErrInvalid
	}
	p.ProjectionSHA256, e = Digest(normalized)
	if e != nil {
		return DevEmailPlan{}, ErrInvalid
	}
	return p, nil
}
func selectDevEmails(p *DevEmailPlan, tables map[string]any) error {
	owners := map[string]OwnerPlan{}
	users := map[string]map[string]any{}
	phoneOwners := map[string]bool{}
	emailRows := map[string][]map[string]any{}
	seenIDs := map[string]bool{}
	canonicalOwners := map[string]string{}
	for _, o := range p.Original.Owners {
		owners[o.SourceID] = o
	}
	for _, v := range tables["users"].([]any) {
		r := v.(map[string]any)
		key, e := id(r["id"])
		if e != nil || users[key] != nil {
			return ErrInvalid
		}
		users[key] = r
	}
	for _, v := range tables["auth_identities"].([]any) {
		r := v.(map[string]any)
		key, e := id(r["id"])
		if e != nil || seenIDs[key] {
			return ErrInvalid
		}
		seenIDs[key] = true
		owner, e := id(r["user_id"])
		if e != nil {
			return ErrInvalid
		}
		if _, ok := owners[owner]; !ok {
			return ErrInvalid
		}
		provider, e := originalText(r["provider"])
		if e != nil {
			return ErrInvalid
		}
		switch provider {
		case "phone":
			phoneOwners[owner] = true
		case "email":
			p.Counts.EmailIdentities++
			emailRows[owner] = append(emailRows[owner], r)
			raw, e := originalText(r["subject"])
			if e != nil {
				return ErrInvalid
			}
			canonical := strings.ToLower(strings.TrimSpace(raw))
			if prior, ok := canonicalOwners[canonical]; ok && prior != owner {
				return ErrInvalid
			}
			canonicalOwners[canonical] = owner
		default:
			return ErrInvalid
		}
	}
	seenEmails := map[[32]byte]bool{}
	for owner, rows := range emailRows {
		if phoneOwners[owner] {
			continue
		}
		p.Counts.EmailOnlyOwners++
		if len(rows) != 1 || owners[owner].SourceStatus != "active" {
			return ErrInvalid
		}
		r := rows[0]
		raw, e := originalText(r["subject"])
		if e != nil {
			return ErrInvalid
		}
		email, ok := devauth.NormalizeEmail(raw)
		if !ok {
			return ErrInvalid
		}
		originalEmail, e := originalText(users[owner]["email"])
		if e != nil || strings.ToLower(strings.TrimSpace(originalEmail)) != email {
			return ErrInvalid
		}
		verified, e := originalText(r["verified_at"])
		if e != nil || verified == "" {
			return ErrInvalid
		}
		if _, e = time.Parse(time.RFC3339Nano, verified); e != nil {
			return ErrInvalid
		}
		digest := devauth.EmailDigest(email)
		if seenEmails[digest] {
			return ErrInvalid
		}
		seenEmails[digest] = true
		identitySHA, e := Digest(r)
		if e != nil {
			return ErrInvalid
		}
		o := owners[owner]
		p.Bindings = append(p.Bindings, DevEmailBinding{owner, o.SourceVersion, o.SourceSHA256, identitySHA, digest})
	}
	sort.Slice(p.Bindings, func(i, j int) bool {
		return bytes.Compare(p.Bindings[i].EmailDigest[:], p.Bindings[j].EmailDigest[:]) < 0
	})
	p.Counts.BindableOwners = len(p.Bindings)
	return nil
}
