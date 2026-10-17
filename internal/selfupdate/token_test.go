package selfupdate

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A GitHub token raises the API limit, but only api.github.com ever receives it.
func TestTokenOnlyGoesToTheGitHubAPI(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "fake-token")
	t.Setenv("GH_TOKEN", "")
	seen := map[string]string{}
	c := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		seen[r.URL.Host] = r.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	for _, u := range []string{DefaultAPI, DefaultBase + "/v1.0.0/SHA256SUMS", "https://api.github.com.evil.test/x"} {
		if _, err := download(c, u, maxSmall); err != nil {
			t.Fatal(err)
		}
	}
	if seen["api.github.com"] != "Bearer fake-token" {
		t.Errorf("api.github.com got %q", seen["api.github.com"])
	}
	if seen["github.com"] != "" || seen["api.github.com.evil.test"] != "" {
		t.Errorf("token sent elsewhere: %v", seen)
	}
}
