package alexa

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"
)

const (
	alexaBase = "https://alexa.amazon.com"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

// Client is an HTTP client for the alexa.amazon.com internal web API.
// It is safe for concurrent use.
type Client struct {
	mu         sync.RWMutex
	http       *http.Client
	csrf       string
	customerID string // logged-in account's customer ID, fetched on init
	debug      bool   // when true, logs request bodies (controlled by ALEXA_DEBUG)
}

// Device represents an Amazon Echo device (speaker, display, etc.).
type Device struct {
	AccountName            string `json:"accountName"`
	SerialNumber           string `json:"serialNumber"`
	DeviceType             string `json:"deviceType"`
	DeviceFamily           string `json:"deviceFamily"`
	SoftwareVersion        string `json:"softwareVersion"`
	Online                 bool   `json:"online"`
	DeviceOwnerCustomerID  string `json:"deviceOwnerCustomerId"`
}

// SmartHomeDevice represents a smart home appliance registered with Alexa.
type SmartHomeDevice struct {
	ID                  string   `json:"id"`
	DisplayName         string   `json:"displayName"`
	Description         string   `json:"description"`          // e.g. "Cync: Dimmer" — manufacturer/model hint
	SupportedProperties []string `json:"supportedProperties"`  // turnOn, turnOff, setBrightness, lock, etc.
	SupportedOperations []string `json:"supportedOperations"`
}

// CommandParams holds the action and optional parameters for a device command.
type CommandParams struct {
	Action     string            // turnOn, turnOff, setBrightness, setTargetTemperature, lock, unlock
	Parameters map[string]string // e.g. {"brightness": "50"}, {"targetTemperature": "72"}
}

// New creates an Alexa client from a Cookie-Editor JSON export file.
// It primes the CSRF token by making an initial GET to the Alexa SPA.
// Set debug to true to enable verbose request logging (ALEXA_DEBUG).
func New(ctx context.Context, cookiesPath string, debug bool) (*Client, error) {
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
		debug: debug,
	}

	// Prime CSRF — non-fatal; GET endpoints work without it.
	_ = c.refreshCSRF(ctx)

	// Fetch logged-in customer ID — non-fatal, used for behaviors/preview commands.
	_ = c.fetchCustomerID(ctx)

	return c, nil
}

// CustomerID returns the logged-in account's Amazon customer ID.
func (c *Client) CustomerID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.customerID
}

// StartKeepAlive launches a background goroutine that pings the bootstrap API
// every interval to keep the Amazon session cookies alive. It stops when ctx
// is cancelled. Call once after New().
func (c *Client) StartKeepAlive(ctx context.Context, interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := c.fetchCustomerID(ctx); err != nil {
					slog.Warn("alexa keep-alive ping failed", slog.Any("err", err))
				} else {
					slog.Info("alexa keep-alive ping ok", slog.String("customer_id", c.CustomerID()))
				}
			}
		}
	}()
}

