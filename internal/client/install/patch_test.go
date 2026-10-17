package install

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// makePatch is what `zstd --patch-from=orig modded` produces: modded compressed
// with orig as raw dictionary.
func makePatch(t *testing.T, orig, modded []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf, zstd.WithEncoderDictRaw(0, orig), zstd.WithWindowSize(1<<22))
	if err != nil {
		t.Fatal(err)
	}
	enc.Write(modded)
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

// A 1 MB "game file", its modded version (a few changed bytes and an appended
// block), and a manifest entry that patches one into the other.
func patchFixture(t *testing.T) (orig, modded []byte, m *Manifest, payload string) {
	orig = randomBytes(1 << 20)
	modded = append(append([]byte{}, orig...), randomBytes(64<<10)...)
	copy(modded[1000:], "OUFT")
	payload = t.TempDir()
	patch := makePatch(t, orig, modded)
	if len(patch) > len(modded)/2 {
		t.Fatalf("patch is not a delta: %d bytes", len(patch))
	}
	if err := os.WriteFile(filepath.Join(payload, "cards0.big.zst"), patch, 0o644); err != nil {
		t.Fatal(err)
	}
	m = &Manifest{Files: []File{{
		Dst: "cards0.big", Patch: "cards0.big.zst",
		Source: sum(string(orig)), SHA256: sum(string(modded)), Size: int64(len(modded)),
	}}}
	return orig, modded, m, payload
}

func TestPatchInstallAndRestore(t *testing.T) {
	orig, modded, m, payload := patchFixture(t)
	game, rt := t.TempDir(), t.TempDir()
	write(t, filepath.Join(game, "cards0.big"), string(orig))

	if err := m.Install(payload, game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(game, "cards0.big")) != string(modded) {
		t.Fatal("game file not patched")
	}
	if read(t, filepath.Join(rt, "install-backup/cards0.big")) != string(orig) {
		t.Fatal("original not backed up")
	}
	if n := len(m.Outdated(game)); n != 0 {
		t.Fatalf("still outdated: %d", n)
	}
	// Already patched: a second install changes nothing.
	if err := m.Install(payload, game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if err := m.Restore(game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(game, "cards0.big")) != string(orig) {
		t.Fatal("restore did not bring the original back")
	}
}

// An older modded version (from a previous launcher) is re-patched from the backup.
func TestPatchUpgradesFromBackup(t *testing.T) {
	orig, modded, m, payload := patchFixture(t)
	game, rt := t.TempDir(), t.TempDir()
	write(t, filepath.Join(rt, "install-backup/cards0.big"), string(orig))
	write(t, filepath.Join(game, "cards0.big"), "an older modded version")
	if err := m.Install(payload, game, rt, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(game, "cards0.big")) != string(modded) {
		t.Fatal("not upgraded from the backed-up original")
	}
}

func TestPatchUnknownVersion(t *testing.T) {
	_, _, m, payload := patchFixture(t)
	game, rt := t.TempDir(), t.TempDir()
	write(t, filepath.Join(game, "cards0.big"), "some other game build")
	err := m.Install(payload, game, rt, func(string) {})
	if !errors.Is(err, ErrUnknownVersion) {
		t.Fatalf("err = %v, want unknown version", err)
	}
	if read(t, filepath.Join(game, "cards0.big")) != "some other game build" {
		t.Fatal("unknown file was touched")
	}
}

func TestPatchDamaged(t *testing.T) {
	orig, _, m, payload := patchFixture(t)
	game, rt := t.TempDir(), t.TempDir()
	write(t, filepath.Join(game, "cards0.big"), string(orig))
	p := filepath.Join(payload, "cards0.big.zst")
	b, _ := os.ReadFile(p)
	b[len(b)/2] ^= 0xff
	os.WriteFile(p, b, 0o644)
	if err := m.Install(payload, game, rt, func(string) {}); err == nil {
		t.Fatal("damaged patch accepted")
	}
	if read(t, filepath.Join(game, "cards0.big")) != string(orig) {
		t.Fatal("game file changed by a failed patch")
	}
	if _, err := os.Stat(filepath.Join(game, "cards0.big.ouft-part")); err == nil {
		t.Fatal("temporary file left behind")
	}
}

// Real files (optional): patches made by the zstd CLI against the repack originals
// must reproduce today's modded files. PATCH_REAL=<dir with *.zst>:<originals>:<modded>.
func TestPatchRealFiles(t *testing.T) {
	spec := os.Getenv("PATCH_REAL")
	if spec == "" {
		t.Skip("set PATCH_REAL to run")
	}
	parts := bytes.Split([]byte(spec), []byte(":"))
	dir, origDir, modDir := string(parts[0]), string(parts[1]), string(parts[2])
	for _, f := range []string{"CardsDLLzf.dll", "ItsAMe_Origin.dll", "cards0.bh", "data_patch.bh", "cards0.big", "data_patch.big"} {
		want, _ := HashFile(filepath.Join(modDir, f))
		out := filepath.Join(t.TempDir(), f)
		if err := applyPatch(filepath.Join(origDir, f), filepath.Join(dir, f+".zst"), out, want); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	if status, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range bytes.Split(status, []byte("\n")) {
			if bytes.HasPrefix(line, []byte("VmHWM")) {
				t.Logf("peak memory: %s", bytes.TrimSpace(line[6:]))
			}
		}
	}
}
