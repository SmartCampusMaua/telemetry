package mqtt

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

// Package main — device registry.
//
// Single source of truth for:
//   – hardware model constants (DeviceModel)
//   – battery voltage range for battery_level % calculation
//
// sensor_data (and audit_iot's raw) always holds exactly the value
// transmitted by the sensor, untouched — no calibration/unit-conversion math
// happens in this pipeline. That's a client-side/user-space concern for
// whatever consumes the SmartCampusMaua API.

// ─── Device model constants ───────────────────────────────────────────────────

// DeviceModel is a typed string identifying the hardware model.
// It is the sole routing key in parseDeviceModel.
type DeviceModel string

const (
	LNV3    DeviceModel = "LNV3"    // IMT LoraNodeV3 series
	DTL200  DeviceModel = "DTL200"  // Khomp DTL200 series
	EM300   DeviceModel = "EM300"   // Milesight EM300 series
	EM500   DeviceModel = "EM500"   // Milesight EM500 series
	KS3000  DeviceModel = "KS3000"  // Kron KS3000 series
	NIT21LI DeviceModel = "NIT21LI" // Khomp NIT21LI model
	WS101   DeviceModel = "WS101"   // Milesight smart button model
	UC100   DeviceModel = "UC100"   // Milesight UC100 RS485/Modbus controller (Modbus-only, no GPIO)
	UC300   DeviceModel = "UC300"   // Milesight UC300 RS485/Modbus controller (Modbus + fixed GPIO/PT100/ADC)
	UC501   DeviceModel = "UC501"   // Milesight UC501/UC50x RS485/Modbus controller (Modbus + site-wired GPIO/analog input)
	UC511   DeviceModel = "UC511"   // Milesight UC511/UC512 irrigation valve + pipe-pressure controller (fixed-purpose, no RS485)
	VS370   DeviceModel = "VS370"   // Milesight VS370 single-zone occupancy/illuminance presence sensor
)

// DeviceConfig bundles model and battery range.
type DeviceConfig struct {
	Model             DeviceModel
	DeviceType        string // "LORAWAN" or "IP"
	SerialNumber      string
	MacAddress        string  // IP/WiFi devices — written as mac_address InfluxDB3 tag
	AssetID           string  // this device's own/"home" asset — see SensorConfig.AssetID for the per-sensor override
	BatteryVoltageMax float64 // V, full charge  (default 4.2 — LiPo/Li-Ion)
	BatteryVoltageMin float64 // V, cutoff       (default 3.3 — LoRa safe minimum)
	ProbeRangeM       float64 // DTL200: overrides bytes[3] when 0 (not configured via downlink)
}

// ─── Registry API client ───────────────────────────────────────────────────────
//
// devices/assets/sensors/modbus-channels are no longer this repo's own JSON
// files — the SmartCampusMaua `api` repo's Postgres-backed registry owns
// them now (the JSON files were a one-time bootstrap import, not the source
// of truth). These four fetches replace four file reads; every route read
// here is open (no auth token needed — this repo never writes).
var registryHTTPClient = &http.Client{Timeout: 10 * time.Second}

// fetchRegistryList performs one GET against the registry API and decodes a
// JSON array response into T. Shared by all four fetchers below — a network
// error, non-200 status, or unparseable body are all just "reload failed,"
// handled by the caller exactly like a failed file read used to be.
func fetchRegistryList[T any](url string) ([]T, error) {
	resp, err := registryHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: unexpected status %s", url, resp.Status)
	}
	var out []T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", url, err)
	}
	return out, nil
}

// ─── Device registry ─────────────────────────────────────────────────────────

