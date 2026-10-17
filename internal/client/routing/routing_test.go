package routing

import (
	"os"
	"path/filepath"
	"testing"
)

// testdata/*.python is _write_runtime_routing's output for 203.0.113.7 (VERSION "X").
func TestMatchesPython(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, "203.0.113.7", "X"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"cl.ini", "EA-MITM.ini", "p2p-hook.ini"} {
		got, _ := os.ReadFile(filepath.Join(dir, name))
		want, err := os.ReadFile(filepath.Join("testdata", name+".python"))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s differs:\n--- go\n%s\n--- python\n%s", name, got, want)
		}
	}
}
