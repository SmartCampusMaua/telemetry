package milesight

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

func findBy(records []record.SensorDataRecord, sensorType string, deviceIndex int) (record.SensorDataRecord, bool) {
	for _, r := range records {
		if r.SensorType == sensorType && r.DeviceIndex == deviceIndex {
			return r, true
		}
	}
	return record.SensorDataRecord{}, false
}

func TestDecodeVS135_MultiLine(t *testing.T) {
	payload := []byte{
		0x03, 0xD2, 0xE8, 0x03, 0x00, 0x00, // line 1 total_in: 1000
		0x04, 0xD2, 0xB6, 0x03, 0x00, 0x00, // line 1 total_out: 950
		0x05, 0xCC, 5, 0, 3, 0, // line 1 period_in=5, period_out=3
		0x06, 0xD2, 0xC8, 0x00, 0x00, 0x00, // line 2 total_in: 200
		0x16, 0xCC, 1, 0, 0, 0, // line 2 child period_in=1, period_out=0
	}

	records := DecodeVS135(payload, "dev-1", "chirpstackv4", time.Now())

	st := record.ST
	for _, r := range records {
		if r.DeviceModel != "vs135" {
			t.Errorf("DeviceModel = %q, want vs135", r.DeviceModel)
		}
	}

	if r, ok := findBy(records, st.PeopleLineTotalIn, 1); !ok || r.ValueInt == nil || *r.ValueInt != 1000 {
		t.Errorf("line 1 PeopleLineTotalIn: got %+v (found=%v), want 1000", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineTotalOut, 1); !ok || r.ValueInt == nil || *r.ValueInt != 950 {
		t.Errorf("line 1 PeopleLineTotalOut: got %+v (found=%v), want 950", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineIn, 1); !ok || r.ValueInt == nil || *r.ValueInt != 5 {
		t.Errorf("line 1 PeopleLineIn: got %+v (found=%v), want 5", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineOut, 1); !ok || r.ValueInt == nil || *r.ValueInt != 3 {
		t.Errorf("line 1 PeopleLineOut: got %+v (found=%v), want 3", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineTotalIn, 2); !ok || r.ValueInt == nil || *r.ValueInt != 200 {
		t.Errorf("line 2 PeopleLineTotalIn: got %+v (found=%v), want 200", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineInChild, 2); !ok || r.ValueInt == nil || *r.ValueInt != 1 {
		t.Errorf("line 2 PeopleLineInChild: got %+v (found=%v), want 1", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineOutChild, 2); !ok || r.ValueInt == nil || *r.ValueInt != 0 {
		t.Errorf("line 2 PeopleLineOutChild: got %+v (found=%v), want 0", r, ok)
	}

	// No line-3/line-4/region/dwell/occlusion data was sent, so nothing
	// beyond the 7 records above should exist (3 + 1 + 2 + 2 + nothing extra
	// for line 2's missing total_out/period).
	if len(records) != 7 {
		t.Errorf("got %d records, want 7: %+v", len(records), records)
	}
}

// Captured from a real chirpstackv4 uplink (line 1 only).
func TestDecodeVS135_RealPayload(t *testing.T) {
	payload, err := hex.DecodeString("03d26e00000004d20100000005cc05000000")
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}

	records := DecodeVS135(payload, "dev-1", "chirpstackv4", time.Now())

	st := record.ST
	if r, ok := findBy(records, st.PeopleLineTotalIn, 1); !ok || r.ValueInt == nil || *r.ValueInt != 110 {
		t.Errorf("PeopleLineTotalIn: got %+v (found=%v), want 110", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineTotalOut, 1); !ok || r.ValueInt == nil || *r.ValueInt != 1 {
		t.Errorf("PeopleLineTotalOut: got %+v (found=%v), want 1", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineIn, 1); !ok || r.ValueInt == nil || *r.ValueInt != 5 {
		t.Errorf("PeopleLineIn: got %+v (found=%v), want 5", r, ok)
	}
	if r, ok := findBy(records, st.PeopleLineOut, 1); !ok || r.ValueInt == nil || *r.ValueInt != 0 {
		t.Errorf("PeopleLineOut: got %+v (found=%v), want 0", r, ok)
	}
	if len(records) != 4 {
		t.Errorf("got %d records, want 4: %+v", len(records), records)
	}
}

// Region-count/dwell/occlusion channels must not break tokenization of
// whatever follows them, even though none of them produce a record.
func TestDecodeVS135_RegionAndOcclusionChannelsConsumedButNotDecoded(t *testing.T) {
	payload := []byte{
		0x0F, 0xE3, 1, 2, 3, 4, // region_1..4_count — skipped
		0x1D, 0xE3, 0, 1, 0, 1, // region_1..4_child_count — skipped
		0x10, 0xE4, 1, 10, 0, 20, 0, // region dwell time — skipped
		0x50, 0xFC, 0, 1, 2, // occlusion alarm (3 bytes) — skipped
		0x03, 0xD2, 0x0A, 0x00, 0x00, 0x00, // line 1 total_in=10 — must still decode after the above
	}

	records := DecodeVS135(payload, "dev-1", "chirpstackv4", time.Now())

	if len(records) != 1 {
		t.Fatalf("expected exactly 1 record (region/dwell/occlusion channels skipped), got %d: %+v", len(records), records)
	}
	if records[0].SensorType != record.ST.PeopleLineTotalIn || records[0].DeviceIndex != 1 || records[0].ValueInt == nil || *records[0].ValueInt != 10 {
		t.Errorf("expected line 1 PeopleLineTotalIn=10, got %+v", records[0])
	}
}
