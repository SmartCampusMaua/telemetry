// Package internal — signal decoding. See candb for the CAN2 message/signal
// database (mirrors docs/architecture/zc2x-can2.dbc). This file decodes
// each validated CAN_FRAME packet's payload into named, scaled
// engineering-unit signals and republishes each one as its own JSON
// telemetry record, additionally to (never instead of) the raw packet
// republish in adapter.go's handleMessage. Unknown CAN IDs are expected
// (OBU/RSU forward every CAN ID unfiltered) and are silently skipped.
//
// The published payload is plain JSON, not InfluxDB Line Protocol: the
// nearest-term consumer is the apps/web Next.js dashboard, which can
// JSON.parse this directly, whereas Line Protocol isn't natively parseable
// in the JS/TS ecosystem. services/output/influxdb3 is also a consumer of
// this same stream — encoding JSON -> InfluxDB3 points is a small, natural
// piece for that writer to own, cheaper overall than making every
// non-Influx consumer parse InfluxDB's format instead. One record per
// signal, e.g. GPS2's GPSSpeed:
//
//	{"device_id":"010000000000","origin":"obu","can_message_id":784,"can_message_name":"gps_2","sensor_type":"gps_speed","value":120.3,"value_kind":"float","timestamp_ms":1737045296123}
//
// Fields: device_id, asset_id (what device_id is physically embedded in —
// see assets.go; omitted when the registry has no entry for this device),
// origin (relay path — see the comment on DefaultTelemetryPublishSubject),
// can_message_id, can_message_name, sensor_type, value (every signal's value
// lands in this one field regardless of physical type; sensor_type is what
// tells them apart — deliberately not one JSON field per signal name),
// value_kind ("float"/"int"/"bool", see TelemetryRecord's doc), timestamp_ms.
// Snake_case keys, not idiomatic JS camelCase: kept identical to the
// tag/field names used throughout this pipeline (candb signal names ->
// sensor_type via toSnakeCase) rather than adding a second naming
// convention at this one hop.
//
// timestamp_ms is milliseconds since the Unix epoch, assigned HERE at Go
// receive time — not pkt.Timestamp, which is esp_timer_get_time()
// (microseconds since the transmitting ESP32's own boot, not wall-clock
// time, and not comparable across devices or reboots). Milliseconds, not
// the nanoseconds this package used internally before: JS numbers are
// IEEE-754 doubles, exact only up to 2^53, and a nanosecond epoch is
// already past that — silently lossy through JSON.parse on the Next.js
// side. Milliseconds is also Date's own native unit in JS, and far more
// resolution than anything meaningful for a decoded CAN signal.
package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/rogeriocassares/zc2x/services/input/nats/internal/candb"
)

// TelemetryPublishSubjectFn maps a packet's source NATS Core subject and one
// decoded signal's name to the JetStream subject that signal's telemetry
// record should be published on.
type TelemetryPublishSubjectFn func(sourceSubject, sensorType string) string

// DefaultTelemetryPublishSubject publishes telemetry for signal "EngineRPM"
// (already snake_cased by the caller) arriving on sourceSubject
// "zc2x.can.obu" to e.g. prefix+"obu.engine_rpm" (so "zc2x.js.telemetry."
// -> "zc2x.js.telemetry.obu.engine_rpm").
//
// Origin comes before sensor type deliberately: NATS's ">" wildcard only
// matches trailing tokens, so this ordering keeps "everything from OBU"
// (zc2x.js.telemetry.obu.>) a clean trailing-wildcard query — the more
// common need (e.g. "is OBU's path getting data through at all") — while
// "one sensor from any origin" still works fine with "*" in the middle
// (zc2x.js.telemetry.*.engine_rpm). The reverse ordering would make the
// first pattern need "*" instead, which breaks if a fourth token is ever
// added later. can_message_id/can_message_name stay on every record
// regardless (see TelemetryRecord) — this subject token is a
// routing optimization for consumers who only want one sensor (e.g. a
// live dashboard gauge), not a replacement for that grouping.
//
// The trimmed origin token is the relay path (which NATS Core subject the
// packet arrived on — OBU's direct WiFi link vs RSU's XBee relay), not the
// originating device: RSU forwards packet bytes unchanged (RFC-0001
// immutability), so a packet's DeviceID is always OBU's, even when RSU
// relayed it. The relay path is only recoverable from sourceSubject, which
// is why it's threaded through here (and into the "origin" tag) rather than
// read back out of the decoded payload.
func DefaultTelemetryPublishSubject(telemetryPrefix, sourcePrefix string) TelemetryPublishSubjectFn {
	return func(sourceSubject, sensorType string) string {
		origin := strings.TrimPrefix(sourceSubject, sourcePrefix)
		return telemetryPrefix + origin + "." + sensorType
	}
}

var (
	// Word boundary before an uppercase letter that follows a lowercase
	// letter or digit: "EngineRPM" -> the R after e (not inside RPM itself).
	snakeCaseLowerToUpper = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	// Word boundary inside a run of uppercase letters immediately followed
	// by another capitalized word: "GForce" -> split before "Force", but
	// "RPM" (nothing capitalized follows) stays one word.
	snakeCaseAcronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
	// Digit boundary: "Temperature1" -> "Temperature_1".
	snakeCaseDigitBoundary = regexp.MustCompile(`([A-Za-z])([0-9])`)
)

