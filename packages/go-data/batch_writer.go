// Package data fans points from many concurrent producers into a single
// InfluxDB3 client — shared by every service in this monorepo that writes
// to InfluxDB3.
//
// Previously, each service's decode workers called WritePoints directly,
// once per message — with several worker goroutines all doing this
// independently and concurrently, every uplink became its own tiny,
// uncoordinated write request. Direct inspection of the on-disk Parquet
// files showed this fragmenting into 2-4 separate files per 10-minute
// window instead of one — confirmed by disjoint sensor sets across those
// files (the signature of multiple independent write streams landing in the
// same window, not one writer flushing once). That multiplies the effective
// file count InfluxDB3 has to scan for any time-range query, eventually
// exceeding its file-scan cap even for ranges well inside the nominal safe
// window.
//
// BatchWriter fixes this by making exactly one goroutine responsible for
// ever calling WritePoints against a given InfluxDB3 client. Producers keep
// running fully in parallel — they just hand their finished points to this
// writer over a channel instead of writing them directly — and the writer
// accumulates into a buffer, flushing on a timer or once the buffer hits a
// size threshold, whichever comes first. One writer, one flush at a time,
// one InfluxDB3 write stream per client — no more concurrent uncoordinated
// writers to fragment across.
package data

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
)

// ANSI color codes. logError prints in bold red; logWarn in bold yellow.
// Both write to the same destination as log.Printf so timestamps align.
func logError(format string, args ...any) {
	log.Printf("\033[1;31m[ERROR] "+format+"\033[0m", args...)
}
func logWarn(format string, args ...any) {
	log.Printf("\033[1;33m[WARN]  "+format+"\033[0m", args...)
}

// influxWriter is the subset of *influxdb3.Client this file depends on,
// narrowed to an interface purely so tests can substitute a fake instead of
// making a real HTTP call. *influxdb3.Client satisfies this as-is — nothing
// about production behavior changes.
type influxWriter interface {
	WritePoints(ctx context.Context, points []*influxdb3.Point, options ...influxdb3.WriteOption) error
}

// writeWithRetry attempts to write points up to maxAttempts times with
// exponential back-off. Unrecoverable 4xx errors (except 429) are returned
// immediately without retrying.
func writeWithRetry(ctx context.Context, iot influxWriter, points []*influxdb3.Point, maxAttempts int) error {
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err := iot.WritePoints(ctx, points, influxdb3.WithNoSync(true))
		if err == nil {
			return nil
		}
		errStr := err.Error()
		// 4xx errors other than 429 (rate limit) are not recoverable – return immediately.
		if is4xx(errStr) && !strings.Contains(errStr, "429") {
			if strings.Contains(errStr, "404") {
				logError("write rejected with 404 — database may not exist in InfluxDB3 (create it first): %v", err)
			} else {
				logError("write rejected by InfluxDB3 (unrecoverable %s): %v", extractHTTPStatus(errStr), err)
			}
			return err
		}
		if is5xxTransient(errStr) {
			if attempt < maxAttempts {
				backoff := time.Duration(attempt*attempt) * 250 * time.Millisecond
				logWarn("write attempt %d/%d: InfluxDB3 unreachable (proxy/server down: %v) — retrying in %v", attempt, maxAttempts, err, backoff)
				time.Sleep(backoff)
				continue
			}
			logError("write failed after %d attempts — InfluxDB3 unreachable: %v", maxAttempts, err)
			return err
		}
		if attempt < maxAttempts {
			backoff := time.Duration(attempt*attempt) * 250 * time.Millisecond
			logWarn("write attempt %d/%d failed (%v) — retrying in %v", attempt, maxAttempts, err, backoff)
			time.Sleep(backoff)
		} else {
			logError("write failed after %d attempts: %v", maxAttempts, err)
			return err
		}
	}
	return nil // unreachable
}

// is4xx reports whether an error string contains an unrecoverable HTTP 4xx status code.
// 429 (rate limit) is excluded — it is transient and should be retried.
func is4xx(errStr string) bool {
	for _, code := range []string{"400", "401", "403", "404", "405", "422"} {
		if strings.Contains(errStr, code) {
			return true
		}
	}
	return false
}

// is5xxTransient reports HTTP 5xx errors that are worth retrying (proxy/server down).
func is5xxTransient(errStr string) bool {
	for _, s := range []string{"502", "503", "Bad Gateway", "Service Unavailable"} {
		if strings.Contains(errStr, s) {
			return true
		}
	}
	return false
}

