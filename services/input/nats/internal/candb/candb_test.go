package candb

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

const epsilon = 1e-9

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestDecodeSignals_PE1(t *testing.T) {
	// EngineRPM=3500 rpm, ThrottlePosition=42%, Lambda1=1.000 (raw 500,
	// unsigned with offset 0.5: 500*0.001+0.5=1.0),
	// ManifoldAirPressure=101.3 kPa (raw 1013), Gear=3.
	data := []byte{0xAC, 0x0D, 42, 0xF4, 0x01, 0xF5, 0x03, 3}
	got, err := DecodeSignals(0x200, data)
	if err != nil {
		t.Fatalf("DecodeSignals: %v", err)
	}
	if !approxEqual(got["EngineRPM"], 3500) {
		t.Errorf("EngineRPM = %v, want 3500", got["EngineRPM"])
	}
	if !approxEqual(got["ThrottlePosition"], 42) {
		t.Errorf("ThrottlePosition = %v, want 42", got["ThrottlePosition"])
	}
	if !approxEqual(got["Lambda1"], 1.0) {
		t.Errorf("Lambda1 = %v, want 1.0", got["Lambda1"])
	}
	if !approxEqual(got["ManifoldAirPressure"], 101.3) {
		t.Errorf("ManifoldAirPressure = %v, want 101.3", got["ManifoldAirPressure"])
	}
	if !approxEqual(got["Gear"], 3) {
		t.Errorf("Gear = %v, want 3", got["Gear"])
	}
}

func TestDecodeSignals_UnsignedWithNegativeOffset(t *testing.T) {
	// GForceLateral = -1.234 g -> raw uint16 3766 (0x0EB6) little-endian,
	// via this DBC's unsigned+offset encoding: physical = raw*0.001 + (-5).
	// GForceLateral lives in CD4 (0x170), the central-IMU accelerometer
	// message -- see candb.go's CD4 comment for why it moved out of CD2.
	data := []byte{0xB6, 0x0E, 0, 0, 0, 0}
	got, err := DecodeSignals(0x170, data)
	if err != nil {
		t.Fatalf("DecodeSignals: %v", err)
	}
	if !approxEqual(got["GForceLateral"], -1.234) {
		t.Errorf("GForceLateral = %v, want -1.234", got["GForceLateral"])
	}
}

func TestDecodeSignals_GPS1_Signed32(t *testing.T) {
	// Latitude = -23.5505 deg, Longitude = -46.6333 deg, at 1e-7 deg/LSB,
	// signed raw, offset 0 (see candb.go's GPS1 comment for why Latitude/
	// Longitude stay signed instead of this DBC's usual unsigned+offset
	// pattern). var, not const: forces runtime evaluation below, since Go
	// disallows converting an out-of-range *constant* expression straight
	// to uint32 even though the int32->uint32 bit-reinterpretation is fine
	// at runtime.
	lat, lon := -23.5505, -46.6333
	data := make([]byte, 8)
	binary.LittleEndian.PutUint32(data[0:], uint32(int32(lat/0.0000001)))
	binary.LittleEndian.PutUint32(data[4:], uint32(int32(lon/0.0000001)))

	got, err := DecodeSignals(0x300, data)
	if err != nil {
		t.Fatalf("DecodeSignals: %v", err)
	}
	// 1e-7 deg/LSB can't round-trip a float64 division exactly; 1e-6 deg
	// (~11cm) is well within the resolution this signal claims.
	const tolerance = 1e-6
	if math.Abs(got["Latitude"]-lat) > tolerance {
		t.Errorf("Latitude = %v, want %v", got["Latitude"], lat)
	}
	if math.Abs(got["Longitude"]-lon) > tolerance {
		t.Errorf("Longitude = %v, want %v", got["Longitude"], lon)
	}
}

