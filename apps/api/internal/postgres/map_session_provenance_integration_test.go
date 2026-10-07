package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/mapprojection"
	"github.com/jackc/pgx/v5/pgconn"
)

// Original native fixtures and SQL, exclusively owned synthetic identities.
// The worker only compiles; root executes on its isolated DB after migration111.
func mapSessionProvenanceFixture(t *testing.T) (*placeMemoryFixture, mp.Query, mp.Access) {
	t.Helper()
	f, q := mapProjectionFixture(t)
	b := f.private.base
	var installed bool
	if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema='public' AND table_name='sessions' AND column_name='authorization_generation')`).Scan(&installed); e != nil || !installed {
		t.Fatal("requires original session migration111", e)
	}
	// The normal fixture starts at one-hour idle. Normal Authenticate renews to
	// thirty minutes, so establish a still-live five-minute original window
	// before capture to exercise a true extension instead of a shortening.
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '5 minutes' WHERE id=$1`, f.private.ownerSession)
	a := mp.Access{Actor: identity.Actor{ID: b.person.ID, AccountType: "person"}, Digest: f.private.owner.SessionDigest}
	return f, q, a
}

func mapSessionNativeGeneration(t *testing.T, f *placeMemoryFixture) int64 {
	t.Helper()
	var generation int64
	b := f.private.base
	if e := b.pool.QueryRow(b.ctx, `SELECT authorization_generation FROM sessions WHERE id=$1`, f.private.ownerSession).Scan(&generation); e != nil {
		t.Fatal("native generation read", e)
	}
	return generation
}

func mapSessionLegacyProof(t *testing.T, f *placeMemoryFixture, q mp.Query, a mp.Access) string {
	t.Helper()
	b := f.private.base
	// The old actual native token shape reproduces the defect after a real
	// Authenticate update. All original source/session SQL predicates remain.
	query := strings.Replace(mapProjectionSQL(q.Private), mapProjectionSessionAuthoritySQL, `jsonb_build_array(a.id,a.xmin::text,se.id,se.xmin::text,se.created_at,se.expires_at,se.idle_expires_at)`, 1)
	var at, until time.Time
	var raw, human []byte
	var truncated, current bool
	var proof string
	if e := b.pool.QueryRow(b.ctx, query, q.CityID, q.West, q.South, q.East, q.North, a.Actor.ID, a.Digest[:], b.store.devPhoneEnabled).Scan(&at, &until, &raw, &truncated, &proof, &current, &human); e != nil || !current {
		t.Fatal("original proof source read", e)
	}
	return proof
}

func TestMapSessionProvenanceNativeOriginalAuthenticateRefreshIntegration(t *testing.T) {
	for _, private := range []bool{false, true} {
		name := "public"
		if private {
			name = "own_private"
		}
		t.Run(name, func(t *testing.T) {
			f, q, a := mapSessionProvenanceFixture(t)
			q.Private = private
			b := f.private.base
			r, e := b.store.ReadMapLayers(b.ctx, a, q)
			if e != nil {
				t.Fatal("initial current map", e)
			}
			legacyBefore := mapSessionLegacyProof(t, f, q, a)
			generation := mapSessionNativeGeneration(t, f)
			for i := 0; i < 3; i++ {
				actor, e := b.store.AuthenticateHumanSocial(b.ctx, a.Digest)
				if e != nil || actor.ID != a.Actor.ID {
					t.Fatal("actual original human auth refresh", e)
				}
				if mapSessionNativeGeneration(t, f) != generation {
					t.Fatal("ordinary live idle extension changed authorization generation")
				}
				if e = b.store.RevalidateMapLayers(b.ctx, a, r); e != nil {
					t.Fatal("normal parallel auth refresh retired original current map", e)
				}
			}
			if mapSessionLegacyProof(t, f, q, a) == legacyBefore {
				t.Fatal("fixture did not actually reproduce old xmin/idle proof change")
			}
		})
	}
}

