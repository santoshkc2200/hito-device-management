// Command hdms-cli will hold import, seed, admin bootstrap and label export
// subcommands (docs/02-architecture.md). Phase 0 wires the entrypoint and
// the `seed` subcommand the local dev stack depends on; the rest arrive with
// the modules they operate on.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: hdms-cli <seed|migrate>")
		os.Exit(2)
	}

	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, "hdms-cli:", err)
		os.Exit(1)
	}
}

func run(cmd string) error {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	switch cmd {
	case "migrate":
		return db.Migrate(ctx, cfg.DatabaseURL)
	case "seed":
		return db.Migrate(ctx, cfg.DatabaseURL) // Phase 1 adds fixture data beyond the schema.
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}
