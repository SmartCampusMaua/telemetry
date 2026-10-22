// Package candb defines the ZC2X CAN2 signal database: the message/signal
// layout MoTeC M1 Tune (or the ecu/ firmware simulator) transmits on CAN2.
// This table mirrors docs/architecture/zc2x-can2.dbc byte-for-byte — keep
// both in sync by hand when either changes.
package candb

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// ValueType is the wire encoding of a signal's raw bytes.
type ValueType int

const (
	Uint8 ValueType = iota
	Int16
	Uint16
	Uint32
	Int32
	// Bit is a single-bit flag (PDM output/input status arrays — see
	// docs/architecture/zc2x-can2.dbc's BO_ 1033/1034 comments). Unlike
	// every other type here, a Bit signal uses BitOffset, not ByteOffset —
	// it isn't byte-aligned, so it needs its own decode path.
	Bit
)

// Signal describes one named value packed into a CAN message payload.
// Physical value = raw*Scale + Bias. Every 8/16-bit signal in this DBC is
// encoded unsigned with Bias set to the signal's own physical minimum (so
// raw=0 decodes to exactly that minimum) — see
// docs/architecture/zc2x-can2.dbc's BO_ 256 comment for why: one consistent
// decode path, no per-signal signed/unsigned branching. Exception: the
// three 32-bit signals (Latitude, Longitude, FuelUsedRaw) are Int32, not
// Uint32 — MoTeC's Dash Manager CAN import doesn't support unsigned 32-bit
// signals (confirmed in practice), a constraint specific to 32-bit width.
type Signal struct {
	Name       string
	ByteOffset int
	// BitOffset is used only when Type == Bit: the bit's 0-based position
	// within the payload (bit 0 = payload[0]'s LSB), matching DBC's own
	// little-endian bit-numbering for @1 signals. Unused (0) otherwise.
	BitOffset int
	Type      ValueType
	Scale     float64
	Bias      float64
	Unit      string
	// Min/Max are the signal's declared physical range, copied from this
	// DBC's SG_ line ([min|max]) — NOT derived from the raw wire domain
	// (raw*Scale+Bias over the type's full bit width). Many signals use only
	// part of their raw domain (e.g. Lambda1's 0.5-1.5 physical range over a
	// full 16-bit field), so this has to be carried explicitly rather than
	// computed from Type/Scale/Bias alone. Used by cmd/simulator to generate
	// physically plausible synthetic values instead of arbitrary ones that
	// merely decode without error.
	Min, Max float64
}

// ValueKind classifies a signal's decoded value for typed-column storage
// (see services/output/influxdb3, which writes exactly one of
// value_float/value_int/value_bool per point based on this).
type ValueKind int

const (
	KindFloat ValueKind = iota
	KindInt
	KindBool
)

func (k ValueKind) String() string {
	switch k {
	case KindBool:
		return "bool"
	case KindInt:
		return "int"
	default:
		return "float"
	}
}

// ValueKind classifies s's decoded physical value as boolean, integer, or
// floating-point, derived from Min/Max/Scale rather than a separately
// maintained field -- one less thing to keep in sync by hand when adding a
// signal. A signal whose declared range is exactly [0,1] is a boolean flag
// (covers both packed Bit signals like OutputStatus/InputState and
// single-byte 0/1 signals like EngineRunning/IgnitionCutRequest, which use
// Uint8 rather than Bit but are semantically the same kind of value).
// Otherwise, Scale == 1 means every representable raw value decodes to a
// whole number (raw*1 + Bias, and Bias is always a whole number wherever
// Scale is 1 in this DBC) -- e.g. EngineRPM, Gear, ExhaustCylinderTemperature*
// (deliberately 1 C/LSB, see the DBC's BO_ 544 comment). Any other scale
// (0.1, 0.001, 0.01, 0.2, 1e-7, ...) means fractional precision is
// meaningful, so it's a float -- e.g. Lambda1, GForce*, GPSHeading. Verified
// against all 284 signals in this database (see candb_test.go).
func (s Signal) ValueKind() ValueKind {
	if s.Min == 0 && s.Max == 1 {
		return KindBool
	}
	if s.Scale == 1 {
		return KindInt
	}
	return KindFloat
}

