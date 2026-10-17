// Package accounts is the OUFT accounts service: invite-only registration,
// username + password login (PocketBase's own auth endpoints) and a small internal
// API that game servers use to check a player's login. It knows nothing about any
// game. This is the only package that imports PocketBase.
package accounts

import (
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/router"
	"github.com/spf13/cobra"
)

//go:embed web
var webFS embed.FS

// Config comes from the environment (see ConfigFromEnv).
type Config struct {
	// PublicURL is where players reach the web app, used for invite links.
	PublicURL string
	// ServiceKeys maps a game server's name to the key it sends to the internal API.
	ServiceKeys map[string]string
}

// ConfigFromEnv reads OUFT_PUBLIC_URL and OUFT_SERVICE_KEYS ("fut15:key1,other:key2").
func ConfigFromEnv() Config {
	c := Config{PublicURL: os.Getenv("OUFT_PUBLIC_URL"), ServiceKeys: map[string]string{}}
	if c.PublicURL == "" {
		c.PublicURL = "https://ouft.fhun.site"
	}
	for _, pair := range strings.Split(os.Getenv("OUFT_SERVICE_KEYS"), ",") {
		name, key, ok := strings.Cut(strings.TrimSpace(pair), ":")
		if ok && name != "" && len(key) >= 16 {
			c.ServiceKeys[name] = key
		}
	}
	return c
}

// Setup wires everything into a PocketBase app: Bind plus the "invite" command.
func Setup(app *pocketbase.PocketBase, cfg Config) {
	Bind(app, cfg)
	app.RootCmd.AddCommand(inviteCommand(app, cfg))
}

// Bind registers the hooks and routes (also used by the tests with a test app).
func Bind(app core.App, cfg Config) {
	app.OnRecordCreate(CollectionInvites).BindFunc(func(e *core.RecordEvent) error {
		fillInviteDefaults(e.Record)
		return e.Next()
	})
	// API creates by admin users (the /admin page); superusers keep full control.
	app.OnRecordCreateRequest(CollectionInvites).BindFunc(func(e *core.RecordRequestEvent) error {
		if !e.HasSuperuserAuth() && e.Auth != nil {
			limitAdminInvite(e.Record, e.Auth.Id)
		}
		return e.Next()
	})

	app.OnServe().BindFunc(func(e *core.ServeEvent) error {
		if s := e.App.Settings(); s.Meta.AppURL != cfg.PublicURL {
			s.Meta.AppURL = cfg.PublicURL
			if err := e.App.Save(s); err != nil {
				return err
			}
		}
		if len(cfg.ServiceKeys) == 0 {
			e.App.Logger().Warn("OUFT_SERVICE_KEYS is empty: game servers cannot verify logins")
		}
		Routes(e.Router, e.App, cfg)
		return e.Next()
	})
}

