package accounts

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const CollectionInvites = "invites"

// DefaultInviteValidity applies to invites created without an expiry (also from the dashboard).
const DefaultInviteValidity = 7 * 24 * time.Hour

var usernameRe = regexp.MustCompile(UsernamePattern)

// User-facing registration errors (the register page shows them as they are).
var (
	ErrInviteInvalid = errors.New("this invite link is not valid")
	ErrInviteUsed    = errors.New("this invite link has already been used")
	ErrInviteExpired = errors.New("this invite link has expired")
	ErrUsername      = errors.New("the username must be 2-20 characters: letters, digits, _ . - (starting with a letter or digit)")
	ErrUsernameTaken = errors.New("that username is already taken")
	ErrPassword      = errors.New("the password must be 10-72 characters")
	ErrPasswordMatch = errors.New("the passwords do not match")
)

// NewInviteCode returns 20 random characters from an unambiguous alphabet.
func NewInviteCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789"
	return randomString(alphabet, 20)
}

func randomString(alphabet string, n int) string {
	out := make([]byte, 0, n)
	limit := 256 - 256%len(alphabet) // reject bytes that would bias the modulo
	buf := make([]byte, 1)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			panic(err) // crypto/rand never fails on supported platforms
		}
		if int(buf[0]) < limit {
			out = append(out, alphabet[int(buf[0])%len(alphabet)])
		}
	}
	return string(out)
}

// fillInviteDefaults gives an invite a code, one use and an expiry when they are
// missing, so invites created in the PocketBase dashboard work without typing them.
func fillInviteDefaults(r *core.Record) {
	if strings.TrimSpace(r.GetString("code")) == "" {
		r.Set("code", NewInviteCode())
	}
	if r.GetInt("max_uses") < 1 {
		r.Set("max_uses", 1)
	}
	if r.GetDateTime("expires").IsZero() {
		r.Set("expires", types.NowDateTime().Add(DefaultInviteValidity))
	}
}

// CreateInvite stores a new invite and returns it.
func CreateInvite(app core.App, maxUses int, validFor time.Duration, note string) (*core.Record, error) {
	col, err := app.FindCollectionByNameOrId(CollectionInvites)
	if err != nil {
		return nil, err
	}
	r := core.NewRecord(col)
	r.Set("max_uses", maxUses)
	r.Set("note", note)
	if validFor > 0 {
		r.Set("expires", types.NowDateTime().Add(validFor))
	}
	fillInviteDefaults(r)
	if err := app.Save(r); err != nil {
		return nil, err
	}
	return r, nil
}

// InviteLink is the register URL for an invite.
func InviteLink(publicURL string, invite *core.Record) string {
	return strings.TrimRight(publicURL, "/") + "/register?invite=" + invite.GetString("code")
}

// checkInvite returns the invite for code if it can still be used.
func checkInvite(app core.App, code string) (*core.Record, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrInviteInvalid
	}
	inv, err := app.FindFirstRecordByData(CollectionInvites, "code", code)
	if err != nil {
		return nil, ErrInviteInvalid
	}
	if inv.GetInt("uses") >= inv.GetInt("max_uses") {
		return nil, ErrInviteUsed
	}
	if exp := inv.GetDateTime("expires"); !exp.IsZero() && exp.Time().Before(time.Now()) {
		return nil, ErrInviteExpired
	}
	return inv, nil
}

// Register creates a user from an invite. Errors are the user-facing ones above,
// or an internal error.
func Register(app core.App, code, username, password, passwordConfirm string) (*core.Record, error) {
	username = strings.TrimSpace(username)
	if !usernameRe.MatchString(username) {
		return nil, ErrUsername
	}
	if n := len(password); n < 10 || n > 72 { // 72: the bcrypt input limit
		return nil, ErrPassword
	}
	if password != passwordConfirm {
		return nil, ErrPasswordMatch
	}

	var user *core.Record
	err := app.RunInTransaction(func(tx core.App) error {
		inv, err := checkInvite(tx, code)
		if err != nil {
			return err
		}
		if _, err := tx.FindFirstRecordByFilter("users", "username = {:u}", map[string]any{"u": username}); err == nil {
			return ErrUsernameTaken
		}
		users, err := tx.FindCollectionByNameOrId("users")
		if err != nil {
			return err
		}
		user = core.NewRecord(users)
		user.Set("username", username)
		user.SetPassword(password)
		user.SetVerified(true)
		if err := tx.Save(user); err != nil {
			if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "unique") {
				return ErrUsernameTaken
			}
			return fmt.Errorf("saving user: %w", err)
		}
		inv.Set("uses", inv.GetInt("uses")+1)
		inv.Set("used_by+", user.Id)
		return tx.Save(inv)
	})
	if err != nil {
		return nil, err
	}
	return user, nil
}
