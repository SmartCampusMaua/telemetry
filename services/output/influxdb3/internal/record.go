// Package internal implements the JetStream -> InfluxDB3 bridge: it
// consumes decoded CAN telemetry (see
// services/input/nats/internal/signals.go's TelemetryRecord) from the
// ZC2X_TELEMETRY JetStream stream and writes it into InfluxDB3 as one point
// per signal.
package internal

// TelemetryRecord mirrors services/input/nats/internal/signals.go's struct
// of the same name field-for-field. Duplicated rather than imported: that
// struct lives under an internal/ package in a different Go module
// (services/input/nats), and Go's internal-package visibility rule only
// allows imports from within the tree rooted at internal/'s parent --
// services/input/nats/..., not this module. The two are coupled only by
// wire format (this package's own tests decode a literal JSON sample to
// catch drift), the same tradeoff RFC-0001 already makes for the raw packet
// format across the C firmware and this Go codebase.
type TelemetryRecord struct {
	DeviceID       string  `json:"device_id"`
	AssetID        string  `json:"asset_id,omitempty"`
	Origin         string  `json:"origin"`
	CANMessageID   uint32  `json:"can_message_id"`
	CANMessageName string  `json:"can_message_name"`
	SensorType     string  `json:"sensor_type"`
	Value          float64 `json:"value"`
	// ValueKind is "float", "int", or "bool" -- see BuildPoint, which routes
	// Value into InfluxDB3's value_float/value_int/value_bool column
	// accordingly.
	ValueKind   string `json:"value_kind"`
	TimestampMS int64  `json:"timestamp_ms"`
}
