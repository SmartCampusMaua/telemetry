package mqtt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"

	"github.com/SmartCampusMaua/telemetry/packages/go-data"
	"github.com/SmartCampusMaua/telemetry/packages/go-parse/devices/registry"
	"github.com/SmartCampusMaua/telemetry/packages/go-parse/providers/chirpstack"
	"github.com/SmartCampusMaua/telemetry/packages/go-parse/providers/everynet"
	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

// ─────────────────────────────────────────────────────────────────────────────
// Provider detection
// ─────────────────────────────────────────────────────────────────────────────

// lnsProviderProbe captures the minimum root-level fields needed to fingerprint
// an LNS message. json.Unmarshal silently ignores all other fields.
type lnsProviderProbe struct {
	DeduplicationId string `json:"deduplicationId"` // ChirpstackV4
	Meta            struct {
		Device string `json:"device"` // Everynet
	} `json:"meta"`
	Params struct {
		Payload string `json:"payload"` // Everynet
	} `json:"params"`
}

// DetectProvider identifies the message source from JSON content alone.
// Returns "chirpstackv4", "everynet", "custom", or "" (unknown).
func DetectProvider(message string) string {
	var lns lnsProviderProbe
	if json.Unmarshal([]byte(message), &lns) == nil {
		if lns.DeduplicationId != "" {
			return "chirpstackv4"
		}
		if lns.Meta.Device != "" && lns.Params.Payload != "" {
			return "everynet"
		}
	}
	// Custom direct-MQTT: JSON array with variable:"data"
	var probe []struct {
		Variable string `json:"variable"`
	}
	if json.Unmarshal([]byte(message), &probe) == nil && len(probe) > 0 && probe[0].Variable == "data" {
		return "custom"
	}
	return ""
}

// undecryptedEverynetProbe additionally captures encrypted_payload — present
// on some Everynet uplinks instead of the usual params.payload when the
// network server hasn't decrypted the frame (unregistered/mismatched
// session keys for this device on Everynet's own platform, not something
// fixable from this side — see DiagnoseUndetectedEverynet). Confirmed live
// against real traffic from a device registered as everynet whose uplinks
// carry only encrypted_payload.
type undecryptedEverynetProbe struct {
	Type string `json:"type"`
	Meta struct {
		Device string `json:"device"`
	} `json:"meta"`
	Params struct {
		Payload          string `json:"payload"`
		EncryptedPayload string `json:"encrypted_payload"`
	} `json:"params"`
}

// DiagnoseUndetectedEverynet reports whether message looks like an Everynet
// uplink that Everynet's own network server failed to decrypt before
// forwarding — only called on the already-rare "couldn't detect provider"
// path, so a second JSON parse here costs nothing.
func DiagnoseUndetectedEverynet(message string) (device string, ok bool) {
	var p undecryptedEverynetProbe
	if json.Unmarshal([]byte(message), &p) != nil {
		return "", false
	}
	if p.Type == "uplink" && p.Meta.Device != "" && p.Params.Payload == "" && p.Params.EncryptedPayload != "" {
		return p.Meta.Device, true
	}
	return "", false
}

// ─────────────────────────────────────────────────────────────────────────────
// Colored logging helpers
// ─────────────────────────────────────────────────────────────────────────────

// ANSI color codes. logError prints in bold red; logWarn in bold yellow.
// Both write to the same destination as log.Printf so timestamps align.
func logError(format string, args ...any) {
	log.Printf("\033[1;31m[ERROR] "+format+"\033[0m", args...)
}
func logWarn(format string, args ...any) {
	log.Printf("\033[1;33m[WARN]  "+format+"\033[0m", args...)
}

// ─────────────────────────────────────────────────────────────────────────────
// InfluxDB3 point building
// ─────────────────────────────────────────────────────────────────────────────

