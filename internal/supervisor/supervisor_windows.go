//go:build windows

package supervisor

import (
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

var termSignal os.Signal = os.Interrupt

func platformSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
}
