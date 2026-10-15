//go:build !windows

package legacyimagerecovery

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func validatePlatform(n *Normalizer) error {
	v, e := os.Stat(n.PrlimitPath)
	if runtime.GOOS != "linux" || !filepath.IsAbs(n.PrlimitPath) || e != nil || !v.Mode().IsRegular() {
		return ErrUnavailable
	}
	return nil
}
func platformCommand(ctx context.Context, n *Normalizer, p string, args []string) (*exec.Cmd, error) {
	if validatePlatform(n) != nil {
		return nil, ErrUnavailable
	}
	limits := []string{"--as=1073741824", "--cpu=90", "--nofile=64", "--fsize=67174400", "--", p}
	cmd := exec.CommandContext(ctx, n.PrlimitPath, append(limits, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = time.Second
	return cmd, nil
}
func platformStarted(c *exec.Cmd) (func(), error) {
	return func() {
		if c.Process != nil {
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
	}, nil
}

// Linux RLIMIT_RSS is not an enforced resident-memory boundary. The process
// supervisor reads resident pages and kills the whole job when it crosses512MiB;
// RLIMIT_AS separately remains a hard1GiB virtual-address ceiling.
func platformMemoryExceeded(pid int) bool {
	raw, e := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if e != nil {
		missing := os.IsNotExist(e) || errors.Is(e, syscall.ESRCH)
		return !(missing && syscall.Kill(pid, 0) == syscall.ESRCH)
	}
	return statmMemoryExceeded(raw, os.Getpagesize())
}

func statmMemoryExceeded(raw []byte, pageSize int) bool {
	fields := strings.Fields(string(raw))
	if len(fields) != 7 || pageSize <= 0 {
		return true
	}
	var resident uint64
	for i, field := range fields {
		v, e := strconv.ParseUint(field, 10, 64)
		if e != nil {
			return true
		}
		if i == 1 {
			resident = v
		}
	}
	// Unlike status's optional VmRSS, statm supplies all seven counters even
	// after exit_mm: resident is then zero while State may still be R. Compare
	// pages without multiplication so an oversized counter cannot overflow.
	return resident > uint64(512*1024*1024)/uint64(pageSize)
}
