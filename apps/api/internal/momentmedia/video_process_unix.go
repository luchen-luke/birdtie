//go:build !windows

package momentmedia

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func validateVideoPlatform(v *FFmpegVideoNormalizer) error {
	if runtime.GOOS != "linux" {
		return ErrUnavailable
	}
	if !filepath.IsAbs(v.PrlimitPath) {
		return ErrUnavailable
	}
	info, e := os.Stat(v.PrlimitPath)
	if e != nil || !info.Mode().IsRegular() {
		return ErrUnavailable
	}
	return nil
}
func videoPlatformCommand(ctx context.Context, v *FFmpegVideoNormalizer, program string, args []string) (*exec.Cmd, func(), error) {
	if validateVideoPlatform(v) != nil {
		return nil, func() {}, ErrUnavailable
	}
	limits := []string{"--as=536870912", "--rss=268435456", "--cpu=60", "--nofile=64", "--fsize=23068672", "--", program}
	cmd := exec.CommandContext(ctx, v.PrlimitPath, append(limits, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 1000000000
	return cmd, func() {}, nil
}
func videoPlatformStarted(cmd *exec.Cmd) (func(), error) {
	return func() {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
	}, nil
}
func videoPlatformMemoryExceeded(pid int) bool {
	raw, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			parts := strings.Fields(line)
			if len(parts) != 3 {
				return true
			}
			kb, e := strconv.ParseInt(parts[1], 10, 64)
			return e != nil || kb > VideoProcessMemory/1024
		}
	}
	return false
}
