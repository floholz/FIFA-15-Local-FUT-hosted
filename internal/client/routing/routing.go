// Package routing writes the files that point FIFA 15 at a hosted server.
//
// Same content as _write_runtime_routing in payload/localfut15/server.py (client
// mode): cl.ini for the FUT web URLs, EA-MITM.ini for the EA-MITM hook's host/port
// redirects, p2p-hook.ini for the p2p hook's demangler redirect.
package routing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/account"
)

// Write points the game in gameDir at serverIP. The hook does not resolve DNS,
// so serverIP must be an IP address.
func Write(gameDir, serverIP, version string) error {
	cl := fmt.Sprintf("; FIFA 15 Local FUT %s - generated routing (mode=client)\n"+
		"FUT_ENABLE_MENU = 1\n"+
		"FUT_TARGET_HOSTNAME = %[2]s\n"+
		"FUT_TARGET_PORT = %[3]d\n"+
		"FUT_RS4_BASE_URL = http://%[2]s:%[3]d/\n"+
		"FIFA_POW_NUCLEUS_PROXY_URL = http://%[2]s:%[3]d/\n", version, serverIP, account.LegacyFUTPort)

	var b strings.Builder
	fmt.Fprintf(&b, "; FIFA 15 Local FUT %s - generated routing (mode=client)\n; Target host: %s\n", version, serverIP)
	b.WriteString("[Hook]\nInitDelay=3000\n\n[Origin]\nLaunchFailureCheckRva=0x0390AD3F\n\n" +
		"[ProtoSSL]\nConnect=0x0000000143066524\nSend=0\nRecv=0\n\n[Redirect]\nCount=6\n\n")
	fmt.Fprintf(&b, "Redirect.0.Hostname=spring14.gosredirector.ea.com\nRedirect.0.Address=%s\nRedirect.0.Port=%d\nRedirect.0.Secure=0\n",
		serverIP, account.RedirectPort)
	// SourcePort stays on FIFA's original port so hard-coded calls get translated.
	for i, p := range []int{account.BlazePort, account.QoSPort, account.EASWPort, account.FUTPort, account.LegacyFUTPort} {
		fmt.Fprintf(&b, "\nRedirect.%[1]d.Hostname=%[2]s\nRedirect.%[1]d.SourcePort=%[3]d\nRedirect.%[1]d.Address=%[2]s\nRedirect.%[1]d.Port=%[3]d\nRedirect.%[1]d.Secure=0\n",
			i+1, serverIP, p)
	}

	hook := fmt.Sprintf("; FIFA 15 Local FUT %s - p2p hook config (mode=client)\nserver=%s\ndemangler_port=10000\nredirect=on\n",
		version, serverIP)

	files := map[string]string{"cl.ini": cl, "EA-MITM.ini": b.String(), "p2p-hook.ini": hook}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(gameDir, name), []byte(text), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}

// WriteRuntimePorts writes runtime_ports.json like the Python client, for the
// diagnostics scripts that read it.
func WriteRuntimePorts(serverIP, lsxMode string) error {
	ports := map[string]any{
		"mode": "client", "host": serverIP, "lsx_port": account.LSXPort, "lsx_mode": lsxMode,
		"redirect_port": account.RedirectPort, "blaze_port": account.BlazePort, "qos_port": account.QoSPort,
		"easw_port": account.EASWPort, "fut_port": account.FUTPort, "legacy_fut_port": account.LegacyFUTPort,
	}
	raw, _ := json.MarshalIndent(ports, "", "  ")
	root := account.RuntimeRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "runtime_ports.json"), raw, 0o644)
}

// RemoveRuntimePorts deletes runtime_ports.json when the launcher stops.
func RemoveRuntimePorts() {
	_ = os.Remove(filepath.Join(account.RuntimeRoot(), "runtime_ports.json"))
}
