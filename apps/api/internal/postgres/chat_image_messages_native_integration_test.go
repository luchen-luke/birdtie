package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

func nativeHumanImageInput(t *testing.T, op, recipient, caption string) connection.ImageMessageInput {
	t.Helper()
	var b bytes.Buffer
	if e := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 320, 180))); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(b.Bytes())
	return connection.ImageMessageInput{OperationID: op, RecipientAccountID: recipient, Purpose: connection.ChatImagePurpose, Caption: caption, PixelRisk: "UNKNOWN", MIME: "image/png", InputSize: int64(b.Len()), InputSHA256: hex.EncodeToString(h[:]), Content: b.Bytes()}
}
func TestHumanImageNativeAuthorityIsRecheckedAfterCodec(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	in := nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "")
	calls := 0
	_, e := b.store.sendHumanImageMessage(b.ctx, a, f.conversation, in, func(ctx context.Context, raw []byte, mime string) (media.ChatImageDerivatives, error) {
		calls++
		d, e := (&media.LocalChatImageDerivativeBuilder{}).Build(ctx, raw, mime)
		if e != nil {
			return d, e
		}
		// This synchronous change can commit because the precheck transaction
		// released all route/account locks before calling the codec.
		b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.b.actor.ID, f.a.actor.ID)
		return d, nil
	})
	if calls != 1 || !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("stale predecode authority reused", calls, e)
	}
	var messages, images int
	if e = b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2),(SELECT count(*) FROM human_conversation_images WHERE owner_account_id=$1 AND operation_id=$2)`, f.a.actor.ID, in.OperationID).Scan(&messages, &images); e != nil || messages != 0 || images != 0 {
		t.Fatal("late denied send left an effect", messages, images, e)
	}
}
func TestHumanImageNativeAtomicReplayProjectionAndPermissions(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	peer := ea.Access{Actor: f.b.actor, SessionDigest: f.b.digest}
	in := nativeHumanImageInput(t, agentMemoryID(t, f.f.private), f.b.actor.ID, "语音消息")
	var wg sync.WaitGroup
	results := make(chan connection.ImageMessageReceipt, 2)
	failures := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := New(b.pool, false).SendHumanImageMessage(b.ctx, a, f.conversation, in)
			results <- r
			failures <- e
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal("concurrent image send", e)
		}
	}
	var first connection.ImageMessageReceipt
	for r := range results {
		if first.MessageID == "" {
			first = r
		} else if r != first {
			t.Fatal("concurrent retry made a second immutable effect")
		}
	}
	if e := b.store.ValidateHumanImageMessageResponse(b.ctx, a, first); e != nil {
		t.Fatal("original response", e)
	}
	var messages, images, notices int
	if e := b.pool.QueryRow(b.ctx, `SELECT (SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2),(SELECT count(*) FROM human_conversation_images WHERE owner_account_id=$1 AND operation_id=$2),(SELECT count(*) FROM native_notification_decisions WHERE source_id=$3)`, f.a.actor.ID, in.OperationID, first.MessageID).Scan(&messages, &images, &notices); e != nil || messages != 1 || images != 1 || notices != 1 {
		t.Fatal("atomic effect/derivative/notification", messages, images, notices, e)
	}
	if _, e := b.store.SendHumanMessageOperation(b.ctx, a, f.conversation, connection.MessageOperationInput{OperationID: in.OperationID, Body: in.Caption}); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("image alias into text", e)
	}
	if _, e := b.store.SendHumanAudioMessage(b.ctx, a, f.conversation, nativeHumanAudioInput(in.OperationID)); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("image alias into audio", e)
	}
	changed := in
	changed.Caption = "另一文字"
	if _, e := b.store.SendHumanImageMessage(b.ctx, a, f.conversation, changed); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("changed caption replay", e)
	}
	changed = in
	changed.RecipientAccountID = f.a.actor.ID
	if _, e := b.store.SendHumanImageMessage(b.ctx, a, f.conversation, changed); !errors.Is(e, connection.ErrConflict) {
		t.Fatal("wrong approved recipient", e)
	}
	if _, e := b.store.ReadHumanImageMessageOperation(b.ctx, peer, f.conversation, in.OperationID); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("recipient read sender-only operation", e)
	}
	encode := func(r connection.ImageMessageReceipt, m connection.ImageDerivativeMetadata, raw []byte) ([]byte, error) {
		if r != first || !connection.ValidImageContent(m, raw, m == first.Image.Thumbnail) {
			t.Fatal("wrong authorized pixels")
		}
		return append([]byte(nil), raw...), nil
	}
	for _, actor := range []ea.Access{a, peer} {
		for _, variant := range []string{"thumbnail", "full"} {
			raw, e := b.store.ReadHumanImageMessageContent(b.ctx, actor, f.conversation, first.MessageID, variant, encode)
			if e != nil || len(raw) == 0 {
				t.Fatal("participant image read", variant, e)
			}
		}
	}
	legacy, e := b.store.ListMessages(b.ctx, f.b.actor.ID, f.conversation)
	if e != nil || len(legacy) != 1 || legacy[0].Image == nil || *legacy[0].Image != first.Image || legacy[0].Audio != nil {
		t.Fatal("legacy image projection", e)
	}
	page, e := b.store.ListMessagesPage(b.ctx, peer, f.conversation, connection.PageRequest{Limit: 10})
	if e != nil || len(page.Items) != 1 || page.Items[0].Image == nil {
		t.Fatal("image page projection", e)
	}
	if e = b.store.ValidateChatPageResponse(b.ctx, peer, []string{f.conversation}, page); e != nil {
		t.Fatal("final page image projection", e)
	}
	foreign := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.f.private.owner.SessionDigest}
	if _, e = b.store.ReadHumanImageMessageContent(b.ctx, foreign, f.conversation, first.MessageID, "full", encode); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("foreign pixels", e)
	}
	if _, e = b.pool.Exec(b.ctx, `DELETE FROM human_conversation_images WHERE message_id=$1`, first.MessageID); e == nil {
		t.Fatal("derivative orphan allowed")
	}
	b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.b.actor.ID, f.a.actor.ID)
	for _, actor := range []ea.Access{a, peer} {
		if _, e = b.store.ReadHumanImageMessageContent(b.ctx, actor, f.conversation, first.MessageID, "thumbnail", encode); !errors.Is(e, connection.ErrNotFound) {
			t.Fatal("blocked image leaked", e)
		}
	}
	b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.b.actor.ID, f.a.actor.ID)
	if _, e = b.pool.Exec(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '500 milliseconds' WHERE token_sha256=$1`, f.a.digest[:]); e != nil {
		t.Fatal(e)
	}
	calls := 0
	raw, e := b.store.ReadHumanImageMessageContent(b.ctx, a, f.conversation, first.MessageID, "full", func(_ connection.ImageMessageReceipt, _ connection.ImageDerivativeMetadata, raw []byte) ([]byte, error) {
		calls++
		time.Sleep(650 * time.Millisecond)
		return append([]byte(nil), raw...), nil
	})
	if calls != 1 || len(raw) != 0 || !errors.Is(e, identity.ErrUnauthorized) {
		t.Fatal("late expiry released pixels", calls, e)
	}
}
