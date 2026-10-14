package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/follow"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func currentProfileNativeFixture(t *testing.T) *visibilityMatrixFixture {
	t.Helper()
	f := visibilityMatrixNative(t)
	f.exec(`INSERT INTO agents(agent_type,principal_account_id,status) VALUES('personal',$1,'active')`, f.ids[0])
	t.Cleanup(func() {
		ownedSocialFixtureCleanup(t, f.ctx, f.pool, `DELETE FROM agents WHERE principal_account_id=$1`, f.ids[0])
	})
	f.exec(`UPDATE user_profiles SET display_name='已获准的普通资料',bio='PROFILE_NATIVE_APPROVED_BODY' WHERE account_id=$1`, f.ids[0])
	return f
}

type currentProfileMaterializeKey struct{}
type currentProfileMaterializeTracer struct {
	after func()
	fired bool
}

func (tr *currentProfileMaterializeTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	if d.SQL == currentProfileSQL {
		return context.WithValue(ctx, currentProfileMaterializeKey{}, true)
	}
	return ctx
}
func (tr *currentProfileMaterializeTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	if ctx.Value(currentProfileMaterializeKey{}) == true && !tr.fired {
		tr.fired = true
		tr.after()
	}
}
func TestCurrentProfileNativeClockAfterMaterialization(t *testing.T) {
	for _, mode := range []string{"session_expiry", "session_generation_ABA", "grant_deadline"} {
		t.Run(mode, func(t *testing.T) {
			f := currentProfileNativeFixture(t)
			index := 2
			if mode == "grant_deadline" {
				index = 4
			}
			a := currentProfileNativeAccess(t, f, index)
			if mode == "session_expiry" {
				f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '800 milliseconds' WHERE token_sha256=$1`, a.SessionDigest[:])
			}
			if mode == "grant_deadline" {
				f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[0])
				f.exec(`UPDATE consent_grants SET expires_at=clock_timestamp()+interval '800 milliseconds' WHERE owner_account_id=$1 AND recipient_account_id=$2`, f.ids[0], f.ids[4])
			}
			tr := &currentProfileMaterializeTracer{after: func() {
				if mode == "session_generation_ABA" {
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
					f.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, a.SessionDigest[:])
				} else {
					f.exec(`SELECT pg_sleep(0.95)`)
				}
			}}
			config, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			config.ConnConfig.Tracer = tr
			pool, e := pgxpool.NewWithConfig(f.ctx, config)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			s := New(pool, false)
			p, e := s.ReadProfileCurrent(f.ctx, a, f.ids[0])
			want := identity.ErrUnauthorized
			if mode == "grant_deadline" {
				want = identity.ErrConflict
			}
			if !tr.fired || !errors.Is(e, want) || p.AccountID != "" || p.Bio != "" {
				t.Fatal("snapshot clock/source was renewed after native materialization", tr.fired, e)
			}
		})
	}
}
func currentProfileNativeAccess(t *testing.T, f *visibilityMatrixFixture, index int) identity.ProfileAccess {
	t.Helper()
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
  VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.ids[index], digest[:])
	return identity.ProfileAccess{Actor: identity.Actor{ID: f.ids[index], AccountType: "person"}, SessionDigest: digest}
}
func TestCurrentProfileNativeFiveAudiences(t *testing.T) {
	f := currentProfileNativeFixture(t)
	accesses := []identity.ProfileAccess{{}}
	for i := range f.ids {
		accesses = append(accesses, currentProfileNativeAccess(t, f, i))
	}
	for _, scope := range []string{"public", "followers", "connections", "close", "private"} {
		f.exec(`UPDATE user_profiles SET visibility=$2 WHERE account_id=$1`, f.ids[0], scope)
		for i, a := range accesses {
			t.Run(scope+"/"+[]string{"anonymous", "owner", "follower", "close_friend", "friend", "explicit_grant", "stranger", "reverse_follower"}[i], func(t *testing.T) {
				want := scope == "public" || i == 1 || i == 5 || scope == "followers" && i == 2 || scope == "connections" && (i == 3 || i == 4) || scope == "close" && i == 3
				p, e := f.store.ReadProfileCurrent(f.ctx, a, f.ids[0])
				if !want {
					if !errors.Is(e, identity.ErrNotFound) || p.AccountID != "" {
						t.Fatal("denied Profile leaked", e)
					}
					return
				}
				if e != nil || p.Bio != "PROFILE_NATIVE_APPROVED_BODY" || p.Visibility != scope {
					t.Fatal("actual Profile projection failed", e, p.AccountID, p.Visibility)
				}
				if e = f.store.ValidateProfileCurrent(f.ctx, a, f.ids[0], p); e != nil {
					t.Fatal("unchanged native proof failed", e)
				}
				wire, e := json.Marshal(p)
				if e != nil {
					t.Fatal(e)
				}
				var fields map[string]json.RawMessage
				if json.Unmarshal(wire, &fields) != nil || len(fields) != 4 {
					t.Fatal("native proof escaped ordinary DTO")
				}
			})
		}
	}
}
func TestCurrentProfileNativeProofAndSourceABA(t *testing.T) {
	for _, mode := range []string{"profile", "follow", "close", "session", "block_unblock", "idle_extension", "foreign_proof", "changed_payload", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f := currentProfileNativeFixture(t)
			a := currentProfileNativeAccess(t, f, 2)
			p, e := f.store.ReadProfileCurrent(f.ctx, a, f.ids[0])
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "profile":
				f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[0])
				f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, f.ids[0])
			case "follow":
				// Use this actual follower's original proof, rather than an unrelated viewer.
				a = currentProfileNativeAccess(t, f, 1)
				p, e = f.store.ReadProfileCurrent(f.ctx, a, f.ids[0])
				if e != nil {
					t.Fatal(e)
				}
				if e = f.store.Unfollow(f.ctx, f.ids[1], follow.Target{Type: follow.Person, ID: f.ids[0]}); e != nil {
					t.Fatal(e)
				}
				if _, e = f.store.Follow(f.ctx, f.ids[1], follow.Target{Type: follow.Person, ID: f.ids[0]}); e != nil {
					t.Fatal(e)
				}
			case "close":
				v, e := f.store.ListOwnCloseConnectionsCurrent(f.ctx, f.access)
				if e != nil {
					t.Fatal(e)
				}
				for _, entry := range v.Connections {
					if entry.PersonID == f.ids[2] {
						_, e = f.store.DeleteOwnCloseConnectionCurrent(f.ctx, f.access, entry.PersonID, connection.CloseDelete{TieID: entry.TieID, RequestID: entry.RequestID, ExpectedRevision: entry.NativeRevision, Confirmed: true})
						if e != nil {
							t.Fatal(e)
						}
					}
				}
				f.chooseClose(2)
			case "session":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, a.SessionDigest[:])
				f.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, a.SessionDigest[:])
			case "block_unblock":
				if e = f.store.BlockAccount(f.ctx, f.ids[2], f.ids[0]); e != nil {
					t.Fatal(e)
				}
				if e = f.store.UnblockAccount(f.ctx, f.ids[2], f.ids[0]); e != nil {
					t.Fatal(e)
				}
			case "idle_extension":
				if _, e = f.store.Authenticate(f.ctx, a.SessionDigest); e != nil {
					t.Fatal(e)
				}
			case "foreign_proof":
				var restored identity.Profile
				wire, _ := json.Marshal(p)
				if json.Unmarshal(wire, &restored) != nil {
					t.Fatal("fixture wire")
				}
				p = restored
			case "changed_payload":
				p.DisplayName = "caller supplied authority"
			case "cancel":
				ctx, cancel := context.WithCancel(f.ctx)
				cancel()
				e = f.store.ValidateProfileCurrent(ctx, a, f.ids[0], p)
				if !errors.Is(e, identity.ErrProfileUnavailable) {
					t.Fatal("cancelled proof accepted", e)
				}
				return
			}
			e = f.store.ValidateProfileCurrent(f.ctx, a, f.ids[0], p)
			if mode == "idle_extension" {
				if e != nil {
					t.Fatal("normal idle refresh invalidated current original proof", e)
				}
				return
			}
			if mode == "foreign_proof" || mode == "changed_payload" {
				if !errors.Is(e, identity.ErrProfileUnavailable) {
					t.Fatal("untrusted proof/payload accepted", e)
				}
				return
			}
			if !errors.Is(e, identity.ErrConflict) {
				t.Fatal("native source returned to same visible state accepted stale proof", e)
			}
		})
	}
}
func TestCurrentProfileNativeOriginalDeadlineCannotRenew(t *testing.T) {
	f := currentProfileNativeFixture(t)
	a := currentProfileNativeAccess(t, f, 2)
	f.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1500 milliseconds' WHERE token_sha256=$1`, a.SessionDigest[:])
	p, e := f.store.ReadProfileCurrent(f.ctx, a, f.ids[0])
	if e != nil {
		t.Fatal(e)
	}
	proof := p.ResponseProof.(*currentProfileProof)
	if _, e = f.store.Authenticate(f.ctx, a.SessionDigest); e != nil {
		t.Fatal(e)
	}
	// Actual DB wall time moves beyond the captured original idle deadline,
	// while Authenticate has extended the live Session. It cannot renew p.
	if _, e = f.pool.Exec(f.ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.05)`, proof.deadline); e != nil {
		t.Fatal(e)
	}
	if e = f.store.ValidateProfileCurrent(f.ctx, a, f.ids[0], p); !errors.Is(e, identity.ErrConflict) {
		t.Fatal("old Profile deadline renewed by current idle refresh", e)
	}
	current, e := f.store.ReadProfileCurrent(f.ctx, a, f.ids[0])
	if e != nil {
		t.Fatal("fresh explicit read unavailable", e)
	}
	if e = f.store.ValidateProfileCurrent(f.ctx, a, f.ids[0], current); e != nil {
		t.Fatal(e)
	}
}
func TestCurrentProfileNativeGrantAndBlockWithdrawal(t *testing.T) {
	for _, mode := range []string{"grant_revoke", "grant_expiry", "reverse_block", "session_revoke"} {
		t.Run(mode, func(t *testing.T) {
			f := currentProfileNativeFixture(t)
			a := currentProfileNativeAccess(t, f, 4)
			f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, f.ids[0])
			if mode == "grant_expiry" {
				f.exec(`UPDATE consent_grants SET expires_at=clock_timestamp()+interval '1200 milliseconds' WHERE owner_account_id=$1 AND recipient_account_id=$2`, f.ids[0], f.ids[4])
			}
			p, e := f.store.ReadProfileCurrent(f.ctx, a, f.ids[0])
			if e != nil {
				t.Fatal(e)
			}
			switch mode {
			case "grant_revoke":
				var id string
				if e = f.pool.QueryRow(f.ctx, `SELECT id FROM consent_grants WHERE owner_account_id=$1 AND recipient_account_id=$2 AND revoked_at IS NULL`, f.ids[0], f.ids[4]).Scan(&id); e != nil {
					t.Fatal(e)
				}
				if e = f.store.RevokeProfileGrant(f.ctx, f.ids[0], id); e != nil {
					t.Fatal(e)
				}
			case "grant_expiry":
				if _, e = f.pool.Exec(f.ctx, `SELECT pg_sleep(1.3)`); e != nil {
					t.Fatal(e)
				}
			case "reverse_block":
				if e = f.store.BlockAccount(f.ctx, f.ids[4], f.ids[0]); e != nil {
					t.Fatal(e)
				}
			case "session_revoke":
				if e = f.store.RevokeSession(f.ctx, a.SessionDigest); e != nil {
					t.Fatal(e)
				}
			}
			e = f.store.ValidateProfileCurrent(f.ctx, a, f.ids[0], p)
			want := identity.ErrNotFound
			if mode == "session_revoke" {
				want = identity.ErrUnauthorized
			}
			if !errors.Is(e, want) {
				t.Fatal("withdrawn Profile returned", e, want)
			}
		})
	}
}
