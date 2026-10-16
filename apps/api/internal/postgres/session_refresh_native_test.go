package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/sessionrefresh"
)

func deviceNativeLogin(t *testing.T, f *legacyNativeFixture, s *Store, phone string) sessionrefresh.Response {
	t.Helper()
	digest := devauth.PhoneDigest(phone)
	if err := s.RequestDevPhoneChallenge(f.Context, digest); err != nil {
		t.Fatal("isolated challenge", err)
	}
	access, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	refresh, _, err := sessionrefresh.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.VerifyDevPhoneChallengePersistent(f.Context, digest, true, access, refresh)
	if err != nil {
		t.Fatal("isolated explicit login", err)
	}
	if result.AccountID == "" || result.AuthMethod != "dev_phone" || result.RefreshToken == "" {
		t.Fatal("persistent response absent")
	}
	return result
}

func TestPersistentDeviceSessionNativeIdleRecoveryConcurrentRotationAndLogout(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	s := New(f.Pool, true)
	ctx := f.Context
	first := deviceNativeLogin(t, f, s, "+447700900301")
	oldAccess, _ := identity.ParseBearer("Bearer " + first.AccessToken)
	if _, err := f.Pool.Exec(ctx, `UPDATE sessions SET created_at=clock_timestamp()-interval '2 hours',idle_expires_at=clock_timestamp()-interval '1 hour' WHERE token_sha256=$1`, oldAccess[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, oldAccess); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("idle-expired access revived")
	}
	op := "17600000-0000-4000-8000-000000000001"
	results := make([]sessionrefresh.Response, 4)
	errs := make([]error, 4)
	var wg sync.WaitGroup
	// Hold precisely the same identity lock used by explicit fixed-code login.
	// Refresh must wait here before taking Account SHARE, avoiding login deadlock.
	loginLock, err := f.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer loginLock.Rollback(ctx)
	phone := devauth.PhoneDigest("+447700900301")
	subject := hex.EncodeToString(phone[:])
	if _, err = loginLock.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, devPhoneIssuer+":"+subject); err != nil {
		t.Fatal(err)
	}
	if _, err = loginLock.Exec(ctx, `SELECT account_id FROM account_auth_identities WHERE issuer=$1 AND subject=$2 FOR UPDATE`, devPhoneIssuer, subject); err != nil {
		t.Fatal(err)
	}
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = s.RefreshSession(ctx, first.RefreshToken, op) }(i)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting int
		if err = f.Pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory'`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("refresh did not wait on identity serialization")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = loginLock.Exec(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE NOWAIT`, first.AccountID); err != nil {
		t.Fatal("refresh took Account before login identity lock", err)
	}
	if err = loginLock.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("same-operation concurrent rotation %d: %v", i, err)
		}
		if !reflect.DeepEqual(results[0], results[i]) {
			t.Fatal("same operation branched credentials")
		}
	}
	next := results[0]
	if next.AccountID != first.AccountID || !next.RefreshExpiresAt.Equal(first.RefreshExpiresAt) || next.RefreshToken == first.RefreshToken || next.AccessToken == first.AccessToken {
		t.Fatal("rotation identity/deadline changed")
	}
	newAccess, _ := identity.ParseBearer("Bearer " + next.AccessToken)
	if _, err := s.Authenticate(ctx, newAccess); err != nil {
		t.Fatal("fresh ordinary access rejected", err)
	}
	var links, tokens int
	var retired bool
	err = f.Pool.QueryRow(ctx, `SELECT (SELECT count(*)FROM persistent_session_access a JOIN persistent_session_families f ON f.id=a.family_id WHERE f.account_id=$1),(SELECT count(*)FROM persistent_session_tokens t JOIN persistent_session_families f ON f.id=t.family_id WHERE f.account_id=$1),(SELECT revoked_at IS NOT NULL FROM sessions WHERE token_sha256=$2)`, first.AccountID, oldAccess[:]).Scan(&links, &tokens, &retired)
	if err != nil || links != 2 || tokens != 2 || !retired {
		t.Fatal("rotation not one new Session / old retired", err)
	}
	// A retry with the same old token still recovers the exact lost response.
	retry, err := s.RefreshSession(ctx, first.RefreshToken, op)
	if err != nil || !reflect.DeepEqual(retry, next) {
		t.Fatal("bounded response recovery", err)
	}
	if err = New(f.Pool, false).RevokeRefreshFamily(ctx, first.RefreshToken); err != nil {
		t.Fatal("consumed-token logout", err)
	}
	if err = s.RevokeRefreshFamily(ctx, next.RefreshToken); err != nil {
		t.Fatal("idempotent family logout", err)
	}
	if _, err = s.Authenticate(ctx, newAccess); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("family logout left access alive")
	}
	if _, err = s.RefreshSession(ctx, next.RefreshToken, "17600000-0000-4000-8000-000000000002"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("logout family refreshed")
	}
}

