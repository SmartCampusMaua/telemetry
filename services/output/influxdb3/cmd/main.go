// Command influxdb3 is the ZC2X JetStream -> InfluxDB3 output service. It
// subscribes (as a durable JetStream consumer, so a restart resumes rather
// than replays or drops) to the decoded telemetry stream services/input/nats
// publishes, and writes each record into InfluxDB3 as one point per signal.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/rogeriocassares/zc2x/services/output/influxdb3/internal"
)

func main() {
	natsURL := getenv("NATS_URL", nats.DefaultURL)
	streamName := getenv("JETSTREAM_TELEMETRY_STREAM", "ZC2X_TELEMETRY")
	subjectFilter := getenv("NATS_TELEMETRY_SUBJECT_FILTER", "zc2x.js.telemetry.>")
	consumerName := getenv("JETSTREAM_CONSUMER", "influxdb3-writer")

	// INFLUXDB_TOKEN may legitimately be empty: local dev runs
	// influxdb3-core with --without-auth (see infra/docker/docker-compose.yaml).
	influxHost := getenv("INFLUXDB_HOST", "http://localhost:8181")
	influxToken := getenv("INFLUXDB_TOKEN", "")
	influxDatabase := getenv("INFLUXDB_DATABASE", "zc2x")
	influxTable := getenv("INFLUXDB_TABLE", "telemetry")

	batchSize, err := strconv.Atoi(getenv("INFLUXDB_BATCH_SIZE", "5000"))
	if err != nil {
		log.Fatalf("main: invalid INFLUXDB_BATCH_SIZE: %v", err)
	}
	flushEvery, err := time.ParseDuration(getenv("INFLUXDB_FLUSH_INTERVAL", "2s"))
	if err != nil {
		log.Fatalf("main: invalid INFLUXDB_FLUSH_INTERVAL: %v", err)
	}

	client, err := influxdb3.New(influxdb3.ClientConfig{
		Host:     influxHost,
		Token:    influxToken,
		Database: influxDatabase,
	})
	if err != nil {
		log.Fatalf("main: influxdb3 client: %v", err)
	}
	defer client.Close()

	nc, err := nats.Connect(natsURL, nats.Name("zc2x-output-influxdb3"))
	if err != nil {
		log.Fatalf("main: nats connect: %v", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatalf("main: jetstream: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// DeliverPolicy defaults to All, which would replay this stream's entire
	// history from day one on first run -- including, for ZC2X_TELEMETRY,
	// millions of pre-JSON-migration Line Protocol records this service can
	// never parse (see signals.go's package doc: JSON replaced Line Protocol
	// earlier in this project). DeliverNew is the sensible default for a
	// newly added consumer: start from whatever's published from here on,
	// not attempt to reprocess years of incompatible history. A durable
	// consumer only honors DeliverPolicy on its first creation, so this has
	// no effect on restarts -- it resumes from its own last-acked position.
	cons, err := js.CreateOrUpdateConsumer(ctx, streamName, jetstream.ConsumerConfig{
		Durable:       consumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subjectFilter,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		log.Fatalf("main: create consumer on stream %q: %v", streamName, err)
	}

	log.Printf("main: writing %s.%s from stream %q (subject %q, consumer %q) to influxdb3 at %s (batch=%d flush=%s)",
		influxDatabase, influxTable, streamName, subjectFilter, consumerName, influxHost, batchSize, flushEvery)

	if err := internal.Run(ctx, cons, client, influxTable, batchSize, flushEvery); err != nil {
		log.Fatalf("main: %v", err)
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