// apiDevice is the raw shape of one row from GET /api/registry/v0.1/devices.
// Its own row identifier is "id" — unlike the old devices.json, where the
// entity's identifier was named "device_id" after itself. Don't confuse this
// with apiSensor.DeviceID/apiModbusChannel.DeviceID below, which are foreign
// keys *to* a device and correctly keep that descriptive name.
type apiDevice struct {
	ID                string      `json:"id"`
	DeviceModel       DeviceModel `json:"device_model"`
	DeviceType        string      `json:"device_type"` // "lorawan" or "ip"
	SerialNumber      string      `json:"serial_number"`
	DevEUI            string      `json:"dev_eui"`
	MacAddress        string      `json:"mac_address"`
	BatteryVoltageMax float64     `json:"battery_voltage_max"`
	BatteryVoltageMin float64     `json:"battery_voltage_min"`
	ProbeRangeM       float64     `json:"probe_range_m"`
	Allow             bool        `json:"allow"` // NOT NULL DEFAULT true on the API's side
	AssetID           string      `json:"asset_id"`
}

var reDevEUI = regexp.MustCompile(`(?i)^[0-9a-f]{16}$`)

// isUUID reports whether s is a syntactically valid UUID of any version.
// The registry API's own writes mint plain crypto.randomUUID() v4 ids (the
// original bootstrap-imported rows are v7, from this repo's old UUIDv7
// convention) — Postgres's own primary-key/FK constraints are the real
// integrity guarantee now, so this only checks basic shape, not version.
func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

// validateAPIDevice checks mandatory fields and format constraints for one
// device row. Returns an error for hard failures; logs warnings for soft issues.
func validateAPIDevice(d apiDevice) error {
	if d.ID == "" {
		return fmt.Errorf("id is required")
	}
	if !isUUID(d.ID) {
		return fmt.Errorf("device %s: id must be a valid UUID", d.ID)
	}
	if d.DeviceModel == "" {
		return fmt.Errorf("device %s: device_model is required", d.ID)
	}

	dt := strings.ToUpper(d.DeviceType)
	if dt != "LORAWAN" && dt != "IP" {
		return fmt.Errorf("device %s: device_type must be \"lorawan\" or \"ip\", got %q", d.ID, d.DeviceType)
	}

	if dt == "LORAWAN" {
		if d.DevEUI == "" {
			return fmt.Errorf("device %s: dev_eui is required for lorawan devices", d.ID)
		}
		if !reDevEUI.MatchString(d.DevEUI) {
			return fmt.Errorf("device %s: dev_eui %q must be 16 hex characters (EUI-64)", d.ID, d.DevEUI)
		}
	}

	if dt == "IP" {
		if d.MacAddress == "" {
			return fmt.Errorf("device %s: mac_address is required for ip devices", d.ID)
		}
		if _, err := net.ParseMAC(d.MacAddress); err != nil {
			return fmt.Errorf("device %s: mac_address %q is invalid: %w", d.ID, d.MacAddress, err)
		}
	}

	if d.SerialNumber == "" {
		log.Printf("[registry] warning: device %s (%s) has no serial_number", d.ID, d.DeviceModel)
	}

	return nil
}

