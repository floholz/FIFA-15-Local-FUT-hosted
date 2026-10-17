package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

const goos = "windows"

func gameCommand(_ Settings, gameDir, _ string) (*exec.Cmd, error) {
	cmd := exec.Command(filepath.Join(gameDir, "fifa15.exe"))
	cmd.Dir = gameDir
	return cmd, nil
}

func prereqWarnings(Settings) []string {
	var out []string
	win := os.Getenv("WINDIR")
	if win == "" {
		win = `C:\Windows`
	}
	for _, sub := range []string{"System32", "SysWOW64"} {
		if _, err := os.Stat(filepath.Join(win, sub, "msvcr110.dll")); err != nil {
			out = append(out, "The Microsoft Visual C++ 2012 runtime seems to be missing, and FIFA 15 needs it. "+
				"Install both vcredist_x64.exe and vcredist_x86.exe from https://www.microsoft.com/en-us/download/details.aspx?id=30679")
			break
		}
	}
	return out
}

// appWindow opens the UI as an app window in Edge (always present on Windows 10/11).
func appWindow(url, profileDir string) *exec.Cmd {
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LOCALAPPDATA"} {
		base := os.Getenv(env)
		if base == "" {
			continue
		}
		for _, rel := range []string{`Microsoft\Edge\Application\msedge.exe`, `Google\Chrome\Application\chrome.exe`} {
			p := filepath.Join(base, rel)
			if _, err := os.Stat(p); err == nil {
				return exec.Command(p, "--app="+url, "--window-size=1000,820", "--user-data-dir="+profileDir,
					"--no-first-run", "--no-default-browser-check")
			}
		}
	}
	return nil
}

func openBrowser(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
