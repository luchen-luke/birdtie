package postgres

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// Synthetic container for real database/ACL tests, not recorded speech.
func nativeHumanAudioInput(op string) connection.AudioMessageInput {
	atom := func(kind string, values ...[]byte) []byte {
		n := 8
		for _, b := range values {
			n += len(b)
		}
		out := make([]byte, 8, n)
		binary.BigEndian.PutUint32(out, uint32(n))
		copy(out[4:], kind)
		for _, b := range values {
			out = append(out, b...)
		}
		return out
	}
	ftyp := atom("ftyp", []byte("isom\x00\x00\x00\x00isom"))
	mdat := atom("mdat", []byte{1, 2, 3, 4})
	mdhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhd[12:], 1000)
	binary.BigEndian.PutUint32(mdhd[16:], 1234)
	hdlr := make([]byte, 12)
	copy(hdlr[8:], "soun")
	entry := make([]byte, 28)
	binary.BigEndian.PutUint16(entry[6:], 1)
	binary.BigEndian.PutUint16(entry[16:], 1)
	binary.BigEndian.PutUint16(entry[18:], 16)
	binary.BigEndian.PutUint32(entry[24:], 44100<<16)
	entry = append(entry, atom("esds", []byte{0, 0, 0, 0, 4, 1, 0x40, 0})...)
	stsd := append([]byte{0, 0, 0, 0, 0, 0, 0, 1}, atom("mp4a", entry)...)
	stsz := []byte{0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 1}
	stco := make([]byte, 12)
	binary.BigEndian.PutUint32(stco[4:], 1)
	binary.BigEndian.PutUint32(stco[8:], uint32(len(ftyp)+8))
	dref := atom("dref", []byte{0, 0, 0, 0, 0, 0, 0, 1}, atom("url ", []byte{0, 0, 0, 1}))
	stbl := atom("stbl", atom("stsd", stsd), atom("stsz", stsz), atom("stco", stco))
	minf := atom("minf", atom("dinf", dref), stbl)
	mdia := atom("mdia", atom("mdhd", mdhd), atom("hdlr", hdlr), minf)
	b := append(append(ftyp, mdat...), atom("moov", atom("trak", mdia))...)
	h := sha256.Sum256(b)
	return connection.AudioMessageInput{OperationID: op, AudioMetadata: connection.AudioMetadata{MIME: "audio/mp4", ByteSize: int64(len(b)), SHA256: hex.EncodeToString(h[:]), DurationMillis: 1234}, Content: b}
}
func TestHumanAudioNativeExplicitSendRecoveryAndPermissions(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	peer := ea.Access{Actor: f.b.actor, SessionDigest: f.b.digest}
	in := nativeHumanAudioInput(agentMemoryID(t, f.f.private))
	if connection.ValidateAudioMessage(in) != nil {
		t.Fatal("synthetic container does not satisfy actual validation")
	}
	first, e := b.store.SendHumanAudioMessage(b.ctx, a, f.conversation, in)
	if e != nil {
		t.Fatal("actual explicit audio send", e)
	}
	for i := 0; i < 2; i++ {
		r, e := New(b.pool, false).SendHumanAudioMessage(b.ctx, a, f.conversation, in)
		if e != nil || r != first {
			t.Fatal("audio retry must preserve original effect", e)
		}
	}
	r, e := b.store.ReadHumanAudioMessageOperation(b.ctx, a, f.conversation, in.OperationID)
	if e != nil || r != first {
		t.Fatal("actual recovery", e)
	}
	var n, notices int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2`, f.a.actor.ID, in.OperationID).Scan(&n); e != nil || n != 1 {
		t.Fatal("duplicate effect", n, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE source_id=$1`, first.MessageID).Scan(&notices); e != nil || notices != 1 {
		t.Fatal("duplicate notification", notices, e)
	}
	encode := func(r connection.AudioMessageReceipt, raw []byte) ([]byte, error) {
		if r != first {
			t.Fatal("foreign receipt exposed")
		}
		return append([]byte(nil), raw...), nil
	}
	content, e := b.store.ReadHumanAudioMessageContent(b.ctx, peer, f.conversation, first.MessageID, encode)
	if e != nil || !bytes.Equal(content, in.Content) {
		t.Fatal("recipient cannot read actual audio", e)
	}
	messages, e := b.store.ListMessages(b.ctx, f.b.actor.ID, f.conversation)
	if e != nil || len(messages) != 1 || messages[0].Audio == nil || *messages[0].Audio != in.AudioMetadata {
		t.Fatal("history must retain audio playback metadata", e)
	}
	t.Run("mixed-text-audio-operation-replay-rejected", func(t *testing.T) {
		if _, e := b.store.SendHumanMessageOperation(b.ctx, a, f.conversation, connection.MessageOperationInput{OperationID: in.OperationID, Body: "语音消息"}); !errors.Is(e, connection.ErrConflict) {
			t.Fatal("audio effect converted to text", e)
		}
	})
	t.Run("foreign-member-does-not-read-or-send", func(t *testing.T) {
		foreign := ea.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, SessionDigest: f.f.private.owner.SessionDigest}
		if _, e := b.store.ReadHumanAudioMessageContent(b.ctx, foreign, f.conversation, first.MessageID, encode); !errors.Is(e, connection.ErrNotFound) {
			t.Fatal("foreign member read audio", e)
		}
		if _, e := b.store.SendHumanAudioMessage(b.ctx, foreign, f.conversation, nativeHumanAudioInput(agentMemoryID(t, f.f.private))); !errors.Is(e, connection.ErrNotFound) {
			t.Fatal("foreign member sent audio", e)
		}
	})
	t.Run("block-both-read-and-new-send", func(t *testing.T) {
		b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.b.actor.ID, f.a.actor.ID)
		defer b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.b.actor.ID, f.a.actor.ID)
		if _, e := b.store.ReadHumanAudioMessageContent(b.ctx, a, f.conversation, first.MessageID, encode); !errors.Is(e, connection.ErrNotFound) {
			t.Fatal("blocked audio leaked", e)
		}
		if _, e := b.store.SendHumanAudioMessage(b.ctx, a, f.conversation, nativeHumanAudioInput(agentMemoryID(t, f.f.private))); !errors.Is(e, connection.ErrNotFound) {
			t.Fatal("blocked audio sent", e)
		}
	})
	t.Run("expired-session-after-encoding-does-not-release-bytes", func(t *testing.T) {
		// The read holds FOR SHARE on this Session until its authorization
		// boundary, so a synchronous external UPDATE here cannot commit. Real
		// clock expiry can occur while that fence is held and must still fail.
		if _, e := b.pool.Exec(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '500 milliseconds' WHERE token_sha256=$1`, f.a.digest[:]); e != nil {
			t.Fatal("set isolated session deadline", e)
		}
		calls := 0
		raw, e := b.store.ReadHumanAudioMessageContent(b.ctx, a, f.conversation, first.MessageID, func(r connection.AudioMessageReceipt, raw []byte) ([]byte, error) {
			calls++
			time.Sleep(650 * time.Millisecond)
			return append([]byte(nil), raw...), nil
		})
		if calls != 1 || !errors.Is(e, identity.ErrUnauthorized) || len(raw) != 0 {
			t.Fatal("late expired content released", calls, e)
		}
	})
	t.Run("revoked-session-receipt-and-bytes-denied", func(t *testing.T) {
		if _, e := b.pool.Exec(b.ctx, `UPDATE sessions SET idle_expires_at=expires_at,revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.digest[:]); e != nil {
			t.Fatal("revoke isolated session", e)
		}
		calls := 0
		raw, e := b.store.ReadHumanAudioMessageContent(b.ctx, a, f.conversation, first.MessageID, func(r connection.AudioMessageReceipt, raw []byte) ([]byte, error) {
			calls++
			return append([]byte(nil), raw...), nil
		})
		if calls != 0 || !errors.Is(e, identity.ErrUnauthorized) || len(raw) != 0 {
			t.Fatal("revoked content released", calls, e)
		}
		if _, e := b.store.ReadHumanAudioMessageOperation(b.ctx, a, f.conversation, in.OperationID); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("revoked receipt leaked", e)
		}
	})
}
