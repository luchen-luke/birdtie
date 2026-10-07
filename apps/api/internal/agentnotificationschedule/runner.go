package agentnotificationschedule

import (
	"context"
	"reflect"
	"time"
)

type RunResult struct {
	Owners    int `json:"owners"`
	Slots     int `json:"slots"`
	Delivered int `json:"delivered"`
	Discarded int `json:"discarded"`
}
type RunnerStore interface {
	ProcessNotificationSchedules(context.Context, int) (RunResult, error)
}
type ReminderStore interface {
	EnqueueStartsSoonReminders(context.Context) (int64, error)
}
type CycleResult struct {
	Reminders   int64
	Digest      RunResult
	RemindersOK bool
	DigestOK    bool
}

func nilPort(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
func Run(ctx context.Context, s RunnerStore, owners int) (RunResult, error) {
	if ctx == nil || ctx.Err() != nil || nilPort(s) || owners < 1 || owners > MaxOwnersPerRun {
		return RunResult{}, ErrUnavailable
	}
	out, e := s.ProcessNotificationSchedules(ctx, owners)
	if e != nil || ctx.Err() != nil {
		return RunResult{}, ErrUnavailable
	}
	if out.Owners < 0 || out.Owners > owners || out.Slots < 0 || out.Slots > out.Owners || out.Delivered < 0 || out.Delivered > out.Slots*MaxContactsPerDay || out.Discarded < 0 {
		return RunResult{}, ErrUnavailable
	}
	return out, nil
}

// CLI uses this real two-stage path. A failed digest never suppresses the
// deterministic activity reminder; its separate old CLI/API also stay intact.
func RunIndependentCycle(ctx context.Context, r ReminderStore, s RunnerStore, owners int, deadline time.Duration) (CycleResult, error) {
	var out CycleResult
	if ctx == nil || owners < 1 || owners > MaxOwnersPerRun || deadline < time.Second || deadline > 30*time.Second {
		return out, ErrInvalid
	}
	rc, cancel := context.WithTimeout(ctx, deadline)
	var n int64
	var e error
	if nilPort(r) {
		e = ErrUnavailable
	} else {
		n, e = r.EnqueueStartsSoonReminders(rc)
	}
	out.RemindersOK = e == nil && rc.Err() == nil
	cancel()
	if out.RemindersOK {
		out.Reminders = n
	}
	dc, cancel := context.WithTimeout(ctx, deadline)
	out.Digest, e = Run(dc, s, owners)
	out.DigestOK = e == nil
	cancel()
	if !out.RemindersOK || !out.DigestOK {
		return out, ErrUnavailable
	}
	return out, nil
}