func TestPersistentDeviceSessionNativeRevocationExpiryReplayAndFalseCredentials(t *testing.T) {
	f := legacyMigrationNativeFixture(t)
	s := New(f.Pool, true)
	ctx := f.Context
	first := deviceNativeLogin(t, f, s, "+447700900302")
	snapshot := func() string {
		t.Helper()
		var x string
		err := f.Pool.QueryRow(ctx, `SELECT md5(concat((SELECT coalesce(jsonb_agg(to_jsonb(x)ORDER BY id)::text,'[]')FROM persistent_session_families x),(SELECT coalesce(jsonb_agg(to_jsonb(x)ORDER BY token_sha256)::text,'[]')FROM persistent_session_tokens x),(SELECT coalesce(jsonb_agg(to_jsonb(x)ORDER BY session_id)::text,'[]')FROM persistent_session_access x),(SELECT coalesce(jsonb_agg(to_jsonb(x)ORDER BY id)::text,'[]')FROM sessions x)))`).Scan(&x)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	before := snapshot()
	unknown, _, _ := sessionrefresh.NewToken()
	if _, err := s.RefreshSession(ctx, unknown, "17600000-0000-4000-8000-000000000003"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("unknown token accepted")
	}
	if err := s.RevokeRefreshFamily(ctx, unknown); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("unknown logout accepted")
	}
	if before != snapshot() {
		t.Fatal("false credentials changed auth rows")
	}
	if _, err := New(f.Pool, false).RefreshSession(ctx, first.RefreshToken, "17600000-0000-4000-8000-000000000004"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("disabled phone grant refreshed")
	}
	if before != snapshot() {
		t.Fatal("disabled method changed auth rows")
	}
	if _, err := f.Pool.Exec(ctx, `UPDATE accounts SET status='suspended' WHERE id=$1`, first.AccountID); err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	if _, err := s.RefreshSession(ctx, first.RefreshToken, "17600000-0000-4000-8000-000000000005"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("suspended fixture refreshed")
	}
	if before != snapshot() {
		t.Fatal("suspended rejection changed auth rows")
	}
	// An expired family is inserted only inside this disposable synthetic DB.
	active := deviceNativeLogin(t, f, s, "+447700900303")
	expired, expiredDigest, _ := sessionrefresh.NewToken()
	var expiredFamily string
	phoneDigest := devauth.PhoneDigest("+447700900303")
	err := f.Pool.QueryRow(ctx, `INSERT INTO persistent_session_families(account_id,identity_issuer,identity_subject,authentication_method,created_at,expires_at)VALUES($1,$2,$3,'dev_phone',clock_timestamp()-interval '31 days',clock_timestamp()-interval '2 days')RETURNING id::text`, active.AccountID, devPhoneIssuer, hex.EncodeToString(phoneDigest[:])).Scan(&expiredFamily)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Pool.Exec(ctx, `INSERT INTO persistent_session_tokens(token_sha256,family_id)VALUES($1,$2)`, expiredDigest[:], expiredFamily); err != nil {
		t.Fatal(err)
	}
	before = snapshot()
	if _, err = s.RefreshSession(ctx, expired, "17600000-0000-4000-8000-000000000006"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("expired device family refreshed")
	}
	if before != snapshot() {
		t.Fatal("expired rejection changed auth rows")
	}
	near, nearDigest, _ := sessionrefresh.NewToken()
	var nearFamily string
	err = f.Pool.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) INSERT INTO persistent_session_families(account_id,identity_issuer,identity_subject,authentication_method,created_at,expires_at) SELECT $1,$2,$3,'dev_phone',stamp.at-interval '29 days',stamp.at+interval '1 minute' FROM stamp RETURNING id::text`, active.AccountID, devPhoneIssuer, hex.EncodeToString(phoneDigest[:])).Scan(&nearFamily)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Pool.Exec(ctx, `INSERT INTO persistent_session_tokens(token_sha256,family_id)VALUES($1,$2)`, nearDigest[:], nearFamily); err != nil {
		t.Fatal(err)
	}
	nearResponse, err := s.RefreshSession(ctx, near, "17600000-0000-4000-8000-000000000011")
	if err != nil || nearResponse.ExpiresInSeconds < 1 || nearResponse.ExpiresInSeconds > 60 || nearResponse.IdleTimeoutSeconds != nearResponse.ExpiresInSeconds {
		t.Fatal("last access exceeded fixed family TTL", err)
	}
	var bounded bool
	err = f.Pool.QueryRow(ctx, `SELECT bool_and(s.expires_at<=f.expires_at AND s.idle_expires_at<=f.expires_at) FROM persistent_session_access l JOIN sessions s ON s.id=l.session_id JOIN persistent_session_families f ON f.id=l.family_id WHERE f.id=$1`, nearFamily).Scan(&bounded)
	if err != nil || !bounded {
		t.Fatal("family deadline did not bind native access", err)
	}
	// Different-operation replay revokes the family, including the new access.
	next, err := s.RefreshSession(ctx, active.RefreshToken, "17600000-0000-4000-8000-000000000007")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RefreshSession(ctx, active.RefreshToken, "17600000-0000-4000-8000-000000000008"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("consumed token replay accepted")
	}
	nd := sha256.Sum256([]byte(next.AccessToken))
	if _, err = s.Authenticate(ctx, nd); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("replay did not revoke successor access")
	}
	// The original bearer logout also revokes its linked device family.
	bearer := deviceNativeLogin(t, f, s, "+447700900304")
	bd := sha256.Sum256([]byte(bearer.AccessToken))
	if err = s.RevokeSession(ctx, bd); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RefreshSession(ctx, bearer.RefreshToken, "17600000-0000-4000-8000-000000000009"); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("bearer logout left device family alive")
	}
	// Server commits a rotation but the 60-second response recovery window lapses.
	late := deviceNativeLogin(t, f, s, "+447700900305")
	lateOp := "17600000-0000-4000-8000-000000000010"
	if _, err = s.RefreshSession(ctx, late.RefreshToken, lateOp); err != nil {
		t.Fatal(err)
	}
	ld, _ := sessionrefresh.ParseToken(late.RefreshToken)
	if _, err = f.Pool.Exec(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE persistent_session_tokens SET consumed_at=stamp.at-interval '2 minutes',receipt_expires_at=stamp.at-interval '1 minute' FROM stamp WHERE token_sha256=$1`, ld[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RefreshSession(ctx, late.RefreshToken, lateOp); !errors.Is(err, identity.ErrUnauthorized) {
		t.Fatal("expired replay window returned credential")
	}
}

// Compile-time optional port assertions leave the original auth interfaces intact.
var _ sessionrefresh.Store = (*Store)(nil)
var _ sessionrefresh.DevPhoneStore = (*Store)(nil)
