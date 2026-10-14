package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func imageHardeningSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var p *pgconn.PgError
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("wanted PostgreSQL %s, got %v", code, err)
	}
}

// Migrations run only after ownedMigrationDatabase has installed the actual
// migration chain in a fresh child. A failed explicit transaction is rolled
// back on the same connection, never returned to the pool in aborted state.
func imageHardeningMigration(t *testing.T, f *nativeEntityShareFixture, name, wantCode string) {
	t.Helper()
	b := f.f.private.base
	raw, err := os.ReadFile("../../migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	c, err := b.pool.Acquire(b.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()
	_, err = c.Exec(b.ctx, string(raw))
	if err != nil {
		_, rollbackErr := c.Exec(context.Background(), "ROLLBACK")
		if rollbackErr != nil {
			t.Fatal("migration rollback", rollbackErr)
		}
	}
	if wantCode == "" {
		if err != nil {
			t.Fatal("actual migration", name, err)
		}
		return
	}
	imageHardeningSQLState(t, err, wantCode)
}

func imageHardeningSnapshot(t *testing.T, f *nativeEntityShareFixture) string {
	t.Helper()
	b := f.f.private.base
	var out string
	err := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object(
 'messages',(SELECT coalesce(jsonb_agg(to_jsonb(m)||jsonb_build_object('xmin',m.xmin::text) ORDER BY m.id),'[]'::jsonb) FROM conversation_messages m WHERE m.sender_account_id=$1),
 'images',(SELECT coalesce(jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY i.message_id),'[]'::jsonb) FROM human_conversation_images i WHERE i.owner_account_id=$1),
 'audio',(SELECT coalesce(jsonb_agg(to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text) ORDER BY a.message_id),'[]'::jsonb) FROM human_conversation_audio a WHERE a.owner_account_id=$1),
 'notifications',(SELECT coalesce(jsonb_agg(to_jsonb(n)||jsonb_build_object('xmin',n.xmin::text) ORDER BY n.id),'[]'::jsonb) FROM native_notification_decisions n WHERE n.actor_id=$1),
 'inbox',(SELECT coalesce(jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY i.id),'[]'::jsonb) FROM inbox_items i WHERE i.recipient_account_id=$2))::text`, f.a.actor.ID, f.b.actor.ID).Scan(&out)
	if err != nil {
		t.Fatal("immutable image/effect snapshot", err)
	}
	return out
}

func TestHumanImageNativeMigrationEmptyRoundtrip(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	op := agentMemoryID(t, f.f.private)
	audio, err := b.store.SendHumanAudioMessage(b.ctx, a, f.conversation, nativeHumanAudioInput(op))
	if err != nil {
		t.Fatal("preexisting audio", err)
	}
	before := imageHardeningSnapshot(t, f)
	imageHardeningMigration(t, f, "121_private_chat_images.down.sql", "")
	var removed, audioPreserved bool
	err = b.pool.QueryRow(b.ctx, `SELECT
 to_regclass('public.human_conversation_images') IS NULL
 AND to_regprocedure('public.birdtie_private_chat_image_shape()') IS NULL
 AND to_regprocedure('public.birdtie_chat_audio_no_image()') IS NULL
 AND NOT EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='human_conversation_audio_image_exclusion'),
 EXISTS(SELECT 1 FROM human_conversation_audio WHERE message_id=$1)
 AND EXISTS(SELECT 1 FROM pg_trigger WHERE tgname='human_conversation_audio_immutable')`, audio.MessageID).Scan(&removed, &audioPreserved)
	if err != nil || !removed || !audioPreserved {
		t.Fatal("121 down removed wrong objects or left its objects", removed, audioPreserved, err)
	}
	imageHardeningMigration(t, f, "121_private_chat_images.sql", "")
	if got := imageHardeningSnapshot(t, f); got != before {
		t.Fatal("empty image migration changed existing audio/message/notification rows or xmin")
	}
	in := nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "恢复迁移后图片")
	if _, err = b.store.SendHumanImageMessage(b.ctx, a, f.conversation, in); err != nil {
		t.Fatal("image send after actual down/up", err)
	}
}

func TestHumanImageNativeUsedDownImmutableAndParentCascade(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	in := nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "不可变图片")
	r, err := b.store.SendHumanImageMessage(b.ctx, a, f.conversation, in)
	if err != nil {
		t.Fatal(err)
	}
	before := imageHardeningSnapshot(t, f)
	imageHardeningMigration(t, f, "121_private_chat_images.down.sql", "55000")
	if imageHardeningSnapshot(t, f) != before {
		t.Fatal("refused down rewrote original effect")
	}
	for _, query := range []string{
		`UPDATE human_conversation_images SET pixel_risk='USER_MASKED' WHERE message_id=$1`,
		`UPDATE human_conversation_images SET input_sha256=repeat('0',64) WHERE message_id=$1`,
		`UPDATE human_conversation_images SET content=decode('00','hex') WHERE message_id=$1`,
		`UPDATE human_conversation_images SET recipient_account_id=owner_account_id WHERE message_id=$1`,
		`UPDATE human_conversation_images SET operation_id=gen_random_uuid() WHERE message_id=$1`,
		`UPDATE human_conversation_images SET created_at=created_at+interval '1 second' WHERE message_id=$1`,
		`UPDATE conversation_messages SET body='替换原批准正文' WHERE id=$1`,
		`DELETE FROM human_conversation_images WHERE message_id=$1`,
	} {
		_, err = b.pool.Exec(b.ctx, query, r.MessageID)
		imageHardeningSQLState(t, err, "55000")
		if imageHardeningSnapshot(t, f) != before {
			t.Fatal("rejected mutation changed bytes or xmin", query)
		}
	}
	// Parent lifecycle deletion is deliberately distinguished from removing an
	// attachment and accidentally exposing its caption as a plain text effect.
	if _, err = b.pool.Exec(b.ctx, `DELETE FROM conversation_messages WHERE id=$1`, r.MessageID); err != nil {
		t.Fatal("owned parent lifecycle cascade", err)
	}
	var messages, images int
	err = b.pool.QueryRow(b.ctx, `SELECT
 (SELECT count(*) FROM conversation_messages WHERE id=$1),
 (SELECT count(*) FROM human_conversation_images WHERE message_id=$1)`, r.MessageID).Scan(&messages, &images)
	if err != nil || messages != 0 || images != 0 {
		t.Fatal("parent cascade left orphan media", messages, images, err)
	}
	imageHardeningMigration(t, f, "121_private_chat_images.down.sql", "")
	imageHardeningMigration(t, f, "121_private_chat_images.sql", "")
}

func TestHumanImageNativeReverseKindAndChangedPixelsConflict(t *testing.T) {
	for _, kind := range []string{"text", "audio", "image"} {
		t.Run(kind, func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := newNativeEntityShareFixture(t)
			b := f.f.private.base
			a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
			op := agentMemoryID(t, f.f.private)
			in := nativeHumanImageInput(t, op, f.b.actor.ID, "语音消息")
			var err error
			switch kind {
			case "text":
				_, err = b.store.SendHumanMessageOperation(b.ctx, a, f.conversation, connection.MessageOperationInput{OperationID: op, Body: in.Caption})
			case "audio":
				_, err = b.store.SendHumanAudioMessage(b.ctx, a, f.conversation, nativeHumanAudioInput(op))
			case "image":
				_, err = b.store.SendHumanImageMessage(b.ctx, a, f.conversation, in)
			}
			if err != nil {
				t.Fatal("original effect", kind, err)
			}
			before := imageHardeningSnapshot(t, f)
			if kind == "image" {
				pixels := image.NewNRGBA(image.Rect(0, 0, 320, 180))
				pixels.SetNRGBA(3, 4, color.NRGBA{R: 230, G: 40, B: 20, A: 255})
				var raw bytes.Buffer
				if err = png.Encode(&raw, pixels); err != nil {
					t.Fatal(err)
				}
				in.Content = raw.Bytes()
				in.InputSize = int64(len(in.Content))
				h := sha256.Sum256(in.Content)
				changedSHA := hex.EncodeToString(h[:])
				if changedSHA == in.InputSHA256 {
					t.Fatal("changed pixel vector accidentally has original hash")
				}
				in.InputSHA256 = changedSHA
			}
			if connection.ValidateImageMessage(in) != nil {
				t.Fatal("conflict vector must be independently valid image input")
			}
			if _, err = b.store.SendHumanImageMessage(b.ctx, a, f.conversation, in); !errors.Is(err, connection.ErrConflict) {
				t.Fatal("same operation changed media kind or approved pixels", kind, err)
			}
			if imageHardeningSnapshot(t, f) != before {
				t.Fatal("conflict mutated original effect/media/notification rows")
			}
		})
	}
}

func TestHumanImageNativeCrossConversationOperationConflict(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	in := nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "同一批准对象")
	original, err := b.store.SendHumanImageMessage(b.ctx, a, f.conversation, in)
	if err != nil {
		t.Fatal(err)
	}
	request, err := b.store.CreateFriendRequest(b.ctx, f.a.actor.ID, b.person.ID, "独立合成好友")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = b.store.DecideRequest(b.ctx, b.person.ID, request.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	ties, err := b.store.ListTies(b.ctx, f.a.actor.ID)
	if err != nil {
		t.Fatal(err)
	}
	second := ""
	for _, tie := range ties {
		c, err := b.store.StartFriendConversation(b.ctx, f.a.actor.ID, tie.ID)
		if err != nil {
			t.Fatal(err)
		}
		if c.ID != f.conversation {
			second = c.ID
		}
	}
	if second == "" {
		t.Fatal("second real accepted conversation missing")
	}
	before := imageHardeningSnapshot(t, f)
	changed := in
	changed.RecipientAccountID = b.person.ID
	if _, err = b.store.SendHumanImageMessage(b.ctx, a, second, changed); !errors.Is(err, connection.ErrConflict) {
		t.Fatal("owner/op rebound to another authorized conversation", err)
	}
	if imageHardeningSnapshot(t, f) != before {
		t.Fatal("cross-conversation conflict left an effect")
	}
	read, err := b.store.ReadHumanImageMessageOperation(b.ctx, a, f.conversation, in.OperationID)
	if err != nil || read != original {
		t.Fatal("original image receipt no longer recoverable", err)
	}
}

func TestHumanImageNativeMixedQuotaConcurrentBoundaryAndReplay(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	// Only these 58 current sender rows count. Historical sender rows and
	// current peer rows are deliberately present to detect broader counting.
	b.exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body)
 SELECT $1,$2,'当前合成文字' FROM generate_series(1,58)`, f.conversation, f.a.actor.ID)
	b.exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body,created_at)
 SELECT $1,$2,'两小时前合成文字',now()-interval '2 hours' FROM generate_series(1,3)`, f.conversation, f.a.actor.ID)
	b.exec(`INSERT INTO conversation_messages(conversation_id,sender_account_id,body)
 SELECT $1,$2,'对方合成文字' FROM generate_series(1,4)`, f.conversation, f.b.actor.ID)
	if _, err := b.store.SendHumanAudioMessage(b.ctx, a, f.conversation, nativeHumanAudioInput(agentMemoryID(t, f.f.private))); err != nil {
		t.Fatal("59th message must accept audio", err)
	}
	inputs := []connection.ImageMessageInput{
		nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "第一个并发图片"),
		nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "第二个并发图片"),
	}
	results := make([]connection.ImageMessageReceipt, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i], errs[i] = New(b.pool, false).SendHumanImageMessage(b.ctx, a, f.conversation, inputs[i])
		}(i)
	}
	close(start)
	wg.Wait()
	winner, successes, limits := -1, 0, 0
	for i, err := range errs {
		if err == nil {
			winner, successes = i, successes+1
		} else if errors.Is(err, connection.ErrRateLimit) {
			limits++
		} else {
			t.Fatal("unexpected concurrent boundary result", err)
		}
	}
	if successes != 1 || limits != 1 {
		t.Fatal("59 existing messages require exactly one new effect", successes, limits)
	}
	var recent, images, audio, notices int
	err := b.pool.QueryRow(b.ctx, `SELECT
 (SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND created_at>now()-interval '1 hour'),
 (SELECT count(*) FROM human_conversation_images WHERE owner_account_id=$1),
 (SELECT count(*) FROM human_conversation_audio WHERE owner_account_id=$1),
 (SELECT count(*) FROM native_notification_decisions n JOIN conversation_messages m ON m.id=n.source_id WHERE m.sender_account_id=$1)`, f.a.actor.ID).Scan(&recent, &images, &audio, &notices)
	if err != nil || recent != 60 || images != 1 || audio != 1 || notices != 2 {
		t.Fatal("mixed quota or atomic notification boundary", recent, images, audio, notices, err)
	}
	before := imageHardeningSnapshot(t, f)
	replayed, err := b.store.SendHumanImageMessage(b.ctx, a, f.conversation, inputs[winner])
	if err != nil || replayed != results[winner] {
		t.Fatal("same approved image must recover at quota", err)
	}
	if _, err = b.store.SendHumanMessageOperation(b.ctx, a, f.conversation, connection.MessageOperationInput{OperationID: agentMemoryID(t, f.f.private), Body: "第61条文字"}); !errors.Is(err, connection.ErrRateLimit) {
		t.Fatal("text escaped shared quota", err)
	}
	if _, err = b.store.SendHumanAudioMessage(b.ctx, a, f.conversation, nativeHumanAudioInput(agentMemoryID(t, f.f.private))); !errors.Is(err, connection.ErrRateLimit) {
		t.Fatal("audio escaped shared quota", err)
	}
	if _, err = b.store.SendHumanImageMessage(b.ctx, a, f.conversation, nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "第61条图片")); !errors.Is(err, connection.ErrRateLimit) {
		t.Fatal("image escaped shared quota", err)
	}
	if imageHardeningSnapshot(t, f) != before {
		t.Fatal("quota replay/rejection changed immutable effects or notifications")
	}
}

func TestHumanImageNativeRawSQLMediaMutualExclusion(t *testing.T) {
	for _, firstKind := range []string{"audio", "image"} {
		t.Run(firstKind+"-holds-parent", func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := newNativeEntityShareFixture(t)
			b := f.f.private.base
			op := agentMemoryID(t, f.f.private)
			var message string
			var created time.Time
			if err := b.pool.QueryRow(b.ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body,client_operation_id)
 VALUES($1,$2,'语音消息',$3) RETURNING id,created_at`, f.conversation, f.a.actor.ID, op).Scan(&message, &created); err != nil {
				t.Fatal(err)
			}
			in := nativeHumanImageInput(t, op, f.b.actor.ID, "语音消息")
			derived, err := (&media.LocalChatImageDerivativeBuilder{}).Build(b.ctx, in.Content, in.MIME)
			if err != nil {
				t.Fatal(err)
			}
			r := connection.NewImageMessageReceipt(f.a.actor.ID, f.conversation, message, in, derived.Metadata, created)
			audio := nativeHumanAudioInput(op)
			insert := func(ctx context.Context, tx pgx.Tx, kind string) error {
				if kind == "audio" {
					_, err := tx.Exec(ctx, `INSERT INTO human_conversation_audio(message_id,owner_account_id,conversation_id,operation_id,mime_type,byte_size,sha256,duration_millis,content,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, message, f.a.actor.ID, f.conversation, op, audio.MIME, audio.ByteSize, audio.SHA256, audio.DurationMillis, audio.Content, created)
					return err
				}
				m, thumb := derived.Metadata.ImageDerivativeMetadata, derived.Metadata.Thumbnail
				_, err := tx.Exec(ctx, `INSERT INTO human_conversation_images(message_id,owner_account_id,recipient_account_id,conversation_id,operation_id,purpose,pixel_risk,input_mime_type,input_size,input_sha256,caption_present,caption_sha256,mime_type,byte_size,sha256,width,height,content,thumbnail_byte_size,thumbnail_sha256,thumbnail_width,thumbnail_height,thumbnail_content,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,true,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`, message, f.a.actor.ID, f.b.actor.ID, f.conversation, op, in.Purpose, in.PixelRisk, in.MIME, in.InputSize, in.InputSHA256, r.CaptionSHA256, m.MIME, m.ByteSize, m.SHA256, m.Width, m.Height, derived.Full, thumb.ByteSize, thumb.SHA256, thumb.Width, thumb.Height, derived.Thumbnail, created)
				return err
			}
			ctx, cancel := context.WithTimeout(b.ctx, 15*time.Second)
			defer cancel()
			first, err := b.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer first.Rollback(context.Background())
			second, err := b.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer second.Rollback(context.Background())
			if err = insert(ctx, first, firstKind); err != nil {
				t.Fatal("first valid media insertion", err)
			}
			otherKind := "audio"
			if firstKind == "audio" {
				otherKind = "image"
			}
			firstPID := first.Conn().PgConn().PID()
			secondPID := second.Conn().PgConn().PID()
			done := make(chan struct{})
			var secondErr error
			go func() {
				defer close(done)
				secondErr = insert(ctx, second, otherKind)
			}()
			defer func() {
				cancel()
				<-done
			}()
			// Observe the actual second backend blocked by the first. No timed
			// guess that two goroutines happened to overlap is accepted.
			poll := time.NewTicker(10 * time.Millisecond)
			defer poll.Stop()
			blocked := false
			for !blocked {
				select {
				case <-done:
					t.Fatal("opposite media did not wait for original parent lock", secondErr)
				case <-ctx.Done():
					t.Fatal("no observed parent lock contention", ctx.Err())
				case <-poll.C:
					err = b.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND wait_event_type='Lock' AND $2=ANY(pg_blocking_pids(pid)))`, int64(secondPID), int64(firstPID)).Scan(&blocked)
					if err != nil {
						t.Fatal("observe PostgreSQL lock contention", err)
					}
				}
			}
			if err = first.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("opposite media did not finish after parent commit", ctx.Err())
			}
			imageHardeningSQLState(t, secondErr, "23514")
			if err = second.Rollback(context.Background()); err != nil {
				t.Fatal("rollback rejected opposing media", err)
			}
			var audioRows, imageRows int
			err = b.pool.QueryRow(b.ctx, `SELECT
 (SELECT count(*) FROM human_conversation_audio WHERE message_id=$1),
 (SELECT count(*) FROM human_conversation_images WHERE message_id=$1)`, message).Scan(&audioRows, &imageRows)
			if err != nil || audioRows+imageRows != 1 || (firstKind == "audio" && audioRows != 1) || (firstKind == "image" && imageRows != 1) {
				t.Fatal("one parent acquired mixed media", audioRows, imageRows, err)
			}
		})
	}
}
