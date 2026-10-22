package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	"github.com/nats-io/nats.go/jetstream"
)

// Run consumes telemetry records from cons until ctx is canceled, batching
// them into InfluxDB3 points and flushing to client whenever batchSize
// points have accumulated or flushEvery elapses, whichever comes first.
// "This shall be used to apply massive data" (the task that created this
// service): a batched bulk write amortizes per-request overhead the same
// way OBU/RSU's own nats_task already batches CAN packets before publishing
// (see services/input/nats/internal/adapter.go's handleMessage comment) and
// cmd/simulator batches its NATS publishes -- writing one HTTP request per
// decoded signal would make this service the bottleneck in the pipeline it
// sits at the end of.
//
// A batch's messages are Ack'd only after a successful write, and Nak'd (for
// JetStream redelivery) on failure -- so a transient InfluxDB3 outage
// delays telemetry rather than silently dropping it. A record that fails to
// even parse or classify is Term'd instead: redelivering a malformed
// message will never succeed, so retrying it forever would just waste
// consumer throughput on the whole batch behind it.
func Run(ctx context.Context, cons jetstream.Consumer, client *influxdb3.Client, table string, batchSize int, flushEvery time.Duration) error {
	iter, err := cons.Messages()
	if err != nil {
		return fmt.Errorf("internal: start consuming: %w", err)
	}
	defer iter.Stop()

	msgCh := make(chan jetstream.Msg)
	errCh := make(chan error, 1)
	go func() {
		for {
			msg, err := iter.Next()
			if err != nil {
				errCh <- err
				return
			}
			select {
			case msgCh <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(flushEvery)
	defer ticker.Stop()

	points := make([]*influxdb3.Point, 0, batchSize)
	pending := make([]jetstream.Msg, 0, batchSize)

	flush := func() {
		if len(points) == 0 {
			return
		}
		writeCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		writeErr := client.WritePoints(writeCtx, points)
		cancel()
		if writeErr != nil {
			log.Printf("internal: write batch of %d points failed, nak'ing for redelivery: %v", len(points), writeErr)
			for _, m := range pending {
				if err := m.Nak(); err != nil {
					log.Printf("internal: nak failed: %v", err)
				}
			}
		} else {
			for _, m := range pending {
				if err := m.Ack(); err != nil {
					log.Printf("internal: ack failed: %v", err)
				}
			}
		}
		points = points[:0]
		pending = pending[:0]
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return nil
		case err := <-errCh:
			flush()
			return fmt.Errorf("internal: consume: %w", err)
		case msg := <-msgCh:
			var rec TelemetryRecord
			if err := json.Unmarshal(msg.Data(), &rec); err != nil {
				log.Printf("internal: dropping malformed telemetry record: %v", err)
				_ = msg.Term()
				continue
			}
			pt, err := BuildPoint(table, rec)
			if err != nil {
				log.Printf("internal: dropping record: %v", err)
				_ = msg.Term()
				continue
			}
			points = append(points, pt)
			pending = append(pending, msg)
			if len(points) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}
