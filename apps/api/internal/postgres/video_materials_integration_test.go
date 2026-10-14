package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// This test owns its complete disposable database. Never run against the API
// database; the repository fixture enforces the migration ownership guard.
func TestVideoMaterialNativePrivateLifecycleAndFencing(t *testing.T) {
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" {
		t.Skip("NOT_RUN: BIRDTIE_DATABASE_URL not configured")
	}
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	s := New(pool, false)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	uuid := func() string {
		t.Helper()
		var id string
		if e := pool.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	account := func() (identity.Actor, [32]byte, string) {
		t.Helper()
		id := uuid()
		exec(`INSERT INTO accounts(id,account_type) VALUES($1,'person')`, id)
		_, d, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		ss := uuid()
		exec(`INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, ss, id, d[:])
		return identity.Actor{ID: id, AccountType: "person"}, d, ss
	}
	a, d, session := account()
	peer, pd, _ := account()
	input := func() media.VideoMaterialInput {
		return media.VideoMaterialInput{OperationID: uuid(), MIME: "video/mp4", ByteSize: 100, SHA256: strings.Repeat("a", 64), DurationMS: 1000, Purpose: media.VideoMaterialPurpose, ConfirmedPrivateSave: true, StoragePolicyID: strings.Repeat("b", 64)}
	}
	reserve := func(in media.VideoMaterialInput) media.VideoMaterialRecord {
		t.Helper()
		v, e := s.ReserveHumanVideo(ctx, d, a, in)
		if e != nil {
			t.Fatal("reserve", e)
		}
		return v
	}
	queue := func(v media.VideoMaterialRecord) media.VideoMaterialRecord {
		t.Helper()
		q, e := s.QueueHumanVideo(ctx, d, a, v.Receipt.ID, v.Receipt.Revision)
		if e != nil || q.Status != "queued" {
			t.Fatal("queue", q, e)
		}
		job, e := s.ClaimVideoJob(ctx)
		if e != nil || job.Receipt.ID != v.Receipt.ID || job.Receipt.Status != "scanning" {
			t.Fatal("claim", job, e)
		}
		return job
	}
	cleanup := func(id string) {
		t.Helper()
		exec(`UPDATE private_video_materials SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1 AND lease_id IS NOT NULL`, id)
		v, e := s.ClaimVideoJob(ctx)
		if e != nil || v.Receipt.ID != id || !v.CleanupPending {
			t.Fatal("cleanup claim", v, e)
		}
		if e = s.FinishVideoCleanup(ctx, v); e != nil {
			t.Fatal("cleanup", e)
		}
		expectedState := v.Receipt.Status
		if expectedState == "deleting" {
			expectedState = "deleted"
		}
		var assetState string
		if e = s.pool.QueryRow(ctx, `SELECT state FROM media_assets WHERE id=$1`, id).Scan(&assetState); e != nil || assetState != expectedState {
			t.Fatal("private video and common media ledger cleanup differ", assetState, expectedState, e)
		}
	}
	encode := func(v media.VideoMaterialRecord) ([]byte, error) { return []byte(v.Receipt.Status), nil }

	in := input()
	v := reserve(in)
	again := reserve(in)
	if again.Receipt.ID != v.Receipt.ID || again.Receipt.Revision != 1 || !again.Receipt.UploadExpiresAt.Equal(v.Receipt.UploadExpiresAt) {
		t.Fatal("idempotent retry changed upload lease", again)
	}
	changed := in
	changed.ByteSize++
	if _, e = s.ReserveHumanVideo(ctx, d, a, changed); !errors.Is(e, content.ErrConflict) {
		t.Fatal("same operation different draft", e)
	}
	if _, e = s.ReadHumanVideo(ctx, pd, peer, v.Receipt.ID, false, encode); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("peer read", e)
	}
	if _, e = s.ReadHumanVideo(ctx, pd, peer, in.OperationID, true, encode); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("peer recovered operation", e)
	}
	if _, e = s.QueueHumanVideo(ctx, pd, peer, v.Receipt.ID, 1); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("peer queue", e)
	}
	if _, e = s.DeleteHumanVideo(ctx, pd, peer, v.Receipt.ID, 1); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("peer delete", e)
	}
	if _, e = s.QueueHumanVideo(ctx, d, a, v.Receipt.ID, 99); !errors.Is(e, content.ErrConflict) {
		t.Fatal("stale queue revision", e)
	}
	job := queue(v)
	if s.VideoJobCurrent(ctx, job) != nil {
		t.Fatal("current bound approval not recognized")
	}
	ready, e := s.FinishVideoJob(ctx, job, media.VideoDescriptor{Width: 640, Height: 360, DurationMS: 1000})
	if e != nil || ready.Status != "ready_private" {
		t.Fatal("ready", ready, e)
	}
	cleanup(v.Receipt.ID)
	raw, e := s.ReadHumanVideo(ctx, d, a, v.Receipt.ID, false, encode)
	if e != nil || string(raw) != "ready_private" {
		t.Fatal("owner read", string(raw), e)
	}
	page, e := s.ListHumanVideos(ctx, d, a, "")
	if e != nil || len(page.Items) != 1 || page.Items[0].ID != v.Receipt.ID {
		t.Fatal("list", page, e)
	}
	current := page.Items[0]
	deleted, e := s.DeleteHumanVideo(ctx, d, a, v.Receipt.ID, current.Revision)
	if e != nil || deleted.Status != "deleting" {
		t.Fatal("delete must be pending physical cleanup", deleted, e)
	}
	if _, e = s.FinishVideoJob(ctx, job, media.VideoDescriptor{Width: 640, Height: 360, DurationMS: 1000}); e == nil {
		t.Fatal("late worker resurrected deletion")
	}
	cleanup(v.Receipt.ID)
	raw, e = s.ReadHumanVideo(ctx, d, a, in.OperationID, true, encode)
	if e != nil || string(raw) != "deleted" {
		t.Fatal("tombstone operation recovery", e, string(raw))
	}

	// Authorization generation is part of the exact original approval.
	v = reserve(input())
	job = queue(v)
	exec(`UPDATE sessions SET authentication_method='UNIT-video-security-change' WHERE id=$1`, session)
	var changedGeneration int64
	if e = pool.QueryRow(ctx, `SELECT authorization_generation FROM sessions WHERE id=$1`, session).Scan(&changedGeneration); e != nil || changedGeneration <= job.ApprovalGeneration {
		t.Fatal("native security change did not retire original approval generation", changedGeneration, job.ApprovalGeneration, e)
	}
	if e = s.VideoJobCurrent(ctx, job); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("generation change reused approval", e)
	}
	if _, e = s.FinishVideoJob(ctx, job, media.VideoDescriptor{Width: 640, Height: 360, DurationMS: 1000}); !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("retired worker ready", e)
	}
	if e = s.RejectVideoJob(ctx, job, "source_retired"); e != nil {
		t.Fatal(e)
	}
	cleanup(v.Receipt.ID)

	// Read expiration is checked after provider/encoder I/O, not only at start.
	v = reserve(input())
	exec(`UPDATE private_video_materials SET retain_until=clock_timestamp()+interval '80 milliseconds',upload_expires_at=clock_timestamp()+interval '40 milliseconds' WHERE id=$1`, v.Receipt.ID)
	raw, e = s.ReadHumanVideo(ctx, d, a, v.Receipt.ID, false, func(media.VideoMaterialRecord) ([]byte, error) {
		time.Sleep(110 * time.Millisecond)
		return []byte("expired-private"), nil
	})
	if !errors.Is(e, content.ErrNotFound) || len(raw) != 0 {
		t.Fatal("expired during read leaked bytes", string(raw), e)
	}
	cleanup(v.Receipt.ID)

	// A delete fences an in-flight decoder and waits for its bounded provider I/O.
	v = reserve(input())
	job = queue(v)
	deleting, e := s.DeleteHumanVideo(ctx, d, a, v.Receipt.ID, job.Receipt.Revision)
	if e != nil || deleting.Status != "deleting" {
		t.Fatal(e)
	}
	if _, e = s.ClaimVideoJob(ctx); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("cleanup ran before in-flight write grace", e)
	}
	if _, e = s.FinishVideoJob(ctx, job, media.VideoDescriptor{Width: 640, Height: 360, DurationMS: 1000}); !errors.Is(e, content.ErrConflict) {
		t.Fatal("deleted lease resurrected ready", e)
	}
	cleanup(v.Receipt.ID)

	// Concurrent ready/delete serializes around native row locks. Whatever wins,
	// the final current owner deletion must fence every captured worker receipt.
	v = reserve(input())
	job = queue(v)
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, _ = s.FinishVideoJob(ctx, job, media.VideoDescriptor{Width: 640, Height: 360, DurationMS: 1000})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, _ = s.DeleteHumanVideo(ctx, d, a, v.Receipt.ID, job.Receipt.Revision)
	}()
	close(start)
	wg.Wait()
	var revision int64
	if e = pool.QueryRow(ctx, `SELECT revision FROM private_video_materials WHERE id=$1`, v.Receipt.ID).Scan(&revision); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DeleteHumanVideo(ctx, d, a, v.Receipt.ID, revision); e != nil {
		t.Fatal(e)
	}
	if _, e = s.FinishVideoJob(ctx, job, media.VideoDescriptor{Width: 640, Height: 360, DurationMS: 1000}); e == nil {
		t.Fatal("concurrent captured worker still valid")
	}
	cleanup(v.Receipt.ID)
}
