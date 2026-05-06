// Build-only helper used by supervisor_test.go. Not compiled into production
// binaries (lives in a sub-directory the regular package doesn't import).
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mode := flag.String("mode", "sleep", "sleep | crash | exit-fast")
	flag.Parse()

	switch *mode {
	case "sleep":
		fmt.Fprintln(os.Stdout, "fake: ready")
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT)
		<-ch
		fmt.Fprintln(os.Stdout, "fake: stopping")
		os.Exit(0)
	case "crash":
		fmt.Fprintln(os.Stderr, "fake: crashing")
		os.Exit(1)
	case "exit-fast":
		time.Sleep(50 * time.Millisecond)
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "fake: unknown mode")
		os.Exit(2)
	}
}
