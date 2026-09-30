package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/hito-hospital/hdms/internal/platform/backup"
)

// runRecovery handles `hdms-cli recovery unwrap --from <folder>`. It needs no
// configuration and no database: install.sh runs it on a new server, before
// any env file exists, to recover the secrets from a backup folder.
func runRecovery(args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "unwrap" {
		return errors.New("usage: hdms-cli recovery unwrap --from <backup folder>  (recovery key on stdin)")
	}
	fs := flag.NewFlagSet("recovery unwrap", flag.ContinueOnError)
	from := fs.String("from", "", "folder holding hdms-recovery.bin and the repo folder")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("--from is required: the backup folder that holds hdms-recovery.bin")
	}

	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read recovery key: %w", err)
	}
	if strings.TrimSpace(line) == "" {
		return errors.New("no recovery key on stdin")
	}
	key, err := backup.ParseRecoveryKey(line)
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyChecksum):
		return errors.New("the recovery key has a typo; check it against the printed sheet")
	case err != nil:
		return errors.New("the recovery key should be 28 letters and digits in seven groups of four")
	}

	bundle, err := backup.ReadRecoveryBundle(*from)
	if errors.Is(err, backup.ErrRecoveryBundleMissing) {
		return fmt.Errorf("no recovery bundle in %s; point --from at the folder that holds hdms-recovery.bin", *from)
	}
	if err != nil {
		return err
	}
	secrets, err := backup.OpenRecoveryBundle(key, bundle)
	switch {
	case errors.Is(err, backup.ErrRecoveryKeyWrong):
		return errors.New("this recovery key does not open the backups in that folder; it may be from an older sheet")
	case err != nil:
		return fmt.Errorf("the recovery bundle in %s is damaged", *from)
	}
	_, err = io.WriteString(stdout, secrets.Env())
	return err
}
