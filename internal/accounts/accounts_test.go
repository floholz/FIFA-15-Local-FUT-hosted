package accounts

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

const testServiceKey = "fut15-test-service-key-0123456789"

var testCfg = Config{PublicURL: "https://ouft.example", ServiceKeys: map[string]string{"fut15": testServiceKey}}

// newApp is a fresh app with only PocketBase's and our migrations (empty data dir).
func newApp(t testing.TB) *tests.TestApp {
	t.Helper()
	app, err := tests.NewTestApp(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	Bind(app, testCfg)
	return app
}

func TestRegisterWithInvite(t *testing.T) {
	app := newApp(t)
	defer app.Cleanup()

	inv, err := CreateInvite(app, 2, 24*time.Hour, "two friends")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(InviteLink(testCfg.PublicURL, inv), "https://ouft.example/register?invite=") {
		t.Fatalf("bad link %s", InviteLink(testCfg.PublicURL, inv))
	}
	code := inv.GetString("code")

	cases := []struct {
		name, user, pass, confirm string
		want                      error
	}{
		{"bad username", "a", "correct-horse-1", "correct-horse-1", ErrUsername},
		{"spaces", "flo holz", "correct-horse-1", "correct-horse-1", ErrUsername},
		{"short password", "floholz", "short", "short", ErrPassword},
		{"mismatch", "floholz", "correct-horse-1", "correct-horse-2", ErrPasswordMatch},
	}
	for _, c := range cases {
		if _, err := Register(app, code, c.user, c.pass, c.confirm); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}

	user, err := Register(app, code, "floholz", "correct-horse-1", "correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	if !user.ValidatePassword("correct-horse-1") || !user.Verified() {
		t.Fatal("password not set or user not verified")
	}
	// Usernames are unique regardless of case.
	if _, err := Register(app, code, "FloHolz", "correct-horse-1", "correct-horse-1"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("duplicate username: err = %v", err)
	}
	if _, err := Register(app, code, "markus", "correct-horse-1", "correct-horse-1"); err != nil {
		t.Fatal(err)
	}
	// Both uses spent.
	if _, err := Register(app, code, "third", "correct-horse-1", "correct-horse-1"); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("third use: err = %v", err)
	}
	inv, _ = app.FindRecordById(CollectionInvites, inv.Id)
	if inv.GetInt("uses") != 2 || len(inv.GetStringSlice("used_by")) != 2 {
		t.Fatalf("invite bookkeeping: uses=%d used_by=%v", inv.GetInt("uses"), inv.GetStringSlice("used_by"))
	}
	if _, err := Register(app, "nope", "fourth", "correct-horse-1", "correct-horse-1"); !errors.Is(err, ErrInviteInvalid) {
		t.Fatalf("unknown code: err = %v", err)
	}
}

func TestExpiredInvite(t *testing.T) {
	app := newApp(t)
	defer app.Cleanup()
	inv, err := CreateInvite(app, 1, time.Hour, "")
	if err != nil {
		t.Fatal(err)
	}
	inv.Set("expires", types.NowDateTime().Add(-time.Minute))
	if err := app.Save(inv); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(app, inv.GetString("code"), "late", "correct-horse-1", "correct-horse-1"); !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("err = %v", err)
	}
}

// An invite created as a bare record (like in the PocketBase dashboard) gets defaults.
func TestDashboardInviteDefaults(t *testing.T) {
	app := newApp(t)
	defer app.Cleanup()
	col, _ := app.FindCollectionByNameOrId(CollectionInvites)
	r := core.NewRecord(col)
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}
	if len(r.GetString("code")) != 20 || r.GetInt("max_uses") != 1 || r.GetDateTime("expires").IsZero() {
		t.Fatalf("defaults missing: code=%q max_uses=%d expires=%v", r.GetString("code"), r.GetInt("max_uses"), r.GetDateTime("expires"))
	}
}

func TestVerifyToken(t *testing.T) {
	app := newApp(t)
	defer app.Cleanup()
	inv, _ := CreateInvite(app, 1, time.Hour, "")
	user, err := Register(app, inv.GetString("code"), "floholz", "correct-horse-1", "correct-horse-1")
	if err != nil {
		t.Fatal(err)
	}
	token, _ := user.NewAuthToken()
	got, err := VerifyToken(app, token)
	if err != nil || got.Id != user.Id {
		t.Fatalf("valid token rejected: %v", err)
	}
	if _, err := VerifyToken(app, token+"x"); err == nil {
		t.Fatal("tampered token accepted")
	}
	// Superuser tokens are not player logins.
	su := core.NewRecord(mustCollection(t, app, core.CollectionNameSuperusers))
	su.SetEmail("admin@example.com")
	su.SetPassword("admin-password-123")
	if err := app.Save(su); err != nil {
		t.Fatal(err)
	}
	suToken, _ := su.NewAuthToken()
	if _, err := VerifyToken(app, suToken); err == nil {
		t.Fatal("superuser token accepted as player login")
	}
}

