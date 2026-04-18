package alexa_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rkrimper1/jarvis/api/internal/facility/alexa"
)

// ── helpers ──────────────────────────────────────────────────────────────────

// writeCookieFile writes a Cookie-Editor JSON export to a temp file and returns the path.
func writeCookieFile(t *testing.T, entries []map[string]any) string {
	t.Helper()
	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("marshal cookie file: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "cookies.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}
	return path
}

// futureUnix returns a Unix timestamp seconds-in-the-future.
func futureUnix(seconds int) float64 {
	return float64(time.Now().Add(time.Duration(seconds) * time.Second).Unix())
}

// pastUnix returns a Unix timestamp seconds in the past.
func pastUnix(seconds int) float64 {
	return float64(time.Now().Add(-time.Duration(seconds) * time.Second).Unix())
}

// minimalCookieEntry returns a minimal, valid Cookie-Editor cookie entry.
func minimalCookieEntry(name, value string, expiration float64) map[string]any {
	return map[string]any{
		"name":           name,
		"value":          value,
		"domain":         ".amazon.com",
		"path":           "/",
		"expirationDate": expiration,
		"httpOnly":       false,
		"secure":         true,
	}
}

// newClientAgainstServer creates an alexa.Client pointed at a test server.
// It writes a minimal valid cookie file and rewires the HTTP transport so all
// requests to "alexa.amazon.com" are routed to ts instead.
func newClientAgainstServer(t *testing.T, ts *httptest.Server) *alexa.Client {
	t.Helper()

	// Write a valid cookies file so New() doesn't error on loadCookies.
	cookiesPath := writeCookieFile(t, []map[string]any{
		minimalCookieEntry("session-id", "abc123", futureUnix(3600)),
	})

	// Monkey-patch http.DefaultTransport is not possible without modifying the
	// package. Instead we rely on the fact that New() makes two non-fatal calls
	// (refreshCSRF and fetchCustomerID) — those will simply fail against the
	// test server's routes (or succeed if the test server handles them).
	// To actually intercept ALL requests we use a custom RoundTripper via the
	// environment variable approach — but since we cannot modify the package we
	// use a real httptest.Server and need to make the client talk to it.
	//
	// The cleanest strategy: set up the test server to answer every path the
	// client constructor calls, plus the paths under test.  The client is
	// hard-coded to use https://alexa.amazon.com, so we cannot redirect it
	// without a custom transport.  We therefore expose a small helper that
	// builds a *Client with a custom HTTP transport.
	//
	// Since alexa.Client does not expose its http.Client we test exported
	// methods that accept a context by building a server-hosted proxy approach:
	// we create a test server that responds to every needed URL and pass the
	// client a cookie jar that redirects to the test server URL.
	//
	// Practically: we build the client via a thin wrapper — write the cookie
	// file, call alexa.NewForTest if it exists; otherwise we use the exported
	// New() and accept that bootstrap calls go out to alexa.amazon.com (they
	// will fail, which is non-fatal per the implementation).  To prevent actual
	// network I/O we configure a no-op proxy via the HTTPS_PROXY env var trick
	// but the simplest correct approach is to use httptest + a custom transport.
	//
	// Because we cannot modify client.go, we accept that New() makes two
	// non-fatal HTTP calls that will simply fail (network refused) during tests.
	// The returned *Client is still usable via its exported methods, which we
	// will drive through our httptest server by patching the URL at the call
	// site — but since the URL is hard-coded we cannot do that either.
	//
	// FINAL APPROACH: We export a testable constructor via a build-tag file
	// OR we accept tests of the cookie-layer only (pure logic, no networking)
	// and use the exported New() for integration-style tests that drive a
	// real httptest.Server by reflecting into the struct.
	//
	// Since reflection into unexported fields is fragile and we cannot modify
	// source, we adopt the following pragmatic strategy:
	//   - Pure-logic tests (cookies) test exported functions directly.
	//   - HTTP-method tests use a NewForTest helper defined in a
	//     test-only file in the *same package* (package alexa, not alexa_test)
	//     so we can access unexported fields.
	//
	// This file is package alexa_test. The internal helper lives in
	// export_test.go (package alexa). See below.
	_ = cookiesPath
	_ = ts
	return nil // populated by the internal test package
}

