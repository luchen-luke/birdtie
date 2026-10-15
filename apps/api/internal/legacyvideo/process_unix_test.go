//go:build linux

package legacyvideo

import (
	"context"
	"os/exec"
	"runtime"
	"testing"
	"time"
)

func TestPrivateVideoLinuxStatmBoundaryAndNormalExit(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		page int
		fail bool
	}{
		{"exit_mm_zero", "0 0 0 0 0 0 0\n", 4096, false},
		{"exact512MiB", "200000 131072 100 2 0 100 0\n", 4096, false},
		{"onePageOver", "200000 131073 100 2 0 100 0\n", 4096, true},
		{"largePageExact", "20000 8192 100 2 0 100 0", 65536, false},
		{"largePageOver", "20000 8193 100 2 0 100 0", 65536, true},
		{"overflowCounter", "1 18446744073709551615 0 0 0 0 0", 4096, true},
		{"invalidCounter", "1 -1 0 0 0 0 0", 4096, true},
		{"corruptOtherField", "1 0 invalid 0 0 0 0", 4096, true},
		{"missing", "", 4096, true},
		{"missingField", "1 0 0 0 0 0", 4096, true},
		{"invalidPageSize", "0 0 0 0 0 0 0", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := statmMemoryExceeded([]byte(tc.raw), tc.page); got != tc.fail {
				t.Fatalf("fail closed = %v, want %v", got, tc.fail)
			}
		})
	}
	// Observe real procfs during short-lived processes, including the interval
	// between exit_mm and reaping. No private media, services or DB are involved.
	for i := 0; i < 128; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		cmd := exec.CommandContext(ctx, "/bin/true")
		if err := cmd.Start(); err != nil {
			cancel()
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
	observing:
		for {
			select {
			case err := <-done:
				cancel()
				if err != nil {
					t.Fatalf("normal child exit: %v", err)
				}
				break observing
			default:
			}
			if platformMemoryExceeded(cmd.Process.Pid) {
				_ = cmd.Process.Kill()
				<-done
				cancel()
				t.Fatalf("normal child %d incorrectly rejected as memory-bound", i)
			}
			runtime.Gosched()
		}
	}
}
