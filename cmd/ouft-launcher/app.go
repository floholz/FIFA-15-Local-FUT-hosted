package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/account"
	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/gamedir"
	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/install"
	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/lsx"
	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/routing"
)

const defaultServer = "fut15.fhun.site"

// retiredServers were renamed; a saved one is moved to defaultServer.
var retiredServers = map[string]bool{"fut.fhun.site": true}

// Settings are what the player can change in the UI.
type Settings struct {
	GameDir string `json:"gameDir"`
	Server  string `json:"server"`
	// AccountsURL is the OUFT account service (login); Username the account to log in with.
	AccountsURL string `json:"accountsUrl"`
	Username    string `json:"username"`
	// Name + AccessCode: the old login, for servers without account login.
	Name       string `json:"name"`
	AccessCode string `json:"accessCode"`
	// Linux only
	Prefix     string `json:"prefix"`
	ProtonPath string `json:"protonPath"`
	WineD3D    bool   `json:"wineD3D"`
	// Play even when fifa15.exe is not a build the hook is known to fit.
	AllowUnknownExe bool `json:"allowUnknownExe"`
}

// launcherPrefs is launcher.json: settings hosted.json/game-dir.txt do not hold.
type launcherPrefs struct {
	Prefix          string `json:"prefix,omitempty"`
	ProtonPath      string `json:"protonPath,omitempty"`
	WineD3D         bool   `json:"wineD3D,omitempty"`
	AllowUnknownExe bool   `json:"allowUnknownExe,omitempty"`
}

type App struct {
	mu sync.Mutex

	version     string
	launcherDir string
	payloadDir  string
	rt          string
	manifest    *install.Manifest
	manifestErr string
	hosted      *account.Hosted
	set         Settings
	log         *log.Logger
	logMu       sync.Mutex // guards logs; never take a.mu while holding it
	logs        []string

	exeHash  string
	exeKnown bool
	exeErr   string
	outdated []string
	checked  bool // install state computed for set.GameDir

	login     *account.Login
	serverMsg string
	serverOK  bool
	// OUFT account session (token saved in hosted.json; the password is never stored).
	session *account.Session
	// update is a newer OUFT Online than this one, when the account service knows one.
	update *account.Release

	busy     string
	lastErr  string
	lastInfo string

	lsxSrv  *lsx.Server
	lsxMode string

	gameCmd     *exec.Cmd
	gameRunning bool
	gameMsg     string
}

