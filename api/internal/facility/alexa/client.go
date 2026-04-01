package alexa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

const (
	alexaBase = "https://alexa.amazon.com"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// Client is an HTTP client for the alexa.amazon.com internal web API.
// It is safe for concurrent use.
type Client struct {
	http *http.Client
	csrf string
}

// Device represents an Amazon Echo device (speaker, display, etc.).
type Device struct {
	AccountName     string `json:"accountName"`
	SerialNumber    string `json:"serialNumber"`
	DeviceType      string `json:"deviceType"`
	DeviceFamily    string `json:"deviceFamily"`
	SoftwareVersion string `json:"softwareVersion"`
	Online          bool   `json:"online"`
}

// SmartHomeDevice represents a smart home appliance registered with Alexa.
type SmartHomeDevice struct {
	ID                  string   `json:"id"`
	DisplayName         string   `json:"displayName"`
	Description         string   `json:"description"`          // e.g. "Cync: Dimmer" — manufacturer/model hint
	SupportedProperties []string `json:"supportedProperties"`  // turnOn, turnOff, setBrightness, lock, etc.
	SupportedOperations []string `json:"supportedOperations"`
}

// DeviceState holds the current power state of a smart home device.
type DeviceState struct {
	ApplianceID string
	PowerState  string // "ON", "OFF", or "" if unknown
}

// CommandParams holds the action and optional parameters for a device command.
type CommandParams struct {
	Action     string            // turnOn, turnOff, setBrightness, setTargetTemperature, lock, unlock
	Parameters map[string]string // e.g. {"brightness": "50"}, {"targetTemperature": "72"}
}

// New creates an Alexa client from a Cookie-Editor JSON export file.
// It primes the CSRF token by making an initial GET to the Alexa SPA.
func New(ctx context.Context, cookiesPath string) (*Client, error) {
	cookies, err := loadCookies(cookiesPath)
	if err != nil {
		return nil, err
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("cookie jar: %w", err)
	}

	// Load cookies for both apex and subdomain so the jar sends them correctly.
	for _, raw := range []string{"https://amazon.com", alexaBase} {
		u, _ := url.Parse(raw)
		jar.SetCookies(u, cookies)
	}

	c := &Client{
		http: &http.Client{
			Jar:     jar,
			Timeout: 15 * time.Second,
		},
	}

	// Prime CSRF — non-fatal; GET endpoints work without it.
	_ = c.refreshCSRF(ctx)

	return c, nil
}

// refreshCSRF fetches the Alexa SPA so Amazon sets the csrf session cookie.
func (c *Client) refreshCSRF(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, alexaBase+"/spa/index.html", nil)
	if err != nil {
		return err
	}
	c.setHeaders(req, false)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	u, _ := url.Parse(alexaBase)
	for _, ck := range c.http.Jar.Cookies(u) {
		if ck.Name == "csrf" {
			c.csrf = ck.Value
			return nil
		}
	}
	return nil
}

// ListDevices returns all Echo devices registered to the account.
func (c *Client) ListDevices(ctx context.Context) ([]Device, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		alexaBase+"/api/devices-v2/device?cached=false", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, false)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list devices: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Devices []Device `json:"devices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode devices: %w", err)
	}
	return result.Devices, nil
}

// ListSmartHomeDevices returns all smart home appliances registered with Alexa.
func (c *Client) ListSmartHomeDevices(ctx context.Context) ([]SmartHomeDevice, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		alexaBase+"/api/behaviors/entities?skillId=amzn1.ask.1p.smarthome", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, false)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list smart home devices: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list smart home devices: HTTP %d", resp.StatusCode)
	}

	var devices []SmartHomeDevice
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return nil, fmt.Errorf("decode smart home devices: %w", err)
	}
	return devices, nil
}

// GetDeviceStates fetches current power state for smart home devices via the
// phoenix API. Returns a map of applianceId → DeviceState.
func (c *Client) GetDeviceStates(ctx context.Context) (map[string]DeviceState, error) {
	// Refresh CSRF before phoenix — Amazon requires it even for GETs on this endpoint.
	_ = c.refreshCSRF(ctx)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		alexaBase+"/api/phoenix", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, true) // phoenix requires CSRF even on GET

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get device states: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("get device states: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read phoenix body: %w", err)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("get device states: empty response (HTTP %d)", resp.StatusCode)
	}

	// Log first 256 bytes to inspect the actual shape.
	preview := body
	if len(preview) > 256 {
		preview = preview[:256]
	}
	_ = preview // consumed below via slog in the caller; assign to suppress lint

	// The phoenix API returns a networkDetail field which is a JSON-encoded string
	// containing the full device graph.
	var outer struct {
		NetworkDetail string `json:"networkDetail"`
	}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("decode phoenix outer (body=%s): %w", preview, err)
	}

	var graph struct {
		Devices []struct {
			ApplianceID string `json:"applianceId"`
			ModelName   string `json:"modelName"`
			FriendlyName string `json:"friendlyName"`
			ApplianceState *struct {
				Online bool `json:"online"`
				GlueState *struct {
					AtomicStates []struct {
						Name  string `json:"name"`
						Value struct {
							Value string `json:"value"`
						} `json:"value"`
					} `json:"atomicStates"`
				} `json:"glueState"`
			} `json:"applianceState"`
		} `json:"devices"`
	}
	if err := json.Unmarshal([]byte(outer.NetworkDetail), &graph); err != nil {
		return nil, fmt.Errorf("decode phoenix graph: %w", err)
	}

	states := make(map[string]DeviceState, len(graph.Devices))
	for _, d := range graph.Devices {
		state := DeviceState{ApplianceID: d.ApplianceID}
		if d.ApplianceState != nil && d.ApplianceState.GlueState != nil {
			for _, s := range d.ApplianceState.GlueState.AtomicStates {
				if s.Name == "powerState" {
					state.PowerState = s.Value.Value
				}
			}
		}
		states[d.ApplianceID] = state
	}
	return states, nil
}

// SendCommand sends a smart home command to an appliance.
func (c *Client) SendCommand(ctx context.Context, applianceID string, cmd CommandParams) error {
	params := map[string]string{"action": cmd.Action}
	for k, v := range cmd.Parameters {
		params[k] = v
	}

	type controlRequest struct {
		EntityID   string            `json:"entityId"`
		EntityType string            `json:"entityType"`
		Parameters map[string]string `json:"parameters"`
	}
	payload, err := json.Marshal(struct {
		ControlRequests []controlRequest `json:"controlRequests"`
	}{
		ControlRequests: []controlRequest{{
			EntityID:   applianceID,
			EntityType: "APPLIANCE",
			Parameters: params,
		}},
	})
	if err != nil {
		return fmt.Errorf("marshal command: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		alexaBase+"/api/phoenix/state", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setHeaders(req, true)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send command: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("send command: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request, withCSRF bool) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", alexaBase+"/spa/index.html")
	req.Header.Set("DNT", "1")
	if withCSRF && c.csrf != "" {
		req.Header.Set("csrf", c.csrf)
	}
}
