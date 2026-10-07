package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	ns "github.com/birdtie/birdtie/apps/api/internal/agentnotificationschedule"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

func TestNotificationScheduleUnitTypedInboxReuse(t *testing.T) {
	for _, kind := range []agentnotification.Kind{agentnotification.KindOpportunityAvailable, agentnotification.KindActivityReminder, agentnotification.KindActivityChange, agentnotification.KindActivityCancelled, agentnotification.KindActivityReview, agentnotification.KindPlaceReview, agentnotification.KindDirectMessage, agentnotification.KindConnectionRequest, agentnotification.KindConnectionDecision, agentnotification.KindCommunityMessage, agentnotification.KindActivityMessage, agentnotification.KindOrganizationInvitation, agentnotification.KindOrganizationMembershipChange, agentnotification.KindAgentTaskCompleted, agentnotification.KindAgentTaskFailed, agentnotification.KindBusinessClaimReview} {
		for _, mode := range []string{"old_normal", "scheduled_digest"} {
			t.Run(string(kind)+"/"+mode, func(t *testing.T) {
				tx := &messagePolicyUnitTx{}
				target := "actual-original-source-ID"
				switch kind {
				case agentnotification.KindActivityReview, agentnotification.KindPlaceReview:
					tx.rows = []messagePolicyUnitRow{{values: []any{"published"}}}
				case agentnotification.KindDirectMessage, agentnotification.KindActivityMessage:
					tx.rows = []messagePolicyUnitRow{{values: []any{&target}}}
				}
				slot := ""
				if mode == "scheduled_digest" {
					slot = "actual-slot"
				}
				yes, e := deliverNativeNotificationDecision(context.Background(), tx, "decision", kind, "source", slot, false)
				if e != nil || !yes || len(tx.execs) != 1 {
					t.Fatal(yes, e)
				}
				q := tx.execs[0]
				if !strings.Contains(q, "ON CONFLICT(recipient_account_id,resource_type,resource_id) DO NOTHING") {
					t.Fatal("old Inbox key changed")
				}
				if mode == "old_normal" {
					if strings.Contains(q, "schedule_delivery_allowed") || len(tx.args[0]) != 8 || !strings.Contains(q, "birdtie_native_notification_visible(d)") {
						t.Fatal("099 dependency leaked into old helper")
					}
				} else {
					if !strings.Contains(q, "birdtie_notification_schedule_delivery_allowed($9,d,$10)") || len(tx.args[0]) != 10 || tx.args[0][8] != "actual-slot" {
						t.Fatal("digest must bind actual slot")
					}
				}
				if kind == agentnotification.KindActivityReminder && tx.args[0][4] != "activity_reminder" {
					t.Fatal("original target must remain")
				}
				if kind == agentnotification.KindConnectionRequest && tx.args[0][4] != "connection_request" {
					t.Fatal("original social target")
				}
			})
		}
	}
	for _, kind := range []agentnotification.Kind{"fake_tool", "unavailable_business_runtime"} {
		tx := &messagePolicyUnitTx{}
		if _, e := deliverNativeNotificationDecision(context.Background(), tx, "d", kind, "s", "slot", false); e == nil || len(tx.execs) != 0 {
			t.Fatal("unknown cannot deliver")
		}
	}
}
func TestNotificationScheduleUnitTransactionLifecycle(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                                  string
		used                                  int
		off, paused, repeated, late, empty    bool
		wantSlots, wantDelivered, wantCommits int
		want                                  error
	}{{name: "real_registered_core_spy", wantSlots: 1, wantDelivered: 1, wantCommits: 1}, {name: "empty_slot_consumed_once", empty: true, wantSlots: 1, wantCommits: 1}, {name: "rolling_quota_full", used: 3, wantSlots: 1, wantCommits: 1}, {name: "off", off: true}, {name: "origin_revoked", paused: true}, {name: "same_slot_retry", repeated: true}, {name: "late_source_policy_session_clock", late: true, want: ns.ErrChanged}} {
		t.Run(tc.name, func(t *testing.T) {
			edit := func(s *ns.Settings) {
				if tc.off {
					s.Enabled = false
				}
			}
			tx := &notificationScheduleUnitTx{messagePolicyUnitTx: messagePolicyUnitTx{rows: []messagePolicyUnitRow{{values: []any{notificationScheduleUnitOwner}}, notificationScheduleUnitPolicyRows(now, !tc.paused, edit), {values: []any{"source-xmin", "session", notificationScheduleUnitAgent, now}}}}, candidateRows: &notificationScheduleUnitRows{}}
			if tc.repeated {
				tx.rows = append(tx.rows, messagePolicyUnitRow{err: pgx.ErrNoRows})
			} else if !tc.off && !tc.paused {
				tx.rows = append(tx.rows, messagePolicyUnitRow{values: []any{"slot"}}, messagePolicyUnitRow{values: []any{tc.used}}, messagePolicyUnitRow{values: []any{!tc.late}})
			}
			if !tc.empty {
				tx.candidateRows.data = []messagePolicyUnitRow{{values: []any{"decision", string(agentnotification.KindConnectionRequest), "request"}}}
			}
			out, e := processNotificationScheduleOwnerTx(context.Background(), tx, notificationScheduleUnitOwner, false)
			if !errors.Is(e, tc.want) || out.Slots != tc.wantSlots || out.Delivered != tc.wantDelivered || tx.commits != tc.wantCommits {
				t.Fatal(out, e, tx.commits)
			}
			if len(tx.operations) < 2 || !strings.Contains(tx.operations[0], "FROM accounts") || !strings.Contains(tx.operations[0], "FOR NO KEY UPDATE") || !strings.Contains(tx.operations[1], "LOCK TABLE") {
				t.Fatal("owner before source relations; real PG concurrency NOT_RUN", tx.operations)
			}
			if tc.wantCommits == 1 {
				last := tx.execs[len(tx.execs)-1]
				if !strings.Contains(last, "SET state=$2") {
					t.Fatal("final transition after audit/current check")
				}
			}
			if tc.off || tc.paused || tc.repeated {
				for _, q := range tx.execs {
					if strings.Contains(q, "INSERT INTO inbox_items") || strings.Contains(q, "native_notification_schedule_deliveries") {
						t.Fatal("closed plan made effects")
					}
				}
			}
			if tc.used == 3 {
				for _, q := range tx.execs {
					if strings.Contains(q, "INSERT INTO inbox_items") {
						t.Fatal("quota reset")
					}
				}
			}
		})
	}
}
func TestNotificationScheduleUnitRollingBudgetAcrossChanges(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	localDates := map[string]bool{}
	for _, tc := range []struct {
		name, zone string
		revision   uint64
		off        bool
	}{{"first_zone_negative", "Pacific/Pago_Pago", 1, false}, {"timezone_edit_positive", "Pacific/Kiritimati", 2, false}, {"off_between", "Pacific/Kiritimati", 3, true}, {"reenable_CAS", "UTC", 4, false}} {
		t.Run(tc.name, func(t *testing.T) {
			settings := notificationScheduleUnitSettings()
			settings.TimeZone = tc.zone
			settings.LocalMinute = 0
			settings.Enabled = !tc.off
			raw, _ := json.Marshal(settings)
			row := notificationScheduleUnitPolicyRows(now, true, nil)
			row.values[0] = tc.revision
			row.values[2] = raw
			tx := &notificationScheduleUnitTx{messagePolicyUnitTx: messagePolicyUnitTx{rows: []messagePolicyUnitRow{{values: []any{notificationScheduleUnitOwner}}, row, {values: []any{"1234", "session", notificationScheduleUnitAgent, now}}}}}
			if !tc.off {
				tx.rows = append(tx.rows, messagePolicyUnitRow{values: []any{"slot"}}, messagePolicyUnitRow{values: []any{3}}, messagePolicyUnitRow{values: []any{true}})
			}
			out, e := processNotificationScheduleOwnerTx(context.Background(), tx, notificationScheduleUnitOwner, false)
			if e != nil || out.Delivered != 0 {
				t.Fatal("quota was reset by native caller", out, e)
			}
			if tc.off {
				if out.Slots != 0 || tx.commits != 0 {
					t.Fatal("off must make no slot")
				}
				return
			}
			if out.Slots != 1 || tx.commits != 1 {
				t.Fatal("logical quota-full slot consumed", out, tx.commits)
			}
			for i, q := range tx.rowSQL {
				if strings.Contains(q, "INSERT INTO native_notification_schedule_slots") {
					a := tx.queryArgs[i]
					localDates[a[5].(string)] = true
					if a[0] != notificationScheduleUnitOwner || a[3] != tc.revision {
						t.Fatal("slot exact owner/revision")
					}
				}
				if strings.Contains(q, "count(*) FROM native_notification_schedule_deliveries") {
					if !strings.Contains(q, "interval '24 hours'") || strings.Contains(q, "local_date") || strings.Contains(q, "schedule_version") || tx.queryArgs[i][0] != notificationScheduleUnitOwner {
						t.Fatal("quota key must not use mutable setting")
					}
				}
			}
			for _, q := range tx.execs {
				if strings.Contains(q, "INSERT INTO inbox_items") {
					t.Fatal("no contact while quota full")
				}
			}
		})
	}
	if len(localDates) != 2 {
		t.Fatal("test must actually cross local dates", localDates)
	}
	for _, tc := range [][3]int{{3, 0, 3}, {3, 3, 0}, {3, 8, 0}, {20, 19, 1}, {0, 0, 0}, {21, 0, 0}, {3, -1, 0}} {
		if notificationScheduleAllowance(tc[0], tc[1]) != tc[2] {
			t.Fatal(tc)
		}
	}
	// SQL is the actual production rolling receipt window. This unit assertion
	// checks the contract, not PostgreSQL execution or current private facts.
}
