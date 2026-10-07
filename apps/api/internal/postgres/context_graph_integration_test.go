package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestContextGraphIntegration(t *testing.T) {
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("set BIRDTIE_DATABASE_URL for PostgreSQL Context Graph integration")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var contextTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.contexts')::text`).Scan(&contextTable); err != nil {
		t.Fatal(err)
	}
	if contextTable == nil {
		t.Skip("apply migration 033 on a disposable database")
	}
	var personID, aberdeenContextID string
	if err := pool.QueryRow(ctx, `SELECT a.id FROM accounts a JOIN agents ag
        ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
        WHERE a.id='b1700000-0000-4000-8000-000000000010'
          AND a.account_type='person' AND a.status='active'`).Scan(&personID); err != nil {
		t.Fatalf("required 002_functional_mvp seed Person/Agent missing: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM contexts
        WHERE context_type='CITY' AND city_id='aberdeen-gb'`).Scan(&aberdeenContextID); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("synthetic-ctx-%d", time.Now().UnixNano())
	var otherContextID, onlineContextID string
	if _, err := pool.Exec(ctx, `INSERT INTO cities
        (id,name,region,country_code,time_zone,source_label,source_ref,maintainer_label)
        VALUES($1,'Synthetic cross-city','Fixture','GB','Europe/London','synthetic',$1,'test')`, key); err != nil {
		t.Fatal(err)
	}
	defer func() { ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM cities WHERE id=$1`, key) }()
	if _, err := pool.Exec(ctx, `INSERT INTO city_contexts(city_id) VALUES($1)`, key); err != nil {
		t.Fatal(err)
	}
	defer func() { ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM city_contexts WHERE city_id=$1`, key) }()
	defer func() {
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM contexts
			WHERE (context_type='CITY' AND city_id=$1) OR (context_type='ONLINE' AND online_key=$1)`, key)
	}()
	if err := pool.QueryRow(ctx, `SELECT id FROM contexts
        WHERE context_type='CITY' AND city_id=$1`, key).Scan(&otherContextID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO contexts(context_type,online_key)
        VALUES('ONLINE',$1) RETURNING id`, key).Scan(&onlineContextID); err != nil {
		t.Fatal(err)
	}
	store := New(pool, false)
	var ownedTaskIDs []string
	defer func() {
		// The actor is a shared seed: only this test's exact Task IDs are owned.
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM audit_events WHERE actor_account_id=$1 AND resource_type='agent_task' AND resource_id=ANY($2::text[])`, personID, ownedTaskIDs)
		ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM agent_tasks WHERE id=ANY($1::uuid[])`, ownedTaskIDs)
		var residue int
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_tasks WHERE id=ANY($2::uuid[]))+(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_type='agent_task' AND resource_id=ANY($2::text[]))`, personID, ownedTaskIDs).Scan(&residue); e != nil || residue != 0 {
			t.Errorf("owned Context Task/audit residue=%d error=%v", residue, e)
		}
	}()
	base := agentworkspace.Task{PrincipalType: "person", PrincipalID: personID,
		ActingUserID: personID, Intent: "FIND_ACTIVITY", Status: agentworkspace.TaskActive,
		Filters: map[string]string{}, Conversation: []agentworkspace.Message{}}
	first := base
	first.CityID, first.Query = "aberdeen-gb", "合成 Aberdeen Context 查询"
	first, err = store.SaveTask(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	ownedTaskIDs = append(ownedTaskIDs, first.ID)
	second := base
	second.CityID, second.Query = key, "合成跨城 Context 查询"
	second, err = store.SaveTask(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	ownedTaskIDs = append(ownedTaskIDs, second.ID)
	online := base
	online.ContextType, online.ContextID, online.Query = "ONLINE", onlineContextID, "合成纯在线 Context 查询"
	online, err = store.SaveTask(ctx, online)
	if err != nil {
		t.Fatal(err)
	}
	ownedTaskIDs = append(ownedTaskIDs, online.ID)
	for _, tc := range []struct {
		task agentworkspace.Task
		kind contextgraph.Type
		id   string
	}{{first, contextgraph.City, aberdeenContextID},
		{second, contextgraph.City, otherContextID},
		{online, contextgraph.Online, onlineContextID}} {
		ref, err := tc.task.ContextRef()
		if err != nil || ref.Type != tc.kind || ref.ID != tc.id || tc.task.PrincipalID != personID {
			t.Fatalf("task Context/Person mismatch: %+v ref=%+v err=%v", tc.task, ref, err)
		}
	}
	if online.CityID != "" {
		t.Fatal("online task acquired a fake city")
	}
	encoded, err := json.Marshal(online)
	if err != nil || !strings.Contains(string(encoded), `"contextType":"ONLINE"`) ||
		!strings.Contains(string(encoded), `"contextId":"`+onlineContextID+`"`) {
		t.Fatalf("online API model lacks typed context: %s %v", encoded, err)
	}
	bad := base
	bad.CityID, bad.ContextType, bad.ContextID, bad.Query =
		"aberdeen-gb", "ONLINE", onlineContextID, "invalid mixed context"
	if _, err := store.SaveTask(ctx, bad); err == nil {
		t.Fatal("online context accepted a city parent")
	}
	online.Status = agentworkspace.TaskCompleted
	updated, err := store.UpdateTask(ctx, online)
	if err != nil || updated.ContextType != "ONLINE" || updated.ContextID != onlineContextID {
		t.Fatalf("online Context lost on update: %+v %v", updated, err)
	}
	got, err := store.GetTask(ctx, personID, online.ID)
	if err != nil || got.ContextType != "ONLINE" || got.CityID != "" {
		t.Fatalf("online Context lost on restore: %+v %v", got, err)
	}
}
