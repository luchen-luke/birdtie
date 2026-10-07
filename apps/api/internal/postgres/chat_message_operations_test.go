package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type humanOperationRow struct{}

func (humanOperationRow) Scan(p ...any) error {
	*p[0].(*string) = "66666666-6666-4666-8666-666666666666"
	*p[1].(*string) = "33333333-3333-4333-8333-333333333333"
	*p[2].(*string) = "11111111-1111-4111-8111-111111111111"
	*p[3].(*string) = "human"
	*p[4].(*string) = "本地消息"
	*p[5].(*time.Time) = time.Date(2026, 10, 7, 1, 2, 3, 0, time.UTC)
	return nil
}

type humanOperationTx struct {
	pgx.Tx
	sql  string
	args []any
}

func (p *humanOperationTx) QueryRow(_ context.Context, s string, a ...any) pgx.Row {
	p.sql = s
	p.args = a
	return humanOperationRow{}
}
func TestHumanMessageOperationOriginalRowSQLSpy(t *testing.T) {
	p := &humanOperationTx{}
	m, e := readHumanMessageOperation(context.Background(), p, "owner", "op")
	if e != nil || m.Body != "本地消息" || len(p.args) != 2 || p.args[0] != "owner" || p.args[1] != "op" {
		t.Fatalf("row binding %v %v", m, e)
	}
	for _, s := range []string{"conversation_messages", "sender_account_id=$1", "client_operation_id=$2", "entity_type IS NULL", "entity_id IS NULL"} {
		if !strings.Contains(p.sql, s) {
			t.Fatal("actual SQL loses " + s)
		}
	}
}
func TestHumanMessageOperationSameTransactionWiring(t *testing.T) {
	b, e := os.ReadFile("connections.go")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	start := strings.Index(s, "func (s *Store) sendMessageWithOperation(")
	end := strings.Index(s[start:], "func ")
	if end == 0 {
		end = strings.Index(s[start+5:], "\nfunc ") + 5
	}
	if end < 0 {
		end = len(s) - start
	}
	s = s[start : start+end]
	for _, v := range []string{"messageConversationFence(ctx, tx, actorID, id)", "conversationMember(ctx, tx, actorID, id)", "readHumanMessageOperation(ctx, tx, actorID, operation)", "existing.ConversationID != id || existing.Body != body || existing.SenderID != actorID", "client_operation_id)", "routeNativeNotification(ctx, tx", "finishMessageRoute(ctx, tx, routeFence)", "tx.Commit(ctx)"} {
		if !strings.Contains(s, v) {
			t.Fatal("original consumer seam loses " + v)
		}
	}
	if strings.Index(s, "return existing, nil") > strings.Index(s, "var recent int") {
		t.Fatal("replay counted quota or wrote notification")
	}
	if strings.Count(s, "BeginTx(") != 1 {
		t.Fatal("receipt must not use second write Tx")
	}
}
func TestHumanMessageOperationMigrationKeepsImmutableEffectAndUsedDown(t *testing.T) {
	up, e := os.ReadFile("../../migrations/105_chat_message_operation_receipts.sql")
	if e != nil {
		t.Fatal(e)
	}
	down, e := os.ReadFile("../../migrations/105_chat_message_operation_receipts.down.sql")
	if e != nil {
		t.Fatal(e)
	}
	old, e := os.ReadFile("../../migrations/074_chat_entity_share_operations.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{"sender_account_id,client_operation_id", "entity_type IS NULL AND entity_id IS NULL", "speaker_kind='human'"} {
		if !strings.Contains(string(up), s) {
			t.Fatal("105 loses " + s)
		}
	}
	if !strings.Contains(string(old), "OLD.client_operation_id IS NOT NULL") || !strings.Contains(string(down), "RAISE EXCEPTION") || strings.Index(string(down), "RAISE EXCEPTION") > strings.Index(string(down), "DROP INDEX") {
		t.Fatal("immutable original receipt/used down guard lost")
	}
}

