//go:build !linux && !windows

package supervisor

import (
	"os"
	"syscall"
)

// termSignal is what Stop() sends. SIGTERM works on darwin.
var termSignal os.Signal = syscall.SIGTERM

func platformSysProcAttr() *syscall.SysProcAttr {
	// macOS: best-effort. Process won't auto-die when parent dies.
	return &syscall.SysProcAttr{}
}
