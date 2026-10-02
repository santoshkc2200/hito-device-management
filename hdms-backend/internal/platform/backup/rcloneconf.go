package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CloudRemote is the rclone remote name for an account.
func CloudRemote(id uuid.UUID) string { return "acct_" + id.String() }

// RenderRcloneConfig writes one remote per account. Values come from
// validated input (no line breaks) or from the provider.
func RenderRcloneConfig(accts []CloudCreds) string {
	var b strings.Builder
	for _, a := range accts {
		fmt.Fprintf(&b, "[%s]\n", CloudRemote(a.ID))
		switch a.Provider {
		case ProviderGoogleDrive:
			b.WriteString("type = drive\n")
			fmt.Fprintf(&b, "client_id = %s\nclient_secret = %s\n", a.ClientID, a.ClientSecret)
			b.WriteString("scope = drive.file\n")
		case ProviderOneDrive:
			b.WriteString("type = onedrive\n")
			fmt.Fprintf(&b, "client_id = %s\n", a.ClientID)
			if a.Tenant != "" && a.Tenant != "common" {
				base := "https://login.microsoftonline.com/" + a.Tenant + "/oauth2/v2.0/"
				fmt.Fprintf(&b, "auth_url = %sauthorize\ntoken_url = %stoken\n", base, base)
			}
			fmt.Fprintf(&b, "drive_id = %s\ndrive_type = %s\n", a.DriveID, a.DriveType)
		}
		fmt.Fprintf(&b, "token = %s\n\n", a.Token)
	}
	return b.String()
}

// ParseRcloneTokens returns each remote's current token line. rclone rewrites
// the file when it refreshes a token, so comparing before and after tells
// which accounts need their stored token replaced.
func ParseRcloneTokens(conf string) map[string]string {
	out := map[string]string{}
	section := ""
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = line[1 : len(line)-1]
		case section != "" && strings.HasPrefix(line, "token"):
			if k, v, ok := strings.Cut(line, "="); ok && strings.TrimSpace(k) == "token" {
				out[section] = strings.TrimSpace(v)
			}
		}
	}
	return out
}

// IsTokenRevoked reports whether an rclone/restic failure means the provider
// no longer accepts the saved sign-in (as opposed to quota or network trouble).
func IsTokenRevoked(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "invalid_grant") || strings.Contains(m, "couldn't fetch token")
}

// Rclone runs the rclone binary against a rendered config. Restic starts its
// own rclone for repositories; this is for the few direct operations: writing
// the recovery bundle and, in the CLI, downloading a folder.
type Rclone struct {
	Binary string
	Exec   ExecFunc
	Config string
}

func (r Rclone) run(ctx context.Context, stdin io.Reader, args ...string) error {
	bin, ex := r.Binary, r.Exec
	if bin == "" {
		bin = "rclone"
	}
	if ex == nil {
		ex = DefaultExec
	}
	return ex(ctx, bin, args, []string{"RCLONE_CONFIG=" + r.Config}, stdin, io.Discard)
}

// Rcat writes data to target ("remote:path/file").
func (r Rclone) Rcat(ctx context.Context, target string, data []byte) error {
	return r.run(ctx, bytes.NewReader(data), "rcat", target)
}

// Copy copies src to dst; either may be a remote ("remote:path").
func (r Rclone) Copy(ctx context.Context, src, dst string) error {
	return r.run(ctx, nil, "copy", src, dst)
}

var errCloudNeedsWorker = errors.New("backup: cloud destinations are backed up by the worker only")

// CloudSession is one run's rclone configuration: a private directory holding
// rclone.conf with a remote per account. Close writes refreshed tokens back
// and deletes the directory. Every method is safe on a nil session, which
// means "no cloud destinations in play".
type CloudSession struct {
	svc      *CloudService
	rclone   Rclone
	dir      string
	rendered map[string]string    // remote -> token JSON as written
	byRemote map[string]uuid.UUID // remote -> account
	blocked  map[uuid.UUID]string // account -> status that keeps it out of the config
}