// validateRecord returns an error if the record would produce a malformed or
// unqueryable point in InfluxDB3.  Checked before building the Point object so
// bad records are rejected at zero network cost.
func validateRecord(r record.SensorDataRecord) error {
	switch {
	case r.SensorType == "":
		return fmt.Errorf("empty sensor_type (device_id=%s)", r.DeviceID)
	case r.DeviceID == "":
		return fmt.Errorf("empty device_id (sensor_type=%s)", r.SensorType)
	case r.DeviceModel == "":
		return fmt.Errorf("empty device_model (device_id=%s, sensor_type=%s)", r.DeviceID, r.SensorType)
	case r.Timestamp.IsZero():
		return fmt.Errorf("zero timestamp (device_id=%s, sensor_type=%s)", r.DeviceID, r.SensorType)
	case r.ValueType != "float" && r.ValueType != "int" && r.ValueType != "bool":
		return fmt.Errorf("unknown value_type %q (device_id=%s, sensor_type=%s)", r.ValueType, r.DeviceID, r.SensorType)
	case r.ValueType == "float" && r.ValueFloat == nil:
		return fmt.Errorf("nil float value (device_id=%s, sensor_type=%s)", r.DeviceID, r.SensorType)
	case r.ValueType == "float" && r.ValueFloat != nil && (math.IsNaN(*r.ValueFloat) || math.IsInf(*r.ValueFloat, 0)):
		return fmt.Errorf("non-finite float %v (device_id=%s, sensor_type=%s)", *r.ValueFloat, r.DeviceID, r.SensorType)
	case r.ValueType == "int" && r.ValueInt == nil:
		return fmt.Errorf("nil int value (device_id=%s, sensor_type=%s)", r.DeviceID, r.SensorType)
	case r.ValueType == "bool" && r.ValueBool == nil:
		return fmt.Errorf("nil bool value (device_id=%s, sensor_type=%s)", r.DeviceID, r.SensorType)
	}
	return nil
}

// buildSensorPoints converts decoded records into valid InfluxDB3 points,
// dropping (and logging) any record that fails validation. Pure — no I/O.
// The actual write happens later, fanned into a single data.BatchWriter (see
// its doc comment) instead of one WritePoints call per message here.
func buildSensorPoints(records []record.SensorDataRecord) []*influxdb3.Point {
	if len(records) == 0 {
		return nil
	}
	points := make([]*influxdb3.Point, 0, len(records))
	for _, r := range records {
		if err := validateRecord(r); err != nil {
			logWarn("dropping record: %v", err)
			continue
		}
		p := influxdb3.NewPointWithMeasurement("sensor_data").
			SetTag("sensor_type", r.SensorType).
			SetTag("device_model", r.DeviceModel).
			SetTag("device_id", r.DeviceID).
			SetTag("provider", r.Provider).
			SetTimestamp(r.Timestamp)
		if r.DevEUI != "" {
			p = p.SetTag("dev_eui", r.DevEUI)
		}
		if r.MacAddress != "" {
			p = p.SetTag("mac_address", r.MacAddress)
		}
		if r.SensorID != "" {
			p = p.SetTag("sensor_id", r.SensorID)
		}
		if r.AssetID != "" {
			p = p.SetTag("asset_id", r.AssetID)
		}
		switch r.ValueType {
		case "float":
			if r.ValueFloat != nil {
				p = p.SetDoubleField("value_float", *r.ValueFloat)
			}
		case "int":
			if r.ValueInt != nil {
				p = p.SetIntegerField("value_int", *r.ValueInt)
			}
		case "bool":
			if r.ValueBool != nil {
				p = p.SetBooleanField("value_bool", *r.ValueBool)
			}
		}
		if p.HasFields() {
			points = append(points, p)
		}
	}
	return points
}

// ─────────────────────────────────────────────────────────────────────────────
// Payload dispatcher
// ─────────────────────────────────────────────────────────────────────────────

func b64ToByte(b64 string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(b64)
}