// GetDevicesMap fetches the canonical device registry from the registry API.
// Returns two maps:
// - deviceID -> DeviceConfig
// - devEUI   -> deviceID
// All mandatory fields are validated; the whole fetch is rejected on any error.
func GetDevicesMap(apiBaseURL string) (map[string]DeviceConfig, map[string]string, error) {
	rows, err := fetchRegistryList[apiDevice](apiBaseURL + "/api/registry/v0.1/devices")
	if err != nil {
		return nil, nil, fmt.Errorf("fetch devices: %w", err)
	}
	log.Printf("[registry] devices=%d", len(rows))

	deviceMap := make(map[string]DeviceConfig, len(rows))
	devEUIToDeviceID := make(map[string]string, len(rows))

	for _, d := range rows {
		if !d.Allow {
			continue
		}

		if err := validateAPIDevice(d); err != nil {
			return nil, nil, fmt.Errorf("%w", err)
		}

		if _, exists := deviceMap[d.ID]; exists {
			return nil, nil, fmt.Errorf("duplicate device id in registry: %s", d.ID)
		}

		batteryMax := d.BatteryVoltageMax
		if batteryMax == 0 {
			batteryMax = 4.2
		}
		batteryMin := d.BatteryVoltageMin
		if batteryMin == 0 {
			batteryMin = 3.3
		}

		deviceMap[d.ID] = DeviceConfig{
			Model:             d.DeviceModel,
			DeviceType:        strings.ToUpper(d.DeviceType),
			SerialNumber:      d.SerialNumber,
			MacAddress:        strings.ToLower(d.MacAddress),
			AssetID:           d.AssetID,
			BatteryVoltageMax: batteryMax,
			BatteryVoltageMin: batteryMin,
			ProbeRangeM:       d.ProbeRangeM,
		}

		if d.DevEUI != "" {
			devEUINormalized := strings.ToLower(d.DevEUI)
			if mapped, exists := devEUIToDeviceID[devEUINormalized]; exists && mapped != d.ID {
				return nil, nil, fmt.Errorf("duplicate dev_eui mapping for %s (%s and %s)", devEUINormalized, mapped, d.ID)
			}
			devEUIToDeviceID[devEUINormalized] = d.ID
		}
	}

	return deviceMap, devEUIToDeviceID, nil
}

// ─── Asset registry ───────────────────────────────────────────────────────────

// apiAsset is the raw shape of one row from GET /api/registry/v0.1/assets.
// asset_name/asset_slug/asset_coords/parent_asset_id are intentionally not
// modeled here — this repo only ever needed an asset's id (for the asset_id
// tag), never its metadata. Fetched purely to validate that devices/sensors
// still reference an asset that actually exists — see ValidateAssetReferences.
type apiAsset struct {
	ID string `json:"id"`
}

// GetAssetIDSet fetches every known asset id from the registry API, for
// ValidateAssetReferences to check device/sensor asset_id values against.
func GetAssetIDSet(apiBaseURL string) (map[string]bool, error) {
	rows, err := fetchRegistryList[apiAsset](apiBaseURL + "/api/registry/v0.1/assets")
	if err != nil {
		return nil, fmt.Errorf("fetch assets: %w", err)
	}
	log.Printf("[registry] assets=%d", len(rows))

	out := make(map[string]bool, len(rows))
	for _, a := range rows {
		out[a.ID] = true
	}
	return out, nil
}

// ValidateAssetReferences logs a warning (never fails a reload) for every
// device/sensor whose asset_id doesn't match a currently-known asset — same
// lenient, soft-FK philosophy this file has always used for cross-entity
// references that might just be a timing gap, not a real error.
func ValidateAssetReferences(deviceMap map[string]DeviceConfig, sensorMap map[string]SensorConfig, validAssetIDs map[string]bool) {
	for deviceID, cfg := range deviceMap {
		if cfg.AssetID != "" && !validAssetIDs[cfg.AssetID] {
			log.Printf("[registry] warning: device %s references asset_id %s which does not exist", deviceID, cfg.AssetID)
		}
	}
	for sensorID, s := range sensorMap {
		if s.AssetID != "" && !validAssetIDs[s.AssetID] {
			log.Printf("[registry] warning: sensor %s references asset_id %s which does not exist", sensorID, s.AssetID)
		}
	}
}

// ─── Sensor registry ──────────────────────────────────────────────────────────

// apiSensor is the raw shape of one row from GET /api/registry/v0.1/sensors.
// Its own row identifier is "id" (see apiDevice's doc comment on this naming
// trap); DeviceID here is a genuine foreign key to a device, unchanged.
type apiSensor struct {
	ID          string `json:"id"`
	SensorType  string `json:"sensor_type"`
	DeviceID    string `json:"device_id"`
	DeviceIndex int    `json:"device_index"`
	SensorName  string `json:"sensor_name"`
	// AssetID is an override, independent of the sensor's device's own
	// asset_id — set only for a sensor on a bus-listening device (e.g. a
	// UC300) that's physically relaying readings for a different asset than
	// wherever the device itself sits. Effective asset for a sensor is
	// sensor.AssetID if set, else the device's own AssetID — see the
	// resolution step in parseDeviceModel.
	AssetID string `json:"asset_id"`
}

