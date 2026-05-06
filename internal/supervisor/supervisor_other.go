//go:build !linux

package supervisor

import (
	"os"
	"syscall"
)

// termSignal is what Stop() sends. SIGTERM works on darwin/windows.
var termSignal os.Signal = syscall.SIGTERM

func platformSysProcAttr() *syscall.SysProcAttr {
	// macOS / Windows: best-effort. Process won't auto-die when parent dies.
	return &syscall.SysProcAttr{}
}
