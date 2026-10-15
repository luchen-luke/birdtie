//go:build windows

package legacyimagerecovery

import (
	"context"
	"os/exec"
	"syscall"
	"unsafe"
)

var imageKernel = syscall.NewLazyDLL("kernel32.dll")
var imageCreateJob = imageKernel.NewProc("CreateJobObjectW")
var imageSetJob = imageKernel.NewProc("SetInformationJobObject")
var imageAssignJob = imageKernel.NewProc("AssignProcessToJobObject")
var imageOpenProcess = imageKernel.NewProc("OpenProcess")
var imageCloseHandle = imageKernel.NewProc("CloseHandle")
var imageResumeProcess = syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess")

type imageJobLimits struct {
	ProcessTime, JobTime                                       int64
	Flags                                                      uint32
	MinWorkingSet, MaxWorkingSet                               uintptr
	ActiveProcesses                                            uint32
	Affinity                                                   uintptr
	Priority, Scheduling                                       uint32
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

func validatePlatform(v *Normalizer) error { return nil }
func platformCommand(ctx context.Context, v *Normalizer, program string, args []string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	// Job assignment precedes execution of any input decoder.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000 | 0x00000004}
	return cmd, nil
}
func platformStarted(cmd *exec.Cmd) (func(), error) {
	job, _, _ := imageCreateJob.Call(0, 0)
	if job == 0 {
		return func() {}, ErrUnavailable
	}
	cleanup := func() { imageCloseHandle.Call(job) }
	limits := imageJobLimits{Flags: 0x2000 | 0x100 | 0x2, ProcessMemory: (512 * 1024 * 1024), ProcessTime: 180 * 10000000}
	ok, _, _ := imageSetJob.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	process, _, _ := imageOpenProcess.Call(0x0100|0x0001|0x0400|0x0800, 0, uintptr(cmd.Process.Pid))
	if process == 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	defer imageCloseHandle.Call(process)
	ok, _, _ = imageAssignJob.Call(job, process)
	if ok == 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	status, _, _ := imageResumeProcess.Call(process)
	if status != 0 {
		cleanup()
		return func() {}, ErrUnavailable
	}
	return cleanup, nil
}
func platformMemoryExceeded(pid int) bool { return false } // Job Object enforces the hard private-memory bound.
