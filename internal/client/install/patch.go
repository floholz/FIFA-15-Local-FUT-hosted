package install

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/klauspost/compress/zstd"
)

// Modded game files ship as zstd patches made with
//
//	zstd -19 --long=28 --patch-from=<original> <modded> -o <patch>
//
// so the zip contains none of the original (EA / repack) files. The original is the
// zstd "raw dictionary"; decoding the patch with it yields the modded file.

// maxPatchWindow caps the decoder window (the patches use 256 MB, --long=28).
const maxPatchWindow = 1 << 30

// ErrUnknownVersion: the player's file is neither the original the patch was made
// for nor the already patched result.
var ErrUnknownVersion = errors.New("unknown version")

// applyPatch decodes patchPath against the file at srcPath and writes the result to
// dstPath (srcPath may equal dstPath). The result must hash to wantSHA.
func applyPatch(srcPath, patchPath, dstPath, wantSHA string) error {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	pf, err := os.Open(patchPath)
	if err != nil {
		return err
	}
	defer pf.Close()
	dec, err := zstd.NewReader(pf,
		zstd.WithDecoderDictRaw(0, src),
		zstd.WithDecoderMaxWindow(maxPatchWindow),
		zstd.WithDecoderConcurrency(1),
		zstd.WithDecoderLowmem(true),
	)
	if err != nil {
		return err
	}
	defer dec.Close()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	tmp := dstPath + ".ouft-part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), dec); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("applying %s: %w", filepath.Base(patchPath), err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		os.Remove(tmp)
		return fmt.Errorf("patch %s gave an unexpected result; download the launcher zip again", filepath.Base(patchPath))
	}
	if err := os.Rename(tmp, dstPath); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// installPatched brings one patched file up to date: patch the current file when it
// is the original, else the backed-up original (the game file was replaced since).
func (m *Manifest) installPatched(f File, payloadDir, gameDir, backup string) error {
	dst := filepath.Join(gameDir, filepath.FromSlash(f.Dst))
	cur, curErr := HashFile(dst)
	if curErr == nil && cur == f.SHA256 {
		return nil
	}
	patch := filepath.Join(payloadDir, filepath.FromSlash(f.Patch))
	if curErr == nil && cur == f.Source {
		return applyPatch(dst, patch, dst, f.SHA256)
	}
	bak := filepath.Join(backup, filepath.FromSlash(legacyBackupName(f.Dst)))
	if h, err := HashFile(bak); err == nil && h == f.Source {
		return applyPatch(bak, patch, dst, f.SHA256)
	}
	if curErr != nil {
		return fmt.Errorf("%s is missing from the game folder; use the same game copy as your friends: %w", f.Dst, ErrUnknownVersion)
	}
	return fmt.Errorf("%s is a different version than the one the online files are made for (%s…); use the same game copy as your friends: %w",
		f.Dst, cur[:12], ErrUnknownVersion)
}
