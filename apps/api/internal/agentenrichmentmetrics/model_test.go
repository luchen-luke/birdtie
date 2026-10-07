package agentenrichmentmetrics

import (
	"math"
	"testing"
	"time"
)

func TestEnrichmentMetricsWarningClosedExactThreshold(t *testing.T) {
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON", "ROOT", "TASK"} {
		for _, d := range []string{"REQUESTS", "INPUT_TOKENS", "OUTPUT_TOKENS", "COST_MICROS"} {
			for _, v := range []struct {
				used   int64
				status string
			}{{0, "NORMAL"}, {79, "NORMAL"}, {80, "NEAR_LIMIT"}, {99, "NEAR_LIMIT"}, {100, "EXHAUSTED"}} {
				got, e := Warning(scope, d, 100, v.used)
				if e != nil || got.Status != v.status {
					t.Fatal(got, e)
				}
			}
		}
	}
	v, e := Warning("ROOT", "COST_MICROS", math.MaxInt64, math.MaxInt64-1)
	if e != nil || v.Status != "NEAR_LIMIT" {
		t.Fatal(v, e)
	}
	for _, v := range []struct {
		s, d      string
		max, used int64
	}{{"MONTH", "REQUESTS", 1, 0}, {"ROOT", "UNKNOWN", 1, 0}, {"ROOT", "REQUESTS", 0, 0}, {"ROOT", "REQUESTS", 1, -1}, {"ROOT", "REQUESTS", 1, 2}} {
		if _, e := Warning(v.s, v.d, v.max, v.used); e != ErrInvalid {
			t.Fatal(v, e)
		}
	}
}
func TestEnrichmentMetricsClosedKindsAndBoundedWindow(t *testing.T) {
	if len(MetricKinds()) != 6 || ValidLifecycle(PolicyTriggered) || ValidLifecycle("candidate_expired") {
		t.Fatal("invented lifecycle")
	}
	for _, m := range MetricKinds()[:5] {
		if !ValidLifecycle(m) {
			t.Fatal(m)
		}
	}
	n := time.Now().UTC()
	for _, w := range []Window{{n.Add(-time.Hour), n}, {n.Add(-MaxWindow), n}} {
		if !w.Valid(n) {
			t.Fatal(w)
		}
	}
	for _, w := range []Window{{}, {n, n}, {n, n.Add(time.Second)}, {n.Add(-MaxWindow - time.Second), n}} {
		if w.Valid(n) {
			t.Fatal(w)
		}
	}
}
