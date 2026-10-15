//go:build linux

package momentmedia

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Increasing the virtual mapping allowance must not relax the physical-memory
// rejection. This uses a controlled allocator, no media or private source.
func TestPublicMomentVideoRSSBudgetStillEnforced(t *testing.T) {
	ffmpeg, ffprobe, prlimit := os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFMPEG"), os.Getenv("BIRDTIE_PUBLIC_VIDEO_FFPROBE"), os.Getenv("BIRDTIE_PUBLIC_VIDEO_PRLIMIT")
	if ffmpeg == "" || ffprobe == "" || prlimit == "" {
		t.Skip("set explicit Linux codec paths for the resident-memory regression")
	}
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Fatal("controlled Linux allocator unavailable")
	}
	codec := &FFmpegVideoNormalizer{FFmpegPath: ffmpeg, FFprobePath: ffprobe, PrlimitPath: prlimit, TempRoot: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	started := time.Now()
	_, err := codec.run(ctx, "/usr/bin/python3", "-c", "import time; allocated=bytearray(320*1024*1024); time.sleep(4)")
	if !errors.Is(err, ErrInvalid) || ctx.Err() != nil || time.Since(started) >= 3*time.Second {
		t.Fatal("256 MiB resident-memory guard did not reject the controlled allocator promptly", err)
	}
	if videoPlatformMemoryExceeded(os.Getpid()) {
		t.Fatal("ordinary test process incorrectly reported over the resident-memory cap")
	}
}
