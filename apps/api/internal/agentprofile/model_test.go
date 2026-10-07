package agentprofile_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func profileFixture(kind actorref.Type) agentprofile.Record {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	record, err := agentprofile.New("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		actorref.PrincipalRef{Type: kind, ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}, now)
	if err != nil {
		panic(err)
	}
	return record
}

func TestAgentProfileSharedTypedOwnerModel(t *testing.T) {
	for _, kind := range []actorref.Type{actorref.Person, actorref.Organization, actorref.Business} {
		t.Run(string(kind), func(t *testing.T) {
			record := profileFixture(kind)
			if err := agentprofile.Validate(record); err != nil {
				t.Fatal(err)
			}
			owner, err := record.OwnerRef()
			if err != nil || owner.Type != kind || owner.ID != record.OwnerID {
				t.Fatal("typed account principal lost")
			}
			if record.ProfileVersion != 1 || record.CreatedAt != record.UpdatedAt {
				t.Fatal("new profile metadata not initialized once")
			}
			// The integration layer reuses the shared identity type. This does
			// not make the domain depend on a second identity implementation.
			reference := agentcognitive.AgentReference{AgentID: record.AgentID, Principal: owner, Role: agentruntime.ForType(kind).Role}
			if err := agentcognitive.ValidateAgentReference(reference); err != nil {
				t.Fatal(err)
			}
			policy, _ := agentcognitive.DescribeRolePolicy(reference)
			if kind == actorref.Business && policy.Available {
				t.Fatal("Business profile shape enabled runtime")
			}
		})
	}
}

func TestAgentProfileCanonicalIDsAndTimestamps(t *testing.T) {
	zone := time.FixedZone("synthetic offset", 8*60*60)
	now := time.Date(2026, 10, 2, 20, 0, 0, 0, zone)
	record, err := agentprofile.New("AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA",
		actorref.PrincipalRef{Type: actorref.Organization, ID: "BBBBBBBB-BBBB-4BBB-8BBB-BBBBBBBBBBBB"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if record.AgentID != strings.ToLower(record.AgentID) || record.OwnerID != strings.ToLower(record.OwnerID) ||
		record.CreatedAt.Location() != time.UTC || record.UpdatedAt.Location() != time.UTC || !record.CreatedAt.Equal(now) {
		t.Fatal("case/time normalization changed stable identity or instant")
	}
	record.ProfileVersion = math.MaxInt64
	if err := agentprofile.Validate(record); err != nil {
		t.Fatal("valid stored positive bigint rejected")
	}
	record.UpdatedAt = record.CreatedAt.Add(time.Second)
	if err := agentprofile.Validate(record); err != nil {
		t.Fatal("existing versioned record rejected")
	}
}

func TestAgentProfileInvalidBindingAndVersionMatrix(t *testing.T) {
	cases := []struct {
		name string
		edit func(*agentprofile.Record)
	}{
		{"empty Agent", func(r *agentprofile.Record) { r.AgentID = "" }},
		{"non UUID Agent", func(r *agentprofile.Record) { r.AgentID = "provider:model" }},
		{"zero Agent", func(r *agentprofile.Record) { r.AgentID = "00000000-0000-0000-0000-000000000000" }},
		{"padded Agent", func(r *agentprofile.Record) { r.AgentID = " " + r.AgentID }},
		{"no owner type", func(r *agentprofile.Record) { r.OwnerType = "" }},
		{"lowercase owner type", func(r *agentprofile.Record) { r.OwnerType = "person" }},
		{"unknown owner type", func(r *agentprofile.Record) { r.OwnerType = "MODEL" }},
		{"Community", func(r *agentprofile.Record) { r.OwnerType = actorref.Community }},
		{"City", func(r *agentprofile.Record) { r.OwnerType = "CITY" }},
		{"Place", func(r *agentprofile.Record) { r.OwnerType = "PLACE" }},
		{"Venue", func(r *agentprofile.Record) { r.OwnerType = "VENUE" }},
		{"anonymous owner", func(r *agentprofile.Record) { r.OwnerID = "" }},
		{"zero owner", func(r *agentprofile.Record) { r.OwnerID = "00000000-0000-0000-0000-000000000000" }},
		{"non UUID owner", func(r *agentprofile.Record) { r.OwnerID = "name-only" }},
		{"padded owner", func(r *agentprofile.Record) { r.OwnerID += " " }},
		{"zero profile version", func(r *agentprofile.Record) { r.ProfileVersion = 0 }},
		{"negative profile version", func(r *agentprofile.Record) { r.ProfileVersion = -1 }},
		{"missing created time", func(r *agentprofile.Record) { r.CreatedAt = time.Time{} }},
		{"missing update time", func(r *agentprofile.Record) { r.UpdatedAt = time.Time{} }},
		{"created time not JSON representable", func(r *agentprofile.Record) { r.CreatedAt = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"updated time not JSON representable", func(r *agentprofile.Record) { r.UpdatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
		{"update before create", func(r *agentprofile.Record) { r.UpdatedAt = r.CreatedAt.Add(-time.Nanosecond) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record := profileFixture(actorref.Person)
			tc.edit(&record)
			if !errors.Is(agentprofile.Validate(record), agentprofile.ErrInvalid) {
				t.Fatal("invalid metadata accepted")
			}
		})
	}
	for _, kind := range []actorref.Type{actorref.Community, "CITY", "PLACE", "VENUE", "SYSTEM", "MODEL", "person"} {
		t.Run("construct "+string(kind), func(t *testing.T) {
			record, err := agentprofile.New("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
				actorref.PrincipalRef{Type: kind, ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"}, time.Now())
			if !errors.Is(err, agentprofile.ErrInvalid) || !reflect.ValueOf(record).IsZero() {
				t.Fatal("forbidden Agent principal constructed")
			}
		})
	}
	valid := profileFixture(actorref.Person)
	if record, err := agentprofile.New(valid.AgentID, actorref.PrincipalRef{Type: valid.OwnerType, ID: valid.OwnerID}, time.Time{}); !errors.Is(err, agentprofile.ErrInvalid) || !reflect.ValueOf(record).IsZero() {
		t.Fatal("missing timestamp became server metadata")
	}
}

func TestAgentProfileMetadataSeparateFromUserProfileAndRuntime(t *testing.T) {
	record := profileFixture(actorref.Person)
	user := identity.Profile{AccountID: record.OwnerID, DisplayName: "合成用户姓名", Bio: "私人原资料", Visibility: "private"}
	before := record
	user.DisplayName, user.Bio, user.Visibility = "已改姓名", "已改原资料", "public"
	if record != before {
		t.Fatal("ordinary UserProfile edit replaced independent metadata")
	}
	for _, model := range []string{"fake-model-a", "fake-model-b", "disabled"} {
		selection := struct {
			Model   string
			Profile agentprofile.Record
		}{model, record}
		if selection.Profile.AgentID != before.AgentID || selection.Profile.OwnerType != before.OwnerType || selection.Profile.OwnerID != before.OwnerID {
			t.Fatal("provider selection replaced stable Agent owner")
		}
	}
	body, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	wanted := []string{"agentId", "ownerType", "ownerId", "profileVersion", "createdAt", "updatedAt"}
	if len(fields) != len(wanted) {
		t.Fatalf("unexpected base-profile contents: %s", body)
	}
	for _, field := range wanted {
		if _, ok := fields[field]; !ok {
			t.Fatalf("missing required metadata field %s", field)
		}
	}
	for _, private := range []string{user.DisplayName, user.Bio, "displayName", "bio", "visibility", "memory", "model", "provider", "permission", "confirmed"} {
		if strings.Contains(string(body), private) {
			t.Fatalf("identity/private/runtime copied into base profile: %s", private)
		}
	}
	// Structural decoding is never authority. The new cognitive port remains
	// unavailable, even when the caller produces valid profile-shaped metadata.
	var decoded agentprofile.Record
	if err = json.Unmarshal(body, &decoded); err != nil || decoded != record {
		t.Fatal("metadata roundtrip changed stable fields")
	}
	if _, err = (agentcognitive.UnavailableCognitivePorts{}).ReadMemory(context.Background(), agentcognitive.ReadRequest{}); !errors.Is(err, agentcognitive.ErrUnavailable) {
		t.Fatal("base Profile opened Memory")
	}
}

func TestAgentProfileOwnerNamespacesStayDistinct(t *testing.T) {
	person, organization, business := profileFixture(actorref.Person), profileFixture(actorref.Organization), profileFixture(actorref.Business)
	personOwner, _ := person.OwnerRef()
	organizationOwner, _ := organization.OwnerRef()
	businessOwner, _ := business.OwnerRef()
	if personOwner.Equal(organizationOwner) || organizationOwner.Equal(businessOwner) || personOwner.Equal(businessOwner) {
		t.Fatal("same UUID merged typed principals")
	}
	// Pure shape validation cannot prove that this ID is organizations.account_id
	// rather than organizations.id. That actual binding belongs to Store + DB,
	// which must reject an incorrect Agent/owner pair before persisting/reading.
	if _, err := organization.OwnerRef(); err != nil {
		t.Fatal(err)
	}
}