// parseDeviceModel decodes rawPayload and writes sensor_data points to InfluxDB3.
//
// provider == "custom": rawPayload is raw JSON; timestamp comes from the message.
// LNS providers:        rawPayload is base64-encoded binary; timestamp from LNS frame.
func parseDeviceModel(sensorWriter *data.BatchWriter,
	deviceID, devEUI string, config DeviceConfig, provider, rawPayload string, port uint64, ts time.Time,
	reg RegistrySnapshot) {

	if rawPayload == "" {
		log.Printf("[parse] empty payload for deviceID=%s", deviceID)
		return
	}

	var records []record.SensorDataRecord

	deviceModel := config.Model
	if provider == "custom" {
		// Custom path: raw JSON, decoder extracts its own timestamp.
		var ok bool
		records, ok = registry.ParseCustom(string(deviceModel), "", rawPayload, deviceID)
		if !ok {
			log.Printf("[parse] no custom decoder for device model %s", deviceModel)
			return
		}
	} else {
		// LNS path: base64-encoded binary payload.
		b, err := b64ToByte(rawPayload)
		if err != nil {
			log.Printf("[parse] b64 decode error for %s: %v", deviceID, err)
			return
		}
		var ok bool
		records, ok = registry.DecodeLNS(string(deviceModel), "", b, deviceID, provider, port, ts)
		if !ok {
			log.Printf("[parse] no LNS decoder for device model %s", deviceModel)
			return
		}
	}

	if len(records) == 0 {
		log.Printf("[parse] decoder returned 0 records for %s (%s)", deviceID, deviceModel)
		return
	}
	resolved := records[:0]
	for i := range records {
		r := records[i]
		r.DevEUI = devEUI
		r.MacAddress = config.MacAddress
		r.AssetID = config.AssetID
		switch {
		case r.ModbusChannel != 0:
			if !resolveChannelRecord(&r, deviceID, r.ModbusChannel, reg.DeviceChannels[deviceID].Modbus, reg, "modbus") {
				continue
			}
		case r.IOChannel != 0:
			if !resolveChannelRecord(&r, deviceID, r.IOChannel, reg.DeviceChannels[deviceID].IO, reg, "io") {
				continue
			}
		default:
			if sensorID, ok := reg.SensorIndex[sensorIndexKey(deviceID, r.SensorType, r.DeviceIndex)]; ok {
				r.SensorID = sensorID
			}
		}
		// Only applies once SensorID is actually resolved; an unresolved
		// reading keeps the device-level default already set on r.AssetID above.
		if r.SensorID != "" {
			r.AssetID = effectiveAssetID(r.AssetID, reg.SensorMap[r.SensorID])
		}
		resolved = append(resolved, r)
	}
	records = resolved
	if len(records) == 0 {
		log.Printf("[parse] all records dropped after sensor resolution for %s (%s)", deviceID, deviceModel)
		return
	}
	points := buildSensorPoints(records)
	if len(points) == 0 {
		log.Printf("[parse] no valid points after building for %s (%s)", deviceID, deviceModel)
		return
	}
	sensorWriter.Enqueue(points)
	if devEUI != "" {
		log.Printf("[influxdb3] queued %d points: device_model=%s device_id=%s dev_eui=%s",
			len(points), strings.ToLower(string(deviceModel)), deviceID, devEUI)
	} else {
		log.Printf("[influxdb3] queued %d points: device_model=%s device_id=%s",
			len(points), strings.ToLower(string(deviceModel)), deviceID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Message entry point – single provider switch, no redundant checks
// ─────────────────────────────────────────────────────────────────────────────

// ParseMsg routes an incoming MQTT message to the correct decoder.
func ParseMsg(sensorWriter *data.BatchWriter, provider, deviceID, devEUI string, config DeviceConfig, message string, reg RegistrySnapshot) {
	if message == "" {
		return
	}
	switch provider {

	case "custom":
		// Direct MQTT device: raw JSON, no LNS frame wrapping.
		parseDeviceModel(sensorWriter, deviceID, "", config, "custom", message, 0, time.Time{}, reg)

	case "chirpstackv4":
		frame, err := chirpstack.Parse(message)
		if err != nil {
			log.Printf("[parseMsg/chirpstackv4] %v (deviceID=%s)", err, deviceID)
			return
		}
		parseDeviceModel(sensorWriter, deviceID, devEUI, config, "chirpstackv4", frame.Data, frame.Port, frame.Timestamp, reg)

	case "everynet":
		frame, err := everynet.Parse(message)
		if err != nil {
			log.Printf("[parseMsg/everynet] %v (deviceID=%s)", err, deviceID)
			return
		}
		parseDeviceModel(sensorWriter, deviceID, devEUI, config, "everynet", frame.Data, frame.Port, frame.Timestamp, reg)

	default:
		log.Printf("[parseMsg] unsupported provider=%s for device_id=%s (%.80s…)", provider, deviceID, message)
	}
}

// ResolveIncomingDevice maps topic identifier to canonical deviceID.
// For custom provider, topic contains deviceID directly.
// For LoRaWAN providers, topic contains devEUI and must be resolved via registry map.
func ResolveIncomingDevice(provider, topicIdentifier string, deviceMap map[string]DeviceConfig, devEUIToDeviceID map[string]string) (deviceID, devEUI string, config DeviceConfig, ok bool) {
	if provider == "custom" {
		deviceID = topicIdentifier
		config, ok = deviceMap[deviceID]
		return deviceID, "", config, ok
	}

	if provider != "chirpstackv4" && provider != "everynet" {
		return "", "", DeviceConfig{}, false
	}

	devEUI = strings.ToLower(topicIdentifier)
	deviceID, ok = devEUIToDeviceID[devEUI]
	if !ok {
		return "", devEUI, DeviceConfig{}, false
	}
	config, ok = deviceMap[deviceID]
	if !ok {
		return deviceID, devEUI, DeviceConfig{}, false
	}
	return deviceID, devEUI, config, true
}
