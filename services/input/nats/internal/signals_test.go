package internal

import (
	"encoding/json"
	"testing"
)

func TestToSnakeCase(t *testing.T) {
	cases := map[string]string{
		"EngineRPM":                   "engine_rpm",
		"GForceLongitudinal":          "g_force_longitudinal",
		"GForceLateral":               "g_force_lateral",
		"WheelSpeedFR":                "wheel_speed_fr",
		"WheelSpeedFL":                "wheel_speed_fl",
		"ExhaustCylinderTemperature1": "exhaust_cylinder_temperature_1",
		"ExhaustCylinderTemperature2": "exhaust_cylinder_temperature_2",
		"GPSSpeed":                    "gps_speed",
		"GPS2":                        "gps_2",
		"GPS1":                        "gps_1",
		"CD1":                         "cd_1",
		"PE2":                         "pe_2",
		"SteeringAngle":               "steering_angle",
		"ManifoldAirPressure":         "manifold_air_pressure",
		"ThrottlePosition":            "throttle_position",
		"Gear":                        "gear",
		"Lambda1":                     "lambda_1",
		"EngineCoolantTemperature":    "engine_coolant_temperature",
		"BrakePressureFront":          "brake_pressure_front",
		"TireTempInnerFL":             "tire_temp_inner_fl",
		"TireTempMiddleRR":            "tire_temp_middle_rr",
	}
	for in, want := range cases {
		if got := toSnakeCase(in); got != want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecimalPlaces(t *testing.T) {
	cases := []struct {
		scale float64
		want  int
	}{
		{0.1, 1},
		{0.001, 3},
		{0.0000001, 7},
		{1, 0},
	}
	for _, c := range cases {
		if got := decimalPlaces(c.scale); got != c.want {
			t.Errorf("decimalPlaces(%v) = %d, want %d", c.scale, got, c.want)
		}
	}
}

func TestTelemetryRecord_JSON(t *testing.T) {
	record := TelemetryRecord{
		DeviceID:       "010000000000",
		Origin:         "obu",
		CANMessageID:   784,
		CANMessageName: "gps_2",
		SensorType:     "gps_speed",
		Value:          120.3,
		ValueKind:      "float",
		TimestampMS:    1737045296123,
	}
	got, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"device_id":"010000000000","origin":"obu","can_message_id":784,"can_message_name":"gps_2","sensor_type":"gps_speed","value":120.3,"value_kind":"float","timestamp_ms":1737045296123}`
	if string(got) != want {
		t.Errorf("TelemetryRecord JSON =\n%s\nwant\n%s", got, want)
	}
}

func TestRoundToDecimals(t *testing.T) {
	// 1013 * 0.1 in float64 arithmetic produces 101.30000000000001 without
	// rounding to the signal's declared scale precision.
	if got := roundToDecimals(1013*0.1, decimalPlaces(0.1)); got != 101.3 {
		t.Errorf("roundToDecimals = %v, want 101.3", got)
	}
}
