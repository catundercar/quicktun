//go:build windows

package supervisor

import (
	"fmt"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobHandle is a process-wide Job Object handle. Children assigned to it die
// when the agent process exits (KILL_ON_JOB_CLOSE). Best-effort: if the job
// can't be created, the supervisor still works without parent-death linkage.
var (
	jobOnce   sync.Once
	jobHandle windows.Handle
)

func ensureJob() {
	jobOnce.Do(func() {
		h, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		_, err = windows.SetInformationJobObject(
			h,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		)
		if err != nil {
			windows.CloseHandle(h)
			return
		}
		jobHandle = h
	})
}

// platformAfterStart assigns the just-started child to the agent's Job Object
// so it dies when the agent dies. There's a tiny race between cmd.Start and
// AssignProcessToJobObject; if the child crashes inside that window it just
// won't be killed by the job (which is the same as no job — best-effort).
func platformAfterStart(cmd *exec.Cmd) error {
	ensureJob()
	if jobHandle == 0 || cmd.Process == nil {
		return nil
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		return fmt.Errorf("open child process: %w", err)
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(jobHandle, h); err != nil {
		return fmt.Errorf("assign to job: %w", err)
	}
	return nil
}