func mustCollection(t *testing.T, app core.App, name string) *core.Collection {
	t.Helper()
	c, err := app.FindCollectionByNameOrId(name)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The HTTP surface: public signup through PocketBase's own endpoint stays closed,
// registration needs an invite, login works with the username, and the internal
// verify API needs a service key.
func TestHTTP(t *testing.T) {
	var code, token string
	setup := func(t testing.TB) *tests.TestApp {
		app := newApp(t)
		inv, _ := CreateInvite(app, 1, time.Hour, "")
		code = inv.GetString("code")
		inv2, _ := CreateInvite(app, 1, time.Hour, "")
		user, err := Register(app, inv2.GetString("code"), "existing", "correct-horse-1", "correct-horse-1")
		if err != nil {
			t.Fatal(err)
		}
		token, _ = user.NewAuthToken()
		return app
	}
	scenarios := []tests.ApiScenario{
		{
			Name: "public signup closed", Method: http.MethodPost, URL: "/api/collections/users/records",
			Body:           strings.NewReader(`{"username":"sneaky","password":"correct-horse-1","passwordConfirm":"correct-horse-1"}`),
			ExpectedStatus: 403, ExpectedContent: []string{`"data":{}`}, TestAppFactory: setup,
		},
		{
			Name: "register page", Method: http.MethodGet, URL: "/register",
			ExpectedStatus: 200, ExpectedContent: []string{"Join OUFT"}, TestAppFactory: setup,
		},
		{
			Name: "logo", Method: http.MethodGet, URL: "/static/logo.svg",
			ExpectedStatus: 200, ExpectedContent: []string{"<svg"}, TestAppFactory: setup,
		},
		{
			Name: "invite check unknown", Method: http.MethodGet, URL: "/api/ouft/invite/doesnotexist",
			ExpectedStatus: 200, ExpectedContent: []string{`"valid":false`, "not valid"}, TestAppFactory: setup,
		},
		{
			Name: "register ok", Method: http.MethodPost, URL: "/api/ouft/register",
			ExpectedStatus: 200, ExpectedContent: []string{`"username":"newbie"`}, TestAppFactory: setup,
		},
		{
			Name: "register taken", Method: http.MethodPost, URL: "/api/ouft/register",
			ExpectedStatus: 400, ExpectedContent: []string{"already taken"}, TestAppFactory: setup,
		},
		{
			Name: "login with username", Method: http.MethodPost, URL: "/api/collections/users/auth-with-password",
			Body:           strings.NewReader(`{"identity":"existing","password":"correct-horse-1"}`),
			ExpectedStatus: 200, ExpectedContent: []string{`"token":`}, TestAppFactory: setup,
		},
		{
			Name: "verify without key", Method: http.MethodPost, URL: "/api/ouft/internal/verify",
			Body:           strings.NewReader(`{"token":"x"}`),
			ExpectedStatus: 401, ExpectedContent: []string{"Invalid service key"}, TestAppFactory: setup,
		},
		{
			Name: "verify ok", Method: http.MethodPost, URL: "/api/ouft/internal/verify",
			Headers:        map[string]string{"Authorization": "Bearer " + testServiceKey},
			ExpectedStatus: 200, ExpectedContent: []string{`"valid":true`, `"username":"existing"`}, TestAppFactory: setup,
		},
		{
			Name: "verify bad token", Method: http.MethodPost, URL: "/api/ouft/internal/verify",
			Headers:        map[string]string{"Authorization": "Bearer " + testServiceKey},
			Body:           strings.NewReader(`{"token":"not-a-token"}`),
			ExpectedStatus: 200, ExpectedContent: []string{`"valid":false`}, TestAppFactory: setup,
		},
	}
	for _, s := range scenarios {
		s := s
		switch s.Name {
		case "register ok", "register taken":
			user := "newbie"
			if s.Name == "register taken" {
				user = "existing"
			}
			// The body needs the invite code that setup creates, so render it lazily.
			s.Body = &lazyBody{f: func() string {
				return `{"invite":"` + code + `","username":"` + user + `","password":"correct-horse-1","passwordConfirm":"correct-horse-1"}`
			}}
		case "verify ok":
			s.Body = &lazyBody{f: func() string { return `{"token":"` + token + `"}` }}
		}
		s.Test(t)
	}
}

// lazyBody renders the request body when it is first read (after the app factory ran).
type lazyBody struct {
	f func() string
	r *strings.Reader
}

func (b *lazyBody) Read(p []byte) (int, error) {
	if b.r == nil {
		b.r = strings.NewReader(b.f())
	}
	return b.r.Read(p)
}
