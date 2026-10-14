package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
	"github.com/jackc/pgx/v5/pgxpool"
)

type systemPushNativeFixture struct {
	f    *nativeEntityShareFixture
	p    *SystemPush
	a    sp.Access
	ring *sp.Keyring
}

func newSystemPushFixture(t *testing.T) *systemPushNativeFixture {
	t.Helper()
	f := newNativeEntityShareFixture(t)
	b := f.f.private.base
	ring, e := sp.ParseKeyring(pushTestKeys(t, "one", map[string]byte{"one": 1}))
	if e != nil {
		t.Fatal(e)
	}
	p, e := NewSystemPush(b.store, ring, pushTestConfig())
	if e != nil {
		t.Fatal(e)
	}
	iid := agentMemoryID(t, f.f.private)
	a := sp.Access{ProfileAccess: identity.ProfileAccess{Actor: f.b.actor, SessionDigest: f.b.digest}, InstallationID: iid, ProofDigest: sha256.Sum256([]byte("synthetic-installation-proof-" + iid))}
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM system_push_attempts WHERE event_id IN(SELECT id FROM system_push_events WHERE installation_id=$1)`, `DELETE FROM system_push_events WHERE installation_id=$1`, `DELETE FROM system_push_operations WHERE installation_id=$1`, `DELETE FROM system_push_installations WHERE installation_id=$1`} {
			if _, e := b.pool.Exec(context.Background(), q, iid); e != nil {
				t.Error("owned system push cleanup", e)
			}
		}
	})
	return &systemPushNativeFixture{f, p, a, ring}
}
func (f *systemPushNativeFixture) registration(t *testing.T, version int64, kind sp.Kind) sp.RegistrationInput {
	t.Helper()
	in := sp.RegistrationInput{Schema: "system-push-registration-v1", OperationID: agentMemoryID(t, f.f.f.private), ExpectedVersion: version, RegistrationKind: kind, Platform: "ANDROID", Permission: "AUTHORIZED", Address: sp.Address("synthetic-fcm-address-" + f.a.InstallationID)}
	if kind == sp.APNSToken {
		in.Platform = "IOS"
		in.Address = sp.Address(strings.ReplaceAll(f.a.InstallationID, "-", "") + strings.ReplaceAll(f.a.InstallationID, "-", ""))
	}
	return in
}
func (f *systemPushNativeFixture) register(t *testing.T, version int64, kind sp.Kind) sp.Operation {
	t.Helper()
	v, e := f.p.RegisterInstallation(f.f.f.private.base.ctx, f.a, f.registration(t, version, kind))
	if e != nil || !sp.ValidOperation(v) {
		t.Fatal("actual registration", e)
	}
	return v
}
func (f *systemPushNativeFixture) event(t *testing.T) (string, string) {
	t.Helper()
	b := f.f.f.private.base
	m, e := b.store.SendMessage(b.ctx, f.f.a.actor.ID, f.f.conversation, "本地合成原生通知私密正文")
	if e != nil {
		t.Fatal("actual message source", e)
	}
	var event, inbox string
	if e = b.pool.QueryRow(b.ctx, `SELECT e.id::text,i.id::text FROM system_push_events e JOIN inbox_items i ON i.id=e.inbox_id WHERE e.installation_id=$1 AND i.resource_id=$2`, f.a.InstallationID, m.ID).Scan(&event, &inbox); e != nil {
		t.Fatal("native inbox transport enqueue", e)
	}
	return event, inbox
}
func (f *systemPushNativeFixture) claim(t *testing.T) sp.Claim {
	t.Helper()
	b := f.f.f.private.base
	v, e := f.p.Claim(b.ctx, []sp.Kind{sp.FCMToken, sp.APNSToken}, 25)
	if e != nil || len(v) != 1 {
		t.Fatal("single current claim", len(v), e)
	}
	return v[0]
}
func TestSystemPushNativeRegistrationCASReceiptsRebindAndAddressCheck(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newSystemPushFixture(t)
	b := f.f.f.private.base
	initial, e := f.p.ReadInstallation(b.ctx, f.a)
	if e != nil || initial.Version != 0 || initial.Enabled {
		t.Fatal("implicit registration", e)
	}
	disabled, _ := NewSystemPush(b.store, nil, sp.Config{})
	options, e := disabled.ReadOptions(b.ctx, f.a.ProfileAccess)
	if e != nil || options.Available || len(options.RegistrationKinds) != 0 {
		t.Fatal("default OFF", e)
	}
	if _, e = disabled.RegisterInstallation(b.ctx, f.a, f.registration(t, 0, sp.FCMToken)); !errors.Is(e, sp.ErrDisabled) {
		t.Fatal("OFF wrote device", e)
	}
	in := f.registration(t, 0, sp.FCMToken)
	var wg sync.WaitGroup
	ops := make(chan sp.Operation, 6)
	errs := make(chan error, 6)
	for n := 0; n < 6; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); v, e := f.p.RegisterInstallation(b.ctx, f.a, in); ops <- v; errs <- e }()
	}
	wg.Wait()
	close(ops)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("same operation concurrency", e)
		}
	}
	var first sp.Operation
	for v := range ops {
		if first.BindingID != nil && *first.BindingID != *v.BindingID {
			t.Fatal("same operation duplicated binding")
		}
		first = v
	}
	view, e := f.p.ReadInstallation(b.ctx, f.a)
	if e != nil || view.Version != 1 || !view.Enabled {
		t.Fatal("current installation", e)
	}
	var ciphertext []byte
	var operationCount int
	if e = b.pool.QueryRow(b.ctx, `SELECT address_ciphertext FROM system_push_installations WHERE installation_id=$1`, f.a.InstallationID).Scan(&ciphertext); e != nil || strings.Contains(string(ciphertext), string(in.Address.Bytes())) {
		t.Fatal("plaintext token persisted", e)
	}
	if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM system_push_operations WHERE installation_id=$1`, f.a.InstallationID).Scan(&operationCount); e != nil || operationCount != 1 {
		t.Fatal("duplicate operation facts", operationCount, e)
	}
	check := sp.AddressCheckInput{Schema: "system-push-address-check-v1", ExpectedVersion: 1, RegistrationKind: sp.FCMToken, Address: in.Address}
	match, e := f.p.CheckAddress(b.ctx, f.a, check)
	if e != nil || !match.Matches || match.Version != 1 {
		t.Fatal("current address not matched", e)
	}
	if e = f.p.ValidateAddressMatch(b.ctx, f.a, check, match); e != nil {
		t.Fatal(e)
	}
	check.Address = sp.Address("synthetic-different-fcm-address-123456789")
	different, e := f.p.CheckAddress(b.ctx, f.a, check)
	if e != nil || different.Matches {
		t.Fatal("changed address matched", e)
	}
	after, e := f.p.ReadInstallation(b.ctx, f.a)
	if e != nil || after.Version != 1 {
		t.Fatal("read-only address check wrote", e)
	}
	wrong := f.a
	wrong.ProofDigest = sha256.Sum256([]byte("wrong-installation-secret"))
	if _, e = f.p.ReadInstallation(b.ctx, wrong); !errors.Is(e, sp.ErrNotFound) {
		t.Fatal("installation UUID was control proof", e)
	}
	inputs := []sp.RegistrationInput{f.registration(t, 1, sp.FCMToken), f.registration(t, 1, sp.FCMToken)}
	results := make(chan error, 2)
	for _, next := range inputs {
		wg.Add(1)
		go func(v sp.RegistrationInput) {
			defer wg.Done()
			_, e := f.p.RegisterInstallation(b.ctx, f.a, v)
			results <- e
		}(next)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for e := range results {
		if e == nil {
			wins++
		} else if errors.Is(e, sp.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(e)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatal("stale CAS did not serialize", wins, conflicts)
	}
	old, e := f.p.ReadOperation(b.ctx, f.a, in.OperationID)
	if e != nil || old.ResultVersion != 1 || *old.BindingID != *first.BindingID {
		t.Fatal("causal old receipt lost", e)
	}
	if _, e = f.p.RegisterInstallation(b.ctx, f.a, in); e != nil {
		t.Fatal("causal identical retry lost after rotation", e)
	}
	check.ExpectedVersion = 1
	if e = f.p.ValidateAddressMatch(b.ctx, f.a, check, match); e == nil {
		t.Fatal("late address match released after rotation")
	}
	moved := f.a
	moved.ProfileAccess = identity.ProfileAccess{Actor: f.f.a.actor, SessionDigest: f.f.a.digest}
	peerView, e := f.p.ReadInstallation(b.ctx, moved)
	if e != nil || peerView.Enabled || peerView.BindingID != nil || peerView.Version != 2 {
		t.Fatal("other account saw old owner binding", e)
	}
	bind := f.registration(t, 2, sp.FCMToken)
	bind.OperationID = agentMemoryID(t, f.f.f.private)
	op, e := f.p.RegisterInstallation(b.ctx, moved, bind)
	if e != nil || op.ResultVersion != 3 {
		t.Fatal("same installation account rebind", e)
	}
	former, e := f.p.ReadInstallation(b.ctx, f.a)
	if e != nil || former.Enabled {
		t.Fatal("old owner resurrected", e)
	}
	if _, e = f.p.ReadOperation(b.ctx, moved, in.OperationID); !errors.Is(e, sp.ErrNotFound) {
		t.Fatal("new account read old owner's operation", e)
	}
	revoked, e := f.p.RevokeInstallation(b.ctx, moved, sp.RevokeInput{Schema: "system-push-revoke-v1", OperationID: agentMemoryID(t, f.f.f.private), ExpectedVersion: 3})
	if e != nil || revoked.ResultVersion != 4 || revoked.BindingID != nil {
		t.Fatal("exact revoke", e)
	}
}
func TestSystemPushNativeOpaqueDeliveryUnknownNoResendAndLateInvalidation(t *testing.T) {
	ownedMigrationDatabase(t)
	t.Run("unknown-no-resend", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		registration := f.register(t, 0, sp.FCMToken)
		event, _ := f.event(t)
		c := f.claim(t)
		r, e := f.p.PrepareSend(b.ctx, c)
		if e != nil || r.EventID != event || r.BindingVersion != 1 || r.BindingID != *registration.BindingID || len(r.Data()) != 4 {
			t.Fatal("opaque send permit", e)
		}
		wire, _ := json.Marshal(r.Data())
		for _, private := range []string{f.a.Actor.ID, f.f.conversation, "本地合成原生通知私密正文"} {
			if strings.Contains(string(wire), private) {
				t.Fatal("private provider data")
			}
		}
		if e = f.p.CompleteSend(b.ctx, c, sp.ProviderResult{Outcome: sp.Unknown, Code: "TRANSPORT_UNKNOWN"}); e != nil {
			t.Fatal(e)
		}
		again, e := f.p.Claim(b.ctx, []sp.Kind{sp.FCMToken}, 25)
		if e != nil || len(again) != 0 {
			t.Fatal("UNKNOWN automatically re-sent", len(again), e)
		}
		if e = f.p.CompleteSend(b.ctx, c, sp.ProviderResult{Outcome: sp.Accepted, Code: "ACCEPTED", MessageID: "synthetic-late"}); !errors.Is(e, sp.ErrConflict) {
			t.Fatal("terminal receipt overwritten", e)
		}
	})
	t.Run("crash-after-send-start", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.FCMToken)
		f.event(t)
		c := f.claim(t)
		if _, e := f.p.PrepareSend(b.ctx, c); e != nil {
			t.Fatal(e)
		}
		b.exec(`UPDATE system_push_events SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, c.EventID)
		v, e := f.p.Claim(b.ctx, []sp.Kind{sp.FCMToken}, 25)
		if e != nil || len(v) != 0 {
			t.Fatal("crashed send re-sent", e)
		}
		var state string
		if e = b.pool.QueryRow(b.ctx, `SELECT state FROM system_push_events WHERE id=$1`, c.EventID).Scan(&state); e != nil || state != "UNKNOWN" {
			t.Fatal("crash not honest UNKNOWN", state, e)
		}
	})
	t.Run("late-old-token-invalidation", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.APNSToken)
		f.event(t)
		c := f.claim(t)
		if _, e := f.p.PrepareSend(b.ctx, c); e != nil {
			t.Fatal(e)
		}
		next := f.register(t, 1, sp.APNSToken)
		old := time.Now().UTC().Add(-time.Hour)
		if e := f.p.CompleteSend(b.ctx, c, sp.ProviderResult{Outcome: sp.InvalidToken, Code: "UNREGISTERED", InvalidatedAt: &old}); e != nil {
			t.Fatal(e)
		}
		v, e := f.p.ReadInstallation(b.ctx, f.a)
		if e != nil || !v.Enabled || v.Version != 2 || *v.BindingID != *next.BindingID {
			t.Fatal("old invalidation revoked new binding", e)
		}
	})
}
func TestSystemPushNativeConcurrentSessionRevokeAndCurrentInboxOpen(t *testing.T) {
	ownedMigrationDatabase(t)
	t.Run("pending-register-generation-ABA", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.FCMToken)
		block, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer block.Rollback(context.Background())
		if _, e = block.Exec(b.ctx, `SELECT installation_id FROM system_push_installations WHERE installation_id=$1 FOR UPDATE`, f.a.InstallationID); e != nil {
			t.Fatal(e)
		}
		input := f.registration(t, 1, sp.FCMToken)
		done := make(chan error, 1)
		go func() { _, e := f.p.RegisterInstallation(b.ctx, f.a, input); done <- e }()
		placeMemoryWaitForBlock(t, f.f.f, block.Conn().PgConn().PID())
		b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.SessionDigest[:])
		b.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, f.a.SessionDigest[:])
		if e = block.Commit(b.ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("pending operation reused restored Session epoch", e)
		}
	})
	t.Run("registration-revoke-during-installation-wait", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.FCMToken)
		block, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer block.Rollback(context.Background())
		if _, e = block.Exec(b.ctx, `SELECT installation_id FROM system_push_installations WHERE installation_id=$1 FOR UPDATE`, f.a.InstallationID); e != nil {
			t.Fatal(e)
		}
		in := f.registration(t, 1, sp.FCMToken)
		done := make(chan error, 1)
		go func() { _, e := f.p.RegisterInstallation(b.ctx, f.a, in); done <- e }()
		placeMemoryWaitForBlock(t, f.f.f, block.Conn().PgConn().PID())
		b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.SessionDigest[:])
		if e = block.Commit(b.ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, identity.ErrUnauthorized) {
			t.Fatal("stale session admitted after installation wait", e)
		}
		var n int
		if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM system_push_operations WHERE installation_id=$1`, f.a.InstallationID).Scan(&n); e != nil || n != 1 {
			t.Fatal("late unauthorized write", n, e)
		}
	})
	t.Run("dispatch-revoke-during-inbox-wait", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.FCMToken)
		_, iid := f.event(t)
		c := f.claim(t)
		block, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer block.Rollback(context.Background())
		if _, e = block.Exec(b.ctx, `SELECT id FROM inbox_items WHERE id=$1 FOR UPDATE`, iid); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { _, e := f.p.PrepareSend(b.ctx, c); done <- e }()
		placeMemoryWaitForBlock(t, f.f.f, block.Conn().PgConn().PID())
		b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.SessionDigest[:])
		if e = block.Commit(b.ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, sp.ErrNotFound) {
			t.Fatal("revoked registration emitted permit", e)
		}
		var n int
		if e = b.pool.QueryRow(b.ctx, `SELECT attempt_count FROM system_push_events WHERE id=$1`, c.EventID).Scan(&n); e != nil || n != 0 {
			t.Fatal("provider dispatch marked after revoke", n, e)
		}
	})
	t.Run("authoritative-object-and-final-ACL", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		op := f.register(t, 0, sp.FCMToken)
		event, iid := f.event(t)
		in := sp.OpenInput{EventID: event, BindingID: *op.BindingID, BindingVersion: 1}
		v, e := f.p.ResolveEvent(b.ctx, f.a, in)
		if e != nil || v.Item.ID != iid || v.Item.TargetConversationID == nil || *v.Item.TargetConversationID != f.f.conversation {
			t.Fatal("original native target lost", e)
		}
		if e = f.p.ValidateEvent(b.ctx, f.a, in, v); e != nil {
			t.Fatal(e)
		}
		var read *time.Time
		if e = b.pool.QueryRow(b.ctx, `SELECT read_at FROM inbox_items WHERE id=$1`, iid).Scan(&read); e != nil || read != nil {
			t.Fatal("open prematurely marked read", e)
		}
		block, e := b.pool.Begin(b.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer block.Rollback(context.Background())
		if _, e = block.Exec(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, f.a.SessionDigest[:]); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- f.p.ValidateEvent(b.ctx, f.a, in, v) }()
		placeMemoryWaitForBlock(t, f.f.f, block.Conn().PgConn().PID())
		b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.a.Actor.ID, f.f.a.actor.ID)
		if e = block.Commit(b.ctx); e != nil {
			t.Fatal(e)
		}
		if e = <-done; !errors.Is(e, sp.ErrNotFound) {
			t.Fatal("late source/ACL loss released original object", e)
		}
	})
	t.Run("authorization-generation-ABA", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.FCMToken)
		f.event(t)
		c := f.claim(t)
		b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.SessionDigest[:])
		b.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, f.a.SessionDigest[:])
		v, e := f.p.ReadInstallation(b.ctx, f.a)
		if e != nil || v.Enabled {
			t.Fatal("ABA restored old grant", e)
		}
		if _, e = f.p.PrepareSend(b.ctx, c); !errors.Is(e, sp.ErrNotFound) {
			t.Fatal("ABA emitted old transport grant", e)
		}
	})
}

