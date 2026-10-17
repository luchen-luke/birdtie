package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

func TestSelectedPlaceFollowupPredicatePrecedesOriginalLimit(t *testing.T) {
	sql := resultProjectionSQL()
	selected := selectedNativePlaceProjectionSQL(sql)
	pos := strings.Index(selected, "s.id=q->>'SelectedNativePlaceID'")
	limit := strings.Index(selected, "s.title,s.id LIMIT 30")
	if pos < 0 || limit <= pos || strings.Count(selected, "SelectedNativePlaceID") != 1 || strings.Contains(selected, "HistoryRefs") {
		t.Fatal("native selector was not independent and before LIMIT")
	}
}

func TestSelectedPlaceFollowupInvalidSelectorStopsBeforeSQL(t *testing.T) {
	_, err := (&Store{}).ContinueOwnSelectedPlace(context.Background(), arp.Access{}, "地址", agentworkspace.SelectedPlaceSelector{PlaceID: "osm:node:1"})
	if !errors.Is(err, arp.ErrDenied) {
		t.Fatal("invalid selector attempted native read", err)
	}
}

// Root runs this exact test once against an isolated database. It is skipped
// without BIRDTIE_DATABASE_URL; no model/provider or production DB is opened.
func TestSelectedPlaceFollowupNativeThirtyFirstAndRetiredSource(t *testing.T) {
	// The old LIVE fixture creates a Moment through a retired human-control
	// path. This test needs only the current owned schema and native Task owner.
	ownedMigrationDatabase(t)
	private := agentPrivateTestFixture(t)
	b := private.base
	id := func() string {
		var value string
		if err := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	access := func(task agentworkspace.Task) arp.Access {
		raw, err := json.Marshal(agentworkspace.SanitizeTaskForResponse(task))
		if err != nil {
			t.Fatal(err)
		}
		return arp.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: private.owner.SessionDigest, TaskID: task.ID, ExpectedTask: raw}
	}
	city, foreignCity := "unit-selected-"+id(), "unit-selected-foreign-"+id()
	for _, value := range []string{city, foreignCity} {
		b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'UNIT selected city','LOCAL','GB','Europe/London','published','UNIT_PUBLIC_SOURCE','https://example.com/unit-selected-city','合成维护者',$2)`, value, b.other.ID)
		b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, value)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, sql := range []string{`DELETE FROM agent_tasks WHERE owner_account_id=ANY($1::uuid[])`} {
			if _, err := b.pool.Exec(ctx, sql, b.accounts); err != nil {
				t.Error(err)
			}
		}
		for _, sql := range []string{`DELETE FROM places WHERE city_id=ANY($1::text[])`, `DELETE FROM contexts WHERE city_id=ANY($1::text[])`, `DELETE FROM city_contexts WHERE city_id=ANY($1::text[])`, `DELETE FROM cities WHERE id=ANY($1::text[])`} {
			if _, err := b.pool.Exec(ctx, sql, []string{city, foreignCity}); err != nil {
				t.Error(err)
			}
		}
	})
	name := "UNIT selected repeated public venue"
	ids := []string{}
	for i := 0; i < 32; i++ {
		value := id()
		ids = append(ids, value)
		b.exec(`INSERT INTO places(id,city_id,name,category_code,latitude,longitude,location_precision,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,$3,'sports',57.15,-2.09,'point','published','UNIT_PUBLIC_SOURCE','https://example.com/unit-selected-public-place','合成维护者',$4)`, value, city, name, b.other.ID)
	}
	var last string
	if err := b.pool.QueryRow(b.ctx, `SELECT id::text FROM places WHERE id=ANY($1::uuid[]) ORDER BY id DESC LIMIT 1`, ids).Scan(&last); err != nil {
		t.Fatal(err)
	}
	query := "UNIT explicit original public place search"
	previous, err := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: city, Query: query, Intent: agentworkspace.FindPlace, Status: agentworkspace.TaskActive, Filters: map[string]string{"currentQuery": query, "searchTerm": name, "targetIntent": agentworkspace.FindPlace, "locationPreference": "city", "timePreference": "anytime"}, Conversation: []agentworkspace.Message{{Role: "user", Text: query}}})
	if err != nil {
		t.Fatal("current native Task", err)
	}
	previous, err = b.store.CaptureOwnHumanReply(b.ctx, access(previous))
	if err != nil {
		t.Fatal("current native original reply", err)
	}
	for _, ref := range previous.Conversation[len(previous.Conversation)-1].ResultMembership.Refs {
		if ref.ID == last {
			t.Fatal("31st+ row unexpectedly entered the original LIMIT30 membership")
		}
	}
	foreignPlace := id()
	b.exec(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'UNIT foreign selected place','sports','published','UNIT_PUBLIC_SOURCE','https://example.com/unit-foreign-place','合成维护者',$3)`, foreignPlace, foreignCity, b.other.ID)
	if _, err := b.store.ContinueOwnSelectedPlace(b.ctx, access(previous), "地址", agentworkspace.SelectedPlaceSelector{PlaceID: foreignPlace}); err == nil {
		t.Fatal("foreign-city selector was accepted")
	}
	next, err := b.store.ContinueOwnSelectedPlace(b.ctx, access(previous), "把这个加入第二天", agentworkspace.SelectedPlaceSelector{PlaceID: last})
	if err != nil || next.ID != previous.ID || next.Filters[agentworkspace.PlaceFollowupIDFilter] != last || next.Filters[agentworkspace.SelectedPlaceDayFilter] != "2" || !agentworkspace.SelectedPlaceDayDraftTask(next) {
		t.Fatal("31st exact selected row lost to native limit or day was dropped", err)
	}
	if question, ok := currentLocalPlaceQuestion(next); !ok || question.Name != name || question.Field != agentworkspace.SelectedPlaceDayDraft {
		t.Fatal("day draft was not a bounded local target")
	}
	for _, mode := range []string{"withdrawn", "task_changed"} {
		t.Run(mode, func(t *testing.T) {
			currentAccess := access(next)
			if mode == "withdrawn" {
				b.exec(`UPDATE places SET publication_status='draft' WHERE id=$1`, last)
				defer b.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, last)
			}
			if mode == "task_changed" {
				currentAccess.ExpectedTask = []byte(`{}`)
			}
			if _, _, err := b.store.TryOwnLocalPlaceReply(b.ctx, currentAccess); err == nil {
				t.Fatal("retired/foreign/changed current selector remained usable")
			}
		})
	}
	// A map selection can start a real native Task without inventing a prior
	// assistant membership. Only the original user turn is retained once.
	unique := id()
	b.exec(`INSERT INTO places(id,city_id,name,category_code,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'UNIT uniquely selected venue','sports','published','UNIT_PUBLIC_SOURCE','https://example.com/unit-unique-selected-place','合成维护者',$3)`, unique, city, b.other.ID)
	startQuery := "把这个加入第二天"
	started, err := b.store.SaveTask(b.ctx, agentworkspace.Task{PrincipalType: "person", PrincipalID: b.person.ID, ActingUserID: b.person.ID, CityID: city, Query: startQuery, Intent: "PENDING", Status: agentworkspace.TaskActive, Filters: map[string]string{}, Conversation: []agentworkspace.Message{{Role: "user", Text: startQuery}}})
	if err != nil {
		t.Fatal(err)
	}
	started, err = b.store.ContinueOwnSelectedPlace(b.ctx, access(started), startQuery, agentworkspace.SelectedPlaceSelector{PlaceID: unique})
	if err != nil || len(started.Conversation) != 1 || started.Filters[agentworkspace.PlaceFollowupIDFilter] != unique {
		t.Fatal("new native Task selector invented a prior membership or duplicated its turn", err)
	}
	completed, err := b.store.CaptureOwnHumanReply(b.ctx, access(started))
	if err != nil {
		t.Fatal("selected native membership capture", err)
	}
	continued, err := b.store.ContinueOwnPublicPlace(b.ctx, access(completed), "它需要门票吗？")
	if err != nil || continued.Filters[agentworkspace.SelectedPlaceContextFilter] != "" || continued.Filters[agentworkspace.PlaceFollowupIDFilter] != unique || continued.Filters[agentworkspace.SelectedPlaceDayFilter] != "2" || !livePlaceGeneration(continued.Filters[agentworkspace.PlaceFollowupGenerationFilter]) {
		t.Fatal("ordinary prior-place continuation kept map-only marker or lost its native proof/pending target", err)
	}
}
