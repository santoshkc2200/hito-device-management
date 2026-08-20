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
	"path/filepath"
	"syscall"
	"time"

	"github.com/hito-hospital/hdms/internal/cliimport"
	"github.com/hito-hospital/hdms/internal/modules/audit"
	"github.com/hito-hospital/hdms/internal/modules/catalog"
	"github.com/hito-hospital/hdms/internal/modules/checkout"
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
	fmt.Fprintln(os.Stderr, "usage: hdms-cli <seed|migrate|admin bootstrap|import devices|import users|export machine|export scenarios|kiosk register|kiosk rotate|kiosk pairing-code>")
}

func run(cmd string, args []string) error {
	if cmd == "export" {
		return runExport(args)
	}

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
	case "kiosk":
		return runKiosk(ctx, cfg, args)
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
		return fmt.Errorf("usage: hdms-cli admin bootstrap --email <email> --name <full name> [--role admin|technician|viewer]")
	}

	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	email := fs.String("email", "", "admin account email (required)")
	name := fs.String("name", "", "admin account full name (required)")
	role := fs.String("role", "admin", "admin role: admin | technician | viewer")
	passwordFlag := fs.String("password", "", "admin account password (optional; prompted if omitted)")
	totpSecretFlag := fs.String("totp-secret", "", "optional base32 TOTP secret (for test environments)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *email == "" || *name == "" {
		return fmt.Errorf("--email and --name are required")
	}
	switch *role {
	case "admin", "technician", "viewer", "superadmin", "operator":
	default:
		return fmt.Errorf("--role must be one of admin, technician, viewer")
	}

	password := *passwordFlag
	if password == "" {
		var err error
		password, err = promptPassword()
		if err != nil {
			return err
		}
	} else if len(password) < 12 {
		return fmt.Errorf("password must be at least 12 characters")
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
	id, secret, otpauthURL, err := authSvc.CreateAdminAccountWithSecret(ctx, *email, *name, password, *role, *totpSecretFlag)
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

func runKiosk(ctx context.Context, cfg config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: hdms-cli kiosk <register --name --location|rotate <id>|pairing-code <id>>")
	}

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	authSvc := auth.New(pool, cfg.TokenPepper, cfg.TOTPSecretEncKey, cfg.AdminSessionTTL)

	switch args[0] {
	case "register":
		fs := flag.NewFlagSet("kiosk register", flag.ContinueOnError)
		name := fs.String("name", "", "kiosk name (required)")
		location := fs.String("location", "", "kiosk location")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" {
			return fmt.Errorf("--name is required")
		}

		id, token, err := authSvc.RegisterKiosk(ctx, *name, *location)
		if err != nil {
			return fmt.Errorf("register kiosk: %w", err)
		}
		fmt.Printf("Kiosk registered: %s (%s)\n\n", id, *name)
		fmt.Println("Bearer token — shown once, store it now:")
		fmt.Println()
		fmt.Println(" ", token)
		fmt.Println()
		return nil

	case "rotate":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("usage: hdms-cli kiosk rotate <id>")
		}
		token, err := authSvc.RotateKioskToken(ctx, args[1])
		if err != nil {
			return fmt.Errorf("rotate kiosk token: %w", err)
		}
		fmt.Println("New bearer token — shown once, store it now:")
		fmt.Println()
		fmt.Println(" ", token)
		fmt.Println()
		return nil

	case "pairing-code":
		if len(args) < 2 || args[1] == "" {
			return fmt.Errorf("usage: hdms-cli kiosk pairing-code <id>")
		}
		code, expiresAt, err := authSvc.IssuePairingCode(ctx, args[1])
		if err != nil {
			return fmt.Errorf("issue pairing code: %w", err)
		}
		fmt.Printf("Pairing code (expires %s):\n\n", expiresAt.Format(time.RFC3339))
		fmt.Println(" ", code)
		fmt.Println()
		return nil

	default:
		return fmt.Errorf("usage: hdms-cli kiosk <register --name --location|rotate <id>|pairing-code <id>>")
	}
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

func runExport(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: hdms-cli export <machine|scenarios|all> [--out <path>] [--domain-dir <path>]")
	}

	target := args[0]
	fs := flag.NewFlagSet("export "+target, flag.ContinueOnError)
	out := fs.String("out", "", "output file path (optional)")
	domainDir := fs.String("domain-dir", "", "path to packages/domain directory (optional)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *out != "" && target == "all" {
		return fmt.Errorf("--out is not supported with target \"all\" (it writes two files); use --domain-dir or export machine/scenarios individually")
	}

	getDomainDir := func() string {
		if *domainDir != "" {
			return *domainDir
		}
		candidates := []string{
			"../hdms-frontend/packages/domain",
			"hdms-frontend/packages/domain",
			"packages/domain",
		}
		for _, c := range candidates {
			if fi, err := os.Stat(c); err == nil && fi.IsDir() {
				return c
			}
		}
		return "../hdms-frontend/packages/domain"
	}

	exportFile := func(filename string, data []byte) error {
		var outPath string
		if *out != "" {
			outPath = *out
		} else {
			outPath = filepath.Join(getDomainDir(), filename)
		}

		if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
			return fmt.Errorf("create parent directory: %w", err)
		}
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", outPath, err)
		}
		fmt.Printf("Exported %s\n", outPath)
		return nil
	}

	switch target {
	case "machine":
		data, err := checkout.ExportMachineJSON()
		if err != nil {
			return err
		}
		return exportFile("session-machine.json", data)

	case "scenarios":
		data, err := checkout.ExportScenariosJSON()
		if err != nil {
			return err
		}
		return exportFile("scenarios.json", data)

	case "all":
		mdata, err := checkout.ExportMachineJSON()
		if err != nil {
			return err
		}
		if err := exportFile("session-machine.json", mdata); err != nil {
			return err
		}
		sdata, err := checkout.ExportScenariosJSON()
		if err != nil {
			return err
		}
		return exportFile("scenarios.json", sdata)

	default:
		return fmt.Errorf("usage: hdms-cli export <machine|scenarios|all> [--out <path>] [--domain-dir <path>]")
	}
}
