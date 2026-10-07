package postgres

import (
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"os"
	"strings"
	"testing"
	"time"
)

// This inspects the production SQL and the actual validator. It does not run
// PostgreSQL, migrations, locks, sessions or a schedule delivery.
func TestNotificationScheduleWriteClockSingleStampSQL(t *testing.T) {
	raw, err := os.ReadFile("notification_schedule.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(raw)
	start := strings.Index(source, "WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n), saved AS (")
	if start < 0 {
		t.Fatal("schedule write must materialize one shared clock value, not two volatile timestamps")
	}
	end := strings.Index(source[start:], "SELECT `+notificationScheduleColumns+` FROM saved p")
	if end < 0 {
		t.Fatal("missing actual write/scan path")
	}
	write := source[start : start+end]
	if strings.Count(write, "clock_timestamp()") != 1 || !strings.Contains(write, "SELECT $1,$2,$3,1,$4,stamp.n,$5,stamp.n FROM stamp") {
		t.Fatal("valid_from and updated_at must consume the same materialized stamp")
	}
	for _, invariant := range []string{"ON CONFLICT(owner_id) DO UPDATE", "version=native_notification_schedules.version+1", "session_id=EXCLUDED.session_id", "native_notification_schedules.version=$6", "finishNotificationScheduleEdit", "insertDomainAudit"} {
		if !strings.Contains(source, invariant) {
			t.Fatal("lost existing CAS/session/audit/final fence", invariant)
		}
	}
}

func TestNotificationScheduleWriteClockActualValidator(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{0, time.Microsecond} {
		t.Run(offset.String(), func(t *testing.T) {
			row := notificationScheduleUnitPolicyRows(now, true, nil)
			updated := row.values[3].(*time.Time).Add(offset)
			row.values[5] = &updated
			_, err := scanNotificationSchedule(row)
			if offset == 0 && err != nil {
				t.Fatal("equal stamp control", err)
			}
			if offset != 0 && err != ns.ErrUnavailable {
				t.Fatal("real validator must retain rejection of unequal stamps", err)
			}
		})
	}
}
