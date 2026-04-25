package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const usage = `Wire — self-hosted feed reader

Usage:
  wire [serve|migrate] [flags]

Commands:
  serve     Run the HTTP server (default).
  migrate   Apply database migrations and exit.

Environment:
  WIRE_DB_PATH                    SQLite path (default ./wire.db)
  WIRE_LISTEN                     HTTP listen address (default :8080)
  WIRE_LOG_LEVEL                  debug|info|warn|error (default info)
  WIRE_LOG_FORMAT                 json|text (default json)
  WIRE_HONKER_EXTENSION_PATH      Honker SQLite extension cdylib (default ./build/libhonker_ext.so)
`

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		fmt.Print(usage)
		return
	}
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	var err error
	switch cmd {
	case "serve":
		err = serve(ctx)
	case "migrate":
		err = migrate(ctx)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}