// extractHTTPStatus returns the first HTTP status code found in errStr, or "4xx".
func extractHTTPStatus(errStr string) string {
	for _, code := range []string{"400", "401", "403", "404", "405", "422"} {
		if strings.Contains(errStr, code) {
			return code
		}
	}
	return "4xx"
}

// BatchWriter is the single goroutine responsible for ever calling
// WritePoints against a given InfluxDB3 client — see the package doc for why.
type BatchWriter struct {
	client   influxWriter
	name     string // for logging, e.g. "sensor_data" / "raw"
	batches  chan []*influxdb3.Point
	interval time.Duration
	maxBatch int
	wg       sync.WaitGroup
}

// writeBatchChanCapacity is how many message-batches (not points — each
// Enqueue call pushes one message's worth of points as a single slice) the
// writer's input channel can hold before Enqueue starts blocking the
// calling producer. Generously sized so a slow flush doesn't stall decoding;
// not exposed as an env var since the caller's flush interval/batch size are
// the actual tuning knobs — this just needs to be "big enough."
const writeBatchChanCapacity = 4096

// NewBatchWriter starts the writer's background flush loop and returns
// immediately; call Close to flush whatever's buffered and stop it.
func NewBatchWriter(client influxWriter, name string, interval time.Duration, maxBatch int) *BatchWriter {
	bw := &BatchWriter{
		client:   client,
		name:     name,
		batches:  make(chan []*influxdb3.Point, writeBatchChanCapacity),
		interval: interval,
		maxBatch: maxBatch,
	}
	bw.wg.Add(1)
	go bw.run()
	return bw
}

// Enqueue hands one message's worth of points to the writer. Non-blocking in
// practice (buffered channel); only blocks the caller if the writer has
// fallen far enough behind to fill writeBatchChanCapacity, which is the
// intended backpressure signal if flush interval/batch size ever need retuning.
func (bw *BatchWriter) Enqueue(points []*influxdb3.Point) {
	if len(points) == 0 {
		return
	}
	bw.batches <- points
}

// Close does not itself stop new work from arriving — callers must stop
// calling Enqueue first (e.g. by draining/closing whatever feeds them).
// Close closes the input channel and blocks until run has flushed
// everything already buffered and exited — the graceful-shutdown guarantee
// that no enqueued points are silently lost on a clean shutdown.
func (bw *BatchWriter) Close() {
	close(bw.batches)
	bw.wg.Wait()
}

func (bw *BatchWriter) run() {
	defer bw.wg.Done()
	ticker := time.NewTicker(bw.interval)
	defer ticker.Stop()
	ctx := context.Background()
	buf := make([]*influxdb3.Point, 0, bw.maxBatch)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		bw.writeBatch(ctx, buf)
		buf = make([]*influxdb3.Point, 0, bw.maxBatch)
	}

	for {
		select {
		case batch, ok := <-bw.batches:
			if !ok {
				flush() // final flush on shutdown — see Close
				return
			}
			buf = append(buf, batch...)
			if len(buf) >= bw.maxBatch {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// writeBatch writes one consolidated batch, retrying the batch as a whole
// first; if that still fails, falls back to per-point writes so one bad
// point doesn't drop the entire flush interval's data.
func (bw *BatchWriter) writeBatch(ctx context.Context, points []*influxdb3.Point) {
	const maxAttempts = 3
	batchErr := writeWithRetry(ctx, bw.client, points, maxAttempts)
	if batchErr == nil {
		log.Printf("[influxdb3] wrote %d points to %s", len(points), bw.name)
		return
	}
	// Batch failed — fall back to per-point writes so one bad point does not
	// silently drop the whole flush interval's data.
	logWarn("%s: batch of %d points failed (%v) – retrying individually", bw.name, len(points), batchErr)

	var dropped int
	for i, pt := range points {
		if err := writeWithRetry(ctx, bw.client, []*influxdb3.Point{pt}, maxAttempts); err != nil {
			// Include the tag values so the bad point is identifiable in logs.
			devID, _ := pt.GetTag("device_id")
			sensorType, _ := pt.GetTag("sensor_type")
			logError("%s: point %d/%d dropped permanently: device_id=%s sensor_type=%s error=%v",
				bw.name, i+1, len(points), devID, sensorType, err)
			dropped++
		}
	}
	if dropped > 0 {
		logError("%s: %d/%d points dropped from this flush", bw.name, dropped, len(points))
	}
}
