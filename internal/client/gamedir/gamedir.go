// Package gamedir finds the FIFA 15 folder (the one holding fifa15.exe).
package gamedir

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Valid normalises dir to the folder that holds fifa15.exe ("" if none).
// It accepts the folder itself or its "Game" subfolder.
func Valid(dir string) string {
	dir = strings.TrimSpace(strings.Trim(strings.TrimSpace(dir), `"`))
	if dir == "" {
		return ""
	}
	for _, d := range []string{dir, filepath.Join(dir, "Game")} {
		if st, err := os.Stat(filepath.Join(d, "fifa15.exe")); err == nil && !st.IsDir() {
			abs, err := filepath.Abs(d)
			if err != nil {
				return d
			}
			return abs
		}
	}
	return ""
}

// Detect returns the first plausible game folder. remembered is tried first,
// then the launcher's own folder and its parents (players often unzip it into
// the game folder), then platform-specific places.
func Detect(remembered, launcherDir string) string {
	cands := []string{remembered, launcherDir, filepath.Dir(launcherDir)}
	cands = append(cands, platformCandidates()...)
	for _, c := range cands {
		if d := Valid(c); d != "" {
			return d
		}
	}
	return ""
}

// searchGames looks up to depth levels below root for fifa15.exe.
func searchGames(root string, depth int) []string {
	var out []string
	var walk func(dir string, left int)
	walk = func(dir string, left int) {
		if Valid(dir) != "" {
			out = append(out, dir)
			return
		}
		if left == 0 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				walk(filepath.Join(dir, e.Name()), left-1)
			}
		}
	}
	walk(root, depth)
	return out
}

// Browse opens a native folder picker. ok is false when the user cancelled or no
// picker is available.
func Browse() (string, bool) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		ps := `Add-Type -AssemblyName System.Windows.Forms; ` +
			`$d = New-Object System.Windows.Forms.FolderBrowserDialog; ` +
			`$d.Description = 'Select the FIFA 15 folder (the one with fifa15.exe)'; ` +
			`$d.ShowNewFolderButton = $false; ` +
			`$f = New-Object System.Windows.Forms.Form; $f.TopMost = $true; ` +
			`if ($d.ShowDialog($f) -eq 'OK') { [Console]::Out.Write($d.SelectedPath) }`
		cmd = exec.Command("powershell", "-NoProfile", "-STA", "-Command", ps)
		hideWindow(cmd)
	default:
		if p, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.Command(p, "--file-selection", "--directory", "--title=Select the FIFA 15 folder (with fifa15.exe)")
		} else if p, err := exec.LookPath("kdialog"); err == nil {
			home, _ := os.UserHomeDir()
			cmd = exec.Command(p, "--getexistingdirectory", home, "--title", "Select the FIFA 15 folder (with fifa15.exe)")
		} else {
			return "", false
		}
	}
	out, err := cmd.Output()
	if err != nil {
		return "", false
	}
	dir := strings.TrimSpace(string(out))
	return dir, dir != ""
}