// Routes registers the web pages and the OUFT API.
func Routes(r *router.Router[*core.RequestEvent], app core.App, cfg Config) {
	page := func(name string) func(*core.RequestEvent) error {
		return func(e *core.RequestEvent) error {
			b, err := webFS.ReadFile("web/" + name)
			if err != nil {
				return e.NotFoundError("", nil)
			}
			e.Response.Header().Set("Cache-Control", "no-cache")
			return e.Blob(http.StatusOK, "text/html; charset=utf-8", b)
		}
	}
	r.GET("/{$}", page("index.html"))
	r.GET("/register", page("register.html"))
	r.GET("/admin", page("admin.html"))
	static, _ := fs.Sub(webFS, "web/static")
	r.GET("/static/{path...}", func(e *core.RequestEvent) error {
		e.Response.Header().Set("Cache-Control", "public, max-age=3600")
		return e.FileFS(static, e.Request.PathValue("path"))
	})

	// The register page checks its invite before showing the form.
	r.GET("/api/ouft/invite/{code}", func(e *core.RequestEvent) error {
		inv, err := checkInvite(e.App, e.Request.PathValue("code"))
		if err != nil {
			msg := err.Error()
			return e.JSON(http.StatusOK, map[string]any{"valid": false, "error": strings.ToUpper(msg[:1]) + msg[1:] + "."})
		}
		return e.JSON(http.StatusOK, map[string]any{"valid": true, "expires": inv.GetDateTime("expires")})
	})

	r.POST("/api/ouft/register", func(e *core.RequestEvent) error {
		var body struct {
			Invite          string `json:"invite"`
			Username        string `json:"username"`
			Password        string `json:"password"`
			PasswordConfirm string `json:"passwordConfirm"`
		}
		if err := e.BindBody(&body); err != nil {
			return e.BadRequestError("invalid request", nil)
		}
		user, err := Register(e.App, body.Invite, body.Username, body.Password, body.PasswordConfirm)
		if err != nil {
			if isUserError(err) {
				return e.BadRequestError(err.Error(), nil)
			}
			e.App.Logger().Error("register failed", "error", err)
			return e.InternalServerError("registration failed, please try again later", nil)
		}
		e.App.Logger().Info("user registered", "username", user.GetString("username"), "ip", e.RealIP())
		return e.JSON(http.StatusOK, map[string]any{"username": user.GetString("username")})
	})

	// Internal API for game servers: never routed through the public proxy, and
	// every call needs a service key.
	r.POST("/api/ouft/internal/verify", func(e *core.RequestEvent) error {
		service, ok := checkServiceKey(cfg, e.Request.Header.Get("Authorization"))
		if !ok {
			return e.UnauthorizedError("invalid service key", nil)
		}
		var body struct {
			Token string `json:"token"`
		}
		if err := e.BindBody(&body); err != nil || body.Token == "" {
			return e.BadRequestError("missing token", nil)
		}
		user, err := VerifyToken(e.App, body.Token)
		if err != nil {
			return e.JSON(http.StatusOK, map[string]any{"valid": false})
		}
		e.App.Logger().Debug("token verified", "service", service, "username", user.GetString("username"))
		return e.JSON(http.StatusOK, map[string]any{
			"valid": true, "id": user.Id, "username": user.GetString("username"),
		})
	})
}

func isUserError(err error) bool {
	for _, e := range []error{ErrInviteInvalid, ErrInviteUsed, ErrInviteExpired, ErrUsername,
		ErrUsernameTaken, ErrPassword, ErrPasswordMatch} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// checkServiceKey matches "Bearer <key>" against the configured keys.
func checkServiceKey(cfg Config, header string) (string, bool) {
	key, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || key == "" {
		return "", false
	}
	for name, want := range cfg.ServiceKeys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(want)) == 1 {
			return name, true
		}
	}
	return "", false
}

// VerifyToken returns the user a login token belongs to (users collection only:
// superuser tokens are not player logins).
func VerifyToken(app core.App, token string) (*core.Record, error) {
	user, err := app.FindAuthRecordByToken(token, core.TokenTypeAuth)
	if err != nil {
		return nil, err
	}
	if user.Collection().Name != "users" {
		return nil, errors.New("not a player token")
	}
	return user, nil
}

func inviteCommand(app *pocketbase.PocketBase, cfg Config) *cobra.Command {
	var uses, days int
	var note string
	cmd := &cobra.Command{
		Use:          "invite",
		Short:        "Create an invite link for new players",
		Example:      "invite --uses 1 --days 7 --note \"for Markus\"",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := app.RunAllMigrations(); err != nil {
				return err
			}
			if uses < 1 || days < 1 {
				return errors.New("--uses and --days must be at least 1")
			}
			inv, err := CreateInvite(app, uses, time.Duration(days)*24*time.Hour, note)
			if err != nil {
				return err
			}
			fmt.Println(InviteLink(cfg.PublicURL, inv))
			fmt.Printf("(%d use(s), valid until %s)\n", uses, inv.GetDateTime("expires").Time().Local().Format("2006-01-02 15:04"))
			return nil
		},
	}
	cmd.Flags().IntVar(&uses, "uses", 1, "how many players can register with it")
	cmd.Flags().IntVar(&days, "days", 7, "how many days it stays valid")
	cmd.Flags().StringVar(&note, "note", "", "a note for yourself (shown in the dashboard)")
	return cmd
}
