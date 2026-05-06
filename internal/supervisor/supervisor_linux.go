//go:build linux

package supervisor

import (
	"os"
	"syscall"
)

var termSignal os.Signal = syscall.SIGTERM

func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
		Setpgid:   true,
	}
}
