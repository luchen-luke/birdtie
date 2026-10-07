package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Fault injection is confined to a newly created owned database. A sequence
// survives a rolled-back savepoint and counts actual native INSERT attempts.
// This tests recovery and atomicity, not an emulated physical system clock.
func TestAgentOutboxCaptureBoundedRetry(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var database string
	if err = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&database); err != nil || !strings.HasPrefix(database, "birdtie_owned_migration_") {
		t.Fatal("fault injection requires the newly created owned database", err)
	}
	for _, test := range []struct {
		name, code, message string
		permanent           bool
		wantAttempts        int64
	}{
		{"one_shot_capture_denial", "P0001", "native outbox capture requires current Person binding and fresh source metadata", false, 2},
		{"persistent_capture_denial", "P0001", "native outbox capture requires current Person binding and fresh source metadata", true, 3},
		{"source_denial", "P0001", "outbox capture requires exact current native Moment mutation metadata", true, 1},
		{"constraint_denial", "23514", "owned injected constraint denial", true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newOutboxFixture(t)
			b := f.private.base
			var ownerRowsBefore int
			if err := b.pool.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1`, b.person.ID).Scan(&ownerRowsBefore); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `CREATE SEQUENCE owned_capture_retry_attempts`); err != nil {
				t.Fatal(err)
			}
			faultSQL := fmt.Sprintf(`CREATE FUNCTION owned_capture_retry_fault() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE attempt bigint:=nextval('owned_capture_retry_attempts');
BEGIN IF %t OR attempt=1 THEN RAISE EXCEPTION '%s' USING ERRCODE='%s'; END IF; RETURN NEW; END $$;
CREATE TRIGGER aaa_owned_capture_retry_fault BEFORE INSERT ON agent_domain_outbox FOR EACH ROW EXECUTE FUNCTION owned_capture_retry_fault();`, test.permanent, test.message, test.code)
			if _, err := pool.Exec(ctx, faultSQL); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if _, err := pool.Exec(context.Background(), `DROP TRIGGER aaa_owned_capture_retry_fault ON agent_domain_outbox; DROP FUNCTION owned_capture_retry_fault(); DROP SEQUENCE owned_capture_retry_attempts;`); err != nil {
					t.Errorf("owned fault cleanup: %v", err)
				}
			})
			m, err := b.store.CreateMomentDraft(ctx, b.person.ID, outboxMomentInput())
			if test.permanent {
				var native *pgconn.PgError
				if !errors.As(err, &native) || native.Code != test.code || native.Message != test.message {
					t.Fatal("native denial must propagate without a successful domain write", err)
				}
			} else if err != nil {
				t.Fatal("one-shot trusted capture should recover under the original guard", err)
			}
			var attempts int64
			if err := pool.QueryRow(ctx, `SELECT last_value FROM owned_capture_retry_attempts`).Scan(&attempts); err != nil || attempts != test.wantAttempts {
				t.Fatalf("actual attempts=%d want=%d error=%v", attempts, test.wantAttempts, err)
			}
			var moments, events, audits int
			if err := pool.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM moments WHERE author_account_id=$1),
 (SELECT count(*) FROM agent_domain_outbox WHERE subject_id=$1),
 (SELECT count(*) FROM audit_events WHERE actor_account_id=$1)-$2`, b.person.ID, ownerRowsBefore).Scan(&moments, &events, &audits); err != nil {
				t.Fatal(err)
			}
			if test.permanent {
				if moments != 0 || events != 0 || audits != 0 {
					t.Fatalf("failed domain transaction leaked moments=%d events=%d audits=%d", moments, events, audits)
				}
			} else {
				if moments != 1 || events != 1 || audits != 1 {
					t.Fatalf("recovery duplicated domain writes moments=%d events=%d audits=%d", moments, events, audits)
				}
				event := f.row(t, m.ID, m.Revision).Event
				if !event.OccurredAt.Equal(m.CreatedAt) || !event.ExpiresAt.Equal(m.CreatedAt.Add(15*time.Minute)) || event.Source.ID != m.ID {
					t.Fatal("retry changed authoritative source time/TTL/ID", event)
				}
			}
		})
	}
}
