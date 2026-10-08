package upgrade

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const maxDownload = 512 << 20

// Install verifies the release archive before replacing the executable. It resolves
// symlinks and uses a same-directory atomic rename on supported Unix platforms:
// every failure before that single commit point leaves the old executable intact.
func (c *Client) Install(ctx context.Context, release Release, executable string) error {
	if !release.UpdateAvailable {
		return nil
	}
	if release.assetURL == "" || release.checksumURL == "" {
		return errors.New("upgrade requires a checked release with download and checksum assets")
	}
	executable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return fmt.Errorf("locate installed binary: %w", err)
	}
	info, err := os.Stat(executable)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("installed binary is not a regular file")
	}
	staged, err := os.CreateTemp(filepath.Dir(executable), ".repocli-upgrade-*")
	if err != nil {
		return fmt.Errorf("prepare upgrade beside %s (use the installer or package manager if this directory is not writable): %w", executable, err)
	}
	defer os.Remove(staged.Name())
	defer staged.Close()

	checksum, err := c.checksum(ctx, release)
	if err != nil {
		return err
	}
	archive, err := os.CreateTemp("", "repocli-release-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	response, err := c.get(ctx, release.assetURL)
	if err != nil {
		return fmt.Errorf("download release: %w", err)
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response.Body, maxDownload+1))
	response.Body.Close()
	if copyErr != nil {
		return fmt.Errorf("download release: %w", copyErr)
	}
	if n > maxDownload {
		return errors.New("release archive exceeds download limit")
	}
	if hex.EncodeToString(hash.Sum(nil)) != checksum {
		return errors.New("release archive SHA-256 mismatch; installed binary unchanged")
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := extract(archive, staged); err != nil {
		return fmt.Errorf("extract release binary: %w", err)
	}
	if err := staged.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(staged.Name(), executable); err != nil {
		return fmt.Errorf("replace %s (installed binary unchanged): %w", executable, err)
	}
	return nil
}

func (c *Client) checksum(ctx context.Context, release Release) (string, error) {
	response, err := c.get(ctx, release.checksumURL)
	if err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 1<<20))
	var checksum string
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != release.assetName {
			continue
		}
		if checksum != "" {
			return "", errors.New("duplicate release archive checksum")
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return "", errors.New("invalid release archive checksum")
		}
		checksum = strings.ToLower(fields[0])
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	if checksum == "" {
		return "", fmt.Errorf("checksums.txt has no entry for %s", release.assetName)
	}
	return checksum, nil
}

func extract(source io.Reader, destination io.Writer) error {
	gz, err := gzip.NewReader(source)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	found := false
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		// Only consume the named binary; never extract paths or links from the archive.
		if header.Name != "repocli" && header.Name != "./repocli" {
			return fmt.Errorf("unexpected archive entry %q", header.Name)
		}
		if found || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > maxDownload {
			return errors.New("release must contain one nonempty regular repocli binary")
		}
		if _, err := io.Copy(destination, reader); err != nil {
			return err
		}
		found = true
	}
	if !found {
		return errors.New("release archive has no repocli binary")
	}
	// Finish the gzip stream so a truncated footer cannot pass extraction.
	n, err := io.Copy(io.Discard, io.LimitReader(gz, maxDownload+1))
	if n > maxDownload {
		return errors.New("release archive exceeds extraction limit")
	}
	return err
}
