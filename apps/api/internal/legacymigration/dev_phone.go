package legacymigration

import (
	"bytes"
	"sort"

	"github.com/birdtie/birdtie/apps/api/internal/devauth"
)

// No raw phone, subject, email, credential or old Session survives projection.
type DevPhoneBinding struct {
	SourceOwnerID        string
	SourceOwnerVersion   int64
	SourceOwnerSHA256    string
	SourceIdentitySHA256 string
	PhoneDigest          [32]byte
}
type DevPhoneCounts struct {
	Owners          int `json:"owners"`
	PhoneIdentities int `json:"phoneIdentities"`
	BindableOwners  int `json:"bindableOwners"`
	EmailOnlyOwners int `json:"emailOnlyOwners"`
}
type DevPhonePlan struct {
	Original Plan              `json:"-"`
	Bindings []DevPhoneBinding `json:"-"`
	Counts   DevPhoneCounts    `json:"counts"`
}

func (DevPhoneBinding) String() string { return "private exact test phone binding" }
func (DevPhonePlan) String() string    { return "private fixed-code binding plan; NOT_IMPORTED" }

// Revalidate the original full, closed snapshot before reading phone identities.
// Historical verification does not become a production phone ownership claim.
func BuildDevPhonePlan(run string, sourceRaw, manifestRaw []byte) (DevPhonePlan, error) {
	var out DevPhonePlan
	original, e := BuildPlan(run, sourceRaw, manifestRaw)
	if e != nil {
		return out, e
	}
	decoded, e := decode(sourceRaw)
	if e != nil {
		return out, ErrInvalid
	}
	src := decoded.(map[string]any)
	tables := src["tables"].(map[string]any)
	owners := map[string]OwnerPlan{}
	for _, o := range original.Owners {
		owners[o.SourceID] = o
	}
	seenIDs := map[string]bool{}
	seenPhones := map[[32]byte]bool{}
	phoneOwners := map[string]bool{}
	emailOwners := map[string]bool{}
	out.Original = original
	out.Bindings = []DevPhoneBinding{}
	out.Counts.Owners = len(original.Owners)
	for _, raw := range tables["auth_identities"].([]any) {
		r := raw.(map[string]any)
		identityID, e := id(r["id"])
		if e != nil || seenIDs[identityID] {
			return DevPhonePlan{}, ErrInvalid
		}
		seenIDs[identityID] = true
		ownerID, e := id(r["user_id"])
		if e != nil {
			return DevPhonePlan{}, ErrInvalid
		}
		owner, ok := owners[ownerID]
		if !ok {
			return DevPhonePlan{}, ErrInvalid
		}
		provider, e := originalText(r["provider"])
		if e != nil {
			return DevPhonePlan{}, ErrInvalid
		}
		if provider == "email" {
			emailOwners[ownerID] = true
			continue
		}
		if provider != "phone" || owner.SourceStatus != "active" {
			return DevPhonePlan{}, ErrInvalid
		}
		rawPhone, e := originalText(r["subject"])
		if e != nil {
			return DevPhonePlan{}, ErrInvalid
		}
		canonical, ok := devauth.NormalizePhone(rawPhone)
		if !ok {
			return DevPhonePlan{}, ErrInvalid
		}
		digest := devauth.PhoneDigest(canonical)
		if seenPhones[digest] || phoneOwners[ownerID] {
			return DevPhonePlan{}, ErrInvalid
		}
		seenPhones[digest] = true
		phoneOwners[ownerID] = true
		identitySHA, e := Digest(r)
		if e != nil {
			return DevPhonePlan{}, ErrInvalid
		}
		out.Bindings = append(out.Bindings, DevPhoneBinding{ownerID, owner.SourceVersion, owner.SourceSHA256, identitySHA, digest})
		out.Counts.PhoneIdentities++
	}
	for ownerID := range emailOwners {
		if !phoneOwners[ownerID] {
			out.Counts.EmailOnlyOwners++
		}
	}
	out.Counts.BindableOwners = len(out.Bindings)
	sort.Slice(out.Bindings, func(i, j int) bool {
		return bytes.Compare(out.Bindings[i].PhoneDigest[:], out.Bindings[j].PhoneDigest[:]) < 0
	})
	return out, nil
}
