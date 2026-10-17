//go:build !windows

package gamedir

import (
	"os"
	"os/exec"
	"path/filepath"
)

func hideWindow(*exec.Cmd) {}

func platformCandidates() []string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	return append(searchGames(filepath.Join(home, "Games"), 3), searchGames(home, 1)...)
}
