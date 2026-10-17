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

const defaultServer = "fut.fhun.site"

// Settings are what the player can change in the UI.
type Settings struct {
	GameDir    string `json:"gameDir"`
	Server     string `json:"server"`
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
	if a.set.Server == "" {
		a.set.Server = defaultServer
	}
	a.set.Name = a.hosted.Get("player_name")
	a.set.AccessCode = a.hosted.Get("access_code")
	var prefs launcherPrefs
	if raw, err := os.ReadFile(filepath.Join(a.rt, "launcher.json")); err == nil {
		_ = json.Unmarshal(raw, &prefs)
	}
	a.set.Prefix, a.set.ProtonPath, a.set.WineD3D, a.set.AllowUnknownExe = prefs.Prefix, prefs.ProtonPath, prefs.WineD3D, prefs.AllowUnknownExe
	remembered, _ := os.ReadFile(filepath.Join(a.rt, "game-dir.txt"))
	a.set.GameDir = gamedir.Detect(strings.TrimSpace(string(remembered)), launcherDir)
	a.log.Printf("FUT15 launcher %s, runtime folder %s", version, a.rt)
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
		Busy: a.busy, LastErr: a.lastErr, LastInfo: a.lastInfo,
		LSX: a.lsxMode, GameRunning: a.gameRunning, GameMsg: a.gameMsg,
	}
	a.logMu.Lock()
	s.Logs = append([]string{}, a.logs...)
	a.logMu.Unlock()
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
	if in.Server != a.set.Server || in.Name != a.set.Name || in.AccessCode != a.set.AccessCode {
		a.login, a.serverOK, a.serverMsg = nil, false, ""
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
	outdated := m.Outdated(dir)
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
	host, name, code := a.set.Server, a.set.Name, a.set.AccessCode
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
	if name == "" {
		return fail("Pick a player name first. It identifies your club on the server.")
	}
	a.progress("Checking the server " + host)
	if missing := account.Probe(host, 4*time.Second); len(missing) > 0 {
		return fail(fmt.Sprintf("The server %s is not reachable on: %s. Check the address and your internet connection, or ask the host whether the server is running.",
			host, strings.Join(missing, ", ")))
	}
	a.progress("Logging in as " + name)
	a.mu.Lock()
	secret := a.hosted.EnsureSecret()
	err := a.hosted.Save()
	a.mu.Unlock()
	if err != nil {
		return fail("Could not save your player identity: " + err.Error())
	}
	login, err := account.Register(host, name, secret, code, "launcher-"+a.version)
	if errors.Is(err, account.ErrNameTaken) {
		return fail(fmt.Sprintf("The name %q already belongs to another player on this server. Choose a different name.", name))
	}
	if err != nil {
		return fail(err.Error())
	}
	a.mu.Lock()
	a.login, a.serverOK = login, true
	a.serverMsg = fmt.Sprintf("Logged in as %s: club %q, %d coins.", login.Name, login.ClubName, login.Credits)
	if a.lsxSrv != nil {
		a.lsxSrv.SetIdentity(identity(login))
	}
	a.mu.Unlock()
	a.log.Printf("Logged in as %s (user %d, persona %d) on %s, server %s", login.Name, login.UserID, login.PersonaID, host, login.ServerVersion)
	return nil
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
		return errors.New("close FIFA 15 first")
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
			return errors.New("close FIFA 15 first")
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
			return errors.New("FIFA 15 is already running")
		}
		if err := a.checkGame(); err != nil {
			return err
		}
		a.mu.Lock()
		known, allow, outdated := a.exeKnown, a.set.AllowUnknownExe, len(a.outdated)
		a.mu.Unlock()
		if !known && !allow {
			return errors.New("this fifa15.exe is not the version the launcher supports, so the game would most likely not connect. Use the same FIFA 15 copy as your friends, or tick \"Try anyway\"")
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
			a.log.Printf("Something already listens on %s (EA App/Origin?); FIFA will use it", addr)
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

	a.progress("Launching FIFA 15")
	cmd, err := gameCommand(set, dir, a.rt)
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not start FIFA 15: %w", err)
	}
	a.mu.Lock()
	a.gameCmd, a.gameRunning, a.gameMsg = cmd, true, "FIFA 15 is running. Keep the launcher open while you play."
	a.mu.Unlock()
	a.log.Printf("FIFA 15 started (pid %d)", cmd.Process.Pid)
	go func() {
		err := cmd.Wait()
		a.mu.Lock()
		a.gameRunning, a.gameCmd = false, nil
		a.gameMsg = "FIFA 15 has closed."
		if err != nil {
			a.gameMsg = "FIFA 15 has closed (" + err.Error() + ")."
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