// SensorConfig is the resolved projection of one apiSensor, keyed by
// sensor_id in the map GetSensorsMap returns.
type SensorConfig struct {
	SensorType  string
	DeviceID    string
	DeviceIndex int
	SensorName  string
	AssetID     string // override — see apiSensor.AssetID
}

// validateAPISensor checks mandatory fields and format constraints for one
// sensor row. validTypes is record.AllSensorTypes(), computed once by the
// caller rather than per-entry. Returns an error for hard failures.
func validateAPISensor(s apiSensor, validTypes map[string]bool) error {
	if s.ID == "" {
		return fmt.Errorf("id is required")
	}
	if !isUUID(s.ID) {
		return fmt.Errorf("sensor %s: id must be a valid UUID", s.ID)
	}
	if s.SensorType == "" {
		return fmt.Errorf("sensor %s: sensor_type is required", s.ID)
	}
	if !validTypes[s.SensorType] {
		return fmt.Errorf("sensor %s: sensor_type %q is not a recognized sensor_type (see record.SensorTypes) — add it there first if it's genuinely new", s.ID, s.SensorType)
	}
	if s.DeviceID == "" {
		return fmt.Errorf("sensor %s: device_id is required", s.ID)
	}
	if !isUUID(s.DeviceID) {
		return fmt.Errorf("sensor %s: device_id must be a valid UUID", s.ID)
	}
	if s.DeviceIndex < 0 {
		return fmt.Errorf("sensor %s: device_index must not be negative", s.ID)
	}
	return nil
}

// GetSensorsMap fetches the canonical sensor registry from the registry API.
// Returns sensorID -> SensorConfig. All mandatory fields are validated; the
// whole fetch is rejected on any error. Cross-entity references (device_id
// existing in the device registry) are deliberately NOT checked here — same
// decoupling as before: a row pointing at a device_id that doesn't (yet)
// exist just never resolves to anything at decode time, rather than failing
// the whole fetch. Not unique on (device_id, sensor_type) alone — two
// sensors of the same type can legitimately share a device (e.g. three
// soil-moisture probes at different depths); device_index disambiguates
// them, and (device_id, sensor_type, device_index) together must be unique.
func GetSensorsMap(apiBaseURL string) (map[string]SensorConfig, error) {
	rows, err := fetchRegistryList[apiSensor](apiBaseURL + "/api/registry/v0.1/sensors")
	if err != nil {
		return nil, fmt.Errorf("fetch sensors: %w", err)
	}
	log.Printf("[registry] sensors=%d", len(rows))

	validTypes := record.AllSensorTypes()
	sensorMap := make(map[string]SensorConfig, len(rows))
	seenTuple := make(map[string]string, len(rows)) // (device_id,sensor_type,device_index) -> sensor_id

	for _, s := range rows {
		if err := validateAPISensor(s, validTypes); err != nil {
			return nil, fmt.Errorf("%w", err)
		}
		if _, exists := sensorMap[s.ID]; exists {
			return nil, fmt.Errorf("duplicate sensor id in registry: %s", s.ID)
		}
		tupleKey := sensorIndexKey(s.DeviceID, s.SensorType, s.DeviceIndex)
		if existing, exists := seenTuple[tupleKey]; exists {
			return nil, fmt.Errorf("device_id/sensor_type/device_index already assigned to sensor %s: %s", existing, tupleKey)
		}
		seenTuple[tupleKey] = s.ID
		sensorMap[s.ID] = SensorConfig{
			SensorType: s.SensorType, DeviceID: s.DeviceID, DeviceIndex: s.DeviceIndex,
			SensorName: s.SensorName, AssetID: s.AssetID,
		}
	}

	return sensorMap, nil
}

// ─── Modbus channel registry (UC100/UC300 RS485 routing) ─────────────────────

