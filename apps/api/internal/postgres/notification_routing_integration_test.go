package postgres

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/jackc/pgx/v5/pgxpool"
)

func nativeNotificationFixture(t *testing.T) *agentPrivateFixture {
	t.Helper()
	f := agentPrivateTestFixture(t)
	b := f.base
	var ready bool
	if err := b.pool.QueryRow(b.ctx, `SELECT to_regclass('native_notification_policies') IS NOT NULL AND to_regclass('native_notification_decisions') IS NOT NULL`).Scan(&ready); err != nil || !ready {
		t.Fatal("native notification integration requires actual061")
	}
	if _, err := b.store.EnsureAgentProfile(b.ctx, b.personID, b.person); err != nil {
		t.Fatal(err)
	}
	if _, err := b.store.EnsureAgentProfile(b.ctx, b.otherID, b.other); err != nil {
		t.Fatal(err)
	}
	return f
}

func nativeNotificationInput(t *testing.T, f *agentPrivateFixture, route agentnotification.Route) agentnotification.PutInput {
	t.Helper()
	var now time.Time
	if err := f.base.pool.QueryRow(f.base.ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatal(err)
	}
	return agentnotification.PutInput{Enabled: true, DefaultRoute: route, Rules: []agentnotification.Rule{}, ExpiresAt: now.UTC().Add(time.Hour).Truncate(time.Microsecond)}
}

func TestNativeNotificationPolicyPersistenceCASAndIsolationIntegration(t *testing.T) {
	f := nativeNotificationFixture(t)
	b := f.base
	before := agentMemoryOwnedSourceSnapshot(t, f)
	initial, err := b.store.GetOwnNotificationPolicy(b.ctx, f.owner)
	if err != nil || initial.Version != 0 || initial.Enabled || initial.AgentID != b.personID {
		t.Fatal("native unconfigured preference did not retain exact Agent", err)
	}
	input := nativeNotificationInput(t, f, agentnotification.Silent)
	created, err := b.store.PutOwnNotificationPolicy(b.ctx, f.owner, input)
	if err != nil || created.Version != 1 || created.DefaultRoute != agentnotification.Silent {
		t.Fatal("native notification policy save", err)
	}
	rebuilt, err := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer rebuilt.Close()
	read, err := New(rebuilt, false).GetOwnNotificationPolicy(b.ctx, f.owner)
	if err != nil || !reflect.DeepEqual(created, read) {
		t.Fatal("policy did not survive independent pool", err)
	}
	if got, err := b.store.PutOwnNotificationPolicy(b.ctx, f.owner, input); !errors.Is(err, agentnotification.ErrConflict) || got.Version != 0 {
		t.Fatal("stale policy CAS was not empty conflict", err)
	}
	other, err := b.store.GetOwnNotificationPolicy(b.ctx, f.peer)
	if err != nil || other.Version != 0 || other.AgentID != b.otherID {
		t.Fatal("policy crossed owner", err)
	}
	for label, access := range map[string]agentprofile.PrivateAccess{"wrong_owner": {SessionDigest: f.owner.SessionDigest, WorkspacePrincipal: b.other}, "organization": f.org, "business": f.biz, "anonymous": {}} {
		t.Run(label, func(t *testing.T) {
			got, err := b.store.GetOwnNotificationPolicy(b.ctx, access)
			if err == nil || got.Version != 0 || got.AgentID != "" {
				t.Fatal("invalid native owner released policy", err)
			}
		})
	}
	if after := agentMemoryOwnedSourceSnapshot(t, f); after != before {
		t.Fatal("notification policy changed private/native source rows")
	}
	input.ExpectedVersion = 1
	input.Enabled = false
	closed, err := b.store.PutOwnNotificationPolicy(b.ctx, f.owner, input)
	if err != nil || closed.Version != 2 || closed.Enabled {
		t.Fatal("disable did not persist independent version", err)
	}
	decision, err := agentnotification.ChooseRoute(closed, agentnotification.CategoryMessage, *closed.UpdatedAt)
	if err != nil || decision.Route != agentnotification.Normal || decision.Reason != "disabled" {
		t.Fatal("disabled was falsely interpreted as mute", err)
	}
}

func TestNativeNotificationPolicyConcurrentCASIntegration(t *testing.T) {
	f := nativeNotificationFixture(t)
	b := f.base
	input := nativeNotificationInput(t, f, agentnotification.Normal)
	if _, err := b.store.PutOwnNotificationPolicy(b.ctx, f.owner, input); err != nil {
		t.Fatal(err)
	}
	input.ExpectedVersion = 1
	input.DefaultRoute = agentnotification.Immediate
	var wg sync.WaitGroup
	results := make(chan error, 12)
	start := make(chan struct{})
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := b.store.PutOwnNotificationPolicy(b.ctx, f.owner, input)
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, agentnotification.ErrConflict) {
			conflict++
		} else {
			t.Fatal("unexpected policy concurrent failure", err)
		}
	}
	if success != 1 || conflict != 11 {
		t.Fatalf("native CAS winners=%d conflicts=%d", success, conflict)
	}
}

func TestNativeNotificationPolicySQLShapesAndRevocationIntegration(t *testing.T) {
	f := nativeNotificationFixture(t)
	b := f.base
	input := nativeNotificationInput(t, f, agentnotification.Block)
	created, err := b.store.PutOwnNotificationPolicy(b.ctx, f.owner, input)
	if err != nil {
		t.Fatal(err)
	}
	for name, sql := range map[string]string{
		"version_same":    `UPDATE native_notification_policies SET version=version WHERE owner_id=$1`,
		"version_gap":     `UPDATE native_notification_policies SET version=version+2 WHERE owner_id=$1`,
		"unknown_rule":    `UPDATE native_notification_policies SET version=version+1,rules='[{"category":"UNKNOWN","route":"NORMAL"}]' WHERE owner_id=$1`,
		"numeric_rule":    `UPDATE native_notification_policies SET version=version+1,rules='[{"category":2,"route":"NORMAL"}]' WHERE owner_id=$1`,
		"duplicate_rule":  `UPDATE native_notification_policies SET version=version+1,rules='[{"category":"MESSAGE","route":"NORMAL"},{"category":"MESSAGE","route":"BLOCK"}]' WHERE owner_id=$1`,
		"unknown_key":     `UPDATE native_notification_policies SET version=version+1,rules='[{"category":"MESSAGE","route":"NORMAL","confirmed":true}]' WHERE owner_id=$1`,
		"null_rule":       `UPDATE native_notification_policies SET version=version+1,rules='[null]' WHERE owner_id=$1`,
		"past_expiry":     `UPDATE native_notification_policies SET version=version+1,expires_at=clock_timestamp()-interval '1 second' WHERE owner_id=$1`,
		"infinite_expiry": `UPDATE native_notification_policies SET version=version+1,expires_at='infinity' WHERE owner_id=$1`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := b.pool.Exec(b.ctx, sql, b.person.ID); err == nil {
				t.Fatal("invalid SQL notification shape accepted")
			}
		})
	}
	read, err := b.store.GetOwnNotificationPolicy(b.ctx, f.owner)
	if err != nil || !reflect.DeepEqual(read, created) {
		t.Fatal("invalid SQL changed policy", err)
	}
	if _, err = b.pool.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.ownerSession); err != nil {
		t.Fatal(err)
	}
	if got, err := b.store.GetOwnNotificationPolicy(b.ctx, f.owner); !errors.Is(err, agentnotification.ErrForbidden) || got.AgentID != "" {
		t.Fatal("revoked native session released policy", err)
	}
	_ = context.Background()
}