// Message describes one CAN message: its ID, DLC, and ordered signals.
type Message struct {
	ID      uint32
	Name    string
	DLC     uint8
	Signals []Signal
}

// ErrUnknownMessage is returned by DecodeSignals when canID has no entry in
// this database. This is expected, not exceptional: OBU/RSU forward every
// CAN ID unfiltered (platform/esp-idf/firmware/obu/main/main.c can_init()),
// including IDs with no signal definition (yet).
var ErrUnknownMessage = errors.New("candb: unknown CAN message id")

// Names follow MoTeC's generic dash/logger CAN convention: CD# = Chassis
// Dynamics, PE# = Powertrain/Engine, numbered per message rather than named
// after their exact field mix.
var messages = buildMessages()

func buildMessages() []Message {
	msgs := append([]Message{}, baseMessages...)
	msgs = append(msgs, pdmOutputBankMessages(0x401, 2, "OutputCurrent", "A", 0.1, 0, 25.5, 30)...)
	msgs = append(msgs, pdmOutputBankMessages(0x405, 6, "OutputVoltage", "V", 0.1, 0, 25.5, 30)...)
	msgs = append(msgs, pdmBitfieldMessage(0x409, "PD10", "OutputStatus", 30))
	msgs = append(msgs, pdmBitfieldMessage(0x40A, "PD11", "InputState", 16))
	msgs = append(msgs, pdmInputVoltageBankMessages(0x40B, 12, "InputVoltage", 16, 0, 20)...)
	msgs = append(msgs, pdmOutputBankMessages(0x40F, 16, "OutputLoad", "%", 1, 0, 100, 30)...)
	msgs = append(msgs, ecPerCylinderMessage(0x447, "EC8", "KnockLevelCylinder", "%", 1, 0, 0, 100))
	msgs = append(msgs, ecPerCylinderMessage(0x448, "EC9", "IgnitionKnockTrimCylinder", "deg", 0.2, -20, -20, 31))
	return msgs
}

// ecPerCylinderMessage builds an 8-channel, 1-byte-per-channel message
// (KnockLevelCylinder1-8 or IgnitionKnockTrimCylinder1-8) -- see
// docs/architecture/zc2x-can2.dbc's CM_ BU_ M150 comment for why 8
// cylinders (the max the source document enumerates). Generated for the
// same reason as the PDM bank messages above: repetitive, indexed data.
func ecPerCylinderMessage(id uint32, msgName, sigNamePrefix, unit string, scale, bias, min, max float64) Message {
	sigs := make([]Signal, 8)
	for i := 0; i < 8; i++ {
		sigs[i] = Signal{Name: fmt.Sprintf("%s%d", sigNamePrefix, i+1), ByteOffset: i, Type: Uint8, Scale: scale, Bias: bias, Unit: unit, Min: min, Max: max}
	}
	return Message{ID: id, Name: msgName, DLC: 8, Signals: sigs}
}

// pdmOutputBankMessages splits n indexed 8-bit unsigned channels (PDM
// output current, voltage, or load) into ceil(n/8) messages of up to 8
// channels each (1 byte/channel), named "PD"+startMsgNum, startMsgNum+1,
// ... and signals sigNamePrefix+"1"..sigNamePrefix+"N". 8-bit deliberately
// mirrors MoTeC's own real PDM DBC field width for these channels
// (confirmed via a genuine PDM15 CAN-import screenshot) -- see
// docs/architecture/zc2x-can2.dbc's BO_ 1025 comment. Generated rather
// than hand-typed: 30 near-identical channels x 3 (current/voltage/load)
// is genuinely repetitive, indexed data, same reasoning as
// ecu/main/main.c's encode_pd_bank_u8.
func pdmOutputBankMessages(startID uint32, startMsgNum int, sigNamePrefix, unit string, scale, min, max float64, n int) []Message {
	var msgs []Message
	ch := 1
	for msgNum := startMsgNum; ch <= n; msgNum++ {
		count := 8
		if remaining := n - ch + 1; remaining < 8 {
			count = remaining
		}
		sigs := make([]Signal, count)
		for j := 0; j < count; j++ {
			sigs[j] = Signal{Name: fmt.Sprintf("%s%d", sigNamePrefix, ch), ByteOffset: j, Type: Uint8, Scale: scale, Unit: unit, Min: min, Max: max}
			ch++
		}
		msgs = append(msgs, Message{ID: startID, Name: fmt.Sprintf("PD%d", msgNum), DLC: uint8(count), Signals: sigs})
		startID++
	}
	return msgs
}