// toSnakeCase converts a PascalCase/mixedCase DBC identifier to snake_case,
// treating runs of uppercase letters as one word (so "EngineRPM" ->
// "engine_rpm", not "engine_r_p_m") and splitting off trailing digits
// ("ExhaustCylinderTemperature1" -> "exhaust_cylinder_temperature_1").
func toSnakeCase(s string) string {
	s = snakeCaseLowerToUpper.ReplaceAllString(s, "${1}_${2}")
	s = snakeCaseAcronymBoundary.ReplaceAllString(s, "${1}_${2}")
	s = snakeCaseDigitBoundary.ReplaceAllString(s, "${1}_${2}")
	return strings.ToLower(s)
}

// decimalPlaces returns how many decimal digits are meaningful for a signal
// with the given scale (0.1 -> 1, 0.001 -> 3, 1e-7 -> 7, 1 -> 0). Rounding
// published values to this width avoids float64 multiplication noise like
// "101.30000000000001" — every scale in this DBC is an exact power of ten
// (see docs/architecture/zc2x-can2.dbc), so this is exact, not a heuristic.
func decimalPlaces(scale float64) int {
	if scale <= 0 || scale >= 1 {
		return 0
	}
	return int(math.Round(-math.Log10(scale)))
}

// TelemetryRecord is one decoded CAN signal's value, JSON-encoded before
// publishing — see the package doc for why JSON (not InfluxDB Line
// Protocol) and what each field means.
type TelemetryRecord struct {
	DeviceID       string  `json:"device_id"`
	AssetID        string  `json:"asset_id,omitempty"`
	Origin         string  `json:"origin"`
	CANMessageID   uint32  `json:"can_message_id"`
	CANMessageName string  `json:"can_message_name"`
	SensorType     string  `json:"sensor_type"`
	Value          float64 `json:"value"`
	// ValueKind is "float", "int", or "bool" (candb.Signal.ValueKind,
	// stringified) -- additive alongside Value rather than a replacement, so
	// existing consumers reading Value unchanged keep working. Added for
	// services/output/influxdb3, which can't import candb itself (it's a
	// different Go module, and candb lives under an internal/ package only
	// importable from within services/input/nats) -- this field is how the
	// bool/int/float classification travels downstream to become InfluxDB3's
	// value_bool/value_int/value_float columns instead of one untyped float.
	ValueKind   string `json:"value_kind"`
	TimestampMS int64  `json:"timestamp_ms"`
}

// roundToDecimals rounds value to the given number of decimal digits,
// avoiding float64 multiplication noise like "101.30000000000001" showing
// up verbatim in the published JSON — every scale in this DBC is an exact
// power of ten (see docs/architecture/zc2x-can2.dbc), so decimals from
// decimalPlaces is exact, not a heuristic.
func roundToDecimals(value float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(value*p) / p
}

// decodeAndPublishSignals decodes pkt's CAN payload via candb and, if
// pkt.CANID is a known ZC2X CAN2 message, republishes each decoded signal
// as its own JSON TelemetryRecord. Unknown CAN IDs are expected (OBU/RSU
// forward every CAN ID unfiltered) and are silently skipped; any other
// decode error is logged and dropped — best-effort, never blocks or fails
// the raw-packet republish that already happened in handleMessage.
func (a *Adapter) decodeAndPublishSignals(ctx context.Context, pkt Packet, sourceSubject string) {
	msg, ok := candb.MessageByID(pkt.CANID)
	if !ok {
		return
	}
	signals, err := candb.DecodeSignals(pkt.CANID, pkt.Data[:pkt.DLC])
	if err != nil {
		log.Printf("internal: signal decode failed for can_id=0x%03x on %q: %v", pkt.CANID, sourceSubject, err)
		return
	}

	origin := strings.TrimPrefix(sourceSubject, a.cfg.SourcePrefix)
	messageName := toSnakeCase(msg.Name)
	deviceID := fmt.Sprintf("%x", pkt.DeviceID)
	assetID := a.cfg.AssetRegistry[deviceID]
	// One timestamp per received frame: every signal decoded from it
	// genuinely occurred at the same instant, and capturing it once avoids
	// N redundant time.Now() calls for an N-signal message.
	timestampMS := time.Now().UnixMilli()

	for _, sig := range msg.Signals {
		value, ok := signals[sig.Name]
		if !ok {
			continue // can't happen: DecodeSignals populates one entry per msg.Signals
		}
		sensorType := toSnakeCase(sig.Name)
		record := TelemetryRecord{
			DeviceID:       deviceID,
			AssetID:        assetID,
			Origin:         origin,
			CANMessageID:   msg.ID,
			CANMessageName: messageName,
			SensorType:     sensorType,
			Value:          roundToDecimals(value, decimalPlaces(sig.Scale)),
			ValueKind:      sig.ValueKind().String(),
			TimestampMS:    timestampMS,
		}
		payload, err := json.Marshal(record)
		if err != nil {
			log.Printf("internal: marshal telemetry json failed for %s: %v", sensorType, err)
			continue
		}
		subject := a.cfg.TelemetryPublishSubjectFn(sourceSubject, sensorType)
		if _, err := a.js.Publish(ctx, subject, payload); err != nil {
			log.Printf("internal: publish telemetry to jetstream %q failed: %v", subject, err)
		}
	}
}
