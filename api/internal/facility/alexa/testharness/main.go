// Command testharness exercises the Alexa client directly against alexa.amazon.com
// using your local cookies file. Run with:
//
//	go run ./api/internal/facility/alexa/testharness \
//	  -cookies /home/vagrant/credentials/jarvis/alexa-cookies.json \
//	  -cmd "office lights off"
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/rkrimper1/jarvis/api/internal/facility/alexa"
)

func main() {
	cookiesPath := flag.String("cookies", os.Getenv("ALEXA_COOKIES_PATH"), "path to Cookie-Editor JSON export")
	cmd := flag.String("cmd", "", "text command to send via behaviors/preview")
	serial := flag.String("serial", "", "target device serial number")
	flag.String("action", "", "direct appliance action (turnOn/turnOff/lock/unlock) — requires --serial with an applianceId")
	flag.Parse()

	if *cookiesPath == "" {
		log.Fatal("--cookies is required (or set ALEXA_COOKIES_PATH)")
	}

	ctx := context.Background()

	client, err := alexa.New(ctx, *cookiesPath, true)
	if err != nil {
		log.Fatalf("init client: %v", err)
	}

	// List Echo devices so we can inspect them and pick a target.
	devices, err := client.ListDevices(ctx)
	if err != nil {
		log.Fatalf("list devices: %v", err)
	}

	fmt.Printf("=== Logged-in Customer ID: %s ===\n\n", client.CustomerID())
	fmt.Println("=== Echo Devices ===")
	var target *alexa.Device
	for i, d := range devices {
		online := "offline"
		if d.Online {
			online = "ONLINE"
		}
		fmt.Printf("[%s] %-30s family=%-10s type=%-16s serial=%-32s customerID=%s\n",
			online, d.AccountName, d.DeviceFamily, d.DeviceType, d.SerialNumber, d.DeviceOwnerCustomerID)

		if target == nil && d.Online && d.DeviceFamily == "ECHO" {
			if *serial == "" || d.SerialNumber == *serial {
				target = &devices[i]
			}
		}
	}

	// List smart home devices too.
	smDevices, err := client.ListSmartHomeDevices(ctx)
	if err != nil {
		log.Printf("list smart home devices: %v", err)
	} else {
		fmt.Println("\n=== Smart Home Devices ===")
		for _, d := range smDevices {
			fmt.Printf("%-30s id=%-50s props=%v\n", d.DisplayName, d.ID, d.SupportedProperties)
		}
	}

	action := flag.Lookup("action")
	if action != nil && action.Value.String() != "" {
		if *serial == "" {
			log.Fatal("--serial required for --action")
		}
		act := action.Value.String()

		// Strategy A: GraphQL /nexus/v1/graphql (what the Alexa web app actually uses).
		fmt.Printf("\n[A] graphql/nexus: appliance=%s action=%s\n", *serial, act)
		err := client.SendCommand(ctx, *serial, alexa.CommandParams{Action: act})
		if err != nil {
			fmt.Printf("    FAILED: %v\n", err)
		} else {
			fmt.Println("    ✓ accepted")
			return
		}

		// Strategy B: behaviors/preview ConnectedDevice (powerToggle only).
		opMap := map[string]string{
			"turnOn":  "Alexa.Operation.ConnectedDevice#Alexa.PowerController#3#TurnOn",
			"turnOff": "Alexa.Operation.ConnectedDevice#Alexa.PowerController#3#TurnOff",
			"toggle":  "Alexa.Operation.ConnectedDevice#Alexa.PowerController#3#powerToggle",
			"lock":    "Alexa.Operation.ConnectedDevice#Alexa.LockController#3#Lock",
			"unlock":  "Alexa.Operation.ConnectedDevice#Alexa.LockController#3#Unlock",
		}
		opType, ok := opMap[act]
		if ok {
			fmt.Printf("\n[B] behaviors/preview ConnectedDevice: appliance=%s op=%s\n", *serial, opType)
			err = testConnectedDevicePreview(ctx, client, *serial, opType)
			if err != nil {
				fmt.Printf("    FAILED: %v\n", err)
			} else {
				fmt.Println("    ✓ accepted")
				return
			}
		}
		os.Exit(1)
	}

	if *cmd == "" {
		fmt.Println("\nNo --cmd specified. Listing only.")
		return
	}

	if target == nil {
		log.Fatal("no eligible ECHO device found — use --serial to specify one")
	}

	requesterID := client.CustomerID()
	if requesterID == "" {
		requesterID = target.DeviceOwnerCustomerID
	}
	fmt.Printf("\n→ Sending %q to [%s] %s (serial=%s deviceOwner=%s requester=%s)\n",
		*cmd, target.DeviceFamily, target.AccountName, target.SerialNumber, target.DeviceOwnerCustomerID, requesterID)

	// Print the payload for inspection before sending.
	payload := buildPreviewPayload(*cmd, *target, requesterID)
	pretty, _ := json.MarshalIndent(payload, "", "  ")
	fmt.Println("\n=== Payload ===")
	fmt.Println(string(pretty))

	if err := client.SendTextCommand(ctx, *cmd, *target); err != nil {
		log.Fatalf("send command: %v", err)
	}
	fmt.Println("\n✓ Command accepted")
}

