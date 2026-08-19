// Command hdms-cli will hold import, seed, admin bootstrap and label export
// subcommands (docs/02-architecture.md). Phase 0 wired the entrypoint and
// the `seed` subcommand the local dev stack depends on; Phase 1 adds
// `admin bootstrap`, the only way an admin_accounts row is ever created
// (there is no self-service enrollment, matching every other
// administrator-only creation path in this system).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"syscall"

	"github.com/hito-hospital/hdms/internal/cliimport"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/credentials"
	"github.com/hito-hospital/hdms/internal/modules/identity"
	"github.com/hito-hospital/hdms/internal/platform/auth"
	"github.com/hito-hospital/hdms/internal/platform/config"
	"github.com/hito-hospital/hdms/internal/platform/db"
	"golang.org/x/term"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "hdms-cli:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hdms-cli <seed|migrate|admin bootstrap|import devices|import users>")
}

func run(cmd string, args []string) error {
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
	case "admin":
		return runAdmin(ctx, cfg, args)
	case "import":
		return runImport(ctx, cfg, args)
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}

func runImport(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) < 1 || (args[0] != "devices" && args[0] != "users") {
		return fmt.Errorf("usage: hdms-cli import <devices|users> --file <path> [--dry-run] [--mint-credentials]")
	}
	resource := args[0]

	fs := flag.NewFlagSet("import "+resource, flag.ContinueOnError)
	file := fs.String("file", "", "CSV file to import (required)")
	dryRun := fs.Bool("dry-run", false, "report what would happen without writing")
	mintCredentials := fs.Bool("mint-credentials", false, "issue a QR credential for each newly created row")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	f, err := os.Open(*file)
	if err != nil {
		return fmt.Errorf("open %s: %w", *file, err)
	}
	defer func() { _ = f.Close() }()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	auditSvc := audit.New(pool)
	credentialsSvc := credentials.New(pool, auditSvc, cfg.TokenPepper, cfg.CredentialEncKey)

	var report cliimport.Report
	switch resource {
	case "devices":
		catalogSvc := catalog.New(pool, auditSvc)
		report, err = cliimport.ImportDevices(ctx, cliimport.DeviceImportDeps{
			Catalog:     catalogSvc,
			Credentials: credentialsSvc,
		}, f, *dryRun, *mintCredentials)
	case "users":
		identitySvc := identity.New(pool, auditSvc)
		report, err = cliimport.ImportUsers(ctx, cliimport.UserImportDeps{
			Identity:    identitySvc,
			Credentials: credentialsSvc,
		}, f, *dryRun, *mintCredentials)
	}
	if err != nil {
		return err
	}

	_ = report.Print(os.Stdout)
	if _, _, rejected := report.Counts(); rejected > 0 {
		return fmt.Errorf("%d row(s) rejected", rejected)
	}
	return nil
}

func runAdmin(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) < 1 || args[0] != "bootstrap" {
		return fmt.Errorf("usage: hdms-cli admin bootstrap --email <email> --name <full name> [--role superadmin|admin|operator]")
	}

	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	email := fs.String("email", "", "admin account email (required)")
	name := fs.String("name", "", "admin account full name (required)")
	role := fs.String("role", "superadmin", "admin role: superadmin | admin | operator")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" || *name == "" {
		return fmt.Errorf("--email and --name are required")
	}
	switch *role {
	case "superadmin", "admin", "operator":
	default:
		return fmt.Errorf("--role must be one of superadmin, admin, operator")
	}

	password, err := promptPassword()
	if err != nil {
		return err
	}

	if err := db.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL)
	id, secret, otpauthURL, err := authSvc.CreateAdminAccount(ctx, *email, *name, password, *role)
	if err != nil {
		return fmt.Errorf("create admin account: %w", err)
	}

	fmt.Printf("Admin account created: %s (%s, role=%s)\n\n", id, *email, *role)
	fmt.Println("Scan this into your authenticator app now — it will not be shown again:")
	fmt.Println()
	fmt.Println("  otpauth URL:", otpauthURL)
	fmt.Println("  raw secret: ", secret)
	fmt.Println()
	return nil
}

func promptPassword() (string, error) {
	fmt.Print("Password: ")
	pw1, err := term.ReadPassword(syscall.Stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	fmt.Print("Confirm password: ")
	pw2, err := term.ReadPassword(syscall.Stdin)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	if string(pw1) != string(pw2) {
		return "", fmt.Errorf("passwords did not match")
	}
	if len(pw1) < 12 {
		return "", fmt.Errorf("password must be at least 12 characters")
	}
	return string(pw1), nil
}