func TestSystemPushNativeSendBodyRechecksSourceBindingAddressAndInbox(t *testing.T) {
	ownedMigrationDatabase(t)
	for _, change := range []string{"session-revoke", "binding-rotate", "native-source-block", "address-tamper", "already-read"} {
		t.Run(change, func(t *testing.T) {
			f := newSystemPushFixture(t)
			b := f.f.f.private.base
			f.register(t, 0, sp.FCMToken)
			_, iid := f.event(t)
			c := f.claim(t)
			request, e := f.p.PrepareSend(b.ctx, c)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.p.CheckSend(b.ctx, c, request); e != nil {
				t.Fatal("initial body permit", e)
			}
			switch change {
			case "session-revoke":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.SessionDigest[:])
			case "binding-rotate":
				f.register(t, 1, sp.FCMToken)
			case "native-source-block":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.a.Actor.ID, f.f.a.actor.ID)
			case "address-tamper":
				request.Address = sp.Address("synthetic-other-address-1234567890123456789")
			case "already-read":
				if _, e = b.store.ReadHumanInboxItem(b.ctx, f.a.SessionDigest, f.a.Actor, iid); e != nil {
					t.Fatal("real original mark-read", e)
				}
			}
			if e = f.p.CheckSend(b.ctx, c, request); !errors.Is(e, sp.ErrNotFound) {
				t.Fatal("body read reused stale grant", change, e)
			}
		})
	}
	t.Run("provider-invalidation-exact-version", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.FCMToken)
		f.event(t)
		c := f.claim(t)
		if _, e := f.p.PrepareSend(b.ctx, c); e != nil {
			t.Fatal(e)
		}
		if e := f.p.CompleteSend(b.ctx, c, sp.ProviderResult{Outcome: sp.InvalidToken, Code: "UNREGISTERED"}); e != nil {
			t.Fatal(e)
		}
		view, e := f.p.ReadInstallation(b.ctx, f.a)
		if e != nil || view.Enabled || view.Version != 2 {
			t.Fatal("invalid token retained active version", e)
		}
	})
	t.Run("APNS-old-timestamp-not-current-grant", func(t *testing.T) {
		f := newSystemPushFixture(t)
		b := f.f.f.private.base
		f.register(t, 0, sp.APNSToken)
		f.event(t)
		c := f.claim(t)
		if _, e := f.p.PrepareSend(b.ctx, c); e != nil {
			t.Fatal(e)
		}
		older := time.Now().UTC().Add(-time.Hour)
		if e := f.p.CompleteSend(b.ctx, c, sp.ProviderResult{Outcome: sp.InvalidToken, Code: "UNREGISTERED", InvalidatedAt: &older}); e != nil {
			t.Fatal(e)
		}
		v, e := f.p.ReadInstallation(b.ctx, f.a)
		if e != nil || !v.Enabled || v.Version != 1 {
			t.Fatal("410 older timestamp killed current grant", e)
		}
	})
}
func TestSystemPushNativeMigration127RoundtripAndDataGuard(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	down, e := os.ReadFile(filepath.Join("..", "..", "migrations", "127_system_push.down.sql"))
	if e != nil {
		t.Fatal(e)
	}
	up, e := os.ReadFile(filepath.Join("..", "..", "migrations", "127_system_push.sql"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, string(down)); e != nil {
		t.Fatal("empty owned down", e)
	}
	if _, e = pool.Exec(ctx, string(up)); e != nil {
		t.Fatal("actual owned up", e)
	}
	f := newSystemPushFixture(t)
	f.register(t, 0, sp.FCMToken)
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(context.Background())
	if _, e = tx.Exec(ctx, string(down)); e == nil {
		t.Fatal("down discarded device/operation facts")
	}
	_ = tx.Rollback(ctx)
}
