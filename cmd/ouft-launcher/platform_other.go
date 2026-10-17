//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const goos = runtime.GOOS

// gameCommand runs FIFA 15 through umu-launcher with GE-Proton, like tools/linux/play.sh.
func gameCommand(s Settings, gameDir, rt string) (*exec.Cmd, error) {
	umu, err := exec.LookPath("umu-run")
	if err != nil {
		return nil, errors.New("umu-run was not found. Install umu-launcher (Arch: pacman -S umu-launcher, others: see github.com/Open-Wine-Components/umu-launcher)")
	}
	prefix := s.Prefix
	if prefix == "" {
		prefix = defaultPrefix(gameDir)
	}
	proton := s.ProtonPath
	if proton == "" {
		proton = defaultProton()
	}
	cmd := exec.Command(umu, "./fifa15.exe")
	cmd.Dir = gameDir
	env := append(os.Environ(),
		"WINEPREFIX="+prefix,
		"PROTONPATH="+proton,
		"GAMEID=umu-fifa15",
		"STORE=none",
		// The Local FUT hook is a dinput8.dll wrapper: Proton must load the game's copy.
		"WINEDLLOVERRIDES=dinput8=n,b"+prefixed(";", os.Getenv("WINEDLLOVERRIDES")),
	)
	if s.WineD3D {
		env = append(env, "PROTON_USE_WINED3D=1")
	}
	cmd.Env = env
	if f, err := os.Create(filepath.Join(rt, "logs", "proton-console.log")); err == nil {
		cmd.Stdout, cmd.Stderr = f, f
	}
	return cmd, nil
}

func prefixed(sep, v string) string {
	if v == "" {
		return ""
	}
	return sep + v
}

func defaultPrefix(gameDir string) string { return filepath.Join(filepath.Dir(gameDir), "prefix") }

// defaultProton picks the newest locally installed GE-Proton, so umu does not
// download one (~500 MB) on every new release. Falls back to umu's "GE-Proton".
func defaultProton() string {
	home, _ := os.UserHomeDir()
	var found []string
	for _, dir := range []string{
		filepath.Join(home, ".local/share/Steam/compatibilitytools.d"),
		filepath.Join(home, ".steam/root/compatibilitytools.d"),
		filepath.Join(home, ".var/app/com.valvesoftware.Steam/data/Steam/compatibilitytools.d"),
	} {
		matches, _ := filepath.Glob(filepath.Join(dir, "GE-Proton*"))
		for _, m := range matches {
			if st, err := os.Stat(filepath.Join(m, "proton")); err == nil && !st.IsDir() {
				found = append(found, m)
			}
		}
	}
	if len(found) == 0 {
		return "GE-Proton"
	}
	sort.Slice(found, func(i, j int) bool { return versionLess(filepath.Base(found[i]), filepath.Base(found[j])) })
	return found[len(found)-1]
}

// versionLess compares names like GE-Proton9-27 and GE-Proton10-3 numerically.
func versionLess(a, b string) bool {
	na, nb := numbers(a), numbers(b)
	for i := 0; i < len(na) && i < len(nb); i++ {
		if na[i] != nb[i] {
			return na[i] < nb[i]
		}
	}
	return len(na) < len(nb)
}

func numbers(s string) []int {
	var out []int
	n, in := 0, false
	for _, r := range s + "x" {
		if r >= '0' && r <= '9' {
			n, in = n*10+int(r-'0'), true
			continue
		}
		if in {
			out = append(out, n)
		}
		n, in = 0, false
	}
	return out
}

func prereqWarnings(s Settings) []string {
	var out []string
	if _, err := exec.LookPath("umu-run"); err != nil {
		out = append(out, "umu-run was not found, and the launcher needs it to start the game through Proton. Install umu-launcher (Arch: pacman -S umu-launcher).")
	}
	if s.ProtonPath == "" && defaultProton() == "GE-Proton" {
		out = append(out, "No GE-Proton found in Steam's compatibilitytools.d, so the first start downloads the latest one (~500 MB).")
	}
	return out
}

func appWindow(url, profileDir string) *exec.Cmd {
	for _, name := range []string{"chromium", "google-chrome-stable", "google-chrome", "chromium-browser", "brave", "brave-browser", "microsoft-edge", "vivaldi"} {
		if p, err := exec.LookPath(name); err == nil {
			return exec.Command(p, "--app="+url, "--window-size=820,540", "--user-data-dir="+profileDir,
				"--no-first-run", "--no-default-browser-check")
		}
	}
	return nil
}

func openBrowser(url string) error {
	if strings.HasPrefix(runtime.GOOS, "darwin") {
		return exec.Command("open", url).Start()
	}
	return exec.Command("xdg-open", url).Start()
}