// ── CookiesExpiry ─────────────────────────────────────────────────────────────

func TestCookiesExpiry_MissingFile(t *testing.T) {
	_, err := alexa.CookiesExpiry("/does/not/exist/cookies.json")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestCookiesExpiry_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`not json`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := alexa.CookiesExpiry(path)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

func TestCookiesExpiry_AllSessionCookies(t *testing.T) {
	path := writeCookieFile(t, []map[string]any{
		{"name": "a", "value": "1", "domain": ".amazon.com", "path": "/", "expirationDate": 0, "httpOnly": false, "secure": false},
		{"name": "b", "value": "2", "domain": ".amazon.com", "path": "/", "expirationDate": 0, "httpOnly": false, "secure": false},
	})
	exp, err := alexa.CookiesExpiry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exp.IsZero() {
		t.Errorf("expected zero time for all-session cookies, got %v", exp)
	}
}

func TestCookiesExpiry_AllExpired(t *testing.T) {
	// All cookies are already expired — should return the latest (most recent) expiry
	// so callers can detect expired = returned time is in the past.
	past1 := time.Now().Add(-1 * time.Hour).Unix() // expired 1h ago
	past2 := time.Now().Add(-2 * time.Hour).Unix() // expired 2h ago
	path := writeCookieFile(t, []map[string]any{
		minimalCookieEntry("old", "x", float64(past1)),
		minimalCookieEntry("older", "y", float64(past2)),
	})
	exp, err := alexa.CookiesExpiry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exp.IsZero() {
		t.Fatal("expected non-zero time when cookies have expiry dates (even if past)")
	}
	if !exp.Before(time.Now()) {
		t.Errorf("expected returned time to be in the past (session expired), got %v", exp)
	}
	// Should be the LATEST (least old) — past1, not past2.
	diff := exp.Unix() - past1
	if diff < -2 || diff > 2 {
		t.Errorf("expected latest expired=%d, got %v (diff=%d)", past1, exp.Unix(), diff)
	}
}

func TestCookiesExpiry_MixExpiredAndFuture(t *testing.T) {
	// future2 expires later than future1 — latest should be future2.
	future1 := time.Now().Add(1 * time.Hour)
	future2 := time.Now().Add(2 * time.Hour)

	path := writeCookieFile(t, []map[string]any{
		minimalCookieEntry("past", "x", pastUnix(3600)),
		minimalCookieEntry("session", "y", 0),
		minimalCookieEntry("soon", "z", float64(future1.Unix())),
		minimalCookieEntry("later", "w", float64(future2.Unix())),
	})
	exp, err := alexa.CookiesExpiry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exp.IsZero() {
		t.Fatal("expected non-zero expiry")
	}
	// Allow ±2 seconds tolerance for test execution time.
	diff := exp.Unix() - future2.Unix()
	if diff < -2 || diff > 2 {
		t.Errorf("expected latest=%v, got %v (diff=%d)", future2.Unix(), exp.Unix(), diff)
	}
}

func TestCookiesExpiry_SingleFutureCookie(t *testing.T) {
	future := time.Now().Add(24 * time.Hour)
	path := writeCookieFile(t, []map[string]any{
		minimalCookieEntry("auth", "tok", float64(future.Unix())),
	})
	exp, err := alexa.CookiesExpiry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exp.IsZero() {
		t.Fatal("expected non-zero expiry")
	}
	diff := exp.Unix() - future.Unix()
	if diff < -2 || diff > 2 {
		t.Errorf("expected ~%v, got %v", future.Unix(), exp.Unix())
	}
}

func TestCookiesExpiry_EmptyArray(t *testing.T) {
	path := writeCookieFile(t, []map[string]any{})
	exp, err := alexa.CookiesExpiry(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exp.IsZero() {
		t.Errorf("expected zero time for empty cookie array, got %v", exp)
	}
}

// ── HTTP-method tests (via internal test helpers) ─────────────────────────────
// These tests live in export_test.go (package alexa) which can create a Client
// with a custom http.Client.  The functions below are called from that file.

// buildBootstrapResponse returns JSON for the bootstrap endpoint.
func buildBootstrapResponse(customerID string) []byte {
	return []byte(fmt.Sprintf(`{"authentication":{"customerId":%q}}`, customerID))
}

// buildDevicesResponse returns JSON for the devices-v2 endpoint.
func buildDevicesResponse(devices []alexa.Device) []byte {
	wrapped := struct {
		Devices []alexa.Device `json:"devices"`
	}{Devices: devices}
	b, _ := json.Marshal(wrapped)
	return b
}

// buildSmartHomeDevicesResponse returns JSON for the behaviors/entities endpoint.
func buildSmartHomeDevicesResponse(devices []alexa.SmartHomeDevice) []byte {
	b, _ := json.Marshal(devices)
	return b
}

// ── CustomerID concurrent safety ──────────────────────────────────────────────

func TestCustomerID_ConcurrentReads(t *testing.T) {
	// Build a minimal test server.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/bootstrap":
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildBootstrapResponse("CUST-123"))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = c.CustomerID()
		}()
	}
	wg.Wait()
}

