package mailsync

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// writeAttachment streams r to a temp file, computes SHA-256, then renames to
// <attachsDir>/<sha[:2]>/<sha>. Returns the relative disk_path (sha[:2]/sha)
// and sha hex. If the destination already exists, the temp file is removed
// (dedupe hit — the caller still records the metadata row).
func writeAttachment(r io.Reader, attachsDir string) (diskPath, sha256hex string, n int64, err error) {
	tmpDir := filepath.Join(attachsDir, ".tmp")
	if err = os.MkdirAll(tmpDir, 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(tmpDir, "attach-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpPath)
		}
	}()

	h := sha256.New()
	n, err = io.Copy(io.MultiWriter(tmp, h), r)
	_ = tmp.Close()
	if err != nil {
		return
	}

	shaHex := hex.EncodeToString(h.Sum(nil))
	shardDir := filepath.Join(attachsDir, shaHex[:2])
	if err = os.MkdirAll(shardDir, 0o755); err != nil {
		return
	}
	dest := filepath.Join(shardDir, shaHex)
	if _, statErr := os.Stat(dest); statErr == nil {
		// Dedupe hit — destination already exists, discard the temp file.
		_ = os.Remove(tmpPath)
		return filepath.Join(shaHex[:2], shaHex), shaHex, n, nil
	}
	if err = os.Rename(tmpPath, dest); err != nil {
		return
	}
	return filepath.Join(shaHex[:2], shaHex), shaHex, n, nil
}
