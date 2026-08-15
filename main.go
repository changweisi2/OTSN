// otsn tracks disk usage over time: where and when space grows.
package main

import (
	"fmt"
	"io"
	"os"

	"otsn/internal/app"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "otsn: "+err.Error())
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
	case "report":
		return app.Report(args[1:])
	case "top":
		return app.Top(args[1:])
	case "list":
		return app.List(args[1:])
	case "prune":
		return app.Prune(args[1:])
	case "serve":
		return app.Serve(args[1:])
	case "version":
		fmt.Printf("otsn %s\n", app.Version)
		return nil
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q (see 'otsn help')", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `otsn — disk usage tracker

usage:
  otsn <command> [flags] [paths...]

commands:
  scan      scan paths and store a snapshot (default: whole disk)
  report    diff two snapshots: where and when space grew
  top       largest directories in the latest snapshot
  list      show stored snapshots
  prune     remove old snapshots
  serve     local web dashboard (http://127.0.0.1:8787)
  version   print version

run 'otsn <command> -h' for command flags.
`)
}
