// Reference: https://github.com/Milesight-IoT/SensorDecoders/blob/main/vs-series/vs121/vs121-decoder.js
//
// VS121 is an AI vision people-counting camera with a single counting
// line (unlike VS135's four — see vs135.go) — no DeviceIndex needed here.
// No battery channel (PoE/wired device).
//
// Deliberately NOT decoded (registered in milesight.go's genericTypeLengths
// so tokenization stays in sync, but no case below, so nothing is written
// to sensor_data):
//   - channel 0x04 type 0xC9 bytes 1-3 (region_count + 16-region bitmask)
//   - channel 0x07/0x08 type 0xD5 (region_1..16_count — per-region arrays)
//   - channel 0x09-0x0C type 0xDA (A-to-A..D flow matrix)
//   - channel 0x0E type 0xE4 (per-region dwell time)
//   - channel 0x0F type 0x85 (device-side timestamp — redundant with
//     record.Timestamp, already set from the LNS frame)
//   - channel 0x10 type 0xF7 (undocumented second line_in/line_out pair)
//
// Only the single aggregate values are decoded: current total occupancy
// (byte 0 of the 0x04/0xC9 region-summary TLV), period in/out, cumulative
// total in/out, and max simultaneous count.
package milesight

import (
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

func decodeVS121(entries []TLV, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	const dm = "vs121"
	st := record.ST
	var out []record.SensorDataRecord

	for _, e := range entries {
		switch {
		// people_count_all(1B) + region_count(1B) + region_mask(2B) — only
		// the first byte (current aggregate occupancy) is decoded.
		case e.Channel == 0x04 && e.Type == 0xC9:
			out = append(out, record.NewInt(st.PeopleCount, dm, deviceID, provider, int64(e.Data[0]), ts))

		// people_in(2B)+people_out(2B): period count since last uplink.
		case e.Channel == 0x05 && e.Type == 0xCC:
			out = append(out, record.NewInt(st.PeopleLineIn, dm, deviceID, provider, int64(le16(e.Data[0:2])), ts))
			out = append(out, record.NewInt(st.PeopleLineOut, dm, deviceID, provider, int64(le16(e.Data[2:4])), ts))

		case e.Channel == 0x06 && e.Type == 0xCD:
			out = append(out, record.NewInt(st.PeopleCountMax, dm, deviceID, provider, int64(e.Data[0]), ts))

		// people_total_in(2B)+people_total_out(2B): cumulative since last
		// device reset.
		case e.Channel == 0x0D && e.Type == 0xCC:
			out = append(out, record.NewInt(st.PeopleLineTotalIn, dm, deviceID, provider, int64(le16(e.Data[0:2])), ts))
			out = append(out, record.NewInt(st.PeopleLineTotalOut, dm, deviceID, provider, int64(le16(e.Data[2:4])), ts))
		}
	}

	return out
}

// DecodeVS121 decodes VS121 binary payloads from LoRaWAN uplinks.
func DecodeVS121(payload []byte, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	return decodeVS121(ParseMilesightTLV(payload), deviceID, provider, ts)
}
