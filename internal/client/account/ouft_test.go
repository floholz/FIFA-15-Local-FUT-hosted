package account

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewerVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"15.3.0", "15.2.0", true}, {"15.10.0", "15.9.9", true}, {"16.0.0", "15.99.99", true},
		{"15.2.0", "15.2.0", false}, {"15.1.9", "15.2.0", false},
		{"15.3.0", "dev", false}, {"dev", "15.2.0", false}, {"15.3", "15.2.0", false}, {"15.3.0-rc1", "15.2.0", false},
	}
	for _, c := range cases {
		if got := NewerVersion(c.a, c.b); got != c.want {
			t.Errorf("NewerVersion(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestLatestRelease(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ouft/launcher" || r.Header.Get("Authorization") != "tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"version":"15.3.0","zipUrl":"https://example.com/OUFT-Online-15.3.0.zip","pageUrl":"https://example.com/r"}`))
	}))
	defer srv.Close()
	r, err := LatestRelease(srv.URL, "tok")
	if err != nil || r.Version != "15.3.0" {
		t.Fatalf("%v %+v", err, r)
	}
	if _, err := LatestRelease(srv.URL, "bad"); err == nil {
		t.Fatal("expected an error without a valid login")
	}
}
