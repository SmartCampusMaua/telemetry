package milesight

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

func TestDecodeAM103(t *testing.T) {
	payload := []byte{
		0x01, 0x75, 0x55, // battery: 85%
		0x03, 0x67, 0xEB, 0x00, // temperature: 235 -> 23.5°C
		0x04, 0x68, 0x6E, // humidity: 110 -> 55%
		0x07, 0x7D, 0x8A, 0x02, // co2: 650 ppm
	}

	records := DecodeAM103(payload, "dev-1", "chirpstackv4", time.Now())

	st := record.ST
	got := map[string]record.SensorDataRecord{}
	for _, r := range records {
		got[r.SensorType] = r
		if r.DeviceModel != "am103" {
			t.Errorf("DeviceModel = %q, want am103", r.DeviceModel)
		}
	}

	bat := got[st.BatteryLevel]
	if bat.ValueFloat == nil || *bat.ValueFloat != 85 {
		t.Errorf("BatteryLevel: got %+v, want 85", bat)
	}
	temp := got[st.AirTemp]
	if temp.ValueFloat == nil || *temp.ValueFloat != 23.5 {
		t.Errorf("AirTemp: got %+v, want 23.5", temp)
	}
	rh := got[st.AirRH]
	if rh.ValueFloat == nil || *rh.ValueFloat != 55 {
		t.Errorf("AirRH: got %+v, want 55", rh)
	}
	co2 := got[st.CO2]
	if co2.ValueInt == nil || *co2.ValueInt != 650 {
		t.Errorf("CO2: got %+v, want 650", co2)
	}
}

// Captured from a real chirpstackv4 uplink.
func TestDecodeAM103_RealPayload(t *testing.T) {
	payload, err := hex.DecodeString("0175640367c900046899077d6c01")
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}

	records := DecodeAM103(payload, "dev-1", "chirpstackv4", time.Now())

	st := record.ST
	got := map[string]record.SensorDataRecord{}
	for _, r := range records {
		got[r.SensorType] = r
	}

	if bat := got[st.BatteryLevel]; bat.ValueFloat == nil || *bat.ValueFloat != 100 {
		t.Errorf("BatteryLevel: got %+v, want 100", bat)
	}
	if temp := got[st.AirTemp]; temp.ValueFloat == nil || *temp.ValueFloat != 20.1 {
		t.Errorf("AirTemp: got %+v, want 20.1", temp)
	}
	if rh := got[st.AirRH]; rh.ValueFloat == nil || *rh.ValueFloat != 76.5 {
		t.Errorf("AirRH: got %+v, want 76.5", rh)
	}
	if co2 := got[st.CO2]; co2.ValueInt == nil || *co2.ValueInt != 364 {
		t.Errorf("CO2: got %+v, want 364", co2)
	}
}

func TestDecodeAM103_NegativeTemperature(t *testing.T) {
	// -5.2°C: -52 as int16 LE = 0xFFCC
	payload := []byte{0x03, 0x67, 0xCC, 0xFF}

	records := DecodeAM103(payload, "dev-1", "chirpstackv4", time.Now())

	if len(records) != 1 || records[0].ValueFloat == nil || *records[0].ValueFloat != -5.2 {
		t.Errorf("expected AirTemp=-5.2, got %+v", records)
	}
}
