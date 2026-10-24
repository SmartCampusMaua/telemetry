package milesight

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

func TestDecodeAM308(t *testing.T) {
	payload := []byte{
		0x01, 0x75, 0x5A, // battery: 90%
		0x03, 0x67, 0xF3, 0x00, // temperature: 243 -> 24.3°C
		0x04, 0x68, 0x78, // humidity: 120 -> 60%
		0x05, 0x00, 0x01, // pir: trigger
		0x06, 0xCB, 0x80, // illuminance_raw: 128 (uncalibrated)
		0x07, 0x7D, 0xBC, 0x02, // co2: 700 ppm
		0x08, 0x7D, 0x96, 0x00, // tvoc_index: 150 -> 1.50
		0x09, 0x73, 0x94, 0x27, // pressure: 10132 -> 1013.2 hPa
		0x0B, 0x7D, 0x0C, 0x00, // pm2_5: 12 µg/m³
		0x0C, 0x7D, 0x14, 0x00, // pm10: 20 µg/m³
	}

	records := DecodeAM308(payload, "dev-1", "chirpstackv4", time.Now())

	st := record.ST
	got := map[string]record.SensorDataRecord{}
	for _, r := range records {
		got[r.SensorType] = r
		if r.DeviceModel != "am308" {
			t.Errorf("DeviceModel = %q, want am308", r.DeviceModel)
		}
	}

	if bat := got[st.BatteryLevel]; bat.ValueFloat == nil || *bat.ValueFloat != 90 {
		t.Errorf("BatteryLevel: got %+v, want 90", bat)
	}
	if temp := got[st.AirTemp]; temp.ValueFloat == nil || *temp.ValueFloat != 24.3 {
		t.Errorf("AirTemp: got %+v, want 24.3", temp)
	}
	if rh := got[st.AirRH]; rh.ValueFloat == nil || *rh.ValueFloat != 60 {
		t.Errorf("AirRH: got %+v, want 60", rh)
	}
	if pir := got[st.PIRStatus]; pir.ValueBool == nil || !*pir.ValueBool {
		t.Errorf("PIRStatus: got %+v, want true", pir)
	}
	if light := got[st.IlluminanceRaw]; light.ValueInt == nil || *light.ValueInt != 128 {
		t.Errorf("IlluminanceRaw: got %+v, want 128", light)
	}
	if co2 := got[st.CO2]; co2.ValueInt == nil || *co2.ValueInt != 700 {
		t.Errorf("CO2: got %+v, want 700", co2)
	}
	if tvoc := got[st.TVOCIndex]; tvoc.ValueFloat == nil || *tvoc.ValueFloat != 1.5 {
		t.Errorf("TVOCIndex: got %+v, want 1.5", tvoc)
	}
	if press := got[st.AirPress]; press.ValueFloat == nil || *press.ValueFloat != 1013.2 {
		t.Errorf("AirPress: got %+v, want 1013.2", press)
	}
	if pm25 := got[st.PM2_5]; pm25.ValueInt == nil || *pm25.ValueInt != 12 {
		t.Errorf("PM2_5: got %+v, want 12", pm25)
	}
	if pm10 := got[st.PM10]; pm10.ValueInt == nil || *pm10.ValueInt != 20 {
		t.Errorf("PM10: got %+v, want 20", pm10)
	}
}

// Captured from a real chirpstackv4 uplink — exercises the IAQ-index TVOC
// variant (type 0x7D), not the µg/m³ one tested separately below.
func TestDecodeAM308_RealPayload(t *testing.T) {
	payload, err := hex.DecodeString("0367cb0004689905000006cb00077d9901087d4a0109735f240b7d02000c7d0200017564")
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}

	records := DecodeAM308(payload, "dev-1", "chirpstackv4", time.Now())

	st := record.ST
	got := map[string]record.SensorDataRecord{}
	for _, r := range records {
		got[r.SensorType] = r
	}

	if temp := got[st.AirTemp]; temp.ValueFloat == nil || *temp.ValueFloat != 20.3 {
		t.Errorf("AirTemp: got %+v, want 20.3", temp)
	}
	if rh := got[st.AirRH]; rh.ValueFloat == nil || *rh.ValueFloat != 76.5 {
		t.Errorf("AirRH: got %+v, want 76.5", rh)
	}
	if pir := got[st.PIRStatus]; pir.ValueBool == nil || *pir.ValueBool {
		t.Errorf("PIRStatus: got %+v, want false", pir)
	}
	if light := got[st.IlluminanceRaw]; light.ValueInt == nil || *light.ValueInt != 0 {
		t.Errorf("IlluminanceRaw: got %+v, want 0", light)
	}
	if co2 := got[st.CO2]; co2.ValueInt == nil || *co2.ValueInt != 409 {
		t.Errorf("CO2: got %+v, want 409", co2)
	}
	if tvoc := got[st.TVOCIndex]; tvoc.ValueFloat == nil || *tvoc.ValueFloat != 3.3 {
		t.Errorf("TVOCIndex: got %+v, want 3.3", tvoc)
	}
	if press := got[st.AirPress]; press.ValueFloat == nil || *press.ValueFloat != 931.1 {
		t.Errorf("AirPress: got %+v, want 931.1", press)
	}
	if pm25 := got[st.PM2_5]; pm25.ValueInt == nil || *pm25.ValueInt != 2 {
		t.Errorf("PM2_5: got %+v, want 2", pm25)
	}
	if pm10 := got[st.PM10]; pm10.ValueInt == nil || *pm10.ValueInt != 2 {
		t.Errorf("PM10: got %+v, want 2", pm10)
	}
	if bat := got[st.BatteryLevel]; bat.ValueFloat == nil || *bat.ValueFloat != 100 {
		t.Errorf("BatteryLevel: got %+v, want 100", bat)
	}
}

func TestDecodeAM308_PIRIdle(t *testing.T) {
	payload := []byte{0x05, 0x00, 0x00}

	records := DecodeAM308(payload, "dev-1", "chirpstackv4", time.Now())

	if len(records) != 1 || records[0].ValueBool == nil || *records[0].ValueBool {
		t.Errorf("expected PIRStatus=false (idle), got %+v", records)
	}
}

// TVOC's second wire format (channel 0x08 type 0xE6) reports µg/m³ directly,
// mutually exclusive with the IAQ-index variant (type 0x7D) tested above —
// never both on the same device.
func TestDecodeAM308_TVOCMassConcentration(t *testing.T) {
	payload := []byte{0x08, 0xE6, 0x2C, 0x01} // 300 µg/m³

	records := DecodeAM308(payload, "dev-1", "chirpstackv4", time.Now())

	if len(records) != 1 || records[0].SensorType != record.ST.TVOC {
		t.Fatalf("expected a single TVOC record, got %+v", records)
	}
	if records[0].ValueInt == nil || *records[0].ValueInt != 300 {
		t.Errorf("TVOC: got %+v, want 300", records[0])
	}
}
