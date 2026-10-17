package accounts

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pocketbase/pocketbase/tests"
)

const fakeReleases = `[
 {"tag_name":"launcher-15.3.0","name":"draft","draft":true,"prerelease":false,"html_url":"x","published_at":"2026-10-20T00:00:00Z",
  "assets":[{"name":"OUFT-Online-15.3.0.zip","size":1,"browser_download_url":"https://dl/draft.zip","digest":"sha256:aa"}]},
 {"tag_name":"v0.7.0","name":"server","draft":false,"prerelease":false,"html_url":"x","published_at":"2026-10-19T00:00:00Z",
  "assets":[{"name":"OUFT-Online-0.7.0.zip","size":1,"browser_download_url":"https://dl/server.zip","digest":""}]},
 {"tag_name":"launcher-15.2.0","name":"OUFT Online 15.2.0 (beta)","draft":false,"prerelease":true,
  "html_url":"https://gh/rel/15.2.0","published_at":"2026-10-17T13:22:09Z",
  "assets":[{"name":"OUFT-Online-15.2.0.zip.sha256","size":89,"browser_download_url":"https://dl/sum","digest":""},
            {"name":"OUFT-Online-15.2.0.zip","size":44895538,"browser_download_url":"https://dl/15.2.0.zip","digest":"sha256:d026d47d"}]},
 {"tag_name":"launcher-15.1.0","name":"old","draft":false,"prerelease":false,"html_url":"x","published_at":"2026-10-17T10:00:00Z",
  "assets":[{"name":"OUFT-Online-15.1.0.zip","size":1,"browser_download_url":"https://dl/15.1.0.zip","digest":""}]}
]`

func fakeGitHub(t testing.TB, fail *atomic.Bool, calls *atomic.Int32) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() || r.URL.Path != "/repos/owner/repo/releases" {
			http.Error(w, "nope", http.StatusBadGateway)
			return
		}
		w.Write([]byte(fakeReleases))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestReleaseCache(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	gh := fakeGitHub(t, &fail, &calls)
	c := newReleaseCache(gh.URL, "owner/repo")

	rel, err := c.Latest()
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "15.2.0" || rel.ZipURL != "https://dl/15.2.0.zip" || rel.SHA256 != "d026d47d" ||
		!rel.Prerelease || rel.ZipSize != 44895538 || rel.PageURL != "https://gh/rel/15.2.0" {
		t.Fatalf("wrong release: %+v", rel)
	}
	// Cached: no second call within the TTL.
	c.Latest()
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1 (cached)", calls.Load())
	}
	// GitHub down after the TTL: the last good answer is served.
	fail.Store(true)
	c.fetched = time.Now().Add(-time.Hour)
	if rel, err := c.Latest(); err != nil || rel.Version != "15.2.0" {
		t.Fatalf("stale fallback: %v %v", rel, err)
	}
	// Down with nothing cached: an error.
	if _, err := newReleaseCache(gh.URL, "owner/repo").Latest(); err == nil {
		t.Fatal("expected an error with nothing cached")
	}
}

func TestLauncherEndpointNeedsLogin(t *testing.T) {
	var fail atomic.Bool
	var calls atomic.Int32
	gh := fakeGitHub(t, &fail, &calls)
	cfg := testCfg
	cfg.GitHubAPI, cfg.ReleasesRepo = gh.URL, "owner/repo"

	run := func(name, url, who string, status int, content []string) {
		s := tests.ApiScenario{Name: name, Method: http.MethodGet, URL: url,
			ExpectedStatus: status, ExpectedContent: content}
		sc := &s
		sc.TestAppFactory = func(t testing.TB) *tests.TestApp {
			app, err := tests.NewTestApp(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			Bind(app, cfg)
			if who == "player" {
				inv, _ := CreateInvite(app, 1, time.Hour, "")
				u, err := Register(app, inv.GetString("code"), "pleb", "correct-horse-1", "correct-horse-1")
				if err != nil {
					t.Fatal(err)
				}
				tok, _ := u.NewAuthToken()
				sc.Headers = map[string]string{"Authorization": tok}
			}
			return app
		}
		sc.Test(t)
	}
	run("guest is asked to log in", "/api/ouft/launcher", "", 401, []string{"Log in to download"})
	run("player gets the release", "/api/ouft/launcher", "player", 200, []string{`"version":"15.2.0"`, `"zipUrl":"https://dl/15.2.0.zip"`})
	run("landing page is public, with a login form", "/", "", 200, []string{"loginForm", "invite-only"})
}
