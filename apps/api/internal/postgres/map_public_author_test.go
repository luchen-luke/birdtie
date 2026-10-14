package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
)

// SQL assembly assertions only. These do not execute SQL or demonstrate native
// authorization; isolated native publication/profile/block acceptance is separate.
func TestMapPublicAuthorSQLUsesOriginalSourceBoundsAndActor(t *testing.T) {
	sql := mapPublicAuthorProjectionSQL
	if sql == "" || sql != mapPublicAuthorReadSQL() {
		t.Fatal("author SQL assembly unavailable/nondeterministic")
	}
	for _, required := range []string{
		"clock_timestamp() at", "c.publication_status='published'", "p.publication_status='published'",
		currentRetainedPlaceVisibilitySQL("p", "stamp.at"), currentRetainedPlaceDeadlineSQL("p", "stamp.at"), retainedPlaceReadBindingSQL("p"),
		"p.coordinate_system='wgs84'", "p.location_precision='point'", "p.longitude BETWEEN $2 AND $4 AND p.latitude BETWEEN $3 AND $5",
		"JOIN point p ON p.id=m.place_id AND p.city_id=m.city_id", "m.status='published'", "m.visibility='public'", "m.location_precision='place'",
		"m.author_confirmed_at<=m.published_at", "m.published_at<=stamp.at", "author.account_type='person' AND author.status='active'",
		"m.revision::text version,p.retained_until deadline", "JOIN author_target target ON target.id=author.id",
		"rank<=30", "rank<=31", "rank>30", "'entityRef'", "'detailRef'", "'sourceVersion'",
		mapProjectionSessionAuthoritySQL, "se.revoked_at IS NULL", "se.expires_at>stamp.at", "se.idle_expires_at>stamp.at",
		"least(stamp.at+interval '2 minutes',c.expires_at,payload.deadline,(SELECT deadline FROM native_actor))",
	} {
		if !strings.Contains(sql, required) {
			t.Fatal("original source/bounds/actor gate missing", required)
		}
	}
	for _, forbidden := range []string{"PRIVATE_SOURCE_TOKENS", "PRIVATE_HUMAN_PAYLOAD", "'PLACE'::text kind", "'ACTIVITY'", "'BUSINESS'", "'ORGANIZATION'", "'OPPORTUNITY'", "m.body", "m.occurred_at", "private_moment_images", "consent_grants", "FROM follows", "agent_private_profiles", "memories", "saved_items"} {
		if strings.Contains(sql, forbidden) {
			t.Fatal("author public map broadened/read private source", forbidden)
		}
	}
}

func TestMapPublicAuthorSQLCurrentTargetAndEmptyGeneration(t *testing.T) {
	sql := mapPublicAuthorProjectionSQL
	for _, required := range []string{
		"public_author_source AS MATERIALIZED", "a.id=$9::uuid AND a.account_type='person' AND a.status='active'",
		publicMomentAuthorProfileSQL("a"), publicMomentAuthorBindingSQL("a"), "a.xmin::text",
		"author_target AS MATERIALIZED(SELECT id,token FROM public_author_source WHERE public_profile IS NOT NULL)",
		"b.blocker_account_id=$6::uuid AND b.blocked_account_id=a.id", "b.blocker_account_id=a.id AND b.blocked_account_id=$6::uuid",
		"payload.tokens,(SELECT token FROM native_actor),(SELECT token FROM author_target)",
		"FROM city c CROSS JOIN stamp CROSS JOIN payload CROSS JOIN author_target",
		"'[]'::jsonb) items", mapPublicAuthorSafetyGenerationSQL,
	} {
		if !strings.Contains(sql, required) {
			t.Fatal("current/empty author authority missing", required)
		}
	}
	for _, need := range []string{"SELECT max(safety.id) FROM audit_events safety", "safety.resource_type='account'", "safety.decision='allowed'", "safety.purpose='personal_safety'", "safety.action IN('block','unblock')", "safety.actor_account_id=$6::uuid AND safety.resource_id=a.id::text", "safety.actor_account_id=a.id AND safety.resource_id=$6::uuid::text"} {
		if !strings.Contains(mapPublicAuthorSafetyGenerationSQL, need) {
			t.Fatal("Block/Unblock original persistent pair generation missing", need)
		}
	}
	if strings.Contains(mapPublicAuthorSafetyGenerationSQL, "xmin") || strings.Contains(mapPublicAuthorSafetyGenerationSQL, "LIMIT") || strings.Contains(mapPublicAuthorSafetyGenerationSQL, "SELECT *") {
		t.Fatal("safety fence relies on deleted row/count or selects audit text")
	}
}

