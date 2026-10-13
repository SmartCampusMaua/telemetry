package chirpstackv4

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// apiDevice is the raw shape of one row from GET /api/registry/v0.1/devices
// (see the SmartCampusMaua `api` repo). Device model classification stays
// hardcoded in main's switch statement for now — only dev_eui is needed
// here to decide whether to forward a message.
type apiDevice struct {
	DevEUI     string `json:"dev_eui"`
	DeviceType string `json:"device_type"` // "lorawan" or "ip"
	Allow      bool   `json:"allow"`
}

var registryHTTPClient = &http.Client{Timeout: 10 * time.Second}

// GetAllowedDevEUIs fetches every allowed LoRaWAN device's dev_eui from the
// registry API — the allow-list this service forwards MQTT messages for.
func GetAllowedDevEUIs(apiBaseURL string) (map[string]bool, error) {
	url := apiBaseURL + "/api/registry/v0.1/devices"
	resp, err := registryHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %s", url, resp.Status)
	}

	var rows []apiDevice
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", url, err)
	}

	devEUIs := make(map[string]bool, len(rows))
	for _, d := range rows {
		if !d.Allow || !strings.EqualFold(d.DeviceType, "lorawan") || d.DevEUI == "" {
			continue
		}
		devEUIs[strings.ToLower(d.DevEUI)] = true
	}
	return devEUIs, nil
}
