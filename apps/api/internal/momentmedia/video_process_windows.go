//go:build windows

package momentmedia

import (
	"context"
	"os/exec"
	"syscall"
	"unsafe"
)

var videoKernel = syscall.NewLazyDLL("kernel32.dll")
var videoCreateJob = videoKernel.NewProc("CreateJobObjectW")
var videoSetJob = videoKernel.NewProc("SetInformationJobObject")
var videoAssignJob = videoKernel.NewProc("AssignProcessToJobObject")
var videoOpenProcess = videoKernel.NewProc("OpenProcess")
var videoCloseHandle = videoKernel.NewProc("CloseHandle")
var videoResumeProcess = syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess")

type videoJobLimits struct {
	ProcessTime, JobTime                                       int64
	Flags                                                      uint32
	MinWorkingSet, MaxWorkingSet                               uintptr
	ActiveProcesses                                            uint32
	Affinity                                                   uintptr
	Priority, Scheduling                                       uint32
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

func validateVideoPlatform(v *FFmpegVideoNormalizer) error { return nil }
func videoPlatformCommand(ctx context.Context, v *FFmpegVideoNormalizer, program string, args []string) (*exec.Cmd, func(), error) {
	cmd := exec.CommandContext(ctx, program, args...)
	// Job assignment precedes execution of any input decoder.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000 | 0x00000004}
	return cmd, func() {}, nil
}
func videoPlatformStarted(cmd *exec.Cmd) (func(), error) {
	job, _, _ := videoCreateJob.Call(0, 0)
	if job == 0 {
		return func() {}, ErrUnavailable
	}
	cleanup := func() { videoCloseHandle.Call(job) }
	limits := videoJobLimits{Flags: 0x2000 | 0x100 | 0x2, ProcessMemory: VideoProcessMemory, ProcessTime: 60 * 10000000}
	ok, _, _ := videoSetJob.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	process, _, _ := videoOpenProcess.Call(0x0100|0x0001|0x0400|0x0800, 0, uintptr(cmd.Process.Pid))
	if process == 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	defer videoCloseHandle.Call(process)
	ok, _, _ = videoAssignJob.Call(job, process)
	if ok == 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	status, _, _ := videoResumeProcess.Call(process)
	if status != 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	return cleanup, nil
}
func videoPlatformMemoryExceeded(pid int) bool { return false } // Job Object enforces the hard private-memory bound.
