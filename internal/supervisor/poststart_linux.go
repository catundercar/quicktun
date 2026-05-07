//go:build linux

package supervisor

import "os/exec"

func platformAfterStart(_ *exec.Cmd) error { return nil }