// fetchCustomerID calls the bootstrap API to discover the logged-in customer ID.
func (c *Client) fetchCustomerID(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		alexaBase+"/api/bootstrap?version=0", nil)
	if err != nil {
		return err
	}
	c.setHeaders(req, false)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bootstrap: HTTP %d", resp.StatusCode)
	}
	var result struct {
		Authentication struct {
			CustomerID string `json:"customerId"`
		} `json:"authentication"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("bootstrap decode: %w", err)
	}
	c.mu.Lock()
	c.customerID = result.Authentication.CustomerID
	c.mu.Unlock()
	slog.Info("alexa customer ID fetched", slog.String("customer_id", result.Authentication.CustomerID))
	return nil
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
			c.mu.Lock()
			c.csrf = ck.Value
			c.mu.Unlock()
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


// endpointPrefix is prepended to smart home device IDs for the GraphQL API.
const endpointPrefix = "amzn1.alexa.endpoint."

// EndpointState holds the current power and connectivity state of a smart home endpoint.
type EndpointState struct {
	PowerOn bool
	Online  bool
}

const endpointPowerFeatureQuery = `query endpointPowerFeature($endpointId: String!, $latencyTolerance: LatencyToleranceValue) {
  endpoint(id: $endpointId) {
    id
    features(latencyToleranceValue: $latencyTolerance) {
      name
      properties {
        ... on Power {
          powerStateValue
          __typename
        }
        ... on Reachability {
          reachabilityStatusValue
          __typename
        }
        __typename
        name
        type
      }
    }
    __typename
  }
}`

// BatchGetEndpointStates queries the GraphQL API for the power state of multiple endpoints
// in a single batch request. Only endpoints recognized by the GraphQL API are returned —
// entries that return NO_SUCH_ENDPOINT are omitted, which is how we filter out invalid
// behaviors/entities IDs that have no corresponding smart home endpoint.
func (c *Client) BatchGetEndpointStates(ctx context.Context, entityIDs []string) (map[string]EndpointState, error) {
	if len(entityIDs) == 0 {
		return nil, nil
	}

	type gqlOp struct {
		OperationName string            `json:"operationName"`
		Variables     map[string]string `json:"variables"`
		Query         string            `json:"query"`
	}
	ops := make([]gqlOp, len(entityIDs))
	for i, id := range entityIDs {
		ops[i] = gqlOp{
			OperationName: "endpointPowerFeature",
			Variables: map[string]string{
				"endpointId":      endpointPrefix + id,
				"latencyTolerance": "LOW",
			},
			Query: endpointPowerFeatureQuery,
		}
	}

	payload, err := json.Marshal(ops)
	if err != nil {
		return nil, fmt.Errorf("marshal batch: %w", err)
	}

	_ = c.refreshCSRF(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		alexaBase+"/nexus/v1/graphql", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, true)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("batch graphql: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("batch graphql: HTTP %d", resp.StatusCode)
	}

	var results []struct {
		Data struct {
			Endpoint *struct {
				Features []struct {
					Properties []struct {
						TypeName                string `json:"__typename"`
						PowerStateValue         string `json:"powerStateValue"`
						ReachabilityStatusValue string `json:"reachabilityStatusValue"`
					} `json:"properties"`
				} `json:"features"`
			} `json:"endpoint"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, fmt.Errorf("decode batch: %w", err)
	}

	states := make(map[string]EndpointState, len(entityIDs))
	for i, result := range results {
		if i >= len(entityIDs) {
			break
		}
		if result.Data.Endpoint == nil {
			continue // NO_SUCH_ENDPOINT — skip
		}
		state := EndpointState{}
		for _, feature := range result.Data.Endpoint.Features {
			for _, prop := range feature.Properties {
				switch prop.TypeName {
				case "Power":
					state.PowerOn = prop.PowerStateValue == "ON"
				case "Reachability":
					state.Online = prop.ReachabilityStatusValue == "REACHABLE"
				}
			}
		}
		states[entityIDs[i]] = state
	}
	return states, nil
}

// graphqlQuery is the mutation used to control smart home device power state.
const graphqlQuery = `mutation togglePowerFeatureForEndpoint($endpointId: String, $featureOperationName: FeatureOperationName!) {
  setEndpointFeatures(
    setEndpointFeaturesInput: {featureControlRequests: [{endpointId: $endpointId, featureName: power, featureOperationName: $featureOperationName}]}
  ) {
    featureControlResponses {
      endpointId
      __typename
    }
    errors {
      endpointId
      code
      __typename
    }
    __typename
  }
}`

