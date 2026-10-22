package internal

import (
	"testing"
)

func TestBuildPoint_Float(t *testing.T) {
	rec := TelemetryRecord{
		DeviceID:       "010000000000",
		AssetID:        "vehicle-1",
		Origin:         "obu",
		CANMessageID:   784,
		CANMessageName: "gps_2",
		SensorType:     "gps_speed",
		Value:          120.3,
		ValueKind:      "float",
		TimestampMS:    1737045296123,
	}
	p, err := BuildPoint("telemetry", rec)
	if err != nil {
		t.Fatalf("BuildPoint: %v", err)
	}
	if got := p.GetMeasurement(); got != "telemetry" {
		t.Errorf("measurement = %q, want %q", got, "telemetry")
	}
	assertTag(t, p, "sensor_type", "gps_speed")
	assertTag(t, p, "device_id", "010000000000")
	assertTag(t, p, "asset_id", "vehicle-1")
	assertTag(t, p, "origin", "obu")
	assertTag(t, p, "can_message_name", "gps_2")
	assertTag(t, p, "can_message_id", "784") // decimal, matching the DBC's own "BO_ 784 GPS2" convention, not "0x310"

	f := p.GetDoubleField("value_float")
	if f == nil || *f != 120.3 {
		t.Errorf("value_float = %v, want 120.3", f)
	}
	if p.GetField("value_int") != nil || p.GetField("value_bool") != nil {
		t.Errorf("expected only value_float set, got value_int=%v value_bool=%v", p.GetField("value_int"), p.GetField("value_bool"))
	}
}

func TestBuildPoint_Int(t *testing.T) {
	rec := TelemetryRecord{SensorType: "engine_rpm", Value: 7200, ValueKind: "int"}
	p, err := BuildPoint("telemetry", rec)
	if err != nil {
		t.Fatalf("BuildPoint: %v", err)
	}
	i := p.GetIntegerField("value_int")
	if i == nil || *i != 7200 {
		t.Errorf("value_int = %v, want 7200", i)
	}
}

func TestBuildPoint_Bool(t *testing.T) {
	rec := TelemetryRecord{SensorType: "engine_running", Value: 1, ValueKind: "bool"}
	p, err := BuildPoint("telemetry", rec)
	if err != nil {
		t.Fatalf("BuildPoint: %v", err)
	}
	b := p.GetBooleanField("value_bool")
	if b == nil || *b != true {
		t.Errorf("value_bool = %v, want true", b)
	}

	rec.Value = 0
	p, err = BuildPoint("telemetry", rec)
	if err != nil {
		t.Fatalf("BuildPoint: %v", err)
	}
	b = p.GetBooleanField("value_bool")
	if b == nil || *b != false {
		t.Errorf("value_bool = %v, want false", b)
	}
}

func TestBuildPoint_NoAssetID(t *testing.T) {
	// RSU-relay data or an unregistered device_id -- AssetID stays empty and
	// must be an absent tag, not an empty-string one (see BuildPoint's doc).
	rec := TelemetryRecord{SensorType: "engine_rpm", Value: 1, ValueKind: "int"}
	p, err := BuildPoint("telemetry", rec)
	if err != nil {
		t.Fatalf("BuildPoint: %v", err)
	}
	if _, ok := p.GetTag("asset_id"); ok {
		t.Errorf("expected no asset_id tag when AssetID is empty")
	}
}

func TestBuildPoint_UnknownValueKind(t *testing.T) {
	rec := TelemetryRecord{SensorType: "mystery", Value: 1, ValueKind: "string"}
	if _, err := BuildPoint("telemetry", rec); err == nil {
		t.Fatal("expected error for unknown value_kind, got nil")
	}
}

func assertTag(t *testing.T, p interface {
	GetTag(string) (string, bool)
}, name, want string) {
	t.Helper()
	got, ok := p.GetTag(name)
	if !ok || got != want {
		t.Errorf("tag %s = %q (ok=%v), want %q", name, got, ok, want)
	}
}
