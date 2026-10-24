package milesight

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/SmartCampusMaua/telemetry/packages/go-parse/record"
)

func TestDecodeVS121(t *testing.T) {
	payload := []byte{
		0x04, 0xC9, 5, 2, 3, 0, // people_count_all=5; region_count/mask (skipped)
		0x05, 0xCC, 3, 0, 1, 0, // people_in=3, people_out=1 (period)
		0x06, 0xCD, 8, // people_count_max=8
		0x0D, 0xCC, 0xDC, 0x05, 0xC8, 0x05, // people_total_in=1500, people_total_out=1480
	}

	records := DecodeVS121(payload, "dev-1", "chirpstackv4", time.Now())

	st := record.ST
	got := map[string]record.SensorDataRecord{}
	for _, r := range records {
		got[r.SensorType] = r
		if r.DeviceModel != "vs121" {
			t.Errorf("DeviceModel = %q, want vs121", r.DeviceModel)
		}
		if r.DeviceIndex != 0 {
			t.Errorf("%s: expected DeviceIndex 0 (single counting line), got %d", r.SensorType, r.DeviceIndex)
		}
	}

	if c := got[st.PeopleCount]; c.ValueInt == nil || *c.ValueInt != 5 {
		t.Errorf("PeopleCount: got %+v, want 5", c)
	}
	if in := got[st.PeopleLineIn]; in.ValueInt == nil || *in.ValueInt != 3 {
		t.Errorf("PeopleLineIn: got %+v, want 3", in)
	}
	if out := got[st.PeopleLineOut]; out.ValueInt == nil || *out.ValueInt != 1 {
		t.Errorf("PeopleLineOut: got %+v, want 1", out)
	}
	if max := got[st.PeopleCountMax]; max.ValueInt == nil || *max.ValueInt != 8 {
		t.Errorf("PeopleCountMax: got %+v, want 8", max)
	}
	if totIn := got[st.PeopleLineTotalIn]; totIn.ValueInt == nil || *totIn.ValueInt != 1500 {
		t.Errorf("PeopleLineTotalIn: got %+v, want 1500", totIn)
	}
	if totOut := got[st.PeopleLineTotalOut]; totOut.ValueInt == nil || *totOut.ValueInt != 1480 {
		t.Errorf("PeopleLineTotalOut: got %+v, want 1480", totOut)
	}
}

// Captured from real chirpstackv4 uplinks. The first contains two
// back-to-back 0x04/0xC9 TLVs — the device reporting two aggregate
// occupancy readings in one uplink, not a decoder quirk.
func TestDecodeVS121_RealPayloads(t *testing.T) {
	payload1, err := hex.DecodeString("04c90000000004c901000000")
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	records1 := DecodeVS121(payload1, "dev-1", "chirpstackv4", time.Now())
	if len(records1) != 2 {
		t.Fatalf("payload 1: got %d records, want 2: %+v", len(records1), records1)
	}
	for i, want := range []int64{0, 1} {
		if records1[i].SensorType != record.ST.PeopleCount || records1[i].ValueInt == nil || *records1[i].ValueInt != want {
			t.Errorf("payload 1 record %d: got %+v, want PeopleCount=%d", i, records1[i], want)
		}
	}

	payload2, err := hex.DecodeString("04c900000000")
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	records2 := DecodeVS121(payload2, "dev-1", "chirpstackv4", time.Now())
	if len(records2) != 1 || records2[0].SensorType != record.ST.PeopleCount || records2[0].ValueInt == nil || *records2[0].ValueInt != 0 {
		t.Errorf("payload 2: got %+v, want PeopleCount=0", records2)
	}
}

// Region/dwell/flow-matrix channels must not break tokenization of
// whatever follows them in the same uplink, even though none of them
// produce a sensor_data record.
func TestDecodeVS121_RegionChannelsConsumedButNotDecoded(t *testing.T) {
	payload := []byte{
		0x07, 0xD5, 1, 2, 3, 4, 5, 6, 7, 8, // region_1..8_count — skipped
		0x09, 0xDA, 0, 0, 1, 0, 0, 0, 0, 0, // flow matrix A-to-* — skipped
		0x0E, 0xE4, 1, 10, 0, 20, 0, // region dwell time — skipped
		0x06, 0xCD, 42, // people_count_max=42 — must still decode correctly after the above
	}

	records := DecodeVS121(payload, "dev-1", "chirpstackv4", time.Now())

	if len(records) != 1 {
		t.Fatalf("expected exactly 1 record (region/dwell/flow channels skipped), got %d: %+v", len(records), records)
	}
	if records[0].SensorType != record.ST.PeopleCountMax || records[0].ValueInt == nil || *records[0].ValueInt != 42 {
		t.Errorf("expected PeopleCountMax=42, got %+v", records[0])
	}
}
