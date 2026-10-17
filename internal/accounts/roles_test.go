package accounts

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tests"
	"github.com/pocketbase/pocketbase/tools/types"
)

// runAs runs a scenario on an app with an admin user ("boss"), a player ("pleb")
// and one invite. who = "admin" | "player" | "" picks the Authorization token; URLs
// may use {player} for the player's record id. ApiScenario builds the request after
// the factory ran, so the factory fills these in.
func runAs(t *testing.T, s tests.ApiScenario, who string) {
	t.Helper()
	sc := &s
	sc.TestAppFactory = func(t testing.TB) *tests.TestApp {
		app := newApp(t)
		mk := func(name, role string) *core.Record {
			inv, _ := CreateInvite(app, 1, time.Hour, "")
			u, err := Register(app, inv.GetString("code"), name, "correct-horse-1", "correct-horse-1")
			if err != nil {
				t.Fatal(err)
			}
			if role != "" {
				u.Set("role", role)
				if err := app.Save(u); err != nil {
					t.Fatal(err)
				}
			}
			return u
		}
		admin, player := mk("boss", RoleAdmin), mk("pleb", "")
		tokens := map[string]*core.Record{"admin": admin, "player": player}
		if u := tokens[who]; u != nil {
			tok, _ := u.NewAuthToken()
			sc.Headers = map[string]string{"Authorization": tok}
		}
		sc.URL = strings.ReplaceAll(sc.URL, "{player}", player.Id)
		return app
	}
	sc.Test(t)
}

func TestRoles(t *testing.T) {
	far := types.NowDateTime().Add(365 * 24 * time.Hour).String()

	runAs(t, tests.ApiScenario{
		Name: "admin creates an invite: the server sets code, creator, uses and caps", Method: http.MethodPost,
		URL:                "/api/collections/invites/records",
		Body:               strings.NewReader(`{"code":"MyOwnGuessableCode","max_uses":500,"uses":-3,"expires":"` + far + `","note":"for markus"}`),
		ExpectedStatus:     200,
		ExpectedContent:    []string{`"max_uses":20`, `"uses":0`, `"note":"for markus"`},
		NotExpectedContent: []string{"MyOwnGuessableCode"},
		AfterTestFunc: func(t testing.TB, app *tests.TestApp, res *http.Response) {
			inv, err := app.FindFirstRecordByFilter(CollectionInvites, "note = 'for markus'")
			if err != nil {
				t.Fatal(err)
			}
			boss, _ := app.FindFirstRecordByData("users", "username", "boss")
			if inv.GetString("created_by") != boss.Id {
				t.Errorf("created_by = %q, want boss", inv.GetString("created_by"))
			}
			if inv.GetDateTime("expires").Time().After(time.Now().Add(AdminMaxValidity + time.Minute)) {
				t.Errorf("expiry not capped: %v", inv.GetDateTime("expires"))
			}
			if len(inv.GetString("code")) != 20 {
				t.Errorf("code = %q", inv.GetString("code"))
			}
		},
	}, "admin")

	runAs(t, tests.ApiScenario{
		Name: "player cannot create invites", Method: http.MethodPost, URL: "/api/collections/invites/records",
		Body: strings.NewReader(`{"max_uses":1}`), ExpectedStatus: 400, ExpectedContent: []string{"Failed to create record"},
	}, "player")

	runAs(t, tests.ApiScenario{
		Name: "guest cannot create invites", Method: http.MethodPost, URL: "/api/collections/invites/records",
		Body: strings.NewReader(`{"max_uses":1}`), ExpectedStatus: 400, ExpectedContent: []string{"Failed to create record"},
	}, "")

	runAs(t, tests.ApiScenario{
		Name: "player sees no invites", Method: http.MethodGet, URL: "/api/collections/invites/records",
		ExpectedStatus: 200, ExpectedContent: []string{`"totalItems":0`},
	}, "player")

	runAs(t, tests.ApiScenario{
		Name: "admin lists invites", Method: http.MethodGet, URL: "/api/collections/invites/records",
		ExpectedStatus: 200, ExpectedContent: []string{`"totalItems":2`},
	}, "admin")

	runAs(t, tests.ApiScenario{
		Name: "player cannot make themselves admin", Method: http.MethodPatch, URL: "/api/collections/users/records/{player}",
		Body: strings.NewReader(`{"role":"admin"}`), ExpectedStatus: 404, ExpectedContent: []string{"wasn't found"},
	}, "player")

	runAs(t, tests.ApiScenario{
		Name: "player cannot rename themselves", Method: http.MethodPatch, URL: "/api/collections/users/records/{player}",
		Body: strings.NewReader(`{"username":"renamed"}`), ExpectedStatus: 404, ExpectedContent: []string{"wasn't found"},
	}, "player")

	runAs(t, tests.ApiScenario{
		Name: "player can still change their password", Method: http.MethodPatch, URL: "/api/collections/users/records/{player}",
		Body:           strings.NewReader(`{"oldPassword":"correct-horse-1","password":"another-horse-2","passwordConfirm":"another-horse-2"}`),
		ExpectedStatus: 200, ExpectedContent: []string{`"username":"pleb"`},
	}, "player")

	runAs(t, tests.ApiScenario{
		Name: "admin page", Method: http.MethodGet, URL: "/admin",
		ExpectedStatus: 200, ExpectedContent: []string{"OUFT invites"},
	}, "")
}