// apiModbusChannel is the raw shape of one row from GET
// /api/registry/v0.1/modbus-channels — already flattened/joined server-side
// (the underlying device->bus->channel table structure isn't exposed here).
// There is currently no equivalent endpoint for UC300's fixed-hardware IO
// channels (GPIO/PT100/ADC) — see DeviceChannels.IO's doc comment.
type apiModbusChannel struct {
	ID            string `json:"id"`
	DeviceID      string `json:"device_id"`
	BusType       string `json:"bus_type"`
	SensorID      string `json:"sensor_id"`
	ModbusChannel int    `json:"modbus_channel"` // 1-32; protocol never uses 0
	ModbusSlaveID int    `json:"modbus_slave_id"`
}

// DeviceChannels holds one UC-series device's channel->sensor_id routing.
type DeviceChannels struct {
	Modbus map[int]string // modbus_channel -> sensor_id
	// IO would be io_channel -> sensor_id (UC300's fixed-hardware GPIO/PT100/
	// ADC pins), but the registry API has no io-channels endpoint yet —
	// always empty until one exists. No currently-deployed device relies on
	// this (every real device_channels.json entry to date has been Modbus
	// only), so this is a known gap, not a regression in practice today.
	IO map[int]string
}

// GetDeviceChannelsMap fetches the Modbus channel routing registry from the
// registry API. Returns deviceID -> DeviceChannels. Routing only — never
// carries sensor_type. device_id/sensor_id existence in the device/sensor
// registries is deliberately not checked here, same decoupling as
// GetAssetIDSet/GetSensorsMap — a row referencing something that doesn't
// exist yet just never resolves to a tag at decode time. A channel/slave
// combination reused across two rows on the same device IS a hard error
// (mirrored by the API's own unique constraint), since that's a real,
// structural misconfiguration, not a timing gap.
func GetDeviceChannelsMap(apiBaseURL string) (map[string]DeviceChannels, error) {
	rows, err := fetchRegistryList[apiModbusChannel](apiBaseURL + "/api/registry/v0.1/modbus-channels")
	if err != nil {
		return nil, fmt.Errorf("fetch modbus-channels: %w", err)
	}
	log.Printf("[registry] modbus_channels=%d", len(rows))

	out := make(map[string]DeviceChannels)
	seenSensorID := make(map[string]bool, len(rows))

	for _, e := range rows {
		if e.DeviceID == "" || e.SensorID == "" || e.ModbusChannel == 0 || e.ModbusSlaveID == 0 {
			return nil, fmt.Errorf("modbus channel %s: device_id/sensor_id/modbus_channel/modbus_slave_id must all be set", e.ID)
		}
		if seenSensorID[e.SensorID] {
			return nil, fmt.Errorf("duplicate sensor_id in modbus channel registry: %s", e.SensorID)
		}
		seenSensorID[e.SensorID] = true

		dc, ok := out[e.DeviceID]
		if !ok {
			dc = DeviceChannels{Modbus: make(map[int]string), IO: make(map[int]string)}
		}
		if existing, exists := dc.Modbus[e.ModbusChannel]; exists {
			return nil, fmt.Errorf("device %s: modbus_channel %d already assigned to sensor %s", e.DeviceID, e.ModbusChannel, existing)
		}
		dc.Modbus[e.ModbusChannel] = e.SensorID
		out[e.DeviceID] = dc
	}

	return out, nil
}

// sensorIndexKey joins deviceID+sensorType+deviceIndex into the reverse-index
// key used by BuildSensorIndex/resolveSensorID. Not exported; the separator
// can't appear in a UUIDv7 or a record.ST value, so a plain join is safe.
// deviceIndex is 0 for every single-instance sensor_type — same as the zero
// value of record.SensorDataRecord.DeviceIndex, so callers never need to
// special-case it.
func sensorIndexKey(deviceID, sensorType string, deviceIndex int) string {
	return deviceID + "\x00" + sensorType + "\x00" + strconv.Itoa(deviceIndex)
}

