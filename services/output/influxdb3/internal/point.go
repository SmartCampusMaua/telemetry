package internal

import (
	"fmt"
	"strconv"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

// BuildPoint converts one decoded TelemetryRecord into an InfluxDB3 point on
// table (measurement) using this project's fixed telemetry schema:
//
// Tags (indexed strings -- InfluxDB3 tag values are always strings, never a
// typed column, unlike fields below): sensor_type, device_id, asset_id
// (omitted when empty -- see signals.go's AssetRegistry comment: not every
// device_id resolves to one, and an empty-string tag is worse than an
// absent one for "WHERE asset_id IS NOT NULL"-style queries), origin,
// can_message_name, and can_message_id. can_message_id is rec.CANMessageID
// formatted in decimal (strconv.FormatUint base 10), matching
// docs/architecture/zc2x-can2.dbc's own BO_ <id> convention (e.g. "BO_ 512
// PE1" -- the DBC's canonical IDs are decimal, not hex), so a value here is
// directly greppable against the DBC source rather than needing a base
// conversion.
//
// Fields: exactly one of value_float/value_int/value_bool, chosen by
// rec.ValueKind (computed upstream by candb.Signal.ValueKind() -- see
// services/input/nats/internal/candb/candb.go's doc for why that
// classification can't be recomputed here). Writing a typed column per
// signal kind, rather than one untyped float for every signal regardless of
// its real nature, is what lets SQL/Grafana treat e.g. EngineRunning as a
// real boolean and EngineRPM as a real integer instead of every value
// looking like a fractional measurement.
func BuildPoint(table string, rec TelemetryRecord) (*influxdb3.Point, error) {
	tags := map[string]string{
		"sensor_type":      rec.SensorType,
		"device_id":        rec.DeviceID,
		"origin":           rec.Origin,
		"can_message_name": rec.CANMessageName,
		"can_message_id":   strconv.FormatUint(uint64(rec.CANMessageID), 10),
	}
	if rec.AssetID != "" {
		tags["asset_id"] = rec.AssetID
	}

	fields := map[string]any{}
	switch rec.ValueKind {
	case "bool":
		fields["value_bool"] = rec.Value != 0
	case "int":
		fields["value_int"] = int64(rec.Value)
	case "float", "":
		fields["value_float"] = rec.Value
	default:
		return nil, fmt.Errorf("internal: unknown value_kind %q for sensor %q", rec.ValueKind, rec.SensorType)
	}

	return influxdb3.NewPoint(table, tags, fields, time.UnixMilli(rec.TimestampMS)), nil
}
