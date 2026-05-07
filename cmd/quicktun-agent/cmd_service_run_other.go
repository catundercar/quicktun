//go:build !windows

package main

import "github.com/spf13/cobra"

// newServiceRunCmd returns nil on non-Windows platforms. The Windows build
// provides a real implementation in cmd_service_run_windows.go.
func newServiceRunCmd() *cobra.Command { return nil }
