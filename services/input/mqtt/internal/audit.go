package mqtt

import (
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"

	"github.com/SmartCampusMaua/telemetry/packages/go-data"
)

// buildAuditPoint builds (but does not write) one raw-payload audit point.
//
// Schema (measurement = "raw"):
//
//	Tags:  device_id                   — identifies the physical device
//	       asset_id                    — whichever asset the device was assigned to at
//	                                      this instant (see the registry API); omitted when unassigned
//	       event_type = "payload_ingest" — fixed; low-cardinality; extensible
//	Field: raw_data  (string ≤512 chars) — original undecoded message
//	Time:  ingestion timestamp (time.Now() at message receipt)
//
// asset_id here is deliberately just a snapshot of "current" at each instant,
// same as every other tag on this row — raw is already a continuous,
// timestamped record of every message, so the full assignment history is
// already reconstructable from it without any separate change-tracking.
//
// The audit database carries a short retention policy set on the InfluxDB3 server
// (e.g., 7–30 days) independently of the sensor_data database.
func buildAuditPoint(deviceID, assetID, rawData string, ts time.Time) *influxdb3.Point {
	const maxLen = 512
	if len(rawData) > maxLen {
		rawData = rawData[:maxLen] + "…"
	}
	p := influxdb3.NewPointWithMeasurement("raw").
		SetTag("device_id", deviceID).
		SetTag("event_type", "payload_ingest").
		SetStringField("raw_data", rawData).
		SetTimestamp(ts)
	if assetID != "" {
		p = p.SetTag("asset_id", assetID)
	}
	return p
}

// WriteAuditLog hands one raw-payload audit point to auditWriter — see
// data.BatchWriter's doc comment for why this is no longer a direct
// WritePoints call per message.
func WriteAuditLog(auditWriter *data.BatchWriter, deviceID, assetID, rawData string, ts time.Time) {
	auditWriter.Enqueue([]*influxdb3.Point{buildAuditPoint(deviceID, assetID, rawData, ts)})
}
