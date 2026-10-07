package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/jackc/pgx/v5"
	"os"
	"strings"
	"testing"
)

type imageGateRow struct {
	value bool
	err   error
}

func (r imageGateRow) Scan(p ...any) error {
	if r.err != nil {
		return r.err
	}
	*(p[0].(*bool)) = r.value
	return nil
}

type imageGateTx struct {
	pgx.Tx
	sql   string
	args  []any
	value bool
	err   error
}

type imageCommitUnitTx struct {
	pgx.Tx
	cancel context.CancelFunc
	calls  int
}

func (t *imageCommitUnitTx) Commit(context.Context) error {
	t.calls++
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}
func TestPrivateMomentImageCommitCancellationIsNotConfirmed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tx := &imageCommitUnitTx{cancel: cancel}
	if e := commitPrivateImage(ctx, tx); !errors.Is(e, content.ErrUnavailable) || tx.calls != 1 {
		t.Fatal("post-commit cancellation falsely confirmed", e)
	}
	tx = &imageCommitUnitTx{}
	if e := commitPrivateImage(ctx, tx); !errors.Is(e, content.ErrUnavailable) || tx.calls != 0 {
		t.Fatal("pre-cancelled request committed", e)
	}
	tx = &imageCommitUnitTx{}
	if e := commitPrivateImage(context.Background(), tx); e != nil || tx.calls != 1 {
		t.Fatal("normal commit control", e)
	}
}

func (t *imageGateTx) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	t.sql = sql
	t.args = args
	return imageGateRow{t.value, t.err}
}
func TestPrivateMomentImageCollectionFinalGate(t *testing.T) {
	s := &Store{}
	a := identity.Actor{ID: "11111111-1111-4111-8111-111111111111", AccountType: "person"}
	var d [32]byte
	d[0] = 1
	for _, count := range []int{0, 1, 12} {
		t.Run(string(rune('A'+count)), func(t *testing.T) {
			rs := []media.PrivateImageReceipt{}
			for i := 0; i < count; i++ {
				rs = append(rs, media.PrivateImageReceipt{ID: a.ID, Status: "ready_private", Revision: 2, SHA256: strings.Repeat("a", 64)})
			}
			tx := &imageGateTx{value: true}
			if e := s.imageCollectionFinal(context.Background(), tx, d, a, a.ID, "current-source", rs); e != nil {
				t.Fatal(e)
			}
			var encoded []map[string]any
			if e := json.Unmarshal([]byte(tx.args[5].(string)), &encoded); e != nil || len(encoded) != count {
				t.Fatal("encoded subset not bound", e)
			}
			if tx.args[2] != "current-source" || strings.Count(tx.sql, "clock_timestamp()") != 1 || !strings.Contains(tx.sql, "NOT EXISTS(SELECT 1 FROM expected") {
				t.Fatal("collection final gate")
			}
			tx.value = false
			if e := s.imageCollectionFinal(context.Background(), tx, d, a, a.ID, "source", rs); !errors.Is(e, content.ErrConflict) {
				t.Fatal("negative current state accepted")
			}
			tx.err = errors.New("unit query failure")
			if e := s.imageCollectionFinal(context.Background(), tx, d, a, a.ID, "source", rs); !errors.Is(e, content.ErrUnavailable) {
				t.Fatal("SQL error masqueraded as success")
			}
		})
	}
}
func TestPrivateMomentImageFreshHumanReadAndPreviewABA(t *testing.T) {
	old := privateImageRow{receipt: media.PrivateImageReceipt{Status: "preview"}, source: "old-private-xmin", session: "old-session"}
	for _, c := range []struct{ source, session string }{{"text-edit-xmin", "old-session"}, {"old-private-xmin", "new-session"}, {"public-private-ABA-xmin", "old-session"}} {
		if _, e := currentPrivateImageRead(old, c.source, c.session, false); !errors.Is(e, content.ErrConflict) {
			t.Fatal("old preview approval survived source/session change")
		}
	}
	old.receipt.Status = "ready_private"
	v, e := currentPrivateImageRead(old, "legitimate-text-edit-xmin", "current-human-session", true)
	if e != nil || v.source != "legitimate-text-edit-xmin" {
		t.Fatal("fresh owned read incorrectly reused old approval or hid normal edit")
	}
	old.receipt.Status = "deleted"
	if _, e = currentPrivateImageRead(old, "current-private-source", "current-session", true); !errors.Is(e, content.ErrNotFound) {
		t.Fatal("tombstone bytes exposed")
	}
	if _, e = currentPrivateImageRead(old, "current-private-source", "current-session", false); e != nil {
		t.Fatal("unknown delete cannot reconcile tombstone")
	}
}
func TestPrivateMomentImageSQLStorageLifecycleAndOrder(t *testing.T) {
	raw, e := os.ReadFile("human_moment_media.go")
	if e != nil {
		t.Fatal(e)
	}
	source := string(raw)
	for _, v := range []string{"FOR UPDATE OF m", "imageSession", "privateImageCollectionFinalSQL"} {
		if !strings.Contains(source, v) {
			t.Fatal("lost current resource gate", v)
		}
	}
	for _, v := range []string{"jsonb_to_recordset", "ma.state<>'ready_private'", "ma.owner_account_id<>a.id", "ma.sha256_hex IS DISTINCT FROM i.derivative_sha256", "ss.expires_at>cl.at", "ss.idle_expires_at>cl.at", "i.preview_expires_at<=cl.at"} {
		if !strings.Contains(privateImageCollectionFinalSQL, v) {
			t.Fatal("collection currentness missing", v)
		}
	}
	if strings.Contains(source, "finishHumanMomentTx(ctx") {
		t.Fatal("session-only SQL after full final fence")
	}
	if !strings.Contains(source, "encode(receipts, payload)") || strings.Index(source, "encode(receipts, payload)") > strings.Index(source, "imageCollectionFinal(ctx, tx") {
		t.Fatal("encoded response not gated")
	}
	ddl, e := os.ReadFile("../../migrations/100_private_moment_media.sql")
	if e != nil {
		t.Fatal(e)
	}
	text := string(ddl)
	for _, v := range []string{"derivative_sha256 IS NOT NULL", "derivative=NULL", "status='deleted'", "NEW.visibility<>'private'", "NEW.status<>'draft'", "author_account_id IS DISTINCT FROM", "UNIQUE(owner_account_id,operation_id)", "FOR UPDATE"} {
		if !strings.Contains(text, v) {
			t.Fatal("missing lifecycle bound", v)
		}
	}
	for _, v := range []string{"INSERT INTO private_media_metadata", "INSERT INTO media_variants", "PutPublic", "MODEL_CONTEXT_EGRESS"} {
		if strings.Contains(source, v) {
			t.Fatal("unexpected original/public/model write")
		}
	}
	var called bool
	_, e = (&Store{}).ReadHumanPrivateImages(context.Background(), [32]byte{}, identity.Actor{}, "11111111-1111-4111-8111-111111111111", "", false, func([]media.PrivateImageReceipt, []byte) ([]byte, error) { called = true; return nil, nil })
	if !errors.Is(e, content.ErrUnavailable) || called {
		t.Fatal("absent native store should not encode private response")
	}
}

