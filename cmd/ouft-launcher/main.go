// Command ouft-launcher ("OUFT Online", Open Ultimate Football Team) sets up the
// game for a hosted server and starts it.
//
// It replaces CONNECT_TO_SERVER.cmd + PLAY_LOCAL_FUT15.cmd + the Python client mode:
// find the game, check its version, install the payload (with backup), log in on
// the server, write the routing files, run the Origin LSX stub and launch the game.
// The UI is a small local web page shown in an app window.
package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/account"
	"github.com/floholz/FIFA-15-Local-FUT-hosted/internal/client/gamedir"
)

var version = "dev"

//go:embed web/index.html
var indexHTML []byte

//go:embed web/logo.svg
var logoSVG []byte

func main() {
	noWindow := flag.Bool("no-window", false, "only print the UI address, do not open a window")
	flag.Parse()

	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	launcherDir := filepath.Dir(exe)
	if v := os.Getenv("FUT15_LAUNCHER_DIR"); v != "" { // dev: run from a build folder
		launcherDir = v
	}
	// One launcher per PC: a second start just shows the running one's window.
	if url := runningInstance(); url != "" {
		fmt.Println("OUFT Online is already running:", url)
		if !*noWindow {
			showWindow(url)
		}
		return
	}
	app := newApp(version, launcherDir)
	if app.manifest != nil && gamedir.Valid(app.set.GameDir) != "" {
		_ = app.CheckGame()
	}

	var tok [16]byte
	_, _ = rand.Read(tok[:])
	token := hex.EncodeToString(tok[:])
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("Could not start the launcher UI:", err)
		waitEnter()
		os.Exit(1)
	}
	url := fmt.Sprintf("http://%s/%s/", ln.Addr(), token)
	go func() { _ = http.Serve(ln, routes(app, "/"+token)) }()
	writeInstance(url)
	defer removeInstance(url)

	fmt.Println("==============================================================")
	fmt.Println(" OUFT Online", version)
	fmt.Println(" Keep this window open while you play.")
	fmt.Println(" If the launcher window got closed, open this address:")
	fmt.Println("  ", url)
	fmt.Println("==============================================================")

	windowClosed := make(chan struct{})
	if !*noWindow {
		if cmd := appWindow(url, uiProfile()); cmd != nil && cmd.Start() == nil {
			// The window is ours: close it when the launcher stops, so no stale
			// window is left behind.
			defer cmd.Process.Kill()
			started := time.Now()
			go func() {
				_ = cmd.Wait()
				// A browser that hands the window to an already running process
				// exits at once; then we cannot see the window close, so keep
				// running until this console window is closed.
				if time.Since(started) > 5*time.Second {
					close(windowClosed)
				}
			}()
		} else if err := openBrowser(url); err != nil {
			fmt.Println("Open this address in your browser:", url)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	for {
		select {
		case <-sig:
			app.Shutdown()
			return
		case <-windowClosed:
			windowClosed = nil
			// Closing the window ends the launcher, but never under a running game:
			// FIFA needs the Origin stub for the whole session.
			for app.GameRunning() {
				fmt.Println("The launcher window was closed; staying active until the game exits.")
				for app.GameRunning() {
					time.Sleep(2 * time.Second)
				}
			}
			app.Shutdown()
			return
		}
	}
}

func uiProfile() string { return filepath.Join(account.RuntimeRoot(), "launcher-ui") }

func instanceFile() string { return filepath.Join(account.RuntimeRoot(), "launcher-instance.txt") }

// runningInstance returns the UI address of a launcher that is already running.
func runningInstance() string {
	raw, err := os.ReadFile(instanceFile())
	if err != nil {
		return ""
	}
	url := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		return ""
	}
	client := http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(url + "api/state")
	if err != nil {
		return "" // stale file from a launcher that crashed
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	return url
}

func writeInstance(url string) {
	_ = os.MkdirAll(filepath.Dir(instanceFile()), 0o755)
	_ = os.WriteFile(instanceFile(), []byte(url+"\n"), 0o600)
}

func removeInstance(url string) {
	if raw, err := os.ReadFile(instanceFile()); err == nil && strings.TrimSpace(string(raw)) == url {
		_ = os.Remove(instanceFile())
	}
}

// showWindow opens another window onto a running launcher's UI.
func showWindow(url string) {
	if cmd := appWindow(url, uiProfile()); cmd != nil && cmd.Start() == nil {
		go cmd.Wait()
		return
	}
	_ = openBrowser(url)
}

func waitEnter() {
	fmt.Println("Press Enter to close.")
	var s string
	fmt.Scanln(&s)
}

func routes(app *App, base string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+base+"/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != base+"/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(indexHTML)
	})
	mux.HandleFunc("GET "+base+"/logo.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Write(logoSVG)
	})
	mux.HandleFunc("GET "+base+"/api/state", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, app.State())
	})
	action := func(name string, fn func(r *http.Request) error) {
		mux.HandleFunc("POST "+base+"/api/"+name, func(w http.ResponseWriter, r *http.Request) {
			// Only our own page may call the API (the token is in the path as well).
			if o := r.Header.Get("Origin"); o != "" && !strings.HasPrefix(o, "http://"+r.Host) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			if err := fn(r); err != nil {
				w.WriteHeader(http.StatusConflict)
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, app.State())
		})
	}
	action("settings", func(r *http.Request) error {
		var s Settings
		if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(&s); err != nil {
			return err
		}
		old := app.State().Settings.GameDir
		if err := app.SaveSettings(s); err != nil {
			return err
		}
		if app.State().Settings.GameDir != old && gamedir.Valid(s.GameDir) != "" {
			return app.CheckGame()
		}
		return nil
	})
	action("browse", func(r *http.Request) error {
		dir, ok := gamedir.Browse()
		if !ok {
			return nil
		}
		s := app.State().Settings
		s.GameDir = dir
		if gamedir.Valid(dir) == "" {
			return fmt.Errorf("fifa15.exe is not in %s. Pick the folder that contains fifa15.exe", dir)
		}
		if err := app.SaveSettings(s); err != nil {
			return err
		}
		return app.CheckGame()
	})
	action("detect", func(r *http.Request) error {
		s := app.State().Settings
		found := gamedir.Detect(s.GameDir, app.launcherDir)
		if found == "" {
			return fmt.Errorf("the game was not found automatically. Use \"Browse…\" to pick the folder with fifa15.exe")
		}
		s.GameDir = found
		if err := app.SaveSettings(s); err != nil {
			return err
		}
		return app.CheckGame()
	})
	action("login", func(r *http.Request) error {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<14)).Decode(&body); err != nil {
			return err
		}
		return app.LogIn(body.Username, body.Password)
	})
	action("logout", func(*http.Request) error { return app.LogOut() })
	action("check", func(*http.Request) error { return app.CheckGame() })
	action("connect", func(*http.Request) error { return app.Connect() })
	action("install", func(*http.Request) error { return app.Install() })
	action("restore", func(*http.Request) error { return app.Restore() })
	action("play", func(*http.Request) error { return app.Play() })
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
