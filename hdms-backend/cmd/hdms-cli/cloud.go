package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// runCloud handles `hdms-cli cloud fetch`. install.sh runs it on a new server
// before any env file exists, so it needs no configuration and no database:
// it signs in on the terminal and copies the backup folder out of the cloud.
func runCloud(ctx context.Context, args []string, stdin io.Reader, stderr io.Writer) error {
	const usage = "usage: hdms-cli cloud fetch --provider google_drive|onedrive --client-id <id> [--tenant <id>] --folder <name> --to <dir>  (client secret on the first line of stdin; empty for OneDrive)"
	if len(args) == 0 || args[0] != "fetch" {
		return errors.New(usage)
	}
	fs := flag.NewFlagSet("cloud fetch", flag.ContinueOnError)
	provider := fs.String("provider", "", "google_drive or onedrive")
	clientID := fs.String("client-id", "", "OAuth client ID")
	tenant := fs.String("tenant", "", "OneDrive directory (tenant) ID")
	folder := fs.String("folder", "", "folder in the cloud drive that holds the HDMS backups")
	to := fs.String("to", "", "local directory to download into")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *provider == "" || *clientID == "" || *folder == "" || *to == "" {
		return errors.New(usage)
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read client secret: %w", err)
	}
	c := cloudFetch{
		Provider: *provider, ClientID: *clientID, ClientSecret: strings.TrimSpace(line), Tenant: *tenant,
		Folder: *folder, To: *to,
		Flow: &backup.DeviceFlow{HTTP: &http.Client{Timeout: 30 * time.Second}},
		Out:  stderr,
		Wait: func(ctx context.Context, d time.Duration) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
				return nil
			}
		},
	}
	return c.run(ctx)
}

type cloudFetch struct {
	Provider, ClientID, ClientSecret, Tenant, Folder, To string
	Flow                                                 *backup.DeviceFlow
	Rclone                                               backup.Rclone
	Out                                                  io.Writer
	Wait                                                 func(ctx context.Context, d time.Duration) error
}

func (c cloudFetch) run(ctx context.Context) error {
	folder, err := backup.CleanCloudFolder(c.Folder)
	if err != nil {
		return err
	}
	tenant := c.Tenant
	if c.Provider == backup.ProviderGoogleDrive {
		tenant = "common"
	} else if tenant == "" {
		tenant = "common"
	}
	dc, err := c.Flow.Start(ctx, c.Provider, c.ClientID, tenant)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(c.Out, "\nOn any device, open %s and enter the code %s\nWaiting for you to sign in...\n", dc.VerificationURI, dc.UserCode)

	tok, err := c.waitForToken(ctx, dc, tenant)
	if err != nil {
		return err
	}
	prof, err := c.Flow.Profile(ctx, c.Provider, tenant, tok.AccessToken)
	if err != nil && c.Provider == backup.ProviderOneDrive {
		return fmt.Errorf("signed in, but could not read the OneDrive details: %w", err)
	}
	js, err := tok.RcloneJSON()
	if err != nil {
		return err
	}

	dir, err := os.MkdirTemp("", "hdms-cloud-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	id := uuid.New()
	conf := filepath.Join(dir, "rclone.conf")
	rendered := backup.RenderRcloneConfig([]backup.CloudCreds{{
		ID: id, Provider: c.Provider, ClientID: c.ClientID, ClientSecret: c.ClientSecret, Tenant: tenant,
		Token: js, DriveID: prof.DriveID, DriveType: prof.DriveType,
	}})
	if err := os.WriteFile(conf, []byte(rendered), 0o600); err != nil {
		return err
	}

	dst := filepath.Join(c.To, path.Base(folder))
	_, _ = fmt.Fprintf(c.Out, "Signed in. Downloading %q (this can take a while)...\n", folder)
	rc := c.Rclone
	rc.Config = conf
	if err := rc.Copy(ctx, backup.CloudRemote(id)+":"+folder, dst); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if !backup.IsRecoverySource(dst) {
		return fmt.Errorf("no HDMS backups found in %q (looked for repo and %s); check the folder name and that this is the account HDMS backed up to", folder, backup.RecoveryBundleFile)
	}
	_, _ = fmt.Fprintf(c.Out, "Downloaded to %s\n", dst)
	return nil
}

func (c cloudFetch) waitForToken(ctx context.Context, dc backup.DeviceCode, tenant string) (backup.Token, error) {
	ctx, cancel := context.WithTimeout(ctx, dc.ExpiresIn)
	defer cancel()
	interval := dc.Interval
	for {
		if err := c.Wait(ctx, interval); err != nil {
			return backup.Token{}, errors.New("the sign-in code expired before you finished; run install.sh --restore again")
		}
		tok, err := c.Flow.Exchange(ctx, c.Provider, c.ClientID, c.ClientSecret, tenant, dc.DeviceCode)
		switch {
		case err == nil:
			return tok, nil
		case errors.Is(err, backup.ErrAuthorizationPending):
		case errors.Is(err, backup.ErrSlowDown):
			interval += 5 * time.Second
		case errors.Is(err, backup.ErrCodeExpired):
			return backup.Token{}, errors.New("the sign-in code expired; run install.sh --restore again")
		case errors.Is(err, backup.ErrAccessDenied):
			return backup.Token{}, errors.New("sign-in was declined")
		default:
			return backup.Token{}, err
		}
	}
}