// SendCommand sends a smart home power command via the Alexa GraphQL API.
// applianceID is the UUID from ListSmartHomeDevices (without the amzn1.alexa.endpoint. prefix).
// cmd.Action must be "turnOn" or "turnOff".
func (c *Client) SendCommand(ctx context.Context, applianceID string, cmd CommandParams) error {
	if cmd.Action != "turnOn" && cmd.Action != "turnOff" {
		return fmt.Errorf("unsupported action %q: must be turnOn or turnOff", cmd.Action)
	}

	_ = c.refreshCSRF(ctx)

	endpointID := endpointPrefix + applianceID

	payload, err := json.Marshal(map[string]any{
		"operationName": "togglePowerFeatureForEndpoint",
		"variables": map[string]string{
			"endpointId":           endpointID,
			"featureOperationName": cmd.Action,
		},
		"query": graphqlQuery,
	})
	if err != nil {
		return fmt.Errorf("marshal graphql payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		alexaBase+"/nexus/v1/graphql", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setHeaders(req, true)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("graphql: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("graphql: HTTP %d — %s", resp.StatusCode, bytes.TrimSpace(body))
	}

	// Check for GraphQL-level errors in the response body.
	var result struct {
		Data struct {
			SetEndpointFeatures struct {
				Errors []struct {
					EndpointID string `json:"endpointId"`
					Code       string `json:"code"`
				} `json:"errors"`
			} `json:"setEndpointFeatures"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &result); err == nil {
		if len(result.Errors) > 0 {
			return fmt.Errorf("graphql error: %s", result.Errors[0].Message)
		}
		if len(result.Data.SetEndpointFeatures.Errors) > 0 {
			e := result.Data.SetEndpointFeatures.Errors[0]
			return fmt.Errorf("device error: %s (endpoint=%s)", e.Code, e.EndpointID)
		}
	}

	slog.Info("send command", slog.String("appliance", applianceID),
		slog.String("action", cmd.Action), slog.String("endpoint", endpointID))
	return nil
}

// SendTextCommand sends a natural-language text command to a specific Echo device
// via the Alexa behaviors/preview API — equivalent to typing a command in the Alexa app.
// device must be from a Device returned by ListDevices.
func (c *Client) SendTextCommand(ctx context.Context, text string, device Device) error {
	// Refresh CSRF — behaviors/preview requires it.
	_ = c.refreshCSRF(ctx)

	requesterID := c.CustomerID()
	if requesterID == "" {
		requesterID = device.DeviceOwnerCustomerID
	}

	// operationPayload must itself be a JSON-encoded string (Amazon double-encodes it).
	opPayload, err := json.Marshal(map[string]string{
		"deviceType":         device.DeviceType,
		"deviceSerialNumber": device.SerialNumber,
		"locale":             "en-US",
		"customerId":         requesterID,
		"text":               text,
	})
	if err != nil {
		return fmt.Errorf("marshal operation payload: %w", err)
	}

	// sequenceJson is itself a JSON-encoded string inside the outer payload.
	type startNode struct {
		Type             string          `json:"@type"`
		OperationType    string          `json:"type"`
		OperationPayload json.RawMessage `json:"operationPayload"`
	}
	type sequence struct {
		Type      string    `json:"@type"`
		StartNode startNode `json:"startNode"`
	}
	inner, err := json.Marshal(sequence{
		Type: "com.amazon.alexa.behaviors.model.Sequence",
		StartNode: startNode{
			Type:             "com.amazon.alexa.behaviors.model.OpaquePayloadOperationNode",
			OperationType:    "Alexa.Operation.Ubi.UbiNodeType",
			OperationPayload: opPayload,
		},
	})
	if err != nil {
		return fmt.Errorf("marshal sequence: %w", err)
	}

	outer, err := json.Marshal(map[string]any{
		"behaviorId":   "PREVIEW",
		"sequenceJson": string(inner),
		"customerId":   requesterID,
	})
	if err != nil {
		return fmt.Errorf("marshal text command: %w", err)
	}

	if c.debug {
		slog.Info("behaviors/preview payload", slog.String("body", string(outer)))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		alexaBase+"/api/behaviors/preview", bytes.NewReader(outer))
	if err != nil {
		return err
	}
	c.setHeaders(req, true)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("send text command: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("send text command: HTTP %d — %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

// SendRawBehaviorsPreview posts a pre-built JSON payload to the behaviors/preview endpoint.
// Used by the test harness to experiment with different operation types.
func (c *Client) SendRawBehaviorsPreview(ctx context.Context, payload []byte) error {
	_ = c.refreshCSRF(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		alexaBase+"/api/behaviors/preview", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	c.setHeaders(req, true)
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("behaviors/preview: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("behaviors/preview: HTTP %d — %s", resp.StatusCode, bytes.TrimSpace(body))
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request, withCSRF bool) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/javascript, */*; q=0.01")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", alexaBase+"/spa/index.html")
	req.Header.Set("DNT", "1")
	if withCSRF {
		c.mu.RLock()
		csrf := c.csrf
		c.mu.RUnlock()
		if csrf != "" {
			req.Header.Set("csrf", csrf)
		}
	}
}