// ── ListDevices ────────────────────────────────────────────────────────────────

func TestListDevices_HappyPath(t *testing.T) {
	want := []alexa.Device{
		{AccountName: "Kitchen Echo", SerialNumber: "SN001", DeviceFamily: "ECHO", Online: true},
		{AccountName: "Office Dot", SerialNumber: "SN002", DeviceFamily: "ECHO_DOT", Online: false},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/devices-v2/device":
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildDevicesResponse(want))
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	got, err := c.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d devices, want %d", len(got), len(want))
	}
	for i, d := range got {
		if d.SerialNumber != want[i].SerialNumber {
			t.Errorf("[%d] serial: got %q, want %q", i, d.SerialNumber, want[i].SerialNumber)
		}
		if d.AccountName != want[i].AccountName {
			t.Errorf("[%d] name: got %q, want %q", i, d.AccountName, want[i].AccountName)
		}
	}
}

func TestListDevices_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/devices-v2/device" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	_, err := c.ListDevices(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200, got nil")
	}
}

// ── ListSmartHomeDevices ───────────────────────────────────────────────────────

func TestListSmartHomeDevices_HappyPath(t *testing.T) {
	want := []alexa.SmartHomeDevice{
		{ID: "dev-1", DisplayName: "Office Light", SupportedProperties: []string{"turnOn", "turnOff"}},
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/behaviors/entities" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(buildSmartHomeDevicesResponse(want))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	got, err := c.ListSmartHomeDevices(context.Background())
	if err != nil {
		t.Fatalf("ListSmartHomeDevices: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d devices, want 1", len(got))
	}
	if got[0].ID != "dev-1" {
		t.Errorf("ID: got %q, want %q", got[0].ID, "dev-1")
	}
}

func TestListSmartHomeDevices_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/behaviors/entities" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	_, err := c.ListSmartHomeDevices(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200, got nil")
	}
}

// ── BatchGetEndpointStates ─────────────────────────────────────────────────────

