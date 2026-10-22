package internal

import (
	"log"
	"sync"
	"time"
)

// dedupKey identifies one real CAN frame capture, independent of which path
// (OBU's direct WiFi link or RSU's XBee relay) delivered it -- RSU forwards
// OBU's packet bytes unmodified (RFC-0001), so the same (device_id,
// sequence) pair legitimately arrives twice whenever both paths are up.
type dedupKey struct {
	deviceID [DeviceIDSize]byte
	sequence uint32
}

// Deduplicator suppresses a packet already delivered (via either path)
// within window of its first arrival -- see adapter.go's handleMessage.
// First-arrival-wins: whichever of OBU's direct link or RSU's relay
// delivers a given (device_id, sequence) first is kept; the later duplicate
// is dropped before either the raw JetStream republish or telemetry
// decode. This transparently covers OBU-disconnected periods too: RSU is
// then the only path, so there's never a duplicate to compare against, and
// its copy is simply accepted as the first (only) arrival.
//
// A bounded TTL, not permanent growth: window only needs to cover the
// realistic latency gap between OBU's direct hop and RSU's extra
// XBee-relay hop (typically well under a second, generously a few seconds
// under load), while still letting a device's sequence counter safely
// recur after a reboot (which restarts it from 0 -- without expiry, a
// pre-reboot entry could incorrectly suppress a legitimate post-reboot
// packet that happens to reuse the same low sequence number).
//
// Also logs a best-effort "possible gap" line per device when an accepted
// sequence jumps ahead of the last one seen (see Accept's doc) -- initial,
// intentionally modest groundwork for a future SD-card backfill (OBU/RSU
// don't have SD storage yet; when they do, gaps like this are what a bulk
// upload would need to fill). This is a diagnostic tripwire, not a
// confirmed-lost list: a "gap" can still self-heal if the missing sequence
// arrives later via the slower of the two paths. A precise confirmed-lost
// list (only report a gap once window has elapsed with no arrival) is the
// natural next step once there's an actual backfill consumer to feed it.
type Deduplicator struct {
	window time.Duration

	mu      sync.Mutex
	seen    map[dedupKey]time.Time
	lastSeq map[[DeviceIDSize]byte]uint32
}

// NewDeduplicator returns a Deduplicator that suppresses duplicates seen
// within window of each other. window should comfortably exceed the
// realistic latency difference between OBU's direct path and RSU's relay
// path, but stay short relative to how often a device's sequence counter
// might plausibly repeat after a reboot.
func NewDeduplicator(window time.Duration) *Deduplicator {
	return &Deduplicator{
		window:  window,
		seen:    make(map[dedupKey]time.Time),
		lastSeq: make(map[[DeviceIDSize]byte]uint32),
	}
}

// Accept reports whether the packet identified by (deviceID, sequence)
// should be processed. false means it was already accepted (via either
// path) within window and must be silently dropped as a duplicate.
func (d *Deduplicator) Accept(deviceID [DeviceIDSize]byte, sequence uint32) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := dedupKey{deviceID, sequence}
	if _, ok := d.seen[key]; ok {
		return false
	}
	d.seen[key] = time.Now()

	if last, ok := d.lastSeq[deviceID]; ok {
		if sequence > last+1 {
			log.Printf("adapter: device=%x possible sequence gap: %d..%d not yet seen via either path (may still arrive late via the slower path)",
				deviceID, last+1, sequence-1)
		}
		if sequence > last {
			d.lastSeq[deviceID] = sequence
		}
	} else {
		d.lastSeq[deviceID] = sequence
	}

	return true
}

// Sweep evicts entries older than window, bounding the dedup cache's memory
// to roughly (traffic rate * window) instead of growing forever. Call
// periodically (see adapter.go's Run) -- Accept alone never evicts.
func (d *Deduplicator) Sweep() {
	d.mu.Lock()
	defer d.mu.Unlock()

	cutoff := time.Now().Add(-d.window)
	for k, t := range d.seen {
		if t.Before(cutoff) {
			delete(d.seen, k)
		}
	}
}
