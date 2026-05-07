//go:build !linux && !windows

package supervisor

import "os/exec"

func platformAfterStart(_ *exec.Cmd) error { return nil }
