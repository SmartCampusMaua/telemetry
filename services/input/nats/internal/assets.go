// Package internal — asset registry. zc2x_packet_t's DeviceID (see
// adapter.go) identifies the OBU/RSU hardware unit itself; it has no room
// for what that unit is physically embedded in (a vehicle for OBU, a
// trackside pole for RSU — see cmd/simulator, which originates this
// distinction). This file adds that mapping as an out-of-band lookup rather
// than a wire-format change, so real firmware stays untouched: an
// AssetRegistry maps a packet's DeviceID (lowercase hex, as already computed
// in signals.go) to an operator-assigned asset_id string, consulted only
// when building the JSON TelemetryRecord.
//
// RSU's own asset_id (the pole it's mounted on) never reaches this registry
// from a real packet: RSU forwards OBU's packet bytes unmodified (RFC-0001
// immutability), so DeviceID in a relayed packet is always the OBU's, never
// the relaying RSU's — see the "origin" comment in signals.go. A registry
// entry only ever resolves an OBU's vehicle, regardless of whether that
// packet arrived via OBU's direct link or an RSU relay.
package internal

import (
	"encoding/json"
	"fmt"
	"os"
)

// AssetRegistry maps a device's lowercase-hex DeviceID to its asset_id.
type AssetRegistry map[string]string

// LoadAssetRegistry reads an AssetRegistry from a JSON file of the form
// {"020000000001": "vehicle-1", ...}. Intended to be produced by
// cmd/simulator's -registry-out flag, or hand-written for a real fleet.
func LoadAssetRegistry(path string) (AssetRegistry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("internal: read asset registry %s: %w", path, err)
	}
	var reg AssetRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("internal: parse asset registry %s: %w", path, err)
	}
	return reg, nil
}