func TestMapSessionProvenanceNativeSecurityChangeRestoreNeverRevivesIntegration(t *testing.T) {
	for _, mutation := range []string{"revocation", "idle_shorten", "expires", "account", "token", "method", "created", "session_id", "account_status"} {
		t.Run(mutation, func(t *testing.T) {
			f, q, a := mapSessionProvenanceFixture(t)
			b := f.private.base
			session := f.private.ownerSession
			r, e := b.store.ReadMapLayers(b.ctx, a, q)
			if e != nil {
				t.Fatal(e)
			}
			generation := mapSessionNativeGeneration(t, f)
			var expires, idle, created time.Time
			var method string
			if e = b.pool.QueryRow(b.ctx, `SELECT expires_at,idle_expires_at,created_at,authentication_method FROM sessions WHERE id=$1`, session).Scan(&expires, &idle, &created, &method); e != nil {
				t.Fatal(e)
			}
			var restore func()
			switch mutation {
			case "revocation":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session)
				restore = func() { b.exec(`UPDATE sessions SET revoked_at=NULL WHERE id=$1`, session) }
			case "idle_shorten":
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 minute' WHERE id=$1`, session)
				restore = func() { b.exec(`UPDATE sessions SET idle_expires_at=$2 WHERE id=$1`, session, idle) }
			case "expires":
				b.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '10 minutes' WHERE id=$1`, session)
				restore = func() { b.exec(`UPDATE sessions SET expires_at=$2 WHERE id=$1`, session, expires) }
			case "account":
				b.exec(`UPDATE sessions SET account_id=$2 WHERE id=$1`, session, b.other.ID)
				restore = func() { b.exec(`UPDATE sessions SET account_id=$2 WHERE id=$1`, session, b.person.ID) }
			case "token":
				_, digest, e := identity.NewToken()
				if e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE sessions SET token_sha256=$2 WHERE id=$1`, session, digest[:])
				restore = func() { b.exec(`UPDATE sessions SET token_sha256=$2 WHERE id=$1`, session, a.Digest[:]) }
			case "method":
				b.exec(`UPDATE sessions SET authentication_method='UNIT-map-revised-method' WHERE id=$1`, session)
				restore = func() { b.exec(`UPDATE sessions SET authentication_method=$2 WHERE id=$1`, session, method) }
			case "created":
				b.exec(`UPDATE sessions SET created_at=created_at-interval '1 microsecond' WHERE id=$1`, session)
				restore = func() { b.exec(`UPDATE sessions SET created_at=$2 WHERE id=$1`, session, created) }
			case "session_id":
				var otherID string
				if e = b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()`).Scan(&otherID); e != nil {
					t.Fatal(e)
				}
				b.exec(`UPDATE sessions SET id=$2 WHERE id=$1`, session, otherID)
				restore = func() { b.exec(`UPDATE sessions SET id=$2 WHERE id=$1`, otherID, session) }
			case "account_status":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.person.ID)
				restore = func() { b.exec(`UPDATE accounts SET status='active' WHERE id=$1`, b.person.ID) }
			}
			if e = b.store.RevalidateMapLayers(b.ctx, a, r); e == nil {
				t.Fatal("changed security accepted old map")
			}
			restore()
			if e = b.store.RevalidateMapLayers(b.ctx, a, r); !errors.Is(e, mp.ErrChanged) {
				t.Fatal("restored security revived original proof", e)
			}
			if mutation != "account_status" && mapSessionNativeGeneration(t, f) <= generation {
				t.Fatal("security change lost monotonic native retirement")
			}
		})
	}
}

func TestMapSessionProvenanceNativeGenerationCannotBeForgedIntegration(t *testing.T) {
	f, _, _ := mapSessionProvenanceFixture(t)
	b := f.private.base
	before := mapSessionNativeGeneration(t, f)
	for _, statement := range []string{
		`UPDATE sessions SET authorization_generation=authorization_generation-1 WHERE id=$1`,
		`UPDATE sessions SET authorization_generation=authorization_generation+1 WHERE id=$1`,
		`INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at,authorization_generation) SELECT gen_random_uuid(),account_id,sha256(convert_to(gen_random_uuid()::text,'UTF8')),'UNIT-native-map',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '5 minutes',99 FROM sessions WHERE id=$1`,
	} {
		_, e := b.pool.Exec(b.ctx, statement, f.private.ownerSession)
		var rejected *pgconn.PgError
		if !errors.As(e, &rejected) || rejected.Code != "23514" {
			t.Fatal("caller-created generation accepted", e)
		}
		if mapSessionNativeGeneration(t, f) != before {
			t.Fatal("rejected mutation changed native counter")
		}
	}
}

func TestMapSessionProvenanceNativeOriginalLeaseCannotExtendIntegration(t *testing.T) {
	f, q, a := mapSessionProvenanceFixture(t)
	b := f.private.base
	b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '1 second' WHERE id=$1`, f.private.ownerSession)
	r, e := b.store.ReadMapLayers(b.ctx, a, q)
	if e != nil {
		t.Fatal("short current native lease", e)
	}
	var originalIdle time.Time
	if e = b.pool.QueryRow(b.ctx, `SELECT idle_expires_at FROM sessions WHERE id=$1`, f.private.ownerSession).Scan(&originalIdle); e != nil || r.View.ValidUntil.After(originalIdle) {
		t.Fatal("map outlived original current session lease", e)
	}
	if _, e = b.store.AuthenticateHumanSocial(b.ctx, a.Digest); e != nil {
		t.Fatal("live original auth extension", e)
	}
	// This tests the original captured lease after an actual native extension.
	// The only wait is bounded to one second; no server/provider is involved.
	// The duration uses the native captured window, not a host/PG clock offset.
	time.Sleep(r.View.ValidUntil.Sub(r.View.ObservedAt) + 10*time.Millisecond)
	if e = b.store.RevalidateMapLayers(b.ctx, a, r); !errors.Is(e, mp.ErrChanged) {
		t.Fatal("idle extension revived expired captured lease", e)
	}
}
