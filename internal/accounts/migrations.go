package accounts

import (
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// UsernamePattern: 2-20 characters, what game clubs and the old player names allowed
// (minus spaces, which make logging in error-prone).
const UsernamePattern = `^[A-Za-z0-9][A-Za-z0-9_.-]{1,19}$`

// Roles: players by default; admins may create and revoke invites on /admin.
const (
	RolePlayer = "player"
	RoleAdmin  = "admin"
)

// adminRule lets only users with the admin role through (superusers always pass).
const adminRule = "@request.auth.role = '" + RoleAdmin + "'"

// TokenDuration is how long a login stays valid without a refresh (30 days). The
// launcher refreshes it on every Play, so active players never have to log in again.
const TokenDuration = 30 * 24 * 3600

func init() {
	core.AppMigrations.Register(initUp, nil, "1792200000_ouft_accounts_init.go")
}

func initUp(app core.App) error {
	// users: log in with username + password; email optional; no public signup
	// (accounts are created only through an invite, see Register).
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	users.CreateRule = nil
	users.DeleteRule = nil
	// Players may change their own password, but not their role or username.
	users.UpdateRule = types.Pointer("id = @request.auth.id && @request.body.role:isset = false && @request.body.username:isset = false")
	users.Fields.Add(&core.TextField{
		Name:     "username",
		Required: true,
		Min:      2,
		Max:      20,
		Pattern:  UsernamePattern,
	})
	users.Fields.Add(&core.SelectField{
		Name:      "role",
		Values:    []string{RolePlayer, RoleAdmin},
		MaxSelect: 1,
		Help:      "admin: may create and revoke invite links on /admin",
	})
	if email, ok := users.Fields.GetByName(core.FieldNameEmail).(*core.EmailField); ok {
		email.Required = false
	}
	users.AddIndex("idx_users_username", true, "username COLLATE NOCASE", "")
	users.PasswordAuth.Enabled = true
	users.PasswordAuth.IdentityFields = []string{"username"}
	users.AuthToken.Duration = TokenDuration
	if err := app.Save(users); err != nil {
		return err
	}

	// invites: superusers and users with the admin role (the /admin page uses the
	// normal records API); the create hook fills and limits what admins may set.
	invites := core.NewBaseCollection(CollectionInvites)
	invites.ListRule = types.Pointer(adminRule)
	invites.ViewRule = types.Pointer(adminRule)
	invites.CreateRule = types.Pointer(adminRule)
	invites.DeleteRule = types.Pointer(adminRule)
	invites.Fields.Add(
		&core.TextField{Name: "code", Required: true, Min: 12, Max: 64, Pattern: `^[A-Za-z0-9]+$`},
		&core.TextField{Name: "note", Max: 200},
		&core.NumberField{Name: "max_uses", OnlyInt: true, Min: types.Pointer(1.0)},
		&core.NumberField{Name: "uses", OnlyInt: true, Min: types.Pointer(0.0)},
		&core.DateField{Name: "expires"},
		&core.RelationField{Name: "used_by", CollectionId: users.Id, MaxSelect: 1000},
		&core.RelationField{Name: "created_by", CollectionId: users.Id, MaxSelect: 1},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	invites.AddIndex("idx_invites_code", true, "code", "")
	if err := app.Save(invites); err != nil {
		return err
	}

	settings := app.Settings()
	settings.Meta.AppName = "OUFT"
	settings.RateLimits.Enabled = true
	settings.RateLimits.Rules = append(settings.RateLimits.Rules,
		core.RateLimitRule{Label: "POST /api/ouft/register", MaxRequests: 5, Duration: 60},
		core.RateLimitRule{Label: "GET /api/ouft/invite/", MaxRequests: 30, Duration: 60},
	)
	// Only our own Traefik talks to the service; it appends the client address.
	settings.TrustedProxy.Headers = []string{"X-Forwarded-For"}
	settings.TrustedProxy.UseLeftmostIP = false
	return app.Save(settings)
}
