// Reference: https://github.com/Milesight-IoT/SensorDecoders/blob/main/vs-series/vs135/vs135-decoder.js
//
// VS135 is an AI vision people-counting camera with up to 4 counting
// lines, each reporting a cumulative total (since last device reset) and
// a period count (since last uplink) for both in and out — plus a
// separate "child" sub-count on the same 4 lines (a distinct
// Milesight-computed measurement, not a replacement for the main count).
// DeviceIndex = line number, 1-indexed (same convention as
// SoilMoistureRaw's probe depths). No battery channel (PoE/wired device).
//
// Deliberately NOT decoded (registered in milesight.go's genericTypeLengths
// so tokenization stays in sync, but no case below, so nothing is written
// to sensor_data):
//   - channel 0x0F/0x1D type 0xE3 (region_1..4_count / child variant —
//     per-region arrays)
//   - channel 0x10/0x1E type 0xE4 (per-region dwell time / child variant)
//   - channel 0x50 type 0xFC (occlusion alarm — master/node mesh status,
//     out of scope for now)
package milesight

import (
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

// Per-line channel assignments. Index i (0-3) is line i+1 — DeviceIndex
// below is always this index+1, never the raw channel byte.
var (
	vs135TotalInChannels  = [4]byte{0x03, 0x06, 0x09, 0x0C}
	vs135TotalOutChannels = [4]byte{0x04, 0x07, 0x0A, 0x0D}
	vs135PeriodChannels   = [4]byte{0x05, 0x08, 0x0B, 0x0E}

	vs135ChildTotalInChannels  = [4]byte{0x11, 0x14, 0x17, 0x1A}
	vs135ChildTotalOutChannels = [4]byte{0x12, 0x15, 0x18, 0x1B}
	vs135ChildPeriodChannels   = [4]byte{0x13, 0x16, 0x19, 0x1C}
)

// lineIndex returns the 0-based line slot for ch within table, or -1.
func lineIndex(table [4]byte, ch byte) int {
	for i, c := range table {
		if c == ch {
			return i
		}
	}
	return -1
}

func decodeVS135(entries []TLV, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	const dm = "vs135"
	st := record.ST
	var out []record.SensorDataRecord

	newRec := func(sensorType string, lineIdx int, value int64) record.SensorDataRecord {
		r := record.NewInt(sensorType, dm, deviceID, provider, value, ts)
		r.DeviceIndex = lineIdx + 1
		return r
	}

	for _, e := range entries {
		switch e.Type {
		case 0xD2: // total_in or total_out: uint32 LE
			if i := lineIndex(vs135TotalInChannels, e.Channel); i >= 0 {
				out = append(out, newRec(st.PeopleLineTotalIn, i, int64(le32(e.Data))))
			} else if i := lineIndex(vs135TotalOutChannels, e.Channel); i >= 0 {
				out = append(out, newRec(st.PeopleLineTotalOut, i, int64(le32(e.Data))))
			} else if i := lineIndex(vs135ChildTotalInChannels, e.Channel); i >= 0 {
				out = append(out, newRec(st.PeopleLineTotalInChild, i, int64(le32(e.Data))))
			} else if i := lineIndex(vs135ChildTotalOutChannels, e.Channel); i >= 0 {
				out = append(out, newRec(st.PeopleLineTotalOutChild, i, int64(le32(e.Data))))
			}

		case 0xCC: // period_in(2B)+period_out(2B): two uint16 LE
			if i := lineIndex(vs135PeriodChannels, e.Channel); i >= 0 {
				out = append(out, newRec(st.PeopleLineIn, i, int64(le16(e.Data[0:2]))))
				out = append(out, newRec(st.PeopleLineOut, i, int64(le16(e.Data[2:4]))))
			} else if i := lineIndex(vs135ChildPeriodChannels, e.Channel); i >= 0 {
				out = append(out, newRec(st.PeopleLineInChild, i, int64(le16(e.Data[0:2]))))
				out = append(out, newRec(st.PeopleLineOutChild, i, int64(le16(e.Data[2:4]))))
			}
		}
	}

	return out
}

// DecodeVS135 decodes VS135 binary payloads from LoRaWAN uplinks.
func DecodeVS135(payload []byte, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	return decodeVS135(ParseMilesightTLV(payload), deviceID, provider, ts)
}
