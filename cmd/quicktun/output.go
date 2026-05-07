package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// printTable writes rows aligned via tabwriter to stdout. The first row
// is a header; tabwriter handles column alignment so the human-readable
// table renders without manual padding.
func printTable(headers []string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	_ = w.Flush()
}

// printJSON writes v as indented JSON to stdout. Used by every list/get
// when --json is set so operators can pipe output into jq / scripts.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// renderOrJSON picks output format from `--json` flag (managed by caller).
// Pass humanFn to do a table-style render when not JSON. Keeping the
// branch in one place means every list/get command picks the same
// format without each one re-implementing the if/else.
func renderOrJSON(asJSON bool, jsonValue any, humanFn func()) error {
	if asJSON {
		return printJSON(jsonValue)
	}
	humanFn()
	return nil
}

// errStream returns where command errors / interactive prompts should go.
// Centralising this lets tests redirect stderr without reaching across
// every command file.
func errStream() io.Writer { return os.Stderr }
