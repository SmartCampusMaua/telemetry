// Command simulator is a software-only, direct-to-NATS OBU/RSU traffic
// generator. The ecu/ ESP-IDF firmware already simulates one vehicle's CAN
// bus for exercising real OBU/RSU hardware; this command exists for the
// case where that hardware (or a physical CAN bus) isn't available at all —
// it publishes wire-identical zc2x_packet_t packets (internal.EncodePacket)
// straight to NATS Core's zc2x.can.obu / zc2x.can.rsu subjects, so
// cmd (the input service) and everything downstream of it can't tell the
// difference from real firmware traffic.
//
// device_id vs asset_id: zc2x_packet_t's DeviceID identifies the OBU/RSU
// hardware unit itself. asset_id is what that unit is physically embedded
// in — a vehicle for OBU, a trackside pole for RSU — and has no room in the
// fixed 34-byte wire struct (changing that would break real firmware
// compatibility). This command assigns both per simulated device and writes
// the OBU device_id -> vehicle asset_id mapping to a JSON file
// (-registry-out) that cmd's ASSET_REGISTRY_PATH can load, so decoded
// telemetry (internal.TelemetryRecord) carries asset_id for OBU-originated
// signals. RSU's own asset_id (its pole) is logged here for operator
// visibility but never reaches telemetry: real RSU firmware forwards OBU's
// packet bytes unmodified, so DeviceID in a relayed packet is always the
// OBU's, never the relaying RSU's (see internal/assets.go and signals.go's
// "origin" comment) — this simulator is faithful to that same limitation
// rather than inventing a side channel real hardware doesn't have.
//
// Default device counts (3 simulated OBUs/vehicles, 3 simulated
// RSUs/poles): enough to exercise multi-device fan-out, the asset registry
// lookup, and RSU relay behavior end-to-end (a small test grid + a minimal
// trackside relay layout, e.g. start/finish, back straight, final corner)
// without flooding a dev NATS server. Each simulated OBU sweeps the full
// candb.Messages() catalog — the same message set the ecu/ firmware
// simulator transmits for one vehicle — so downstream decoding exercises
// every defined CAN message, not just a subset. Override with -obu-count /
// -rsu-count for a larger fleet, or -stream=obu/rsu/both to control which
// path(s) actually publish (independent of fleet size — see -stream's
// usage text) for exercising the input service's dedup/OBU-disconnected
// handling (see internal/dedup.go).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"hash/fnv"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/rogeriocassares/zc2x/services/input/nats/internal"
	"github.com/rogeriocassares/zc2x/services/input/nats/internal/candb"
)

// publisher is one simulated NATS-direct CAN source: either an OBU (an
// actual vehicle, packetDeviceID == its own device_id) or an RSU relaying
// one vehicle's traffic (packetDeviceID == that vehicle's OBU device_id,
// per the wire-format limitation described in the package doc).
type publisher struct {
	label          string
	subject        string
	packetDeviceID [internal.DeviceIDSize]byte
	seed           uint64
}

