// Package install copies the client payload into a FIFA 15 folder, with a backup
// first and a one-click restore.
//
// The payload folder ships next to the launcher with a manifest.json that lists
// every file, where it goes and its sha256. Backups go to <runtime root>/install-backup,
// the same place PLAY_LOCAL_FUT15.cmd and tools/linux/install_payload.py use, so
// RESTORE_BACKUP.cmd keeps working and an older backup is reused, never overwritten.
package install

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// File is one payload file.
type File struct {
	Src    string `json:"src"`    // path inside the payload folder
	Dst    string `json:"dst"`    // path inside the game folder ("/" separated)
	SHA256 string `json:"sha256"` // of the payload file
	Size   int64  `json:"size"`
	// IfDirExists: only install when the destination's folder already exists
	// (the CardsDLL copy inside dlc/dlc_CardsDLL/dlc).
	IfDirExists bool `json:"if_dir_exists,omitempty"`
	// Added: the game never ships this file (our hook DLLs), so restore may
	// delete it even without a backup.
	Added bool `json:"added,omitempty"`
}

// Manifest is payload/manifest.json.
type Manifest struct {
	Version string `json:"version"`
	// KnownExes are sha256 hashes of fifa15.exe builds the hook's fixed addresses fit.
	KnownExes []string `json:"known_exes"`
	Files     []File   `json:"files"`
	// Generated files the launcher writes into the game folder; backed up and restored too.
	Generated []string `json:"generated"`
}

func LoadManifest(payloadDir string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(payloadDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	return &m, nil
}

// HashFile returns the sha256 of a file as lowercase hex.
func HashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ExeStatus hashes fifa15.exe and reports whether the hook fits that build.
func (m *Manifest) ExeStatus(gameDir string) (hash string, known bool, err error) {
	hash, err = HashFile(filepath.Join(gameDir, "fifa15.exe"))
	if err != nil {
		return "", false, err
	}
	return hash, slices.Contains(m.KnownExes, hash), nil
}

func (f File) applies(gameDir string) bool {
	if !f.IfDirExists {
		return true
	}
	st, err := os.Stat(filepath.Dir(filepath.Join(gameDir, filepath.FromSlash(f.Dst))))
	return err == nil && st.IsDir()
}

// Outdated lists the payload files that are missing or differ in the game folder.
func (m *Manifest) Outdated(gameDir string) []string {
	var out []string
	for _, f := range m.Files {
		if !f.applies(gameDir) {
			continue
		}
		dst := filepath.Join(gameDir, filepath.FromSlash(f.Dst))
		st, err := os.Stat(dst)
		if err != nil || st.Size() != f.Size {
			out = append(out, f.Dst)
			continue
		}
		if h, err := HashFile(dst); err != nil || h != f.SHA256 {
			out = append(out, f.Dst)
		}
	}
	return out
}

func BackupDir(runtimeRoot string) string { return filepath.Join(runtimeRoot, "install-backup") }

// legacyBackupName maps a destination to where the older .cmd/.py installers kept its backup.
func legacyBackupName(dst string) string {
	if dst == "dlc/dlc_CardsDLL/dlc/CardsDLLzf.dll" {
		return "dlc_CardsDLL/CardsDLLzf.dll"
	}
	return dst
}

func (m *Manifest) managed() []string {
	var all []string
	for _, f := range m.Files {
		all = append(all, f.Dst)
	}
	return append(all, m.Generated...)
}

// Install backs up the originals once, then copies every outdated payload file.
// progress gets a short line per step.
func (m *Manifest) Install(payloadDir, gameDir, runtimeRoot string, progress func(string)) error {
	backup := BackupDir(runtimeRoot)
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return err
	}
	// 1. Back up every payload destination once. Files absent before the first
	//    install are recorded so that a later install (with a newer payload) never
	//    mistakes our old copy for an original. Generated files (cl.ini, ...) are
	//    never backed up; restore deletes them unless an older installer saved one.
	absent := loadAbsent(backup)
	for _, f := range m.Files {
		dst := f.Dst
		if !f.applies(gameDir) || absent[dst] {
			continue
		}
		src := filepath.Join(gameDir, filepath.FromSlash(dst))
		bak := filepath.Join(backup, filepath.FromSlash(legacyBackupName(dst)))
		if _, err := os.Stat(bak); err == nil {
			continue
		}
		if _, err := os.Stat(src); err != nil {
			absent[dst] = true
			continue
		}
		if m.isPayloadCopy(src) {
			continue // already ours (installed by an older tool without a backup)
		}
		progress("Backing up " + dst)
		if err := copyFile(src, bak); err != nil {
			return fmt.Errorf("backing up %s: %w", dst, err)
		}
	}
	if err := saveAbsent(backup, absent); err != nil {
		return err
	}
	// 2. Copy what is missing or different.
	outdated := m.Outdated(gameDir)
	for _, f := range m.Files {
		if !slices.Contains(outdated, f.Dst) {
			continue
		}
		progress(fmt.Sprintf("Installing %s (%.1f MB)", f.Dst, float64(f.Size)/1e6))
		src := filepath.Join(payloadDir, filepath.FromSlash(f.Src))
		if h, err := HashFile(src); err != nil {
			return fmt.Errorf("payload file %s: %w", f.Src, err)
		} else if h != f.SHA256 {
			return fmt.Errorf("payload file %s is damaged (checksum mismatch); download the launcher zip again", f.Src)
		}
		if err := copyFile(src, filepath.Join(gameDir, filepath.FromSlash(f.Dst))); err != nil {
			return fmt.Errorf("installing %s: %w", f.Dst, err)
		}
	}
	return nil
}

