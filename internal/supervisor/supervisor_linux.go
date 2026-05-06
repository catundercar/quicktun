//go:build linux

package supervisor

import "syscall"

var termSignal = syscall.SIGTERM

func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM,
		Setpgid:   true,
	}
}