// testConnectedDevicePreview sends behaviors/preview using ConnectedDevice operation types.
// It tries several payload variants to find what Amazon accepts.
func testConnectedDevicePreview(ctx context.Context, client *alexa.Client, applianceID, opType string) error {
	loggedInID := client.CustomerID()

	type variant struct {
		label       string
		outerCustomerID string
		opPayload   map[string]string
	}

	variants := []variant{
		{
			label:           "logged-in outer, entityId+entityType+customerId(logged-in) inner",
			outerCustomerID: loggedInID,
			opPayload:       map[string]string{"entityId": applianceID, "entityType": "APPLIANCE", "customerId": loggedInID},
		},
		{
			label:           "logged-in outer, applianceId+customerId(logged-in) inner",
			outerCustomerID: loggedInID,
			opPayload:       map[string]string{"applianceId": applianceID, "customerId": loggedInID},
		},
	}

	for i, v := range variants {
		opPayloadBytes, _ := json.Marshal(v.opPayload)
		inner, _ := json.Marshal(map[string]any{
			"@type": "com.amazon.alexa.behaviors.model.Sequence",
			"startNode": map[string]any{
				"@type":            "com.amazon.alexa.behaviors.model.OpaquePayloadOperationNode",
				"type":             opType,
				"operationPayload": json.RawMessage(opPayloadBytes),
			},
		})
		outer, _ := json.Marshal(map[string]any{
			"behaviorId":   "PREVIEW",
			"sequenceJson": string(inner),
			"customerId":   v.outerCustomerID,
		})
		fmt.Printf("\n  [B%d] %s\n", i+1, v.label)
		fmt.Printf("       payload: %s\n", outer)
		err := client.SendRawBehaviorsPreview(ctx, outer)
		if err != nil {
			fmt.Printf("       FAILED: %v\n", err)
		} else {
			fmt.Printf("       ✓ accepted\n")
			return nil
		}
	}
	return fmt.Errorf("all ConnectedDevice variants failed")
}

// buildPreviewPayload mirrors what SendTextCommand sends, for inspection.
func buildPreviewPayload(text string, device alexa.Device, requesterID string) map[string]any {
	opPayload := map[string]string{
		"deviceType":         device.DeviceType,
		"deviceSerialNumber": device.SerialNumber,
		"locale":             "en-US",
		"customerId":         device.DeviceOwnerCustomerID,
		"text":               text,
	}
	opBytes, _ := json.Marshal(opPayload)

	inner := map[string]any{
		"@type": "com.amazon.alexa.behaviors.model.Sequence",
		"startNode": map[string]any{
			"@type":            "com.amazon.alexa.behaviors.model.OpaquePayloadOperationNode",
			"type":             "Alexa.Operation.Ubi.UbiNodeType",
			"operationPayload": json.RawMessage(opBytes),
		},
	}
	innerBytes, _ := json.Marshal(inner)

	return map[string]any{
		"behaviorId":   "PREVIEW",
		"sequenceJson": string(innerBytes),
		"customerId":   requesterID,
	}
}