func TestDecodeSignals_TTUFL(t *testing.T) {
	// TireTempInnerFL=85.0 C (raw 1250), TireTempMiddleFL=92.5 C (raw 1325),
	// TireTempOuterFL=78.3 C (raw 1183), unsigned uint16 @ 0.1 C/LSB,
	// offset -40 (e.g. Inner: 1250*0.1-40=125-40=85.0).
	// TirePressureFL=1.85 bar (raw 185, unsigned uint16 @ 0.01 bar/LSB).
	data := []byte{0xE2, 0x04, 0x2D, 0x05, 0x9F, 0x04, 0xB9, 0x00}
	got, err := DecodeSignals(0x130, data)
	if err != nil {
		t.Fatalf("DecodeSignals: %v", err)
	}
	if !approxEqual(got["TireTempInnerFL"], 85.0) {
		t.Errorf("TireTempInnerFL = %v, want 85.0", got["TireTempInnerFL"])
	}
	if !approxEqual(got["TireTempMiddleFL"], 92.5) {
		t.Errorf("TireTempMiddleFL = %v, want 92.5", got["TireTempMiddleFL"])
	}
	if !approxEqual(got["TireTempOuterFL"], 78.3) {
		t.Errorf("TireTempOuterFL = %v, want 78.3", got["TireTempOuterFL"])
	}
	if !approxEqual(got["TirePressureFL"], 1.85) {
		t.Errorf("TirePressureFL = %v, want 1.85", got["TirePressureFL"])
	}
}

func TestDecodeSignals_PD1(t *testing.T) {
	// PDMBatteryVoltage=13.20V (raw 1320), PDMTotalCurrent=45.5A (raw 455),
	// PDMInternalTemp=52.3C (raw 923, offset -40: 923*0.1-40=92.3-40=52.3),
	// PDMGlobalErrorFlags=0, PDMInternalRailVoltage=9.5V (raw 95).
	data := []byte{0x28, 0x05, 0xC7, 0x01, 0x9B, 0x03, 0x00, 0x5F}
	got, err := DecodeSignals(0x400, data)
	if err != nil {
		t.Fatalf("DecodeSignals: %v", err)
	}
	if !approxEqual(got["PDMBatteryVoltage"], 13.20) {
		t.Errorf("PDMBatteryVoltage = %v, want 13.20", got["PDMBatteryVoltage"])
	}
	if !approxEqual(got["PDMTotalCurrent"], 45.5) {
		t.Errorf("PDMTotalCurrent = %v, want 45.5", got["PDMTotalCurrent"])
	}
	if !approxEqual(got["PDMInternalTemp"], 52.3) {
		t.Errorf("PDMInternalTemp = %v, want 52.3", got["PDMInternalTemp"])
	}
	if !approxEqual(got["PDMGlobalErrorFlags"], 0) {
		t.Errorf("PDMGlobalErrorFlags = %v, want 0", got["PDMGlobalErrorFlags"])
	}
	if !approxEqual(got["PDMInternalRailVoltage"], 9.5) {
		t.Errorf("PDMInternalRailVoltage = %v, want 9.5", got["PDMInternalRailVoltage"])
	}
}

func TestPDMOutputBankMessages_Structure(t *testing.T) {
	// 30 channels split into banks of 8 -> PD2/PD3/PD4 have 8 signals each,
	// PD5 (the partial final bank) has 6 -- exercises the "remaining < 8"
	// branch in pdmOutputBankMessages.
	cases := []struct {
		id       uint32
		name     string
		dlc      uint8
		nSignals int
		first    string
		last     string
	}{
		{0x401, "PD2", 8, 8, "OutputCurrent1", "OutputCurrent8"},
		{0x404, "PD5", 6, 6, "OutputCurrent25", "OutputCurrent30"},
		{0x405, "PD6", 8, 8, "OutputVoltage1", "OutputVoltage8"},
		{0x408, "PD9", 6, 6, "OutputVoltage25", "OutputVoltage30"},
	}
	for _, c := range cases {
		msg, ok := MessageByID(c.id)
		if !ok {
			t.Fatalf("MessageByID(0x%03x): not found", c.id)
		}
		if msg.Name != c.name || msg.DLC != c.dlc || len(msg.Signals) != c.nSignals {
			t.Errorf("0x%03x = %+v, want name=%s dlc=%d nSignals=%d", c.id, msg, c.name, c.dlc, c.nSignals)
			continue
		}
		if got := msg.Signals[0].Name; got != c.first {
			t.Errorf("0x%03x first signal = %q, want %q", c.id, got, c.first)
		}
		if got := msg.Signals[len(msg.Signals)-1].Name; got != c.last {
			t.Errorf("0x%03x last signal = %q, want %q", c.id, got, c.last)
		}
	}
}

