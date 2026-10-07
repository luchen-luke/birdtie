package agentcurrentcontext

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCurrentContextSnapshotClockDomains(t *testing.T) {
	issued := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	snapshot := Snapshot{ObservedAt: issued, ExpiresAt: issued.Add(time.Minute)}
	for _, tc := range []struct {
		name           string
		database, host time.Time
		want           error
	}{
		{"database issuance ahead of host", issued.Add(time.Millisecond), issued.Add(-time.Second), nil},
		{"equal database issuance", issued, issued, nil},
		{"ordinary current clocks", issued.Add(time.Second), issued.Add(time.Second), nil},
		{"future database issuance denied", issued.Add(-time.Nanosecond), issued.Add(time.Second), ErrExpired},
		{"exact database expiry denied", snapshot.ExpiresAt, issued, ErrExpired},
		{"exact host expiry denied", issued, snapshot.ExpiresAt, ErrExpired},
		{"database expiry denied with earlier host", issued.Add(2 * time.Minute), issued.Add(-time.Second), ErrExpired},
		{"host expiry denied with earlier database", issued, issued.Add(2 * time.Minute), ErrExpired},
		{"unknown database clock denied", time.Time{}, issued, ErrExpired},
		{"unknown host clock denied", issued, time.Time{}, ErrExpired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := snapshot
			if err := validateSnapshotClocks(snapshot, tc.database, tc.host); !errors.Is(err, tc.want) {
				t.Fatalf("clock boundary = %v; want %v", err, tc.want)
			}
			if !reflect.DeepEqual(snapshot, before) {
				t.Fatal("Clock check renewed or changed the original snapshot")
			}
		})
	}
}
