// dwatch tracks disk usage over time: where and when space grows.
package main

import (
	"fmt"
	"io"
	"os"

	"dwatch/internal/app"
)

var version = "0.1.0"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dwatch: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "scan":
		return app.Scan(args[1:])
	case "watch":
		return app.Watch(args[1:])
	case "report":
		return app.Report(args[1:])
	case "top":
		return app.Top(args[1:])
	case "list":
		return app.List(args[1:])
	case "prune":
		return app.Prune(args[1:])
	case "version":
		fmt.Printf("dwatch %s\n", version)
		return nil
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q (see 'dwatch help')", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `dwatch — disk usage watcher

usage:
  dwatch <command> [flags] [paths...]

commands:
  scan      scan paths and store a snapshot (default: whole disk)
  watch     keep scanning periodically; print growth as it happens
  report    diff two snapshots: where and when space grew
  top       largest directories in the latest snapshot
  list      show stored snapshots
  prune     remove old snapshots
  version   print version

run 'dwatch <command> -h' for command flags.
`)
}
