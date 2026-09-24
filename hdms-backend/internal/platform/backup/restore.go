package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// IsLegacyBackupFile reports whether name is a 5.4a single-file backup rather
// than a restic snapshot id. Old files stay restorable indefinitely, so the
// restore command dispatches on the shape of its argument.
func IsLegacyBackupFile(name string) bool {
	_, ok := ParseFilenameTime(name)
	return ok
}

// RestoreInto streams one snapshot out of repo straight into pg_restore. The
// archive never lands on disk: the only copy in flight is in the pipe.
func RestoreInto(ctx context.Context, r Restic, repo Repo, snapshotID, databaseURL string) error {
	pr, pw := io.Pipe()
	errCh := make(chan error, 1)
	go func() {
		err := r.Dump(ctx, repo, snapshotID, StdinFilename, pw)
		_ = pw.CloseWithError(err)
		errCh <- err
	}()
	restoreErr := pgRestore(ctx, databaseURL, pr)
	_ = pr.Close()
	if dumpErr := <-errCh; dumpErr != nil {
		return fmt.Errorf("backup: restic dump: %w", dumpErr)
	}
	return restoreErr
}

// RestoreLegacyFile restores a 5.4a single-file backup: decrypt, gunzip, then
// pg_restore. Kept so backups taken before the restic cutover stay usable.
func RestoreLegacyFile(ctx context.Context, path string, key []byte, databaseURL string) error {
	plain, err := ReadDecryptedFile(path, key)
	if err != nil {
		return err
	}
	return pgRestore(ctx, databaseURL, bytes.NewReader(plain))
}

// pgRestore feeds a pg_dump custom-format archive on stdin into databaseURL.
// --clean --if-exists makes a repeated restore into the same scratch database
// deterministic; --exit-on-error makes a partial restore an error rather than
// a warning nobody reads.
func pgRestore(ctx context.Context, databaseURL string, archive io.Reader) error {
	// #nosec G204 -- databaseURL is operator input to a local CLI; the binary is fixed.
	cmd := exec.CommandContext(ctx, "pg_restore",
		"--clean", "--if-exists", "--no-owner", "--no-privileges", "--exit-on-error",
		"-d", databaseURL)
	cmd.Stdin = archive
	var serr strings.Builder
	cmd.Stderr = &serr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("backup: pg_restore: %w: %s", err, strings.TrimSpace(serr.String()))
	}
	return nil
}
