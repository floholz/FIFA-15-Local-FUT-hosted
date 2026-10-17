package install

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func TestInstallAndRestore(t *testing.T) {
	payload, game, rt := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(payload, "dinput8.dll"), "HOOK")
	write(t, filepath.Join(payload, "CardsDLLzf.dll"), "CARDS-NEW")
	write(t, filepath.Join(game, "fifa15.exe"), "EXE")
	write(t, filepath.Join(game, "CardsDLLzf.dll"), "CARDS-ORIG")
	write(t, filepath.Join(game, "dlc/dlc_CardsDLL/dlc/CardsDLLzf.dll"), "CARDS-DLC-ORIG")
	m := &Manifest{
		KnownExes: []string{sum("EXE")},
		Files: []File{
			{Src: "dinput8.dll", Dst: "dinput8.dll", SHA256: sum("HOOK"), Size: 4},
			{Src: "CardsDLLzf.dll", Dst: "CardsDLLzf.dll", SHA256: sum("CARDS-NEW"), Size: 9},
			{Src: "CardsDLLzf.dll", Dst: "dlc/dlc_CardsDLL/dlc/CardsDLLzf.dll", SHA256: sum("CARDS-NEW"), Size: 9, IfDirExists: true},
			{Src: "CardsDLLzf.dll", Dst: "nodir/CardsDLLzf.dll", SHA256: sum("CARDS-NEW"), Size: 9, IfDirExists: true},
		},
		Generated: []string{"cl.ini"},
	}
	if _, known, err := m.ExeStatus(game); err != nil || !known {
		t.Fatalf("exe not recognised: %v", err)
	}
	if n := len(m.Outdated(game)); n != 3 {
		t.Fatalf("outdated = %d, want 3", n)
	}
	if err := m.Install(payload, game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(game, "cl.ini"), "generated")
	if n := len(m.Outdated(game)); n != 0 {
		t.Fatalf("still outdated after install: %v", m.Outdated(game))
	}
	if _, err := os.Stat(filepath.Join(game, "nodir")); err == nil {
		t.Fatal("installed into a folder that did not exist")
	}
	// A second install must not overwrite the backup with our own files.
	if err := m.Install(payload, game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"CardsDLLzf.dll":                      "CARDS-ORIG",
		"dlc/dlc_CardsDLL/dlc/CardsDLLzf.dll": "CARDS-DLC-ORIG",
		"dinput8.dll":                         "<missing>",
		"cl.ini":                              "<missing>",
	} {
		if got := read(t, filepath.Join(game, path)); got != want {
			t.Errorf("%s after restore = %q, want %q", path, got, want)
		}
	}
	// The DLC backup uses the older installers' location.
	if got := read(t, filepath.Join(rt, "install-backup/dlc_CardsDLL/CardsDLLzf.dll")); got != "CARDS-DLC-ORIG" {
		t.Errorf("dlc backup = %q", got)
	}
}

// A game set up by the older installers has the EA-MITM hook at dinput8.dll and
// no backup of it: that file is ours, not an original, so restore removes it.
func TestOlderInstallIsNotBackedUp(t *testing.T) {
	payload, game, rt := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, filepath.Join(payload, "dinput8.dll"), "P2PHOOK")
	write(t, filepath.Join(payload, "ea-mitm.dll"), "EAMITM")
	write(t, filepath.Join(game, "dinput8.dll"), "EAMITM")
	m := &Manifest{Files: []File{
		{Src: "dinput8.dll", Dst: "dinput8.dll", SHA256: sum("P2PHOOK"), Size: 7},
		{Src: "ea-mitm.dll", Dst: "ea-mitm.dll", SHA256: sum("EAMITM"), Size: 6},
	}}
	if err := m.Install(payload, game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(rt, "install-backup/dinput8.dll")); got != "<missing>" {
		t.Fatalf("our old hook was backed up as an original: %q", got)
	}
	if err := m.Restore(game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(game, "dinput8.dll")); got != "<missing>" {
		t.Fatalf("dinput8.dll after restore = %q", got)
	}
}
