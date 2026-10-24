// Reference: https://github.com/Milesight-IoT/SensorDecoders/blob/main/am-series/am308/am308-decoder.js
//
// AM308 is AM103's richer sibling: same battery/temperature/humidity base
// (see am103.go), plus CO2, PIR motion, ambient light, barometric pressure,
// TVOC, and PM2.5/PM10. Pressure reuses AirPress (same hPa reading
// NIT21LI-EMW104 already reports under that tag); CO2 reuses AM103's CO2;
// ambient light reuses the Illuminance family (see IlluminanceRaw's own
// comment in record.go for why it's the "_raw" variant, not Illuminance
// itself).
//
// TVOC has two mutually exclusive wire formats (firmware-configured, never
// both on one device): channel 0x08 type 0x7D is an IAQ index (unitless,
// ÷100), type 0xE6 is a real µg/m³ mass concentration. These are different
// physical quantities, not two resolutions of one — decoded under separate
// sensor_types (TVOCIndex / TVOC) rather than merged.
//
// Channel 0x0E (buzzer_status) is a device-state echo, not a sensor
// reading — same category as every other downlink-ack channel this
// package already skips (see vs370.go's doc comment) — intentionally not
// decoded into sensor_data.
package milesight

import (
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

func decodeAM308(entries []TLV, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	const dm = "am308"
	st := record.ST
	var out []record.SensorDataRecord

	for _, e := range entries {
		switch {
		case e.Channel == 0x01 && e.Type == 0x75:
			out = append(out, record.NewFloat(st.BatteryLevel, dm, deviceID, provider, float64(e.Data[0]), ts))
		case e.Channel == 0x03 && e.Type == 0x67:
			out = append(out, record.NewFloat(st.AirTemp, dm, deviceID, provider, float64(int16(le16(e.Data)))/10.0, ts))
		case e.Channel == 0x04 && e.Type == 0x68:
			out = append(out, record.NewFloat(st.AirRH, dm, deviceID, provider, float64(e.Data[0])/2.0, ts))
		case e.Channel == 0x05 && e.Type == 0x00:
			out = append(out, record.NewBool(st.PIRStatus, dm, deviceID, provider, e.Data[0] == 1, ts))
		case e.Channel == 0x06 && e.Type == 0xCB:
			out = append(out, record.NewInt(st.IlluminanceRaw, dm, deviceID, provider, int64(e.Data[0]), ts))
		case e.Channel == 0x07 && e.Type == 0x7D:
			out = append(out, record.NewInt(st.CO2, dm, deviceID, provider, int64(le16(e.Data)), ts))
		case e.Channel == 0x08 && e.Type == 0x7D:
			out = append(out, record.NewFloat(st.TVOCIndex, dm, deviceID, provider, float64(le16(e.Data))/100.0, ts))
		case e.Channel == 0x08 && e.Type == 0xE6:
			out = append(out, record.NewInt(st.TVOC, dm, deviceID, provider, int64(le16(e.Data)), ts))
		case e.Channel == 0x09 && e.Type == 0x73:
			out = append(out, record.NewFloat(st.AirPress, dm, deviceID, provider, float64(le16(e.Data))/10.0, ts))
		case e.Channel == 0x0B && e.Type == 0x7D:
			out = append(out, record.NewInt(st.PM2_5, dm, deviceID, provider, int64(le16(e.Data)), ts))
		case e.Channel == 0x0C && e.Type == 0x7D:
			out = append(out, record.NewInt(st.PM10, dm, deviceID, provider, int64(le16(e.Data)), ts))
		}
	}

	return out
}

// DecodeAM308 decodes AM308 binary payloads from LoRaWAN uplinks.
func DecodeAM308(payload []byte, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	return decodeAM308(ParseMilesightTLV(payload), deviceID, provider, ts)
}
