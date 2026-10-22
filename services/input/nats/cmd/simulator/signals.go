// Command simulator's synthetic-signal generation. See main.go's package
// doc for what this simulator is and isn't modeling.
package main

import (
	"encoding/binary"
	"hash/fnv"
	"math"

	"github.com/rogeriocassares/zc2x/services/input/nats/internal/candb"
)

// rawRange returns the raw-value domain candb.Signal.Type can represent.
func rawRange(t candb.ValueType) (min, max int64) {
	switch t {
	case candb.Uint8:
		return 0, 255
	case candb.Int16:
		return -32768, 32767
	case candb.Uint16:
		return 0, 65535
	case candb.Uint32:
		return 0, 4294967295
	case candb.Int32:
		return -2147483648, 2147483647
	case candb.Bit:
		return 0, 1
	default:
		return 0, 0
	}
}

// syntheticRaw deterministically derives a raw signal value from (seed,
// canID, signal, elapsed-seconds-since-start): a smooth sine wave, in the
// signal's declared physical range (candb.Signal.Min/Max — this DBC's real
// SG_-line bounds, not the wire type's full raw domain), converted to a raw
// value via the signal's own Scale/Bias. Frequency and phase are hashed from
// seed+canID+signal name, so different devices and signals drift
// independently without any device needing to share mutable state. Staying
// within Min/Max (rather than the much wider raw domain most signals only
// partially use, e.g. Lambda1's 0.5-1.5 physical range over a full 16-bit
// field) is what makes this a plausible reading rather than merely a value
// that decodes without error. Two publishers given the same seed (an OBU and
// the RSU relaying it — see main.go) evaluate this identically at the same
// elapsed time, approximating a live relay without piping bytes between
// goroutines.
func syntheticRaw(seed uint64, canID uint32, sig candb.Signal, elapsedSeconds float64) int64 {
	h := fnv.New64a()
	h.Write([]byte{byte(canID), byte(canID >> 8), byte(canID >> 16), byte(canID >> 24)})
	h.Write([]byte(sig.Name))
	var seedBuf [8]byte
	binary.LittleEndian.PutUint64(seedBuf[:], seed)
	h.Write(seedBuf[:])
	mixed := h.Sum64()

	if sig.Type == candb.Bit {
		freq := 0.02 + 0.08*fracOf(mixed) // slow, occasional flips
		if math.Sin(2*math.Pi*freq*elapsedSeconds+phaseOf(mixed)) > 0 {
			return 1
		}
		return 0
	}

	amplitude := (sig.Max - sig.Min) * 0.4
	center := (sig.Min + sig.Max) / 2
	freq := 0.05 + 0.15*fracOf(mixed)
	physical := center + amplitude*math.Sin(2*math.Pi*freq*elapsedSeconds+phaseOf(mixed))
	if physical < sig.Min {
		physical = sig.Min
	}
	if physical > sig.Max {
		physical = sig.Max
	}

	raw := (physical - sig.Bias) / sig.Scale
	// Defensive backstop only: Min/Max are within the wire type's raw domain
	// by construction (they're this DBC's own declared signal bounds), so
	// this shouldn't trigger -- but encodeSignalRaw truncates blindly on
	// whatever it's given, so clamp rather than risk silently wrapping.
	rmin, rmax := rawRange(sig.Type)
	if raw < float64(rmin) {
		raw = float64(rmin)
	}
	if raw > float64(rmax) {
		raw = float64(rmax)
	}
	return int64(math.Round(raw))
}

// fracOf/phaseOf derive a [0,1) fraction / a [0,2*pi) phase from a hash, so
// each signal gets its own frequency and starting phase without persisted
// per-signal state.
func fracOf(h uint64) float64 {
	return float64(h%1_000_000) / 1_000_000
}

func phaseOf(h uint64) float64 {
	return fracOf(h>>20) * 2 * math.Pi
}

// encodeSignalRaw packs raw into buf at sig's offset, mirroring
// candb.Signal.decodeRaw's layout in reverse (little-endian, byte- or
// bit-aligned per sig.Type). buf must be at least as long as the owning
// message's DLC.
func encodeSignalRaw(buf []byte, sig candb.Signal, raw int64) {
	if sig.Type == candb.Bit {
		byteIdx, bitIdx := sig.BitOffset/8, uint(sig.BitOffset%8)
		if raw != 0 {
			buf[byteIdx] |= 1 << bitIdx
		}
		return
	}
	switch sig.Type {
	case candb.Uint8:
		buf[sig.ByteOffset] = byte(raw)
	case candb.Int16, candb.Uint16:
		binary.LittleEndian.PutUint16(buf[sig.ByteOffset:], uint16(raw))
	case candb.Uint32, candb.Int32:
		binary.LittleEndian.PutUint32(buf[sig.ByteOffset:], uint32(raw))
	}
}
