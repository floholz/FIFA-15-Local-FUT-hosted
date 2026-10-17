package gamedir

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// registryValue reads one string value with reg.exe (no extra dependencies).
func registryValue(key, name string) string {
	cmd := exec.Command("reg", "query", key, "/v", name)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "REG_SZ", 2)
		if len(fields) == 2 && strings.EqualFold(strings.TrimSpace(fields[0]), name) {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}

func platformCandidates() []string {
	var out []string
	for _, key := range []string{
		`HKLM\SOFTWARE\EA Games\FIFA 15`, `HKLM\SOFTWARE\WOW6432Node\EA Games\FIFA 15`, `HKCU\SOFTWARE\EA Games\FIFA 15`,
	} {
		for _, name := range []string{"Install Dir", "InstallDir", "InstallLocation"} {
			if v := registryValue(key, name); v != "" {
				out = append(out, v)
			}
		}
	}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)"} {
		if pf := os.Getenv(env); pf != "" {
			out = append(out,
				filepath.Join(pf, "EA Games", "FIFA 15"),
				filepath.Join(pf, "Origin Games", "FIFA 15"))
		}
	}
	for _, drive := range []string{"C:", "D:", "E:", "F:"} {
		out = append(out,
			drive+`\Games\FIFA 15`, drive+`\FIFA 15`, drive+`\Games\EA Games\FIFA 15`)
		out = append(out, searchGames(drive+`\Games`, 2)...)
	}
	return out
}