// BuildSensorIndex derives a (device_id, sensor_type, device_index) ->
// sensor_id lookup from sensors.json, used to stamp a stable sensor_id onto
// every decoded record without decoders needing to know about the sensor
// registry at all.
//
// sensors.json is deliberately NOT unique on (device_id, sensor_type) alone
// — e.g. three soil-moisture probes on one device share a sensor_type,
// disambiguated by device_index (GetSensorsMap enforces uniqueness on the
// full (device_id, sensor_type, device_index) triple). A tuple that's STILL
// ambiguous even including device_index (shouldn't happen given that
// enforcement, but this index doesn't re-trust it) is left out here (logged,
// not an error) rather than resolved arbitrarily.
func BuildSensorIndex(sensorMap map[string]SensorConfig) map[string]string {
	counts := make(map[string]int, len(sensorMap))
	sensorIDByKey := make(map[string]string, len(sensorMap))
	for sensorID, s := range sensorMap {
		key := sensorIndexKey(s.DeviceID, s.SensorType, s.DeviceIndex)
		counts[key]++
		sensorIDByKey[key] = sensorID
	}
	index := make(map[string]string, len(sensorIDByKey))
	for key, sensorID := range sensorIDByKey {
		if counts[key] > 1 {
			log.Printf("[registry] warning: device_id/sensor_type/device_index tuple is ambiguous in sensors.json (%d sensors share it) — skipping from sensor_id index: %s", counts[key], key)
			continue
		}
		index[key] = sensorID
	}
	return index
}

// RegistrySnapshot bundles the hot-reloadable sensor/channel lookup maps a
// single decode+write needs. Callers capture one atomically under their own
// lock per message so a mid-flight reload can't mix an old SensorIndex with
// a new DeviceChannels map.
type RegistrySnapshot struct {
	SensorIndex    map[string]string
	SensorMap      map[string]SensorConfig
	DeviceChannels map[string]DeviceChannels
}

// resolveChannelRecord fills in SensorType/SensorID for a UC100/UC300 record
// carrying ModbusChannel or IOChannel instead of SensorType (see
// record.SensorDataRecord) by looking channel up in channelMap (either
// reg.DeviceChannels[deviceID].Modbus or .IO). kind is "modbus" or "io", used
// only for log messages. Returns ok=false when the channel has no
// device_channels.json entry, or that entry's sensor_id has no matching
// sensors.json entry — such readings are dropped by the caller rather than
// written under an empty/undefined sensor_type.
func resolveChannelRecord(r *record.SensorDataRecord, deviceID string, channel int, channelMap map[int]string, reg RegistrySnapshot, kind string) bool {
	sensorID, ok := channelMap[channel]
	if !ok {
		log.Printf("[parse] device_id=%s %s_channel=%d has no device_channels.json entry — dropping unmapped reading", deviceID, kind, channel)
		return false
	}
	sensor, ok := reg.SensorMap[sensorID]
	if !ok || sensor.SensorType == "" {
		log.Printf("[parse] device_id=%s %s_channel=%d sensor_id=%s not found in sensors.json — dropping unmapped reading", deviceID, kind, channel, sensorID)
		return false
	}
	r.SensorID = sensorID
	r.SensorType = sensor.SensorType
	return true
}

// effectiveAssetID resolves which asset a record should be tagged with: the
// sensor's own asset_id override if it has one, else the device's own home
// asset (deviceAssetID). This is the fix for a bus-listening device (e.g. a
// UC300) that's physically mounted on one asset while relaying sensor
// readings that actually belong to a *different* asset reachable on the
// same bus — see apiSensor.AssetID's doc comment for the full rationale.
// Computing this from the device alone (ignoring the sensor's own override)
// would silently misattribute every such sensor's data to the wrong asset.
func effectiveAssetID(deviceAssetID string, sensor SensorConfig) string {
	if sensor.AssetID != "" {
		return sensor.AssetID
	}
	return deviceAssetID
}
