package internal

import (
	"testing"
	"time"
)

func TestDeduplicator_FirstArrivalWins(t *testing.T) {
	d := NewDeduplicator(30 * time.Second)
	device := [DeviceIDSize]byte{1, 2, 3, 4, 5, 6}

	if !d.Accept(device, 100) {
		t.Fatal("first arrival of seq=100 should be accepted")
	}
	if d.Accept(device, 100) {
		t.Fatal("second arrival of seq=100 (e.g. via RSU relay of the same packet) should be rejected as a duplicate")
	}
	// A different device with the same sequence number is a distinct key.
	other := [DeviceIDSize]byte{9, 9, 9, 9, 9, 9}
	if !d.Accept(other, 100) {
		t.Fatal("seq=100 from a different device should be accepted (distinct key)")
	}
}

func TestDeduplicator_ObuDisconnected_RsuOnlyStillAccepted(t *testing.T) {
	// No prior OBU delivery to deduplicate against -- RSU's copy (the only
	// one that ever arrives) must be accepted, not treated as a duplicate.
	d := NewDeduplicator(30 * time.Second)
	device := [DeviceIDSize]byte{1, 2, 3, 4, 5, 6}
	if !d.Accept(device, 42) {
		t.Fatal("sole arrival via RSU (OBU disconnected) should be accepted")
	}
}

func TestDeduplicator_Sweep_EvictsOldEntries(t *testing.T) {
	d := NewDeduplicator(10 * time.Millisecond)
	device := [DeviceIDSize]byte{1, 2, 3, 4, 5, 6}

	if !d.Accept(device, 1) {
		t.Fatal("first arrival should be accepted")
	}
	if d.Accept(device, 1) {
		t.Fatal("immediate duplicate should be rejected")
	}

	time.Sleep(20 * time.Millisecond)
	d.Sweep()

	if !d.Accept(device, 1) {
		t.Fatal("seq=1 should be accepted again after the entry aged past window and was swept (e.g. a reboot reusing sequence numbers)")
	}
}

func TestDeduplicator_OutOfOrderArrival_StillAccepted(t *testing.T) {
	// RSU's extra relay hop can deliver an older sequence after OBU's
	// direct path has already advanced further -- this must still be
	// accepted (it's a real, not-yet-seen packet), not dropped as if it
	// were a duplicate or "in the past".
	d := NewDeduplicator(30 * time.Second)
	device := [DeviceIDSize]byte{1, 2, 3, 4, 5, 6}

	if !d.Accept(device, 100) {
		t.Fatal("seq=100 should be accepted")
	}
	if !d.Accept(device, 102) {
		t.Fatal("seq=102 should be accepted")
	}
	if !d.Accept(device, 101) {
		t.Fatal("late-arriving seq=101 (e.g. via RSU's slower path) should still be accepted, not dropped")
	}
	if d.Accept(device, 101) {
		t.Fatal("a second copy of seq=101 should now be rejected as a duplicate")
	}
}