func newApp(version, launcherDir string) *App {
	a := &App{version: version, launcherDir: launcherDir, rt: account.RuntimeRoot()}
	_ = os.MkdirAll(filepath.Join(a.rt, "logs"), 0o755)
	logFile, _ := os.OpenFile(filepath.Join(a.rt, "logs", "launcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	a.log = log.New(&ringWriter{app: a, file: logFile}, "", log.LstdFlags)

	a.payloadDir = filepath.Join(launcherDir, "payload")
	if m, err := install.LoadManifest(a.payloadDir); err != nil {
		a.manifestErr = fmt.Sprintf("The payload folder next to the launcher is missing or incomplete (%v). Unzip the whole launcher zip and start it from there.", err)
	} else {
		a.manifest = m
	}

	a.hosted = account.LoadHosted()
	a.set.Server = a.hosted.Get("server_host")
	if a.set.Server == "" || retiredServers[strings.ToLower(a.set.Server)] {
		a.set.Server = defaultServer
	}
	a.set.Name = a.hosted.Get("player_name")
	a.set.AccessCode = a.hosted.Get("access_code")
	a.set.AccountsURL = a.hosted.Get("accounts_url")
	if a.set.AccountsURL == "" {
		a.set.AccountsURL = account.DefaultAccountsURL
	}
	a.set.Username = a.hosted.Get("account_username")
	if tok := a.hosted.Get("account_token"); tok != "" {
		a.session = &account.Session{Token: tok, Username: a.set.Username}
	}
	var prefs launcherPrefs
	if raw, err := os.ReadFile(filepath.Join(a.rt, "launcher.json")); err == nil {
		_ = json.Unmarshal(raw, &prefs)
	}
	a.set.Prefix, a.set.ProtonPath, a.set.WineD3D, a.set.AllowUnknownExe = prefs.Prefix, prefs.ProtonPath, prefs.WineD3D, prefs.AllowUnknownExe
	remembered, _ := os.ReadFile(filepath.Join(a.rt, "game-dir.txt"))
	a.set.GameDir = gamedir.Detect(strings.TrimSpace(string(remembered)), launcherDir)
	a.log.Printf("OUFT Online %s, runtime folder %s", version, a.rt)
	go a.checkUpdate()
	return a
}

// ringWriter keeps the last log lines for the UI and appends to launcher.log.
type ringWriter struct {
	app  *App
	file *os.File
}

func (w *ringWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	w.app.logMu.Lock()
	defer w.app.logMu.Unlock()
	w.app.logs = append(w.app.logs, line)
	if len(w.app.logs) > 300 {
		w.app.logs = w.app.logs[len(w.app.logs)-300:]
	}
	fmt.Println(line)
	if w.file != nil {
		w.file.Write(p)
	}
	return len(p), nil
}

// ---- state for the UI ---------------------------------------------------------

type State struct {
	Version     string   `json:"version"`
	OS          string   `json:"os"`
	Settings    Settings `json:"settings"`
	ManifestErr string   `json:"manifestErr,omitempty"`
	Warnings    []string `json:"warnings"`

	GameFound bool   `json:"gameFound"`
	ExeKnown  bool   `json:"exeKnown"`
	ExeHash   string `json:"exeHash,omitempty"`
	ExeErr    string `json:"exeErr,omitempty"`

	Installed bool     `json:"installed"`
	Outdated  []string `json:"outdated"`
	Checked   bool     `json:"checked"`
	HasBackup bool     `json:"hasBackup"`

	LoggedIn    bool   `json:"loggedIn"`
	AccountUser string `json:"accountUser,omitempty"`
	// Update: a newer OUFT Online is available ("15.3.0"), empty if not.
	Update string `json:"update,omitempty"`

	ServerOK  bool           `json:"serverOK"`
	ServerMsg string         `json:"serverMsg,omitempty"`
	Login     *account.Login `json:"login,omitempty"`

	Busy     string `json:"busy,omitempty"`
	LastErr  string `json:"lastErr,omitempty"`
	LastInfo string `json:"lastInfo,omitempty"`

	LSX         string   `json:"lsx,omitempty"`
	GameRunning bool     `json:"gameRunning"`
	GameMsg     string   `json:"gameMsg,omitempty"`
	Logs        []string `json:"logs"`
}

func (a *App) State() State {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := State{
		Version: a.version, OS: goos, Settings: a.set, ManifestErr: a.manifestErr,
		Warnings:  prereqWarnings(a.set),
		GameFound: gamedir.Valid(a.set.GameDir) != "", ExeKnown: a.exeKnown, ExeHash: a.exeHash, ExeErr: a.exeErr,
		Outdated: a.outdated, Checked: a.checked,
		ServerOK: a.serverOK, ServerMsg: a.serverMsg, Login: a.login,
		LoggedIn: a.session != nil,
		Busy:     a.busy, LastErr: a.lastErr, LastInfo: a.lastInfo,
		LSX: a.lsxMode, GameRunning: a.gameRunning, GameMsg: a.gameMsg,
	}
	a.logMu.Lock()
	s.Logs = append([]string{}, a.logs...)
	a.logMu.Unlock()
	if a.session != nil {
		s.AccountUser = a.session.Username
	}
	if a.update != nil {
		s.Update = a.update.Version
	}
	s.Installed = a.checked && len(a.outdated) == 0
	if _, err := os.Stat(install.BackupDir(a.rt)); err == nil {
		s.HasBackup = true
	}
	return s
}

// ---- actions --------------------------------------------------------------------

// run executes a long action in the background; only one at a time.
func (a *App) run(label string, fn func() error) error {
	a.mu.Lock()
	if a.busy != "" {
		a.mu.Unlock()
		return fmt.Errorf("please wait: %s", a.busy)
	}
	a.busy, a.lastErr, a.lastInfo = label, "", ""
	a.mu.Unlock()
	go func() {
		err := fn()
		a.mu.Lock()
		a.busy = ""
		if err != nil {
			a.lastErr = err.Error()
			a.log.Printf("%s failed: %v", label, err)
		}
		a.mu.Unlock()
	}()
	return nil
}

func (a *App) info(msg string) {
	a.mu.Lock()
	a.lastInfo = msg
	a.mu.Unlock()
	a.log.Print(msg)
}

func (a *App) progress(msg string) {
	a.mu.Lock()
	a.busy = msg
	a.mu.Unlock()
	a.log.Print(msg)
}

// SaveSettings stores what the UI sent. Changing server or name drops the login.
func (a *App) SaveSettings(in Settings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	in.Server = strings.TrimSpace(in.Server)
	in.Name = account.CleanName(in.Name)
	in.AccessCode = strings.TrimSpace(in.AccessCode)
	in.Username = strings.TrimSpace(in.Username)
	in.AccountsURL = strings.TrimRight(strings.TrimSpace(in.AccountsURL), "/")
	if in.AccountsURL == "" {
		in.AccountsURL = account.DefaultAccountsURL
	}
	if in.Server != a.set.Server || in.Name != a.set.Name || in.AccessCode != a.set.AccessCode {
		a.login, a.serverOK, a.serverMsg = nil, false, ""
	}
	if a.session != nil {
		in.Username = a.session.Username // the logged-in account, not an edited field
	}
	if dir := gamedir.Valid(in.GameDir); dir != "" {
		in.GameDir = dir
	}
	if in.GameDir != a.set.GameDir {
		a.checked, a.outdated, a.exeHash, a.exeKnown, a.exeErr = false, nil, "", false, ""
	}
	a.set = in

	a.hosted.Set("mode", "client")
	a.hosted.Set("server_host", in.Server)
	if in.Name != "" {
		a.hosted.Set("player_name", in.Name)
	}
	a.hosted.Set("access_code", in.AccessCode)
	a.hosted.Set("accounts_url", in.AccountsURL)
	if err := a.hosted.Save(); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(launcherPrefs{in.Prefix, in.ProtonPath, in.WineD3D, in.AllowUnknownExe}, "", "  ")
	if err := os.WriteFile(filepath.Join(a.rt, "launcher.json"), raw, 0o644); err != nil {
		return err
	}
	if gamedir.Valid(in.GameDir) != "" {
		_ = os.WriteFile(filepath.Join(a.rt, "game-dir.txt"), []byte(in.GameDir+"\n"), 0o644)
	}
	return nil
}

// CheckGame hashes fifa15.exe and compares the installed files with the payload.
func (a *App) CheckGame() error {
	return a.run("Checking the game folder", a.checkGame)
}

func (a *App) checkGame() error {
	a.mu.Lock()
	dir := gamedir.Valid(a.set.GameDir)
	m := a.manifest
	a.mu.Unlock()
	if dir == "" {
		return errors.New("fifa15.exe was not found in that folder")
	}
	if m == nil {
		return errors.New(a.manifestErr)
	}
	hash, known, err := m.ExeStatus(dir)
	outdated := m.Outdated(dir, a.rt)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.exeHash, a.exeKnown, a.outdated, a.checked = hash, known, outdated, true
	a.exeErr = ""
	if err != nil {
		a.exeErr = err.Error()
	}
	a.log.Printf("Game folder %s: exe %s (known=%v), %d file(s) to install", dir, short(hash), known, len(outdated))
	return nil
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// Connect checks the server's ports and logs the player in.
func (a *App) Connect() error { return a.run("Connecting to the server", a.connect) }

func (a *App) connect() error {
	a.mu.Lock()
	host, code, accountsURL := a.set.Server, a.set.AccessCode, a.set.AccountsURL
	legacyName, legacySecret := a.hosted.Get("player_name"), a.hosted.Get("player_secret")
	session := a.session
	a.mu.Unlock()
	fail := func(msg string) error {
		a.mu.Lock()
		a.serverOK, a.serverMsg, a.login = false, msg, nil
		a.mu.Unlock()
		return errors.New(msg)
	}
	if host == "" {
		return fail("Enter the server address.")
	}
	a.progress("Checking the server " + host)
	if missing := account.Probe(host, 4*time.Second); len(missing) > 0 {
		return fail(fmt.Sprintf("The server %s is not reachable on: %s. Check the address and your internet connection, or ask the host whether the server is running.",
			host, strings.Join(missing, ", ")))
	}
	info, err := account.Status(host)
	if err != nil {
		info = &account.ServerInfo{} // an older server: old login
	}

	var login *account.Login
	if info.Accounts {
		if session == nil {
			return fail("Log in with your OUFT account first.")
		}
		a.progress("Checking your login")
		fresh, err := account.Refresh(accountsURL, session.Token)
		if errors.Is(err, account.ErrLoginExpired) {
			a.forgetSession()
			return fail("Your login has expired. Log in again.")
		}
		if err != nil {
			return fail(err.Error())
		}
		a.saveSession(fresh)
		a.progress("Logging in as " + fresh.Username)
		// The old name + secret (if this PC has them) link the old club on the first login.
		login, err = account.Register(host, fresh.Token, legacyName, legacySecret, "", "launcher-"+a.version)
		if errors.Is(err, account.ErrLoginExpired) {
			a.forgetSession()
			return fail("Your login has expired. Log in again.")
		}
		if err != nil {
			return fail(err.Error())
		}
	} else {
		// Old login: a player name (the old one, else the account name) + this PC's secret.
		name := legacyName
		if name == "" && session != nil {
			name = account.CleanName(session.Username)
		}
		if name == "" {
			return fail("Log in with your OUFT account first.")
		}
		a.mu.Lock()
		a.hosted.Set("player_name", name)
		secret := a.hosted.EnsureSecret()
		err := a.hosted.Save()
		a.mu.Unlock()
		if err != nil {
			return fail("Could not save your player identity: " + err.Error())
		}
		a.progress("Logging in as " + name)
		login, err = account.Register(host, "", name, secret, code, "launcher-"+a.version)
		if errors.Is(err, account.ErrNameTaken) {
			return fail(fmt.Sprintf("The name %q already belongs to another player on this (older) server.", name))
		}
		if err != nil {
			return fail(err.Error())
		}
	}
	a.mu.Lock()
	a.login, a.serverOK = login, true
	a.serverMsg = fmt.Sprintf("Logged in as %s: club %q, %d coins.", login.Name, login.ClubName, login.Credits)
	if login.Club == "linked" {
		a.serverMsg += " Your old club is now linked to your OUFT account."
	}
	if a.lsxSrv != nil {
		a.lsxSrv.SetIdentity(identity(login))
	}
	a.mu.Unlock()
	a.log.Printf("Logged in as %s (user %d, persona %d, club %s) on %s, server %s", login.Name, login.UserID, login.PersonaID, login.Club, host, login.ServerVersion)
	return nil
}

// LogIn exchanges username + password for an OUFT login (the password is not kept).
func (a *App) LogIn(username, password string) error {
	return a.run("Logging in", func() error {
		a.mu.Lock()
		url := a.set.AccountsURL
		a.mu.Unlock()
		if strings.TrimSpace(username) == "" || password == "" {
			return errors.New("enter your OUFT username and password")
		}
		s, err := account.LogIn(url, username, password)
		if err != nil {
			return err
		}
		a.saveSession(s)
		a.mu.Lock()
		a.login, a.serverOK, a.serverMsg = nil, false, ""
		a.mu.Unlock()
		a.info("Logged in as " + s.Username + ".")
		go a.checkUpdate()
		return nil
	})
}

// checkUpdate asks the account service for the newest OUFT Online (needs a login;
// dev builds never see an update). Failures are only logged: it is a hint, not a gate.
func (a *App) checkUpdate() {
	a.mu.Lock()
	session, url := a.session, a.set.AccountsURL
	a.mu.Unlock()
	if session == nil {
		return
	}
	rel, err := account.LatestRelease(url, session.Token)
	if err != nil {
		a.log.Printf("update check: %v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if account.NewerVersion(rel.Version, a.version) {
		a.update = rel
		a.log.Printf("A newer OUFT Online is available: %s (this is %s)", rel.Version, a.version)
	} else {
		a.update = nil
	}
}

// OpenUpdate opens the newer version's download in the default browser. Only the
// URL from the update check is ever opened, never one sent by the page.
func (a *App) OpenUpdate() error {
	a.mu.Lock()
	rel := a.update
	a.mu.Unlock()
	if rel == nil {
		return errors.New("no update available")
	}
	return openBrowser(rel.ZipURL)
}

// LogOut forgets the saved login on this PC.
func (a *App) LogOut() error {
	a.forgetSession()
	a.mu.Lock()
	a.login, a.serverOK, a.serverMsg = nil, false, ""
	a.mu.Unlock()
	a.info("Logged out.")
	return nil
}

func (a *App) saveSession(s *account.Session) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = s
	a.set.Username = s.Username
	a.hosted.Set("account_token", s.Token)
	a.hosted.Set("account_username", s.Username)
	if err := a.hosted.Save(); err != nil {
		a.log.Printf("could not save the login: %v", err)
	}
}

func (a *App) forgetSession() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.session = nil
	a.hosted.Set("account_token", "")
	if err := a.hosted.Save(); err != nil {
		a.log.Printf("could not save: %v", err)
	}
}

func identity(l *account.Login) lsx.Identity {
	return lsx.Identity{Name: l.Name, NucleusID: l.NucleusID, PersonaID: l.PersonaID, Token: l.Token}
}

// Install copies the payload into the game folder.
func (a *App) Install() error { return a.run("Installing", a.install) }

func (a *App) install() error {
	a.mu.Lock()
	dir, m, running := gamedir.Valid(a.set.GameDir), a.manifest, a.gameRunning
	a.mu.Unlock()
	if running {
		return errors.New("close the game first")
	}
	if dir == "" || m == nil {
		return errors.New("choose the game folder first")
	}
	if err := m.Install(a.payloadDir, dir, a.rt, a.progress); err != nil {
		if install.IsPermission(err) {
			return fmt.Errorf("%v. The game folder is write-protected: start the launcher as administrator (right-click, \"Run as administrator\") or move the game out of Program Files", err)
		}
		return err
	}
	if err := a.checkGame(); err != nil {
		return err
	}
	a.info("Files installed.")
	return nil
}

// Restore puts the original game files back.
func (a *App) Restore() error {
	return a.run("Restoring the original files", func() error {
		a.mu.Lock()
		dir, m, running := gamedir.Valid(a.set.GameDir), a.manifest, a.gameRunning
		a.mu.Unlock()
		if running {
			return errors.New("close the game first")
		}
		if dir == "" || m == nil {
			return errors.New("choose the game folder first")
		}
		if err := m.Restore(dir, a.rt, a.progress); err != nil {
			return err
		}
		if err := a.checkGame(); err != nil {
			return err
		}
		a.info("Original files restored. The game is back to how it was before the launcher.")
		return nil
	})
}

// Play does whatever is still missing (check, login, install), then starts the game.
func (a *App) Play() error {
	return a.run("Starting", func() error {
		a.mu.Lock()
		running := a.gameRunning
		a.mu.Unlock()
		if running {
			return errors.New("the game is already running")
		}
		if err := a.checkGame(); err != nil {
			return err
		}
		a.mu.Lock()
		known, allow, outdated := a.exeKnown, a.set.AllowUnknownExe, len(a.outdated)
		a.mu.Unlock()
		if !known && !allow {
			return errors.New("this fifa15.exe is not the version the launcher supports, so the game would most likely not connect. Use the same game copy as your friends, or tick \"Try anyway\"")
		}
		if err := a.connect(); err != nil {
			return err
		}
		if outdated > 0 {
			if err := a.install(); err != nil {
				return err
			}
		}
		return a.launch()
	})
}

func (a *App) launch() error {
	a.mu.Lock()
	set, dir, login := a.set, gamedir.Valid(a.set.GameDir), a.login
	a.mu.Unlock()
	host := set.Server

	ip, err := account.ResolveIPv4(host)
	if err != nil {
		return fmt.Errorf("could not resolve %s: %w", host, err)
	}
	a.progress("Pointing the game at " + ip)
	if err := routing.Write(dir, ip, "launcher-"+a.version); err != nil {
		return err
	}

	a.mu.Lock()
	if a.lsxSrv == nil {
		addr := fmt.Sprintf("127.0.0.1:%d", account.LSXPort)
		if lsx.ListenerPresent(addr, 300*time.Millisecond) {
			a.lsxMode = "external"
			a.log.Printf("Something already listens on %s (EA App/Origin?); the game will use it", addr)
		} else if srv, err := lsx.Listen(addr, identity(login), a.log); err != nil {
			a.mu.Unlock()
			return fmt.Errorf("could not start the Origin stub on %s: %w", addr, err)
		} else {
			a.lsxSrv, a.lsxMode = srv, "local"
			a.log.Printf("Origin LSX stub listening on %s", addr)
		}
	} else {
		a.lsxSrv.SetIdentity(identity(login))
	}
	lsxMode := a.lsxMode
	a.mu.Unlock()
	_ = routing.WriteRuntimePorts(ip, lsxMode)

	a.progress("Launching the game")
	cmd, err := gameCommand(set, dir, a.rt)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start the game: %w", err)
	}
	a.mu.Lock()
	a.gameCmd, a.gameRunning, a.gameMsg = cmd, true, "The game is running. Keep OUFT Online open while you play."
	a.mu.Unlock()
	a.log.Printf("Game started (pid %d)", cmd.Process.Pid)
	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		a.gameRunning, a.gameCmd = false, nil
		a.gameMsg = "The game has closed."
		if err != nil {
			a.gameMsg = "The game has closed (" + err.Error() + ")."
		}
		a.mu.Unlock()
		a.log.Print(a.gameMsg)
	}()
	return nil
}

// Shutdown stops the LSX stub.
func (a *App) Shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lsxSrv != nil {
		a.lsxSrv.Close()
		a.lsxSrv = nil
	}
	routing.RemoveRuntimePorts()
}

func (a *App) GameRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.gameRunning
}
