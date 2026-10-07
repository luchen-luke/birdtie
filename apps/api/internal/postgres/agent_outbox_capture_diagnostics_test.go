package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This diagnostic has its own database. It preserves every production guard
// predicate; observing one actual PG timestamp at each clock predicate gives
// an exact reason when the mixed original error rejects a native capture.
func TestAgentOutboxCaptureDiagnostics(t *testing.T) {
	ownedMigrationDatabase(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var name, original string
	if err = pool.QueryRow(ctx, `SELECT current_database(),pg_get_functiondef('public.birdtie_guard_agent_outbox()'::regprocedure)`).Scan(&name, &original); err != nil || !strings.HasPrefix(name, "birdtie_owned_migration_") {
		t.Fatal("diagnostic must run only inside its own created database", err)
	}
	clockObservers := `CREATE FUNCTION owned_outbox_receipt_future(value timestamptz) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE at timestamptz:=clock_timestamp(); result boolean:=value>at;
BEGIN PERFORM set_config('birdtie.diagnostic.receipt_clock',jsonb_build_object('at',at,'received',value,'future',result)::text,true); RETURN result; END $$;
CREATE FUNCTION owned_outbox_source_expired(value timestamptz) RETURNS boolean LANGUAGE plpgsql AS $$
DECLARE at timestamptz:=clock_timestamp(); result boolean:=value<=at;
BEGIN PERFORM set_config('birdtie.diagnostic.expiry_clock',jsonb_build_object('at',at,'expires',value,'expired',result)::text,true); RETURN result; END $$;`
	if _, err = pool.Exec(ctx, clockObservers); err != nil {
		t.Fatal(err)
	}
	needle := "RAISE EXCEPTION 'native outbox capture requires current Person binding and fresh source metadata';"
	// Later Memory/Preference branches have the same clock predicates. Target
	// only the original Moment block and leave the full installed guard intact.
	needleAt := strings.Index(original, needle)
	if needleAt < 0 || strings.Count(original, needle) != 1 { t.Fatal("unique native Moment denial required") }
	// Anchor on the unique original denial, independent of SQL CRLF/LF layout.
	start := strings.LastIndex(original[:needleAt], "IF TG_OP='INSERT' THEN")
	end := needleAt + len(needle)
	if start < 0 || end <= start {
		t.Fatal("diagnostic requires the exact original Moment capture block")
	}
	block := original[start:end]
	if strings.Count(block, "NEW.received_at>clock_timestamp()") != 1 || strings.Count(block, "NEW.expires_at<=clock_timestamp()") != 1 {
		t.Fatal("diagnostic requires the exact current capture guard")
	}
	observedBlock := strings.Replace(block, "NEW.received_at>clock_timestamp()", "owned_outbox_receipt_future(NEW.received_at)", 1)
	observedBlock = strings.Replace(observedBlock, "NEW.expires_at<=clock_timestamp()", "owned_outbox_source_expired(NEW.expires_at)", 1)
	observedBlock = strings.Replace(observedBlock, needle, `RAISE EXCEPTION 'native outbox capture requires current Person binding and fresh source metadata' USING DETAIL=jsonb_build_object(
 'receiptCheck',current_setting('birdtie.diagnostic.receipt_clock',true),
 'expiryCheck',current_setting('birdtie.diagnostic.expiry_clock',true),
 'controlOK',NEW.delivery_state='PENDING' AND NEW.attempt=0 AND NEW.fence=0 AND NEW.lease_owner IS NULL AND NEW.lease_until IS NULL,
 'bindingOK',EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 WHERE a.id=NEW.subject_id AND a.account_type='person' AND a.status='active' AND ag.id=NEW.agent_id AND ag.agent_type='personal' AND ag.status='active'))::text;`, 1)
	observed := original[:start] + observedBlock + original[end:]
	if _, err = pool.Exec(ctx, observed); err != nil {
		t.Fatal("owned diagnostic instrumentation", err)
	}
	t.Run("future_receipt_rejected", func(t *testing.T) {
		f := placeMemoryNativeFixture(t)
		b := f.private.base
		m := f.moment(t)
		_, err := b.pool.Exec(b.ctx, `INSERT INTO agent_domain_outbox
 (event_id,event_type,tenant_id,subject_id,actor_id,agent_id,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,occurred_at,received_at,expires_at)
 SELECT gen_random_uuid(),event_type,tenant_id,subject_id,actor_id,agent_id,source_id,source_revision,source_fingerprint,source_status,logical_operation_id,root_trace_id,occurred_at,clock_timestamp()+interval '1 second',expires_at
 FROM agent_domain_outbox WHERE source_id=$1`, m.ID)
		var native *pgconn.PgError
		if !errors.As(err, &native) || native.Code != "P0001" {
			t.Fatal("future receipt escaped original native guard", err)
		}
		var detail struct {
			ReceiptCheck string `json:"receiptCheck"`
			BindingOK    bool   `json:"bindingOK"`
			ControlOK    bool   `json:"controlOK"`
		}
		var receipt struct {
			Future bool `json:"future"`
		}
		if json.Unmarshal([]byte(native.Detail), &detail) != nil || json.Unmarshal([]byte(detail.ReceiptCheck), &receipt) != nil || !receipt.Future || !detail.BindingOK || !detail.ControlOK {
			t.Fatal("diagnostic did not identify the actual denied predicate", native.Detail)
		}
		t.Log("future receipt rejected by the original predicate; binding and control remain valid")
	})
	for batch := range 10 {
		t.Run(fmt.Sprintf("native_batch_%02d", batch), func(t *testing.T) {
			f := placeMemoryNativeFixture(t)
			b := f.private.base
			input := outboxMomentInput()
			input.CityID, input.PlaceID, input.LocationPrecision = f.city, f.place, "place"
			claimed := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
			input.OccurredAt = &claimed
			for index := range 400 {
				_, err := b.store.CreateMomentDraft(b.ctx, b.person.ID, input)
				if err != nil {
					var native *pgconn.PgError
					if errors.As(err, &native) {
						t.Fatalf("native capture batch=%d index=%d code=%s message=%s detail=%s", batch, index, native.Code, native.Message, native.Detail)
					}
					t.Fatalf("native capture batch=%d index=%d error=%v", batch, index, err)
				}
			}
			t.Log("400 actual native captures; each preserves the original denial predicates")
		})
	}
}
