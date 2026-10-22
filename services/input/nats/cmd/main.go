// Command nats is the ZC2X NATS Core -> JetStream input service. It
// subscribes to the raw packet subjects OBU/RSU publish to on NATS Core,
// decodes and validates each zc2x_packet_t, and republishes valid packets to
// a durable JetStream stream.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"

	"github.com/rogeriocassares/zc2x/services/input/nats/internal"
)

func main() {
	sourcePrefix := getenv("NATS_SOURCE_PREFIX", "zc2x.can.")
	targetPrefix := getenv("JETSTREAM_SUBJECT_PREFIX", "zc2x.js.can.")
	telemetryPrefix := getenv("NATS_TELEMETRY_SUBJECT_PREFIX", "zc2x.js.telemetry.")

	// 30s comfortably covers the latency gap between OBU's direct WiFi
	// publish and RSU's extra XBee-relay hop, without letting a rebooted
	// device's low, reused sequence numbers collide with a stale entry --
	// see internal/dedup.go. NATS_DEDUP_WINDOW=0 disables deduplication.
	dedupWindow, err := time.ParseDuration(getenv("NATS_DEDUP_WINDOW", "30s"))
	if err != nil {
		log.Fatalf("main: invalid NATS_DEDUP_WINDOW: %v", err)
	}

	var assetRegistry internal.AssetRegistry
	if path := os.Getenv("ASSET_REGISTRY_PATH"); path != "" {
		reg, err := internal.LoadAssetRegistry(path)
		if err != nil {
			log.Fatalf("main: %v", err)
		}
		assetRegistry = reg
		log.Printf("main: loaded asset registry from %s (%d entries)", path, len(reg))
	}

	cfg := internal.Config{
		NATSURL:          getenv("NATS_URL", nats.DefaultURL),
		SourceSubject:    getenv("NATS_SOURCE_SUBJECT", sourcePrefix+"*"),
		StreamName:       getenv("JETSTREAM_STREAM", "ZC2X_CAN"),
		StreamSubjects:   []string{targetPrefix + ">"},
		PublishSubjectFn: internal.DefaultPublishSubject(sourcePrefix, targetPrefix),
		SourcePrefix:     sourcePrefix,

		TelemetryStreamName:       getenv("JETSTREAM_TELEMETRY_STREAM", "ZC2X_TELEMETRY"),
		TelemetryStreamSubjects:   []string{telemetryPrefix + ">"},
		TelemetryPublishSubjectFn: internal.DefaultTelemetryPublishSubject(telemetryPrefix, sourcePrefix),
		AssetRegistry:             assetRegistry,
		DedupWindow:               dedupWindow,
	}

	adapter, err := internal.NewAdapter(cfg)
	if err != nil {
		log.Fatalf("main: %v", err)
	}
	defer adapter.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := adapter.EnsureStream(ctx); err != nil {
		log.Fatalf("main: %v", err)
	}

	if err := adapter.Run(ctx); err != nil {
		log.Fatalf("main: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