func TestDecodeSignals_PD10_Bitfield(t *testing.T) {
	// OutputStatus1-30 packed into 4 bytes. Set bits 0, 7, 8, and 29 (the
	// last valid bit) to exercise both byte boundaries and the top end.
	data := make([]byte, 4)
	data[0] = 0x81 // bits 0 and 7
	data[1] = 0x01 // bit 8
	data[3] = 0x20 // bit 29 (byte 3, bit 5: 3*8+5=29)

	got, err := DecodeSignals(0x409, data)
	if err != nil {
		t.Fatalf("DecodeSignals: %v", err)
	}
	wantOn := map[string]bool{"OutputStatus1": true, "OutputStatus8": true, "OutputStatus9": true, "OutputStatus30": true}
	for i := 1; i <= 30; i++ {
		name := fmt.Sprintf("OutputStatus%d", i)
		want := 0.0
		if wantOn[name] {
			want = 1.0
		}
		if !approxEqual(got[name], want) {
			t.Errorf("%s = %v, want %v", name, got[name], want)
		}
	}
}

func TestDecodeSignals_PD11_Bitfield(t *testing.T) {
	// InputState1-16 packed into 2 bytes -- all off except InputState16
	// (byte 1, bit 7).
	data := []byte{0x00, 0x80}
	got, err := DecodeSignals(0x40A, data)
	if err != nil {
		t.Fatalf("DecodeSignals: %v", err)
	}
	if !approxEqual(got["InputState16"], 1) {
		t.Errorf("InputState16 = %v, want 1", got["InputState16"])
	}
	if !approxEqual(got["InputState1"], 0) {
		t.Errorf("InputState1 = %v, want 0", got["InputState1"])
	}
}

func TestDecodeSignals_UnknownID(t *testing.T) {
	_, err := DecodeSignals(0x999, []byte{0, 0, 0, 0, 0, 0, 0, 0})
	if err == nil {
		t.Fatal("expected ErrUnknownMessage, got nil")
	}
}

func TestDecodeSignals_ShortPayload(t *testing.T) {
	if _, err := DecodeSignals(0x100, []byte{1, 2}); err == nil {
		t.Fatal("expected error for short payload, got nil")
	}
}

func TestMessages_MatchesDBCCount(t *testing.T) {
	// Cross-check against docs/architecture/zc2x-can2.dbc, independently
	// validated with cantools: 52 messages, 284 signals. Catches drift
	// between the DBC and this Go mirror (see candb.go's package comment:
	// "keep both in sync by hand").
	if len(messages) != 52 {
		t.Errorf("len(messages) = %d, want 52", len(messages))
	}
	total := 0
	for _, m := range messages {
		total += len(m.Signals)
	}
	if total != 284 {
		t.Errorf("total signals = %d, want 284", total)
	}
}

func TestSignal_ValueKind(t *testing.T) {
	// One example per kind, plus the count breakdown across the whole
	// database (49 bool: 46 packed-Bit PD10/PD11 signals + 3 single-byte
	// 0/1 flags in EC10; 67 int; 168 float; 49+67+168=284) as a tripwire
	// against a future signal accidentally misclassifying.
	cases := []struct {
		msgID uint32
		sig   string
		want  ValueKind
	}{
		{0x409, "OutputStatus1", KindBool},               // packed Bit, min=0 max=1
		{0x449, "EngineRunning", KindBool},               // Uint8 but min=0 max=1
		{0x200, "EngineRPM", KindInt},                    // scale=1, not a 0/1 flag
		{0x220, "ExhaustCylinderTemperature1", KindInt},  // scale=1
		{0x200, "Lambda1", KindFloat},                    // scale=0.001
		{0x448, "IgnitionKnockTrimCylinder1", KindFloat}, // scale=0.2
	}
	for _, c := range cases {
		msg, ok := MessageByID(c.msgID)
		if !ok {
			t.Fatalf("MessageByID(0x%03x): not found", c.msgID)
		}
		var found bool
		for _, sig := range msg.Signals {
			if sig.Name != c.sig {
				continue
			}
			found = true
			if got := sig.ValueKind(); got != c.want {
				t.Errorf("%s.ValueKind() = %v, want %v", c.sig, got, c.want)
			}
		}
		if !found {
			t.Fatalf("signal %s not found in message 0x%03x", c.sig, c.msgID)
		}
	}

	counts := map[ValueKind]int{}
	for _, m := range messages {
		for _, sig := range m.Signals {
			counts[sig.ValueKind()]++
		}
	}
	if counts[KindBool] != 49 {
		t.Errorf("bool count = %d, want 49", counts[KindBool])
	}
	if counts[KindInt] != 67 {
		t.Errorf("int count = %d, want 67", counts[KindInt])
	}
	if counts[KindFloat] != 168 {
		t.Errorf("float count = %d, want 168", counts[KindFloat])
	}
}