// pdmInputVoltageBankMessages splits n indexed 16-bit unsigned 0.01V
// channels into ceil(n/4) messages of up to 4 channels each (2
// bytes/channel). Finer resolution than pdmOutputBankMessages since these
// are diagnostic sensor-supply readings, not switched power channels --
// see docs/architecture/zc2x-can2.dbc's BO_ 1035 comment.
func pdmInputVoltageBankMessages(startID uint32, startMsgNum int, sigNamePrefix string, n int, min, max float64) []Message {
	var msgs []Message
	ch := 1
	for msgNum := startMsgNum; ch <= n; msgNum++ {
		count := 4
		if remaining := n - ch + 1; remaining < 4 {
			count = remaining
		}
		sigs := make([]Signal, count)
		for j := 0; j < count; j++ {
			sigs[j] = Signal{Name: fmt.Sprintf("%s%d", sigNamePrefix, ch), ByteOffset: j * 2, Type: Uint16, Scale: 0.01, Unit: "V", Min: min, Max: max}
			ch++
		}
		msgs = append(msgs, Message{ID: startID, Name: fmt.Sprintf("PD%d", msgNum), DLC: uint8(count * 2), Signals: sigs})
		startID++
	}
	return msgs
}

// pdmBitfieldMessage packs n indexed 1-bit flags into a single message,
// sigNamePrefix+"1"..sigNamePrefix+"N", bit i (0-based) at BitOffset i --
// matches MoTeC's own real PDM DBC convention of packing many 1-bit flags
// into one frame (see docs/architecture/zc2x-can2.dbc's BO_ 1033/1034
// comments) rather than one message per flag.
func pdmBitfieldMessage(id uint32, msgName, sigNamePrefix string, n int) Message {
	sigs := make([]Signal, n)
	for i := 0; i < n; i++ {
		sigs[i] = Signal{Name: fmt.Sprintf("%s%d", sigNamePrefix, i+1), BitOffset: i, Type: Bit, Scale: 1, Min: 0, Max: 1}
	}
	return Message{ID: id, Name: msgName, DLC: uint8((n + 7) / 8), Signals: sigs}
}

