// Reference: https://github.com/Milesight-IoT/SensorDecoders/blob/main/am-series/am103/am103-decoder.js
//
// AM103 is Milesight's basic indoor air-quality monitor — battery,
// temperature, humidity, CO2. Temperature/humidity/battery use the exact
// same channel/type pairs (0x01/0x75, 0x03/0x67, 0x04/0x68) as EM300-DI's
// built-in sensor, so they're decoded identically and written under the
// same AirTemp/AirRH/BatteryLevel sensor_types rather than a new AM103-only
// name.
package milesight

import (
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

const dmAM103 = "am103"

func decodeAM103(entries []TLV, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	const dm = dmAM103
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
		case e.Channel == 0x07 && e.Type == 0x7D:
			out = append(out, record.NewInt(st.CO2, dm, deviceID, provider, int64(le16(e.Data)), ts))
		}
	}

	return out
}

// DecodeAM103 decodes AM103 binary payloads from LoRaWAN uplinks.
func DecodeAM103(payload []byte, deviceID, provider string, ts time.Time) []record.SensorDataRecord {
	return decodeAM103(ParseMilesightTLV(payload), deviceID, provider, ts)
}