func TestBatchGetEndpointStates_EmptyInput(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	states, err := c.BatchGetEndpointStates(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if states != nil {
		t.Errorf("expected nil result for empty input, got %v", states)
	}
}

func TestBatchGetEndpointStates_HappyPath(t *testing.T) {
	// Server returns one ON+REACHABLE endpoint and one nil (NO_SUCH_ENDPOINT).
	batchResp := `[
		{
			"data": {
				"endpoint": {
					"id": "amzn1.alexa.endpoint.dev-1",
					"features": [
						{"properties": [
							{"__typename": "Power", "powerStateValue": "ON"},
							{"__typename": "Reachability", "reachabilityStatusValue": "REACHABLE"}
						]}
					]
				}
			}
		},
		{
			"data": {
				"endpoint": null
			}
		}
	]`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(batchResp))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	states, err := c.BatchGetEndpointStates(context.Background(), []string{"dev-1", "dev-2"})
	if err != nil {
		t.Fatalf("BatchGetEndpointStates: %v", err)
	}

	// dev-1 should be ON and REACHABLE.
	s, ok := states["dev-1"]
	if !ok {
		t.Fatal("expected dev-1 in result")
	}
	if !s.PowerOn {
		t.Error("expected PowerOn=true")
	}
	if !s.Online {
		t.Error("expected Online=true")
	}

	// dev-2 should be absent (null endpoint = NO_SUCH_ENDPOINT).
	if _, ok := states["dev-2"]; ok {
		t.Error("dev-2 should be filtered out (null endpoint)")
	}
}

func TestBatchGetEndpointStates_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	_, err := c.BatchGetEndpointStates(context.Background(), []string{"dev-1"})
	if err == nil {
		t.Fatal("expected error for non-200, got nil")
	}
}

func TestBatchGetEndpointStates_OfflineEndpoint(t *testing.T) {
	batchResp := `[
		{
			"data": {
				"endpoint": {
					"id": "amzn1.alexa.endpoint.dev-x",
					"features": [
						{"properties": [
							{"__typename": "Power", "powerStateValue": "OFF"},
							{"__typename": "Reachability", "reachabilityStatusValue": "UNREACHABLE"}
						]}
					]
				}
			}
		}
	]`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.Write([]byte(batchResp))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	states, err := c.BatchGetEndpointStates(context.Background(), []string{"dev-x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s := states["dev-x"]
	if s.PowerOn {
		t.Error("expected PowerOn=false")
	}
	if s.Online {
		t.Error("expected Online=false")
	}
}

// ── SendCommand ───────────────────────────────────────────────────────────────

func TestSendCommand_HappyPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.Header().Set("Content-Type", "application/json")
			// Successful response — no errors.
			w.Write([]byte(`{"data":{"setEndpointFeatures":{"featureControlResponses":[{"endpointId":"amzn1.alexa.endpoint.abc","__typename":"FeatureControlResponse"}],"errors":[],"__typename":"SetEndpointFeaturesResponse"}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	err := c.SendCommand(context.Background(), "abc", alexa.CommandParams{Action: "turnOn"})
	if err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
}

func TestSendCommand_TurnOff(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.Write([]byte(`{"data":{"setEndpointFeatures":{"errors":[]}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	err := c.SendCommand(context.Background(), "abc", alexa.CommandParams{Action: "turnOff"})
	if err != nil {
		t.Fatalf("SendCommand turnOff: %v", err)
	}
}

func TestSendCommand_UnsupportedAction(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	err := c.SendCommand(context.Background(), "abc", alexa.CommandParams{Action: "setBrightness"})
	if err == nil {
		t.Fatal("expected error for unsupported action, got nil")
	}
}

func TestSendCommand_GraphQLError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.Write([]byte(`{"errors":[{"message":"unauthorized"}]}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	err := c.SendCommand(context.Background(), "abc", alexa.CommandParams{Action: "turnOn"})
	if err == nil {
		t.Fatal("expected error for graphql error, got nil")
	}
}

func TestSendCommand_DeviceError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.Write([]byte(`{"data":{"setEndpointFeatures":{"errors":[{"endpointId":"amzn1.alexa.endpoint.abc","code":"ENDPOINT_UNREACHABLE"}]}}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	err := c.SendCommand(context.Background(), "abc", alexa.CommandParams{Action: "turnOn"})
	if err == nil {
		t.Fatal("expected error for device error, got nil")
	}
}

func TestSendCommand_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/nexus/v1/graphql" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := alexa.NewTestClient(t, ts.URL)
	err := c.SendCommand(context.Background(), "abc", alexa.CommandParams{Action: "turnOn"})
	if err == nil {
		t.Fatal("expected error for non-200, got nil")
	}
}
