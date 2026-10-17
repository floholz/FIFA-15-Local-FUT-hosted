package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// LauncherRelease is the newest OUFT Online download, from the GitHub releases.
type LauncherRelease struct {
	Version    string    `json:"version"`    // e.g. 15.2.0
	Name       string    `json:"name"`       // release title
	Prerelease bool      `json:"prerelease"` // beta
	Published  time.Time `json:"published"`
	PageURL    string    `json:"pageUrl"` // release notes
	ZipURL     string    `json:"zipUrl"`
	ZipName    string    `json:"zipName"`
	ZipSize    int64     `json:"zipSize"`
	SHA256     string    `json:"sha256,omitempty"`
}

const launcherTagPrefix = "launcher-"

// releaseCache asks GitHub at most every ttl and keeps serving the last good
// answer when GitHub is unreachable.
type releaseCache struct {
	api, repo string
	ttl       time.Duration
	client    *http.Client

	mu      sync.Mutex
	last    *LauncherRelease
	fetched time.Time
}

func newReleaseCache(api, repo string) *releaseCache {
	return &releaseCache{api: strings.TrimRight(api, "/"), repo: repo, ttl: 10 * time.Minute,
		client: &http.Client{Timeout: 6 * time.Second}}
}

// Latest returns the newest launcher release (pre-releases included, drafts not).
func (c *releaseCache) Latest() (*LauncherRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last != nil && time.Since(c.fetched) < c.ttl {
		return c.last, nil
	}
	rel, err := c.fetch()
	if err != nil {
		if c.last != nil {
			return c.last, nil // stale beats nothing
		}
		return nil, err
	}
	c.last, c.fetched = rel, time.Now()
	return rel, nil
}

func (c *releaseCache) fetch() (*LauncherRelease, error) {
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/repos/%s/releases?per_page=20", c.api, c.repo), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ouft-accounts")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github releases: %s", resp.Status)
	}
	var releases []struct {
		TagName    string    `json:"tag_name"`
		Name       string    `json:"name"`
		Draft      bool      `json:"draft"`
		Prerelease bool      `json:"prerelease"`
		HTMLURL    string    `json:"html_url"`
		Published  time.Time `json:"published_at"`
		Assets     []struct {
			Name   string `json:"name"`
			Size   int64  `json:"size"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 4<<20)).Decode(&releases); err != nil {
		return nil, err
	}
	var best *LauncherRelease
	for _, r := range releases {
		if r.Draft || !strings.HasPrefix(r.TagName, launcherTagPrefix) {
			continue
		}
		for _, a := range r.Assets {
			if !strings.HasPrefix(a.Name, "OUFT-Online") || !strings.HasSuffix(a.Name, ".zip") {
				continue
			}
			cand := &LauncherRelease{
				Version: strings.TrimPrefix(r.TagName, launcherTagPrefix), Name: r.Name,
				Prerelease: r.Prerelease, Published: r.Published, PageURL: r.HTMLURL,
				ZipURL: a.URL, ZipName: a.Name, ZipSize: a.Size,
				SHA256: strings.TrimPrefix(a.Digest, "sha256:"),
			}
			if best == nil || cand.Published.After(best.Published) {
				best = cand
			}
		}
	}
	if best == nil {
		return nil, errors.New("no launcher release found")
	}
	return best, nil
}