// baseMessages holds every message whose signals are distinct enough to be
// worth naming individually rather than generating (contrast
// pdmOutputBankMessages etc. above, for the genuinely repetitive PDM
// channel banks).
var baseMessages = []Message{
	{ID: 0x100, Name: "CD1", DLC: 8, Signals: []Signal{
		{Name: "WheelSpeedFL", ByteOffset: 0, Type: Uint16, Scale: 0.1, Unit: "km/h", Min: 0, Max: 300},
		{Name: "WheelSpeedFR", ByteOffset: 2, Type: Uint16, Scale: 0.1, Unit: "km/h", Min: 0, Max: 300},
		{Name: "WheelSpeedRL", ByteOffset: 4, Type: Uint16, Scale: 0.1, Unit: "km/h", Min: 0, Max: 300},
		{Name: "WheelSpeedRR", ByteOffset: 6, Type: Uint16, Scale: 0.1, Unit: "km/h", Min: 0, Max: 300},
	}},
	{ID: 0x110, Name: "CD2", DLC: 4, Signals: []Signal{
		{Name: "SteeringAngle", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -720, Unit: "deg", Min: -720, Max: 720},
		{Name: "GroundSpeed", ByteOffset: 2, Type: Uint16, Scale: 0.1, Unit: "km/h", Min: 0, Max: 300},
	}},
	{ID: 0x120, Name: "CD3", DLC: 4, Signals: []Signal{
		{Name: "BrakePressureFront", ByteOffset: 0, Type: Uint16, Scale: 0.1, Unit: "bar", Min: 0, Max: 200},
		{Name: "BrakePressureRear", ByteOffset: 2, Type: Uint16, Scale: 0.1, Unit: "bar", Min: 0, Max: 200},
	}},
	// CD4/CD7: the car's central chassis IMU, split one message per
	// physical sensor -- CD4 is the 3-axis linear accelerometer (lateral/
	// longitudinal/vertical; lateral/longitudinal used to live in CD2
	// before CD4 existed, moved here to avoid splitting one sensor's 3
	// axes across two unrelated messages), CD7 is the 3-axis gyroscope
	// (yaw/roll/pitch rate). Range +/-163 deg/s / +/-5g matches the Bosch
	// Motorsport MM7.10, a real combined 6-DOF automotive-motorsport IMU.
	// CD5 is this same IMU's AHRS-derived orientation (roll/pitch angle).
	// Deliberately no magnetometer/heading signal anywhere in this DBC --
	// see docs/architecture/zc2x-can2.dbc's BO_ 368 comment for why
	// (magnetic interference in-car); GPS2's GPSHeading covers that need.
	{ID: 0x170, Name: "CD4", DLC: 6, Signals: []Signal{
		{Name: "GForceLateral", ByteOffset: 0, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
		{Name: "GForceLongitudinal", ByteOffset: 2, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
		{Name: "GForceVertical", ByteOffset: 4, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
	}},
	{ID: 0x171, Name: "CD5", DLC: 4, Signals: []Signal{
		{Name: "RollAngle", ByteOffset: 0, Type: Uint16, Scale: 0.01, Bias: -45, Unit: "deg", Min: -45, Max: 45},
		{Name: "PitchAngle", ByteOffset: 2, Type: Uint16, Scale: 0.01, Bias: -45, Unit: "deg", Min: -45, Max: 45},
	}},
	// CD6: damper (suspension) position, all 4 corners -- ECU-aggregated
	// (contrast TTUFL etc below), since damper pots are typically wired
	// directly to the ECU's own analog inputs, not a separate CAN node.
	{ID: 0x172, Name: "CD6", DLC: 8, Signals: []Signal{
		{Name: "DamperPositionFL", ByteOffset: 0, Type: Uint16, Scale: 0.01, Unit: "mm", Min: 0, Max: 150},
		{Name: "DamperPositionFR", ByteOffset: 2, Type: Uint16, Scale: 0.01, Unit: "mm", Min: 0, Max: 150},
		{Name: "DamperPositionRL", ByteOffset: 4, Type: Uint16, Scale: 0.01, Unit: "mm", Min: 0, Max: 150},
		{Name: "DamperPositionRR", ByteOffset: 6, Type: Uint16, Scale: 0.01, Unit: "mm", Min: 0, Max: 150},
	}},
	{ID: 0x173, Name: "CD7", DLC: 6, Signals: []Signal{
		{Name: "YawRate", ByteOffset: 0, Type: Uint16, Scale: 0.01, Bias: -163, Unit: "deg/s", Min: -163, Max: 163},
		{Name: "RollRate", ByteOffset: 2, Type: Uint16, Scale: 0.01, Bias: -163, Unit: "deg/s", Min: -163, Max: 163},
		{Name: "PitchRate", ByteOffset: 4, Type: Uint16, Scale: 0.01, Bias: -163, Unit: "deg/s", Min: -163, Max: 163},
	}},
	// TTUFL/TTUFR/TTURL/TTURR: tire surface temperature, one message per
	// corner, 3-point IR array (inner/middle/outer across the tread), plus
	// tire pressure from the same physical unit (combined IR-temp +
	// pressure "smart valve" TPMS sensors are a real motorsport product
	// category) — see the DBC's BO_ 304 comment for the MLX90614 sensor and
	// why the temperature encoding (unsigned, 0.1 C/LSB, offset -40 = this
	// message's physical minimum) is standardized to match every other
	// temperature signal here rather than the sensor's own native 0.02
	// C/LSB register step.
	{ID: 0x130, Name: "TTUFL", DLC: 8, Signals: []Signal{
		{Name: "TireTempInnerFL", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempMiddleFL", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempOuterFL", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TirePressureFL", ByteOffset: 6, Type: Uint16, Scale: 0.01, Unit: "bar", Min: 0, Max: 4},
	}},
	{ID: 0x140, Name: "TTUFR", DLC: 8, Signals: []Signal{
		{Name: "TireTempInnerFR", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempMiddleFR", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempOuterFR", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TirePressureFR", ByteOffset: 6, Type: Uint16, Scale: 0.01, Unit: "bar", Min: 0, Max: 4},
	}},
	{ID: 0x150, Name: "TTURL", DLC: 8, Signals: []Signal{
		{Name: "TireTempInnerRL", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempMiddleRL", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempOuterRL", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TirePressureRL", ByteOffset: 6, Type: Uint16, Scale: 0.01, Unit: "bar", Min: 0, Max: 4},
	}},
	{ID: 0x160, Name: "TTURR", DLC: 8, Signals: []Signal{
		{Name: "TireTempInnerRR", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempMiddleRR", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TireTempOuterRR", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "TirePressureRR", ByteOffset: 6, Type: Uint16, Scale: 0.01, Unit: "bar", Min: 0, Max: 4},
	}},
	{ID: 0x200, Name: "PE1", DLC: 8, Signals: []Signal{
		{Name: "EngineRPM", ByteOffset: 0, Type: Uint16, Scale: 1, Unit: "rpm", Min: 0, Max: 16000},
		{Name: "ThrottlePosition", ByteOffset: 2, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "Lambda1", ByteOffset: 3, Type: Uint16, Scale: 0.001, Bias: 0.5, Unit: "", Min: 0.5, Max: 1.5},
		{Name: "ManifoldAirPressure", ByteOffset: 5, Type: Uint16, Scale: 0.1, Unit: "kPa", Min: 0, Max: 300},
		{Name: "Gear", ByteOffset: 7, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 255},
	}},
	{ID: 0x210, Name: "PE2", DLC: 8, Signals: []Signal{
		{Name: "EngineCoolantTemperature", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "EngineOilTemperature", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 170},
		{Name: "ManifoldAirTemperature", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 120},
		{Name: "EngineOilPressure", ByteOffset: 6, Type: Uint16, Scale: 0.1, Unit: "bar", Min: 0, Max: 10},
	}},
	{ID: 0x211, Name: "PE3", DLC: 6, Signals: []Signal{
		{Name: "FuelLinePressure", ByteOffset: 0, Type: Uint16, Scale: 0.1, Unit: "bar", Min: 0, Max: 10},
		// Signed (Int32), not this DBC's usual unsigned-with-min-offset
		// pattern: MoTeC's Dash Manager doesn't support unsigned 32-bit
		// signals (confirmed in practice), so the DBC declares this
		// [0|2147483647] instead of the full uint32 span — see the DBC's
		// BO_ 529 comment.
		{Name: "FuelUsedRaw", ByteOffset: 2, Type: Int32, Scale: 1, Unit: "", Min: 0, Max: 2147483647},
	}},
	// Split across PE4/PE5 (4 cylinders each): 16-bit/cylinder, matching this
	// DBC's other temperature signals, caps a single classic-CAN 8-byte frame
	// at 4 cylinders -- unlike EC8/EC9's knock channels, which are 8-bit/
	// cylinder and so fit all 8 in one message. See the DBC's BO_ 544 comment.
	{ID: 0x220, Name: "PE4", DLC: 8, Signals: []Signal{
		{Name: "ExhaustCylinderTemperature1", ByteOffset: 0, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
		{Name: "ExhaustCylinderTemperature2", ByteOffset: 2, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
		{Name: "ExhaustCylinderTemperature3", ByteOffset: 4, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
		{Name: "ExhaustCylinderTemperature4", ByteOffset: 6, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
	}},
	{ID: 0x221, Name: "PE5", DLC: 8, Signals: []Signal{
		{Name: "ExhaustCylinderTemperature5", ByteOffset: 0, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
		{Name: "ExhaustCylinderTemperature6", ByteOffset: 2, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
		{Name: "ExhaustCylinderTemperature7", ByteOffset: 4, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
		{Name: "ExhaustCylinderTemperature8", ByteOffset: 6, Type: Uint16, Scale: 1, Unit: "C", Min: 0, Max: 1200},
	}},
	// Latitude/Longitude are signed (Int32), offset 0 -- not this DBC's
	// usual unsigned-with-min-offset pattern: MoTeC's Dash Manager doesn't
	// support unsigned 32-bit signals, and signed-at-zero is the natural
	// fit anyway since both are already zero-centered on their true
	// physical reference (equator/prime meridian). See the DBC's BO_ 768
	// comment.
	{ID: 0x300, Name: "GPS1", DLC: 8, Signals: []Signal{
		{Name: "Latitude", ByteOffset: 0, Type: Int32, Scale: 0.0000001, Unit: "deg", Min: -90, Max: 90},
		{Name: "Longitude", ByteOffset: 4, Type: Int32, Scale: 0.0000001, Unit: "deg", Min: -180, Max: 180},
	}},
	// GPSHeading is this DBC's only heading/orientation-about-vertical-axis
	// source -- no magnetometer is used anywhere here (see CD4's comment):
	// magnetic interference from the car's own structure/motors makes an
	// in-car compass unreliable, while GPS course over ground is accurate
	// whenever the car is moving at any real speed.
	{ID: 0x310, Name: "GPS2", DLC: 6, Signals: []Signal{
		{Name: "Altitude", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -1000, Unit: "m", Min: -1000, Max: 5553.5},
		{Name: "GPSSpeed", ByteOffset: 2, Type: Uint16, Scale: 0.1, Unit: "km/h", Min: 0, Max: 300},
		{Name: "GPSHeading", ByteOffset: 4, Type: Uint16, Scale: 0.01, Unit: "deg", Min: 0, Max: 360},
	}},
	// PDM30/M150/C125/L180 custom re-aggregation -- see
	// docs/architecture/zc2x-can2.dbc's CM_ BU_ comments for each device
	// for why these are a custom layout, not a mirror of any device's
	// native CAN broadcast. PD2-PD19 (output/input current/voltage/load/
	// status banks) are generated by buildMessages() above, not listed here.
	{ID: 0x400, Name: "PD1", DLC: 8, Signals: []Signal{
		{Name: "PDMBatteryVoltage", ByteOffset: 0, Type: Uint16, Scale: 0.01, Unit: "V", Min: 0, Max: 20},
		{Name: "PDMTotalCurrent", ByteOffset: 2, Type: Uint16, Scale: 0.1, Unit: "A", Min: 0, Max: 400},
		{Name: "PDMInternalTemp", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "PDMGlobalErrorFlags", ByteOffset: 6, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 255},
		// Confirmed real channel: MoTeC's own PDM15 DBC exposes this as
		// "IntRailVoltage" -- the PDM's internal logic supply, distinct
		// from PDMBatteryVoltage (the external supply).
		{Name: "PDMInternalRailVoltage", ByteOffset: 7, Type: Uint8, Scale: 0.1, Unit: "V", Min: 0, Max: 25.5},
	}},
	{ID: 0x440, Name: "EC1", DLC: 8, Signals: []Signal{
		{Name: "Lambda2", ByteOffset: 0, Type: Uint16, Scale: 0.001, Bias: 0.5, Unit: "", Min: 0.5, Max: 1.5},
		{Name: "IgnitionTiming", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -10, Unit: "deg", Min: -10, Max: 60},
		{Name: "ECUBatteryVoltage", ByteOffset: 4, Type: Uint16, Scale: 0.01, Unit: "V", Min: 0, Max: 20},
		{Name: "BarometricPressure", ByteOffset: 6, Type: Uint16, Scale: 0.1, Bias: 80, Unit: "kPa", Min: 80, Max: 110},
	}},
	{ID: 0x441, Name: "EC2", DLC: 6, Signals: []Signal{
		{Name: "KnockLevel", ByteOffset: 0, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "InjectorDutyCycle", ByteOffset: 1, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "CamAngleIntake", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
		{Name: "CamAngleExhaust", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
	}},
	// EC3-EC12: real M1-series channel list beyond EC1/EC2's simple
	// aggregates -- see docs/architecture/zc2x-can2.dbc's CM_ BU_ M150
	// comment for where this list comes from (AiM InfoTech's MoTeC M1
	// integration guide, which lists M150 as a supported model). EC8/EC9
	// (per-cylinder) are generated by buildMessages() above, not listed
	// here.
	{ID: 0x442, Name: "EC3", DLC: 8, Signals: []Signal{
		{Name: "GearboxTemperature", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "IntakeAirTemperature", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "AirTemperature", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "AmbientAirTemperature", ByteOffset: 6, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
	}},
	{ID: 0x443, Name: "EC4", DLC: 8, Signals: []Signal{
		{Name: "FuelTemperature", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "CoolantPressure", ByteOffset: 2, Type: Uint16, Scale: 0.1, Unit: "bar", Min: 0, Max: 10},
		{Name: "SteeringPressure", ByteOffset: 4, Type: Uint16, Scale: 0.1, Unit: "bar", Min: 0, Max: 50},
		{Name: "FuelInjectionTime", ByteOffset: 6, Type: Uint16, Scale: 0.01, Unit: "ms", Min: 0, Max: 20},
	}},
	{ID: 0x444, Name: "EC5", DLC: 7, Signals: []Signal{
		{Name: "BoostPressureTarget", ByteOffset: 0, Type: Uint16, Scale: 0.1, Unit: "kPa", Min: 0, Max: 400},
		{Name: "BoostPressure", ByteOffset: 2, Type: Uint16, Scale: 0.1, Unit: "kPa", Min: 0, Max: 400},
		{Name: "EngineLoadAverage", ByteOffset: 4, Type: Uint16, Scale: 0.1, Unit: "%", Min: 0, Max: 200},
		{Name: "FuelComposition", ByteOffset: 6, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
	}},
	{ID: 0x445, Name: "EC6", DLC: 8, Signals: []Signal{
		{Name: "CamIntakeBank1Position", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
		{Name: "CamIntakeBank2Position", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
		{Name: "CamExhaustBank1Position", ByteOffset: 4, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
		{Name: "CamExhaustBank2Position", ByteOffset: 6, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
	}},
	{ID: 0x446, Name: "EC7", DLC: 8, Signals: []Signal{
		{Name: "CamIntakeTarget", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
		{Name: "CamExhaustTarget", ByteOffset: 2, Type: Uint16, Scale: 0.1, Bias: -30, Unit: "deg", Min: -30, Max: 30},
		{Name: "CamIntakeBank1DutyCycle", ByteOffset: 4, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "CamIntakeBank2DutyCycle", ByteOffset: 5, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "CamExhaustBank1DutyCycle", ByteOffset: 6, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "CamExhaustBank2DutyCycle", ByteOffset: 7, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
	}},
	{ID: 0x449, Name: "EC10", DLC: 7, Signals: []Signal{
		{Name: "FuelOutputLevel", ByteOffset: 0, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "IgnitionOutputLevel", ByteOffset: 1, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "EngineRunning", ByteOffset: 2, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 1},
		{Name: "IgnitionCutRequest", ByteOffset: 3, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 1},
		{Name: "LaunchControlState", ByteOffset: 4, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 10},
		{Name: "AntiLagState", ByteOffset: 5, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 1},
		{Name: "GearLeverPosition", ByteOffset: 6, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 255},
	}},
	// EngineRunTimeTotal is signed (Int32), not this DBC's usual
	// unsigned-with-min-offset pattern -- same 32-bit MoTeC Dash Manager
	// constraint as FuelUsedRaw, see the DBC's BO_ 529 comment.
	{ID: 0x44A, Name: "EC11", DLC: 6, Signals: []Signal{
		{Name: "FuelLevel", ByteOffset: 0, Type: Uint8, Scale: 1, Unit: "%", Min: 0, Max: 100},
		{Name: "IgnitionTimeStage", ByteOffset: 1, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 255},
		{Name: "EngineRunTimeTotal", ByteOffset: 2, Type: Int32, Scale: 1, Unit: "s", Min: 0, Max: 2147483647},
	}},
	{ID: 0x44B, Name: "EC12", DLC: 2, Signals: []Signal{
		{Name: "WarningFlag1", ByteOffset: 0, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 255},
		{Name: "WarningFlag2", ByteOffset: 1, Type: Uint8, Scale: 1, Unit: "", Min: 0, Max: 255},
	}},
	{ID: 0x460, Name: "DA1", DLC: 6, Signals: []Signal{
		{Name: "DashGForceLateral", ByteOffset: 0, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
		{Name: "DashGForceLongitudinal", ByteOffset: 2, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
		{Name: "DashGForceVertical", ByteOffset: 4, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
	}},
	{ID: 0x461, Name: "DA2", DLC: 6, Signals: []Signal{
		{Name: "DashTemperature", ByteOffset: 0, Type: Uint16, Scale: 0.1, Bias: -40, Unit: "C", Min: -40, Max: 150},
		{Name: "DashSensorSupplyVoltage", ByteOffset: 2, Type: Uint16, Scale: 0.01, Unit: "V", Min: 0, Max: 10},
		{Name: "DashBatteryVoltage", ByteOffset: 4, Type: Uint16, Scale: 0.01, Unit: "V", Min: 0, Max: 20},
	}},
	{ID: 0x470, Name: "LG1", DLC: 6, Signals: []Signal{
		{Name: "LoggerGForceLateral", ByteOffset: 0, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
		{Name: "LoggerGForceLongitudinal", ByteOffset: 2, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
		{Name: "LoggerGForceVertical", ByteOffset: 4, Type: Uint16, Scale: 0.001, Bias: -5, Unit: "g", Min: -5, Max: 5},
	}},
}

var messagesByID = func() map[uint32]Message {
	m := make(map[uint32]Message, len(messages))
	for _, msg := range messages {
		m[msg.ID] = msg
	}
	return m
}()

// MessageByID looks up a message definition by CAN ID.
func MessageByID(canID uint32) (Message, bool) {
	msg, ok := messagesByID[canID]
	return msg, ok
}

// Messages returns every defined CAN message, in declaration order. Intended
// for tooling that needs to enumerate the whole database (e.g. the
// NATS-direct simulator in cmd/simulator), rather than look up one message
// by ID.
func Messages() []Message {
	return messages
}

// DecodeSignals decodes data (a CAN frame payload, up to 8 bytes) into named
// engineering-unit values, per the message definition for canID. Returns
// ErrUnknownMessage if canID isn't in this database (expected for CAN IDs
// with no ZC2X signal definition — callers should treat that as "skip",
// not a real error).
func DecodeSignals(canID uint32, data []byte) (map[string]float64, error) {
	msg, ok := messagesByID[canID]
	if !ok {
		return nil, fmt.Errorf("%w: 0x%03x", ErrUnknownMessage, canID)
	}
	if len(data) < int(msg.DLC) {
		return nil, fmt.Errorf("candb: message %s (0x%03x) needs %d bytes, got %d",
			msg.Name, canID, msg.DLC, len(data))
	}

	out := make(map[string]float64, len(msg.Signals))
	for _, sig := range msg.Signals {
		raw, err := sig.decodeRaw(data)
		if err != nil {
			return nil, fmt.Errorf("candb: signal %s: %w", sig.Name, err)
		}
		out[sig.Name] = raw*sig.Scale + sig.Bias
	}
	return out, nil
}

func (s Signal) decodeRaw(data []byte) (float64, error) {
	if s.Type == Bit {
		byteIdx, bitIdx := s.BitOffset/8, uint(s.BitOffset%8)
		if byteIdx >= len(data) {
			return 0, fmt.Errorf("bit offset %d exceeds payload length %d", s.BitOffset, len(data))
		}
		if data[byteIdx]&(1<<bitIdx) != 0 {
			return 1, nil
		}
		return 0, nil
	}

	end := s.ByteOffset + s.byteLen()
	if end > len(data) {
		return 0, fmt.Errorf("byte offset %d+%d exceeds payload length %d", s.ByteOffset, s.byteLen(), len(data))
	}
	switch s.Type {
	case Uint8:
		return float64(data[s.ByteOffset]), nil
	case Int16:
		return float64(int16(binary.LittleEndian.Uint16(data[s.ByteOffset:]))), nil
	case Uint16:
		return float64(binary.LittleEndian.Uint16(data[s.ByteOffset:])), nil
	case Uint32:
		return float64(binary.LittleEndian.Uint32(data[s.ByteOffset:])), nil
	case Int32:
		return float64(int32(binary.LittleEndian.Uint32(data[s.ByteOffset:]))), nil
	default:
		return 0, fmt.Errorf("unknown value type %d", s.Type)
	}
}

func (s Signal) byteLen() int {
	switch s.Type {
	case Uint8:
		return 1
	case Int16, Uint16:
		return 2
	case Uint32, Int32:
		return 4
	default:
		return 0
	}
}
