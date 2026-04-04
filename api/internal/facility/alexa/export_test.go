// export_test.go lives in package alexa (not alexa_test) so it can access
// unexported fields. It is only compiled during testing.
package alexa

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
)

// NewTestClient creates a Client whose underlying http.Client uses a custom
// RoundTripper that rewrites the "alexa.amazon.com" host to baseURL (a
// httptest.Server URL). This lets tests drive all exported methods without any
// real network calls.
func NewTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()

	jar, _ := cookiejar.New(nil)

	transport := &rewriteTransport{
		wrapped: http.DefaultTransport,
		from:    "https://alexa.amazon.com",
		to:      baseURL,
	}

	c := &Client{
		http: &http.Client{
			Jar:       jar,
			Transport: transport,
		},
		debug: false,
	}

	// Prime the jar with a dummy csrf cookie so refreshCSRF does not need to
	// make a real request; just set it directly.
	c.csrf = "test-csrf"
	c.customerID = ""

	u, _ := url.Parse("https://alexa.amazon.com")
	jar.SetCookies(u, []*http.Cookie{
		{Name: "csrf", Value: "test-csrf"},
	})

	return c
}

// rewriteTransport rewrites the scheme+host of every outgoing request from
// `from` to `to` before forwarding to the wrapped transport.
type rewriteTransport struct {
	wrapped http.RoundTripper
	from    string // e.g. "https://alexa.amazon.com"
	to      string // e.g. "http://127.0.0.1:PORT"
}

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request so we do not mutate the original.
	clone := req.Clone(req.Context())
	rawURL := clone.URL.String()
	rawURL = strings.Replace(rawURL, rt.from, rt.to, 1)
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	clone.URL = u
	clone.Host = u.Host
	// Override the Host header so the test server does not reject it.
	clone.Header.Set("Host", u.Host)
	return rt.wrapped.RoundTrip(clone)
}

// LoadCookiesForTest exposes loadCookies for white-box testing.
func LoadCookiesForTest(path string) ([]*http.Cookie, error) {
	return loadCookies(path)
}