func TestMapPublicAuthorSQLLegacyPathsUnchanged(t *testing.T) {
	for _, private := range []bool{false, true} {
		sql := mapProjectionSQL(private)
		for _, forbidden := range []string{"$9", "public_author_source", "author_target", "max(safety.id)"} {
			if strings.Contains(sql, forbidden) {
				t.Fatal("opt-in leaked into original map source", private, forbidden)
			}
		}
		if !strings.Contains(sql, "'PLACE'::text kind") || !strings.Contains(sql, "UNION ALL SELECT 'ACTIVITY'") {
			t.Fatal("original catalog source changed", private)
		}
	}
}

func TestMapPublicAuthorSealBindsEmptyTargetQueryAndAccess(t *testing.T) {
	const author = "27000000-0000-4000-8000-000000000011"
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	r := mp.Receipt{View: mp.View{SchemaVersion: mp.PublicAuthorSchemaVersion, CityID: "aberdeen-gb", Scope: mp.PublicAuthor, AuthorID: author, ObservedAt: now, ValidUntil: now.Add(time.Minute), Items: []mp.Item{}}, Query: mp.Query{CityID: "aberdeen-gb", West: -2.2, South: 57.1, East: -2, North: 57.3, AuthorID: author}, Proof: "synthetic-current-target-profile-and-safety-epoch"}
	seal, e := mapProjectionSeal(mp.Access{}, r)
	if e != nil {
		t.Fatal(e)
	}
	for _, mode := range []string{"author_query", "author_view", "empty_target_epoch", "viewport", "viewer_session"} {
		t.Run(mode, func(t *testing.T) {
			changed := r
			access := mp.Access{}
			switch mode {
			case "author_query":
				changed.Query.AuthorID = "27000000-0000-4000-8000-000000000012"
			case "author_view":
				changed.View.AuthorID = "27000000-0000-4000-8000-000000000012"
			case "empty_target_epoch":
				changed.Proof = "synthetic-later-profile-or-safety-epoch"
			case "viewport":
				changed.Query.East = -2.01
			case "viewer_session":
				access = mp.Access{Actor: identity.Actor{ID: author, AccountType: "person"}, Digest: [32]byte{1}}
			}
			other, e := mapProjectionSeal(access, changed)
			if e != nil || other == seal {
				t.Fatal("empty target native seal did not bind", mode, e)
			}
		})
	}
	raw, e := json.Marshal(r.View)
	if e != nil {
		t.Fatal(e)
	}
	for _, private := range []string{"synthetic-", "Proof", "Seal", "safety", "user_profiles", "profile_version"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("source authority leaked on public wire", string(raw))
		}
	}
}

func TestMapPublicAuthorRevalidationRejectsTamperedSealedEmptySource(t *testing.T) {
	const author = "27000000-0000-4000-8000-000000000011"
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	r := mp.Receipt{View: mp.View{SchemaVersion: mp.PublicAuthorSchemaVersion, CityID: "aberdeen-gb", Scope: mp.PublicAuthor, AuthorID: author, ObservedAt: now, ValidUntil: now.Add(time.Minute), Items: []mp.Item{}}, Query: mp.Query{CityID: "aberdeen-gb", West: -2.2, South: 57.1, East: -2, North: 57.3, AuthorID: author}, Proof: "synthetic-empty-author-generation"}
	var e error
	r.Seal, e = mapProjectionSeal(mp.Access{}, r)
	if e != nil {
		t.Fatal(e)
	}
	// No pool exists: every case must stop before any native capture/read.
	store := &Store{}
	for _, mode := range []string{"another_author", "removed_selector", "changed_target_epoch", "another_viewer", "foreign_scope", "forged_seal"} {
		t.Run(mode, func(t *testing.T) {
			out := r
			access := mp.Access{}
			want := mp.ErrChanged
			switch mode {
			case "another_author":
				out.Query.AuthorID = "27000000-0000-4000-8000-000000000012"
				out.View.AuthorID = out.Query.AuthorID
			case "removed_selector":
				out.Query.AuthorID = ""
				want = mp.ErrInvalid
			case "changed_target_epoch":
				out.Proof = "synthetic-later-target-generation"
			case "another_viewer":
				access = mp.Access{Actor: identity.Actor{ID: author, AccountType: "person"}, Digest: [32]byte{1}}
			case "foreign_scope":
				out.View.Scope = mp.SelfPrivate
				want = mp.ErrInvalid
			case "forged_seal":
				out.Seal = strings.Repeat("0", 64)
			}
			if e := store.RevalidateMapLayers(context.Background(), access, out); !errors.Is(e, want) {
				t.Fatal("tampered sealed empty author source accepted", mode, e)
			}
		})
	}
}
