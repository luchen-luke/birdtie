package agentnotificationschedule

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scheduleRunUnitStore struct {
	reminderCalls, digestCalls int
	reminderErr, digestErr     error
	out                        RunResult
}

func (s *scheduleRunUnitStore) EnqueueStartsSoonReminders(context.Context) (int64, error) {
	s.reminderCalls++
	return 4, s.reminderErr
}
func (s *scheduleRunUnitStore) ProcessNotificationSchedules(context.Context, int) (RunResult, error) {
	s.digestCalls++
	return s.out, s.digestErr
}
func TestNotificationScheduleUnitIndependentFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		mode int
	}{{"nil_digest", 0}, {"typed_nil_digest", 1}, {"digest_error", 2}, {"digest_valid", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			r := &scheduleRunUnitStore{}
			var d RunnerStore
			switch tc.mode {
			case 1:
				var v *scheduleRunUnitStore
				d = v
			case 2:
				d = &scheduleRunUnitStore{digestErr: errors.New("PRIVATE_CANARY")}
			case 3:
				d = &scheduleRunUnitStore{out: RunResult{Owners: 1, Slots: 1, Delivered: 2}}
			}
			out, e := RunIndependentCycle(context.Background(), r, d, 10, time.Second)
			if r.reminderCalls != 1 || !out.RemindersOK || out.Reminders != 4 {
				t.Fatal("AIR dependency must not stop reminders", r, out, e)
			}
			if tc.mode < 3 && (e != ErrUnavailable || out.DigestOK) {
				t.Fatal(e, out)
			}
			if tc.mode == 3 && (e != nil || !out.DigestOK) {
				t.Fatal(e, out)
			}
		})
	}
	r := &scheduleRunUnitStore{reminderErr: errors.New("CANARY")}
	d := &scheduleRunUnitStore{}
	out, e := RunIndependentCycle(context.Background(), r, d, 10, time.Second)
	if d.digestCalls != 1 || out.RemindersOK || !out.DigestOK || e != ErrUnavailable {
		t.Fatal("stages independent", out, e)
	}
}
func TestNotificationScheduleUnitRunnerBounds(t *testing.T) {
	for _, out := range []RunResult{{Owners: 11}, {Slots: 1}, {Owners: 1, Slots: 2}, {Owners: 1, Slots: 1, Delivered: 21}, {Discarded: -1}} {
		s := &scheduleRunUnitStore{out: out}
		if _, e := Run(context.Background(), s, 10); e != ErrUnavailable {
			t.Fatal(out, e)
		}
	}
	for _, n := range []int{0, 101} {
		s := &scheduleRunUnitStore{}
		if _, e := Run(context.Background(), s, n); e == nil || s.digestCalls != 0 {
			t.Fatal(n, e)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &scheduleRunUnitStore{}
	if _, e := Run(ctx, s, 1); e == nil || s.digestCalls != 0 {
		t.Fatal("canceled calls")
	}
}
