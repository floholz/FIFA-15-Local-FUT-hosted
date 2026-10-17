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