// OpenSession renders the config for every account the destinations use.
// Accounts that are not connected stay out of it, and their destinations fail
// with a reconnect hint rather than an rclone error.
func (s *CloudService) OpenSession(ctx context.Context, dests []Destination, rc Rclone) (*CloudSession, error) {
	sess := &CloudSession{
		svc: s, rendered: map[string]string{}, byRemote: map[string]uuid.UUID{}, blocked: map[uuid.UUID]string{},
	}
	seen := map[uuid.UUID]bool{}
	var creds []CloudCreds
	for _, d := range dests {
		if d.CloudAccountID == nil || seen[*d.CloudAccountID] {
			continue
		}
		seen[*d.CloudAccountID] = true
		c, err := s.credentials(ctx, *d.CloudAccountID)
		if err != nil {
			return nil, err
		}
		if c.Status != AccountConnected {
			sess.blocked[c.ID] = c.Status
			continue
		}
		creds = append(creds, c)
	}
	if len(seen) == 0 {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "hdms-rclone-") // mode 0700
	if err != nil {
		return nil, fmt.Errorf("backup: rclone config directory: %w", err)
	}
	path := filepath.Join(dir, "rclone.conf")
	if err := os.WriteFile(path, []byte(RenderRcloneConfig(creds)), 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("backup: write rclone config: %w", err)
	}
	sess.dir = dir
	sess.rclone = rc
	sess.rclone.Config = path
	for _, c := range creds {
		remote := CloudRemote(c.ID)
		sess.rendered[remote] = c.Token
		sess.byRemote[remote] = c.ID
	}
	return sess, nil
}

// Apply returns r configured to use this session's rclone config.
func (s *CloudSession) Apply(r Restic) Restic {
	if s == nil || s.dir == "" {
		return r
	}
	r.ExtraEnv = append(append([]string(nil), r.ExtraEnv...), "RCLONE_CONFIG="+s.rclone.Config)
	return r
}

// Usable reports whether d can be reached in this run.
func (s *CloudSession) Usable(d Destination) error {
	if d.CloudAccountID == nil {
		return nil
	}
	if s == nil {
		return errCloudNeedsWorker
	}
	if status, blocked := s.blocked[*d.CloudAccountID]; blocked {
		return fmt.Errorf("backup: the cloud account behind %q is %s; reconnect it in Backups → Destinations", d.Name, status)
	}
	return nil
}

// PutBundle writes the recovery bundle beside d's repository in the cloud.
func (s *CloudSession) PutBundle(ctx context.Context, d Destination, bundle []byte) error {
	if err := s.Usable(d); err != nil {
		return err
	}
	return s.rclone.Rcat(ctx, CloudRemote(*d.CloudAccountID)+":"+d.Folder+"/"+RecoveryBundleFile, bundle)
}

// NoteFailure moves d's account to "revoked" when the failure says the
// provider no longer accepts the saved sign-in.
func (s *CloudSession) NoteFailure(ctx context.Context, d Destination, cause string) {
	if s == nil || d.CloudAccountID == nil || !IsTokenRevoked(cause) {
		return
	}
	reason := "The provider no longer accepts the saved sign-in. Reconnect the account."
	if err := s.svc.SetStatus(ctx, *d.CloudAccountID, AccountRevoked, reason); err != nil {
		slog.Error("backup: mark cloud account revoked", "account", d.CloudAccountID.String(), "error", err)
	}
}

// Close stores any token rclone refreshed and deletes the config directory.
// The directory is removed even when nothing else here succeeds.
func (s *CloudSession) Close(ctx context.Context) {
	if s == nil || s.dir == "" {
		return
	}
	defer func() { _ = os.RemoveAll(s.dir) }()
	raw, err := os.ReadFile(filepath.Join(s.dir, "rclone.conf")) // #nosec G304 -- our own temp directory
	if err != nil {
		slog.Error("backup: read rclone config back", "error", err)
		return
	}
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	for remote, tok := range ParseRcloneTokens(string(raw)) {
		id, ok := s.byRemote[remote]
		if !ok || tok == "" || tok == s.rendered[remote] {
			continue
		}
		if err := s.svc.StoreToken(wctx, id, tok); err != nil {
			slog.Error("backup: store refreshed cloud token", "account", id.String(), "error", err)
		}
	}
}

// PutRecoveryBundle keeps the sealed recovery bundle beside d's repository: a
// file next to a folder repository, an upload next to a cloud one.
func PutRecoveryBundle(ctx context.Context, cloud *CloudSession, d Destination, repo Repo, bundle []byte) error {
	if d.CloudAccountID != nil {
		return cloud.PutBundle(ctx, d, bundle)
	}
	return WriteRecoveryBundle(repo, bundle)
}