func humanOperationNativeSnapshot(t *testing.T, f *nativeEntityShareFixture) string {
	t.Helper()
	b := f.f.private.base
	var out string
	e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('messages',(SELECT coalesce(jsonb_agg(to_jsonb(m)||jsonb_build_object('xmin',m.xmin::text) ORDER BY m.id),'[]'::jsonb) FROM conversation_messages m WHERE sender_account_id=$1),'notifications',(SELECT coalesce(jsonb_agg(to_jsonb(n)||jsonb_build_object('xmin',n.xmin::text) ORDER BY n.id),'[]'::jsonb) FROM native_notification_decisions n WHERE actor_id=$1),'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a)||jsonb_build_object('xmin',a.xmin::text) ORDER BY a.id),'[]'::jsonb) FROM audit_events a WHERE actor_account_id=$1),'inbox',(SELECT coalesce(jsonb_agg(to_jsonb(i)||jsonb_build_object('xmin',i.xmin::text) ORDER BY i.id),'[]'::jsonb) FROM inbox_items i WHERE recipient_account_id=$2),'catalog',jsonb_build_object('columns',(SELECT jsonb_agg(jsonb_build_array(attname,format_type(atttypid,atttypmod),attnotnull) ORDER BY attnum) FROM pg_attribute WHERE attrelid='conversation_messages'::regclass AND attnum>0 AND NOT attisdropped),'constraints',(SELECT jsonb_agg(jsonb_build_array(conname,pg_get_constraintdef(oid)) ORDER BY conname) FROM pg_constraint WHERE conrelid='conversation_messages'::regclass),'indexes',(SELECT jsonb_agg(indexdef ORDER BY indexname) FROM pg_indexes WHERE schemaname='public' AND tablename='conversation_messages')))::text`, f.a.actor.ID, f.b.actor.ID).Scan(&out)
	if e != nil {
		t.Fatal("native exact effects snapshot", e)
	}
	return out
}
func humanOperationNativeMigration(t *testing.T, pool *pgxpool.Pool, name string, want bool) {
	t.Helper()
	raw, e := os.ReadFile("../../migrations/" + name)
	if e != nil {
		t.Fatal(e)
	}
	c, e := pool.Acquire(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Release()
	_, e = c.Exec(context.Background(), string(raw))
	if !want {
		_, _ = c.Exec(context.Background(), "ROLLBACK")
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "P0001" {
			t.Fatalf("actual used down must refuse P0001: %v", e)
		}
	} else if e != nil {
		_, _ = c.Exec(context.Background(), "ROLLBACK")
		t.Fatal("actual migration", e)
	}
}
func TestHumanMessageOperationNativeImmutableHistory(t *testing.T) {
	ownedMigrationDatabase(t) // Never downgrade a parent runtime used by other packages.
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	in := connection.MessageOperationInput{OperationID: agentMemoryID(t, f.f.private), Body: "明确本人本地原生消息，不是试点"}
	legacy, e := b.store.SendMessageCurrent(b.ctx, a, f.conversation, "原无操作编号的消息", "", "")
	if e != nil {
		t.Fatal("original plain compatibility", e)
	}
	before := humanOperationNativeSnapshot(t, f)
	t.Run("unused-down-and-current-data-reapply", func(t *testing.T) {
		humanOperationNativeMigration(t, b.pool, "105_chat_message_operation_receipts.down.sql", true)
		humanOperationNativeMigration(t, b.pool, "105_chat_message_operation_receipts.sql", true)
		if got := humanOperationNativeSnapshot(t, f); got != before {
			t.Fatal("migration changed original message/notification/audit bytes or xmin", legacy.ID)
		}
	})
	original, e := b.store.SendHumanMessageOperation(b.ctx, a, f.conversation, in)
	if e != nil {
		t.Fatal("native original keyed send", e)
	}
	after := humanOperationNativeSnapshot(t, f)
	t.Run("restart-replay-and-exact-receipt", func(t *testing.T) {
		p, e := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		defer p.Close()
		fresh := New(p, false)
		for i := 0; i < 2; i++ {
			r, e := fresh.SendHumanMessageOperation(b.ctx, a, f.conversation, in)
			if e != nil || r != original {
				t.Fatal("stable original effect", e)
			}
		}
		r, e := fresh.ReadHumanMessageOperation(b.ctx, a, f.conversation, in.OperationID)
		if e != nil || r != original {
			t.Fatal("actual keyed current read", e)
		}
		var n int
		if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE source_id=$1`, original.MessageID).Scan(&n); e != nil || n != 1 {
			t.Fatal("one actual notification decision", n, e)
		}
		if humanOperationNativeSnapshot(t, f) != after {
			t.Fatal("replay/read changed message/inbox/notification/audit rows/xmin")
		}
	})
	t.Run("changed-body-target-and-immutable-used-down", func(t *testing.T) {
		changed := in
		changed.Body = "更换正文"
		if _, e = b.store.SendHumanMessageOperation(b.ctx, a, f.conversation, changed); !errors.Is(e, connection.ErrConflict) {
			t.Fatal("same operation body must conflict", e)
		}
		r, e := b.store.CreateFriendRequest(b.ctx, f.a.actor.ID, b.person.ID, "合成第二原好友路径")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.DecideRequest(b.ctx, b.person.ID, r.ID, "accept"); e != nil {
			t.Fatal(e)
		}
		ties, e := b.store.ListTies(b.ctx, f.a.actor.ID)
		if e != nil {
			t.Fatal(e)
		}
		second := ""
		for _, tie := range ties {
			c, e := b.store.StartFriendConversation(b.ctx, f.a.actor.ID, tie.ID)
			if e != nil {
				t.Fatal(e)
			}
			if c.ID != f.conversation {
				second = c.ID
			}
		}
		if second == "" {
			t.Fatal("second valid conversation missing")
		}
		if _, e = b.store.SendHumanMessageOperation(b.ctx, a, second, in); !errors.Is(e, connection.ErrConflict) {
			t.Fatal("same operation target must conflict", e)
		}
		c, e := b.pool.Acquire(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		_, e = c.Exec(b.ctx, `UPDATE conversation_messages SET body='illegal mutation' WHERE id=$1`, original.MessageID)
		c.Release()
		var pe *pgconn.PgError
		if !errors.As(e, &pe) || pe.Code != "55000" {
			t.Fatal("original074 immutable trigger not enforced", e)
		}
		humanOperationNativeMigration(t, b.pool, "105_chat_message_operation_receipts.down.sql", false)
		var count int
		if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2`, f.a.actor.ID, in.OperationID).Scan(&count); e != nil || count != 1 {
			t.Fatal("immutable effect duplicated", count, e)
		}
	})
	t.Run("block-and-old-session-deny-receipt-without-undoing-effect", func(t *testing.T) {
		b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.b.actor.ID, f.a.actor.ID)
		if _, e = b.store.ReadHumanMessageOperation(b.ctx, a, f.conversation, in.OperationID); !errors.Is(e, connection.ErrNotFound) {
			t.Fatal("blocked receipt leaked", e)
		}
		b.exec(`DELETE FROM account_blocks WHERE blocker_account_id=$1 AND blocked_account_id=$2`, f.b.actor.ID, f.a.actor.ID)
		b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.digest[:])
		if _, e = b.store.ReadHumanMessageOperation(b.ctx, a, f.conversation, in.OperationID); !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("old session receipt leaked", e)
		}
		var n int
		if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE id=$1 AND body=$2`, original.MessageID, in.Body).Scan(&n); e != nil || n != 1 {
			t.Fatal("revocation must not claim undo", n, e)
		}
	})
}
func TestHumanMessageOperationNativeConcurrentSameEffect(t *testing.T) {
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	a := ea.Access{Actor: f.a.actor, SessionDigest: f.a.digest}
	in := connection.MessageOperationInput{OperationID: agentMemoryID(t, f.f.private), Body: "本地并发同一次明确发送"}
	p, e := pgxpool.New(b.ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	stores := []*Store{b.store, New(p, false)}
	results := make([]connection.MessageOperationReceipt, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	for i := range stores {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = stores[i].SendHumanMessageOperation(b.ctx, a, f.conversation, in)
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || results[0] != results[1] {
		t.Fatal("same effect under actual two pools", errs)
	}
	var n, notifications int
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2`, f.a.actor.ID, in.OperationID).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM native_notification_decisions WHERE source_id=$1`, results[0].MessageID).Scan(&notifications); e != nil || notifications != 1 {
		t.Fatal(notifications, e)
	}
}