const absentFile = "launcher-absent.json"

// loadAbsent reads the game-folder paths that did not exist before the first install.
func loadAbsent(backup string) map[string]bool {
	out := map[string]bool{}
	raw, err := os.ReadFile(filepath.Join(backup, absentFile))
	if err != nil {
		return out
	}
	var list []string
	_ = json.Unmarshal(raw, &list)
	for _, p := range list {
		out[p] = true
	}
	return out
}

func saveAbsent(backup string, absent map[string]bool) error {
	list := make([]string, 0, len(absent))
	for p := range absent {
		list = append(list, p)
	}
	slices.Sort(list)
	raw, _ := json.MarshalIndent(list, "", "  ")
	return os.WriteFile(filepath.Join(backup, absentFile), raw, 0o644)
}

// isPayloadCopy reports whether path holds one of our payload files (under any
// name: the older installers put the EA-MITM hook, now ea-mitm.dll, at dinput8.dll).
func (m *Manifest) isPayloadCopy(path string) bool {
	h, err := HashFile(path)
	if err != nil {
		return false
	}
	for _, f := range m.Files {
		if h == f.SHA256 {
			return true
		}
	}
	return false
}

// Restore puts the backed-up originals back and removes files the game did not have.
func (m *Manifest) Restore(gameDir, runtimeRoot string, progress func(string)) error {
	backup := BackupDir(runtimeRoot)
	if _, err := os.Stat(backup); err != nil {
		return errors.New("no backup found, so there is nothing to restore")
	}
	absent := loadAbsent(backup)
	removable := map[string]bool{"p2p-hook.log": true}
	for _, g := range m.Generated {
		removable[g] = true
	}
	for _, f := range m.Files {
		if f.Added {
			removable[f.Dst] = true
		}
	}
	var errs, kept []error
	for _, dst := range append(m.managed(), "p2p-hook.log") {
		target := filepath.Join(gameDir, filepath.FromSlash(dst))
		if _, err := os.Stat(target); err != nil && !absent[dst] {
			continue
		}
		bak := filepath.Join(backup, filepath.FromSlash(legacyBackupName(dst)))
		if _, err := os.Stat(bak); err == nil && !absent[dst] {
			progress("Restoring " + dst)
			if err := copyFile(bak, target); err != nil {
				errs = append(errs, err)
			}
			continue
		}
		if !absent[dst] && !removable[dst] {
			// No original to put back: deleting would break the game.
			kept = append(kept, fmt.Errorf("%s", dst))
			continue
		}
		if _, err := os.Stat(target); err == nil {
			progress("Removing " + dst)
			if err := os.Remove(target); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(kept) > 0 {
		errs = append(errs, fmt.Errorf("kept the modded %v: no original copy was backed up on this PC", errors.Join(kept...)))
	}
	return errors.Join(errs...)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".ouft-part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// IsPermission reports a write failure that elevation or moving the game would fix.
func IsPermission(err error) bool {
	return errors.Is(err, os.ErrPermission) || strings.Contains(strings.ToLower(fmt.Sprint(err)), "access is denied")
}