// Source-order unit evidence, not PostgreSQL deadlock/concurrency execution.
func TestPrivateMomentImageWriteLocksBeforeSession(t *testing.T) {
	raw, e := os.ReadFile("human_moment_media.go")
	if e != nil {
		t.Fatal(e)
	}
	source := string(raw)
	for _, c := range []struct{ start, end, write string }{
		{"func (s *Store) SaveHumanPrivateImage", "func (s *Store) ReadHumanPrivateImages", "INSERT INTO media_assets"},
		{"func (s *Store) DeleteHumanPrivateImage", "// Retention is", "UPDATE media_assets"},
	} {
		body := source[strings.Index(source, c.start):strings.Index(source, c.end)]
		if strings.LastIndex(body, "s.imageSession(") < strings.Index(body, c.write) {
			t.Errorf("%s locks Session before asset mutation", c.start)
		}
	}
}

func TestPrivateMomentImageListFiltersExpiredBeforeLimit(t *testing.T) {
	raw, e := os.ReadFile("human_moment_media.go")
	if e != nil {
		t.Fatal(e)
	}
	source := string(raw)
	start := strings.Index(source, "func (s *Store) readHumanPrivateImages")
	end := strings.Index(source[start:], "ORDER BY created_at,id LIMIT 12")
	if end < 0 || !strings.Contains(source[start:start+end], "retain_until>") {
		t.Fatal("expired READY rows can occupy LIMIT12 and hide newer live own attachments")
	}
}
