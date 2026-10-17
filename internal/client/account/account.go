// Package account holds the player's hosted settings and logs them in on the server.
//
// Same files and wire format as the Python client mode (hosted_config.py,
// _register_with_server in server.py), so an identity made by CONNECT_TO_SERVER.cmd
// or play.sh keeps working in the launcher and the other way round.
package account

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Canonical server ports. Clients rewrite FIFA's hard-coded ports 1:1, so a
// hosted server always uses these.
const (
	RedirectPort  = 42230
	BlazePort     = 10051
	QoSPort       = 17502
	EASWPort      = 42232
	FUTPort       = 8199
	LegacyFUTPort = 8099
	LSXPort       = 3216
)

// RuntimeRoot is where hosted.json, backups and logs live (shared with the Python tools).
func RuntimeRoot() string {
	if v := os.Getenv("FUT15_RUNTIME_ROOT"); v != "" {
		return v
	}
	if v := os.Getenv("LOCALAPPDATA"); v != "" {
		return filepath.Join(v, "FIFA15LocalFUT")
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "AppData", "Local", "FIFA15LocalFUT")
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "fifa15-local-fut")
	}
	return filepath.Join(home, ".local", "share", "fifa15-local-fut")
}

// Hosted is hosted.json. Unknown keys are kept when saving.
type Hosted struct {
	path string
	m    map[string]any
}

func LoadHosted() *Hosted {
	h := &Hosted{path: filepath.Join(RuntimeRoot(), "hosted.json"), m: map[string]any{}}
	if raw, err := os.ReadFile(h.path); err == nil {
		_ = json.Unmarshal(raw, &h.m)
		if h.m == nil {
			h.m = map[string]any{}
		}
	}
	return h
}

func (h *Hosted) Get(key string) string {
	if v, ok := h.m[key].(string); ok {
		return v
	}
	return ""
}

func (h *Hosted) Set(key, value string) { h.m[key] = value }

func (h *Hosted) Save() error {
	if err := os.MkdirAll(filepath.Dir(h.path), 0o755); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(h.m, "", "  ")
	return os.WriteFile(h.path, raw, 0o600)
}

// CleanName applies the server's rules: collapsed spaces, at most 20 characters.
func CleanName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if r := []rune(name); len(r) > 20 {
		name = string(r[:20])
	}
	return name
}

// EnsureSecret returns the player secret, creating one when missing. The secret
// proves ownership of the player name on the server, so it is never replaced.
func (h *Hosted) EnsureSecret() string {
	s := h.Get("player_secret")
	if len(s) < 8 {
		var b [16]byte
		_, _ = rand.Read(b[:])
		s = hex.EncodeToString(b[:])
		h.Set("player_secret", s)
	}
	return s
}

// ServerPorts is what a player's PC must reach on the server (TCP).
var ServerPorts = []struct {
	Label string
	Port  int
}{
	{"Redirector", RedirectPort}, {"Game/Blaze", BlazePort}, {"QoS HTTP", QoSPort},
	{"EASW HTTP", EASWPort}, {"FUT/POW", FUTPort}, {"FUT compat", LegacyFUTPort},
}

// Probe returns the services on host that do not accept TCP connections.
func Probe(host string, timeout time.Duration) []string {
	var mu sync.Mutex
	var wg sync.WaitGroup
	missing := []string{}
	for _, p := range ServerPorts {
		wg.Add(1)
		go func(label string, port int) {
			defer wg.Done()
			c, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), timeout)
			if err != nil {
				mu.Lock()
				missing = append(missing, fmt.Sprintf("%s (%d)", label, port))
				mu.Unlock()
				return
			}
			c.Close()
		}(p.Label, p.Port)
	}
	wg.Wait()
	return missing
}

// Login is the server's /localfut/register reply.
type Login struct {
	UserID        int64  `json:"userId"`
	Name          string `json:"name"`
	NucleusID     int64  `json:"nucleusId"`
	PersonaID     int64  `json:"personaId"`
	Token         string `json:"token"`
	ClubName      string `json:"clubName"`
	Credits       int64  `json:"credits"`
	ServerVersion string `json:"serverVersion"`
	Account       string `json:"account"` // OUFT username when logged in with an account
	Club          string `json:"club"`    // existing | linked | created | legacy
}

// ErrNameTaken means another player owns that name on the server.
var ErrNameTaken = errors.New("name taken")

// Register logs this PC's player in on the server and binds its MAC addresses.
//
// With accountToken (an OUFT login) the server identifies the player through the
// accounts service; name + secret are then only used to link the player's old club
// on the first account login. Without it, the old name + secret + access code login.
func Register(host, accountToken, name, secret, accessCode, version string) (*Login, error) {
	macs := LocalMACs()
	primary := ""
	if len(macs) > 0 {
		primary = macs[0]
	}
	payload := map[string]any{
		"name": name, "secret": secret, "version": version,
		"mac": primary, "macs": macs, "access_code": accessCode,
	}
	if accountToken != "" {
		payload["account_token"] = accountToken
	}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("http://%s/localfut/register", net.JoinHostPort(host, fmt.Sprint(FUTPort)))
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("could not reach %s: %w", url, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, ErrLoginExpired
		}
		if resp.StatusCode == http.StatusForbidden && strings.Contains(e.Error, "already taken") {
			return nil, fmt.Errorf("%w: %s", ErrNameTaken, e.Error)
		}
		return nil, fmt.Errorf("server refused the login (%s): %s", resp.Status, orStatus(e.Error, "no details"))
	}
	var l Login
	if err := json.Unmarshal(raw, &l); err != nil || l.Token == "" {
		return nil, fmt.Errorf("unexpected login reply: %.200s", raw)
	}
	return &l, nil
}

func orStatus(msg, status string) string {
	if msg == "" {
		return status
	}
	return msg
}

// NormalizeMAC turns "d8:43:ae:40:20:1e" into "d843ae40201e" ("" if unusable).
func NormalizeMAC(v string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(v) {
		if strings.ContainsRune("0123456789abcdef", r) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) != 12 || s == "000000000000" {
		return ""
	}
	return s
}

// LocalMACs lists every physical adapter's MAC. FIFA reports the MAC of the
// adapter it uses, and the server recognises the player by it.
func LocalMACs() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(m string) {
		if m = NormalizeMAC(m); m != "" && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		if i.Flags&net.FlagLoopback != 0 || len(i.HardwareAddr) != 6 {
			continue
		}
		if runtime.GOOS == "linux" {
			// physical NICs only (no bridges, tunnels, veths)
			if _, err := os.Stat(filepath.Join("/sys/class/net", i.Name, "device")); err != nil {
				continue
			}
		}
		add(i.HardwareAddr.String())
	}
	if len(out) > 16 {
		out = out[:16]
	}
	return out
}

// ResolveIPv4 resolves host for the routing files: the hook does not resolve DNS.
func ResolveIPv4(host string) (string, error) {
	if ip := net.ParseIP(host); ip != nil {
		return host, nil
	}
	addrs, err := net.LookupIP(host)
	if err != nil {
		return "", err
	}
	for _, a := range addrs {
		if v4 := a.To4(); v4 != nil {
			return v4.String(), nil
		}
	}
	return "", fmt.Errorf("%s has no IPv4 address", host)
}
