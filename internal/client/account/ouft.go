package account

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// DefaultAccountsURL is the OUFT accounts service (register, login).
const DefaultAccountsURL = "https://ouft.fhun.site"

var (
	// ErrBadLogin: wrong username or password.
	ErrBadLogin = errors.New("wrong username or password")
	// ErrLoginExpired: the saved login is no longer valid; log in again.
	ErrLoginExpired = errors.New("your login has expired, please log in again")
)

// Session is an OUFT login.
type Session struct {
	Token    string
	Username string
}

var accountsClient = &http.Client{Timeout: 10 * time.Second}

func accountsCall(url, token string, body any) (*Session, int, error) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", token)
	}
	resp, err := accountsClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("could not reach the OUFT account service: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var out struct {
		Token  string `json:"token"`
		Record struct {
			Username string `json:"username"`
		} `json:"record"`
	}
	if err := json.Unmarshal(data, &out); err != nil || out.Token == "" {
		return nil, resp.StatusCode, fmt.Errorf("unexpected reply from the OUFT account service")
	}
	return &Session{Token: out.Token, Username: out.Record.Username}, resp.StatusCode, nil
}

// LogIn exchanges username + password for a login token.
func LogIn(accountsURL, username, password string) (*Session, error) {
	s, status, err := accountsCall(strings.TrimRight(accountsURL, "/")+"/api/collections/users/auth-with-password", "",
		map[string]string{"identity": strings.TrimSpace(username), "password": password})
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusBadRequest:
		return nil, ErrBadLogin
	case status == http.StatusTooManyRequests:
		return nil, errors.New("too many login attempts, wait a moment and try again")
	case s == nil:
		return nil, fmt.Errorf("the OUFT account service answered %d", status)
	}
	return s, nil
}

// Refresh swaps a valid token for a fresh one (keeps an active player logged in).
func Refresh(accountsURL, token string) (*Session, error) {
	s, status, err := accountsCall(strings.TrimRight(accountsURL, "/")+"/api/collections/users/auth-refresh", token, struct{}{})
	switch {
	case err != nil:
		return nil, err
	case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound:
		return nil, ErrLoginExpired
	case s == nil:
		return nil, fmt.Errorf("the OUFT account service answered %d", status)
	}
	return s, nil
}

// ServerInfo is what a game server reports about its login.
type ServerInfo struct {
	Accounts       bool   `json:"accounts"`
	RequireAccount bool   `json:"requireAccount"`
	Version        string `json:"version"`
}

// Status asks a game server whether it supports OUFT account login.
func Status(host string) (*ServerInfo, error) {
	url := fmt.Sprintf("http://%s/localfut/status", net.JoinHostPort(host, fmt.Sprint(FUTPort)))
	resp, err := (&http.Client{Timeout: 6 * time.Second}).Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var info ServerInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// Release is the newest OUFT Online, as the account service reports it.
type Release struct {
	Version    string `json:"version"`
	Name       string `json:"name"`
	Prerelease bool   `json:"prerelease"`
	PageURL    string `json:"pageUrl"`
	ZipURL     string `json:"zipUrl"`
	ZipSize    int64  `json:"zipSize"`
}

// LatestRelease asks the account service for the newest launcher (needs a login).
func LatestRelease(accountsURL, token string) (*Release, error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(accountsURL, "/")+"/api/ouft/launcher", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", token)
	resp, err := accountsClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("release check: %s", resp.Status)
	}
	var r Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(r.ZipURL, "https://") {
		return nil, errors.New("release check: no https download")
	}
	return &r, nil
}

// NewerVersion reports whether version a (e.g. "15.3.0") is newer than b. Versions
// that are not plain numbers (dev builds) are never newer and never outdated.
func NewerVersion(a, b string) bool {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n := 0
		if p == "" {
			return out, false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return out, false
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out, true
}
