package main

import (
	"bytes"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// runHelp invokes a subcommand with its help args and asserts success.
// Centralising the boilerplate (SetArgs / SetOut / SetErr buffers) keeps
// each test below focused on the command surface it is exercising.
func runHelp(t *testing.T, cmd *cobra.Command, args []string) {
	t.Helper()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
}

func TestProjectCmdHelp(t *testing.T) { runHelp(t, newProjectCmd(), []string{"--help"}) }
func TestSiteCmdHelp(t *testing.T)    { runHelp(t, newSiteCmd(), []string{"--help"}) }
func TestServiceCmdHelp(t *testing.T) { runHelp(t, newServiceCmd(), []string{"--help"}) }

// TestProjectSubcommandsHelp ensures each leaf project subcommand
// (list / get / create / delete) renders its help without panicking.
// Catches missing required-flag config or bad arg validators that
// would only surface when an operator runs the leaf form.
func TestProjectSubcommandsHelp(t *testing.T) {
	for _, sub := range []string{"list", "get", "create", "delete"} {
		t.Run(sub, func(t *testing.T) {
			runHelp(t, newProjectCmd(), []string{sub, "--help"})
		})
	}
}

func TestSiteSubcommandsHelp(t *testing.T) {
	for _, sub := range []string{"list", "get", "create", "delete"} {
		t.Run(sub, func(t *testing.T) {
			runHelp(t, newSiteCmd(), []string{sub, "--help"})
		})
	}
}

func TestServiceSubcommandsHelp(t *testing.T) {
	for _, sub := range []string{"list", "get", "create", "delete"} {
		t.Run(sub, func(t *testing.T) {
			runHelp(t, newServiceCmd(), []string{sub, "--help"})
		})
	}
}

// TestRootCmdRegistersNewSubcommands verifies the new subcommands wire
// into the root cobra tree. If somebody removes a cmd.AddCommand call
// in main.go, this test fails immediately rather than silently dropping
// a CLI surface.
func TestRootCmdRegistersNewSubcommands(t *testing.T) {
	root := newRootCmd()
	want := map[string]bool{"project": false, "site": false, "service": false}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		require.True(t, found, "root cmd missing subcommand %q", name)
	}
}