func main() {
	natsURL := flag.String("nats-url", nats.DefaultURL, "NATS Core server URL")
	obuCount := flag.Int("obu-count", 3, "number of simulated OBUs (vehicles)")
	rsuCount := flag.Int("rsu-count", 3, "number of simulated RSUs (trackside poles)")
	stream := flag.String("stream", "both", `which path(s) actually publish to NATS: "obu", "rsu", or "both". The fleet (-obu-count/-rsu-count) is always defined either way, since RSU always relays a specific vehicle's identity -- this only controls which publisher goroutines run. -stream=rsu simulates every OBU being disconnected (RSU-only delivery, exercising the input service's dedup fallback path); -stream=obu simulates no RSU coverage at all.`)
	obuSubject := flag.String("obu-subject", "zc2x.can.obu", "subject OBU packets publish to (matches real firmware's OBU_NATS_SUBJECT)")
	rsuSubject := flag.String("rsu-subject", "zc2x.can.rsu", "subject RSU packets publish to (matches real firmware's RSU_NATS_SUBJECT)")
	tick := flag.Duration("tick", 20*time.Millisecond, "interval between generated CAN frames per device")
	batch := flag.Int("batch", 4, "packets per NATS publish (mirrors real firmware's batched publishing)")
	registryOut := flag.String("registry-out", "", "optional path to write the OBU device_id -> vehicle asset_id JSON registry")
	flag.Parse()

	if *obuCount < 1 {
		log.Fatal("simulator: -obu-count must be >= 1")
	}
	if *rsuCount < 0 {
		log.Fatal("simulator: -rsu-count must be >= 0")
	}
	if *batch < 1 {
		log.Fatal("simulator: -batch must be >= 1")
	}
	streamOBU := *stream == "obu" || *stream == "both"
	streamRSU := *stream == "rsu" || *stream == "both"
	if !streamOBU && !streamRSU {
		log.Fatalf(`simulator: -stream must be "obu", "rsu", or "both", got %q`, *stream)
	}

	messages := candb.Messages()
	if len(messages) == 0 {
		log.Fatal("simulator: candb has no messages defined")
	}

	obus := make([]publisher, *obuCount)
	registry := make(internal.AssetRegistry, *obuCount)
	for i := 0; i < *obuCount; i++ {
		id := obuDeviceID(i + 1)
		asset := fmt.Sprintf("vehicle-%d", i+1)
		obus[i] = publisher{
			label:          fmt.Sprintf("obu-%d", i+1),
			subject:        *obuSubject,
			packetDeviceID: id,
			seed:           seedFor(id),
		}
		registry[hexDeviceID(id)] = asset
		log.Printf("simulator: %s device_id=%s asset_id=%s", obus[i].label, hexDeviceID(id), asset)
	}

	var publishers []publisher
	if streamOBU {
		publishers = append(publishers, obus...)
	}
	if streamRSU {
		for j := 0; j < *rsuCount; j++ {
			rsuID := rsuDeviceID(j + 1)
			pole := fmt.Sprintf("pole-%d", j+1)
			vehicle := obus[j%len(obus)]
			publishers = append(publishers, publisher{
				label:          fmt.Sprintf("rsu-%d(relays %s)", j+1, vehicle.label),
				subject:        *rsuSubject,
				packetDeviceID: vehicle.packetDeviceID, // real RSU never carries its own identity in the payload
				seed:           vehicle.seed,           // same deterministic stream as the relayed OBU
			})
			log.Printf("simulator: rsu-%d device_id=%s asset_id=%s (pole; not carried in telemetry) relays %s",
				j+1, hexDeviceID(rsuID), pole, vehicle.label)
		}
	}

	if *registryOut != "" {
		data, err := json.MarshalIndent(registry, "", "  ")
		if err != nil {
			log.Fatalf("simulator: marshal asset registry: %v", err)
		}
		if err := os.WriteFile(*registryOut, data, 0o644); err != nil {
			log.Fatalf("simulator: write asset registry %s: %v", *registryOut, err)
		}
		log.Printf("simulator: wrote asset registry (%d vehicles) to %s", len(registry), *registryOut)
	}

	nc, err := nats.Connect(*natsURL, nats.Name("zc2x-simulator"))
	if err != nil {
		log.Fatalf("simulator: connect to nats at %s: %v", *natsURL, err)
	}
	defer nc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	activeOBU, activeRSU := 0, 0
	if streamOBU {
		activeOBU = len(obus)
	}
	if streamRSU {
		activeRSU = *rsuCount
	}
	log.Printf("simulator: stream=%s: starting %d OBU + %d RSU publishers (fleet: %d vehicles, %d poles), %d CAN messages/device, tick=%s batch=%d",
		*stream, activeOBU, activeRSU, *obuCount, *rsuCount, len(messages), *tick, *batch)

	done := make(chan struct{}, len(publishers))
	for _, p := range publishers {
		go func(p publisher) {
			runPublisher(ctx, nc, p, messages, *tick, *batch)
			done <- struct{}{}
		}(p)
	}

	<-ctx.Done()
	log.Print("simulator: shutting down")
	for range publishers {
		<-done
	}
	nc.Drain()
}

// runPublisher generates one synthetic CAN frame per tick, round-robining
// through every message in the database, and flushes a batch of encoded
// packets to NATS Core once batchSize accumulate — mirroring real
// firmware's nats_task, which batches to cut per-publish overhead (see
// adapter.go's handleMessage comment).
func runPublisher(ctx context.Context, nc *nats.Conn, p publisher, messages []candb.Message, tick time.Duration, batchSize int) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	start := time.Now()
	var seq uint32
	msgIdx := 0
	var batchBuf []byte

	flush := func() {
		if len(batchBuf) == 0 {
			return
		}
		if err := nc.Publish(p.subject, batchBuf); err != nil {
			log.Printf("simulator: %s: publish to %q failed: %v", p.label, p.subject, err)
		}
		batchBuf = nil
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return
		case <-ticker.C:
			msg := messages[msgIdx]
			msgIdx = (msgIdx + 1) % len(messages)

			elapsed := time.Since(start).Seconds()
			payload := make([]byte, msg.DLC)
			for _, sig := range msg.Signals {
				raw := syntheticRaw(p.seed, msg.ID, sig, elapsed)
				encodeSignalRaw(payload, sig, raw)
			}

			seq++
			pkt := internal.EncodePacket(p.packetDeviceID, seq, uint64(time.Since(start).Microseconds()), msg.ID, payload)
			batchBuf = append(batchBuf, pkt...)

			if len(batchBuf) >= batchSize*internal.PacketSize {
				flush()
			}
		}
	}
}

// obuDeviceID/rsuDeviceID assign locally-administered, unicast MAC-style
// device IDs (the 0x02 leading octet sets IEEE 802's locally-administered
// bit — see RFC 5342/the IEEE 802 MAC address spec) so simulated devices
// can never collide with a real ESP32's vendor-assigned MAC, which real
// firmware uses as its device_id. The third-from-last octet distinguishes
// OBU (0x00) from RSU (0x01) purely for readability in logs/registries —
// device_id has no such structure in the real wire format, it's just
// whatever 6 bytes the transmitting unit's MAC happens to be.
func obuDeviceID(n int) [internal.DeviceIDSize]byte {
	return [internal.DeviceIDSize]byte{0x02, 0x00, 0x00, 0x00, 0x00, byte(n)}
}

func rsuDeviceID(n int) [internal.DeviceIDSize]byte {
	return [internal.DeviceIDSize]byte{0x02, 0x00, 0x00, 0x01, 0x00, byte(n)}
}

func hexDeviceID(id [internal.DeviceIDSize]byte) string {
	return fmt.Sprintf("%x", id)
}

func seedFor(id [internal.DeviceIDSize]byte) uint64 {
	h := fnv.New64a()
	h.Write(id[:])
	return h.Sum64()
}
