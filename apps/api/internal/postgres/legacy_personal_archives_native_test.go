package postgres

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacymigration"
	"strings"
	"testing"
)

func TestLegacyPersonalArchivesNativeOriginalRunAndImmutableTombstones(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	source, manifest := legacySyntheticInput(t, func(tables map[string]any) {
		row := func(table string) map[string]any {
			v := map[string]any{}
			for _, c := range legacymigration.ColumnNames(table) {
				v[c] = nil
			}
			return v
		}
		owner := "14600000-0000-4000-8000-000000000010"
		journey := row("journeys")
		journey["id"] = "14900000-0000-4000-8000-000000000001"
		journey["owner_id"] = owner
		journey["title"] = "合成旧行程"
		journey["working_revision"] = 1
		for _, k := range []string{"city", "date_label", "cover_url", "scope_label"} {
			journey[k] = ""
		}
		journey["visibility"] = "private"
		journey["days"] = 1
		journey["team_revision"] = 1
		journey["allow_copy"] = false
		journey["operator_created"] = false
		journey["created_at"] = "2026-10-13T00:00:00Z"
		journey["updated_at"] = "2026-10-14T00:00:00Z"
		deleted := row("journeys")
		for k, v := range journey {
			deleted[k] = v
		}
		deleted["id"] = "14900000-0000-4000-8000-000000000002"
		deleted["deleted_at"] = "2026-10-14T00:00:00Z"
		deleted["title"] = "PRIVATE deleted title"
		tables["journeys"] = []any{journey, deleted}
		theme := row("theme_maps")
		theme["id"] = "14900000-0000-4000-8000-000000000003"
		theme["owner_id"] = owner
		theme["title"] = "合成旧主题地图"
		theme["revision"] = 1
		for _, k := range []string{"summary", "cover_url", "audit_status", "moderation_status", "commercial_disclosure", "topic", "review_reason", "decision_reason"} {
			theme[k] = ""
		}
		theme["visibility"] = "private"
		theme["status"] = "active"
		theme["operator_created"] = false
		theme["created_at"] = "2026-10-13T00:00:00Z"
		theme["updated_at"] = "2026-10-14T00:00:00Z"
		tables["theme_maps"] = []any{theme}
	})
	if _, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, source, manifest); !errors.Is(e, ErrLegacyMigrationNotFound) {
		t.Fatal("missing original run became import", e)
	}
	if _, e := f.Importer.Apply(f.Context, legacyTestRunID, source, manifest); e != nil {
		t.Fatal("base preservation", e)
	}
	result, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, source, manifest)
	if e != nil || result.Status != "COMMITTED" || result.Counts.Journeys != 2 || result.Counts.JourneyTombstones != 1 || result.Counts.ThemeMaps != 1 {
		t.Fatal("complete actual projection", result, e)
	}
	again, e := f.Importer.ApplyPersonalArchives(f.Context, legacyTestRunID, source, manifest)
	if e != nil || again != result {
		t.Fatal("same run repeated", e)
	}
	var count, live int
	if e = f.Pool.QueryRow(f.Context, `SELECT count(*),count(*) FILTER(WHERE NOT deleted) FROM legacy_personal_archives`).Scan(&count, &live); e != nil || count != 3 || live != 2 {
		t.Fatal("duplicate or missing effect", e)
	}
	var title string
	var payload []byte
	if e = f.Pool.QueryRow(f.Context, `SELECT title,payload FROM legacy_personal_archives WHERE deleted`).Scan(&title, &payload); e != nil || title != "" || payload != nil {
		t.Fatal("deleted private body revived", e)
	}
	wire, _ := json.Marshal(result)
	if strings.Contains(string(wire), "PRIVATE") {
		t.Fatal("private source in summary")
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE legacy_personal_archives SET title='changed'`); e == nil {
		t.Fatal("immutable old source edited")
	}
	var active int
	if e = f.Pool.QueryRow(f.Context, `SELECT count(*) FROM accounts a JOIN legacy_entity_mappings m ON m.target_id=a.id AND m.entity_kind='user' WHERE a.status='active'`).Scan(&active); e != nil || active != 0 {
		t.Fatal("archive accidentally claimed account", e)
	}
	// Only this disposable fixture supplies a synthetic current owner/session.
	// The real importer above and remote run never perform this activation.
	var owner, archive string
	if e = f.Pool.QueryRow(f.Context, `SELECT target_id FROM legacy_entity_mappings WHERE entity_kind='user'`).Scan(&owner); e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE accounts SET status='active' WHERE id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.Pool.Exec(f.Context, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES(gen_random_uuid(),$1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	store := New(f.Pool, false)
	actor := identity.Actor{ID: owner, AccountType: "person"}
	page, e := store.ListHumanLegacyPersonalArchives(f.Context, digest, actor, "journey", "")
	if e != nil || len(page.Items) != 2 || page.ResponseProof == nil {
		t.Fatal("same owner actual page", e)
	}
	if e = store.FinalizeHumanLegacyPersonalArchivePage(f.Context, digest, actor, page); e != nil {
		t.Fatal("actual encoded source proof", e)
	}
	archive = page.Items[0].ID
	detail, e := store.ReadHumanLegacyPersonalArchive(f.Context, digest, actor, archive)
	if e != nil || detail.ID != archive || store.FinalizeHumanLegacyPersonalArchiveDetail(f.Context, digest, actor, detail) != nil {
		t.Fatal("actual same-owner detail", e)
	}
	if _, e = f.Pool.Exec(f.Context, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, owner); e != nil {
		t.Fatal(e)
	}
	if e = page.ResponseProof.RevalidateHumanControl(f.Context); e == nil {
		t.Fatal("revoked session reused archived payload")
	}
}
