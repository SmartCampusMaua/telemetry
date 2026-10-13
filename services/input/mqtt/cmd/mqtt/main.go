package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	influxdb3 "github.com/InfluxCommunity/influxdb3-go/v2/influxdb3"
	MQTT "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"github.com/SmartCampusMaua/telemetry/packages/go-config"
	"github.com/SmartCampusMaua/telemetry/packages/go-data"
	mqtt "github.com/SmartCampusMaua/telemetry/services/input/mqtt/internal"
)

// ─────────────────────────────────────────────────────────────────────────────
// Multi-database InfluxDB3 client pool
// ─────────────────────────────────────────────────────────────────────────────

// DBClients holds four InfluxDB3 clients for different data streams.
// All clients connect to the same InfluxDB3 host with the same token,
// but write to different hardcoded databases — iot_sensors/audit_iot carry
// sensor data and its raw-payload audit trail, vehicle_telemetry/audit_vehicle
// are the equivalent pair for vehicle GPS/CAN data.
type DBClients struct {
	IoTSensors       *influxdb3.Client // iot_sensors — sensor_data
	AuditIoT         *influxdb3.Client // audit_iot — raw MQTT payload ingest
	VehicleTelemetry *influxdb3.Client // vehicle_telemetry — vehicle GPS/CAN data
	AuditVehicle     *influxdb3.Client // audit_vehicle — raw vehicle audit logs
}

func (c *DBClients) Close() {
	_ = c.IoTSensors.Close()
	_ = c.AuditIoT.Close()
	_ = c.VehicleTelemetry.Close()
	_ = c.AuditVehicle.Close()
}

// ─────────────────────────────────────────────────────────────────────────────
// MQTT handler + main
// ─────────────────────────────────────────────────────────────────────────────

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found; using environment variables")
	}

	// INFRA_CONFIG_FILE holds InfluxDB3 + MQTT connection details (host,
	// token, broker, credentials) — see packages/go-config's doc comment for
	// why this is a JSON file rather than env vars.
	INFRA_CONFIG_FILE := os.Getenv("INFRA_CONFIG_FILE")
	if INFRA_CONFIG_FILE == "" {
		INFRA_CONFIG_FILE = "infra.json"
	}
	infraCfg, err := config.LoadInfraConfig(INFRA_CONFIG_FILE)
	if err != nil {
		log.Fatalf("[infra] %v", err)
	}
	INFLUXDB_HOST := infraCfg.InfluxDB.Host
	INFLUXDB_TOKEN := infraCfg.InfluxDB.Token
	if INFLUXDB_HOST == "" || INFLUXDB_TOKEN == "" {
		log.Fatalf("[infra] %s: influxdb.orgs has no enabled entry with both a host and a token", INFRA_CONFIG_FILE)
	}

	// INFRA_CONFIG_REFRESH_SEC: how often to check INFRA_CONFIG_FILE for a
	// change — see infraChanged below for what happens when one is found.
	INFRA_CONFIG_REFRESH_SEC := 30
	if raw := os.Getenv("INFRA_CONFIG_REFRESH_SEC"); raw != "" {
		if v, convErr := strconv.Atoi(raw); convErr == nil && v > 0 {
			INFRA_CONFIG_REFRESH_SEC = v
		}
	}

	// REGISTRY_API_URL points at the SmartCampusMaua `api` repo's registry
	// endpoints (devices/assets/sensors/modbus-channels) — the source of
	// truth for all four now, replacing this repo's own JSON files. Reads
	// are open on that API; no token needed. Lives in infra.json
	// (registry.api_url) alongside the other external connection targets,
	// not an env var — see packages/go-config.
	REGISTRY_API_URL := infraCfg.Registry.APIURL
	DEVICE_REGISTRY_REFRESH_SEC := 30
	if raw := os.Getenv("DEVICE_REGISTRY_REFRESH_SEC"); raw != "" {
		if v, convErr := strconv.Atoi(raw); convErr == nil && v > 0 {
			DEVICE_REGISTRY_REFRESH_SEC = v
		}
	}
	// WRITE_FLUSH_INTERVAL_SEC/WRITE_BATCH_SIZE tune the data.BatchWriter
	// fan-in (see its doc comment) — how long a batch of points sits
	// buffered before being flushed, and how many points force an early flush.
	WRITE_FLUSH_INTERVAL_SEC := 5
	if raw := os.Getenv("WRITE_FLUSH_INTERVAL_SEC"); raw != "" {
		if v, convErr := strconv.Atoi(raw); convErr == nil && v > 0 {
			WRITE_FLUSH_INTERVAL_SEC = v
		}
	}
	WRITE_BATCH_SIZE := 5000
	if raw := os.Getenv("WRITE_BATCH_SIZE"); raw != "" {
		if v, convErr := strconv.Atoi(raw); convErr == nil && v > 0 {
			WRITE_BATCH_SIZE = v
		}
	}

	// Initialize all four InfluxDB3 clients on the same host+token.
	// Database names are hardcoded; each client writes to a different database.
	var clients DBClients

	// Create iot_sensors client
	clients.IoTSensors, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     INFLUXDB_HOST,
		Token:    INFLUXDB_TOKEN,
		Database: "iot_sensors",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create iot_sensors client: %v", err))
	}

	// Create audit_iot client
	clients.AuditIoT, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     INFLUXDB_HOST,
		Token:    INFLUXDB_TOKEN,
		Database: "audit_iot",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create audit_iot client: %v", err))
	}

	// Create vehicle_telemetry client
	clients.VehicleTelemetry, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     INFLUXDB_HOST,
		Token:    INFLUXDB_TOKEN,
		Database: "vehicle_telemetry",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create vehicle_telemetry client: %v", err))
	}

	// Create audit_vehicle client
	clients.AuditVehicle, err = influxdb3.New(influxdb3.ClientConfig{
		Host:     INFLUXDB_HOST,
		Token:    INFLUXDB_TOKEN,
		Database: "audit_vehicle",
	})
	if err != nil {
		panic(fmt.Sprintf("failed to create audit_vehicle client: %v", err))
	}

	defer clients.Close()
	log.Printf("InfluxDB3: org=%s host=%s databases=[iot_sensors, audit_iot, vehicle_telemetry, audit_vehicle] (all on same host+token)", infraCfg.InfluxDB.Org, INFLUXDB_HOST)

	// Exactly one goroutine each ever calls WritePoints against iot_sensors /
	// audit_iot from here on — see data.BatchWriter's doc comment for why
	// (every decode worker calling WritePoints directly, once per MQTT
	// message, fragmented InfluxDB3's Parquet files: confirmed via direct
	// inspection, 2-4 separate files per 10-minute window instead of one,
	// eventually exceeding InfluxDB3's file-scan cap on ordinary time-range
	// queries).
	flushInterval := time.Duration(WRITE_FLUSH_INTERVAL_SEC) * time.Second
	sensorWriter := data.NewBatchWriter(clients.IoTSensors, "sensor_data", flushInterval, WRITE_BATCH_SIZE)
	auditWriter := data.NewBatchWriter(clients.AuditIoT, "raw", flushInterval, WRITE_BATCH_SIZE)
	log.Printf("[main] batch writers started: flushInterval=%s batchSize=%d", flushInterval, WRITE_BATCH_SIZE)

	deviceMap, devEUIToDeviceID, err := mqtt.GetDevicesMap(REGISTRY_API_URL)
	if err != nil {
		panic(fmt.Sprintf("failed to load device registry (%s): %v", REGISTRY_API_URL, err))
	}
	log.Printf("Device registry: api=%s deviceIDs=%d devEUI mappings=%d", REGISTRY_API_URL, len(deviceMap), len(devEUIToDeviceID))

	// Asset registry is supplementary metadata (no decoder depends on it), so
	// a bad/unreachable registry API logs a warning and starts with no asset
	// validation rather than crashing the service like a bad device fetch would.
	validAssetIDs, assetErr := mqtt.GetAssetIDSet(REGISTRY_API_URL)
	if assetErr != nil {
		log.Printf("[registry] warning: failed to load asset registry (%s): %v", REGISTRY_API_URL, assetErr)
		validAssetIDs = map[string]bool{}
	}
	log.Printf("Asset registry: api=%s assets=%d", REGISTRY_API_URL, len(validAssetIDs))

	// Sensor and device-channel registries are supplementary metadata (no
	// decoder depends on them to produce a value — only to tag it with a
	// stable sensor_id), so a bad/unreachable fetch logs a warning and starts
	// with empty maps rather than crashing the service, mirroring assets.
	sensorMap, sensorErr := mqtt.GetSensorsMap(REGISTRY_API_URL)
	if sensorErr != nil {
		log.Printf("[registry] warning: failed to load sensor registry (%s): %v", REGISTRY_API_URL, sensorErr)
		sensorMap = map[string]mqtt.SensorConfig{}
	}
	mqtt.ValidateAssetReferences(deviceMap, sensorMap, validAssetIDs)
	deviceChannels, channelErr := mqtt.GetDeviceChannelsMap(REGISTRY_API_URL)
	if channelErr != nil {
		log.Printf("[registry] warning: failed to load device channel registry (%s): %v", REGISTRY_API_URL, channelErr)
		deviceChannels = map[string]mqtt.DeviceChannels{}
	}
	sensorIndex := mqtt.BuildSensorIndex(sensorMap)
	log.Printf("Sensor registry: api=%s sensors=%d index=%d", REGISTRY_API_URL, len(sensorMap), len(sensorIndex))
	log.Printf("Device channel registry: api=%s devicesWithChannels=%d", REGISTRY_API_URL, len(deviceChannels))

	var registryMu sync.RWMutex
	refreshTicker := time.NewTicker(time.Duration(DEVICE_REGISTRY_REFRESH_SEC) * time.Second)
	defer refreshTicker.Stop()
	go func() {
		for range refreshTicker.C {
			nextDeviceMap, nextDevEUIToDeviceID, reloadErr := mqtt.GetDevicesMap(REGISTRY_API_URL)
			if reloadErr != nil {
				log.Printf("[registry] reload failed: %v", reloadErr)
				continue
			}
			// Keep the last known-good asset id set on a transient asset
			// fetch failure, rather than wiping validation just because the
			// device fetch happened to succeed.
			if nextValidAssetIDs, assetReloadErr := mqtt.GetAssetIDSet(REGISTRY_API_URL); assetReloadErr != nil {
				log.Printf("[registry] asset reload failed, keeping previous asset id set: %v", assetReloadErr)
			} else {
				validAssetIDs = nextValidAssetIDs
			}

			// Same last-known-good behavior for sensors/modbus-channels.
			// Computed into local vars first so deviceChannels/sensorMap/sensorIndex
			// (all read by workers) flip together atomically under registryMu.
			nextSensorMap := sensorMap
			if m, sensorReloadErr := mqtt.GetSensorsMap(REGISTRY_API_URL); sensorReloadErr != nil {
				log.Printf("[registry] sensor registry reload failed, keeping previous sensor index: %v", sensorReloadErr)
			} else {
				nextSensorMap = m
			}
			mqtt.ValidateAssetReferences(nextDeviceMap, nextSensorMap, validAssetIDs)

			nextDeviceChannels := deviceChannels
			if m, channelReloadErr := mqtt.GetDeviceChannelsMap(REGISTRY_API_URL); channelReloadErr != nil {
				log.Printf("[registry] device channel registry reload failed, keeping previous channels: %v", channelReloadErr)
			} else {
				nextDeviceChannels = m
			}
			nextSensorIndex := mqtt.BuildSensorIndex(nextSensorMap)

			registryMu.Lock()
			deviceMap = nextDeviceMap
			devEUIToDeviceID = nextDevEUIToDeviceID
			sensorMap = nextSensorMap
			deviceChannels = nextDeviceChannels
			sensorIndex = nextSensorIndex
			registryMu.Unlock()
			log.Printf("[registry] reloaded: api=%s deviceIDs=%d devEUI mappings=%d assets=%d sensors=%d sensorIndex=%d devicesWithChannels=%d",
				REGISTRY_API_URL, len(nextDeviceMap), len(nextDevEUIToDeviceID), len(validAssetIDs), len(nextSensorMap), len(nextSensorIndex), len(nextDeviceChannels))
		}
	}()

	// infraChanged fires when INFRA_CONFIG_FILE's content changes on disk —
	// e.g. a Kubernetes ConfigMap volume update, which kubelet syncs into
	// the mounted directory on its own, with no pod restart and no `kubectl`
	// command needed on our end. InfluxDB3's clients and the MQTT
	// connection are live, stateful objects already built from the old
	// config by this point; rather than hot-swapping them in place (real
	// risk of a half-migrated connection), the simpler and safer move is to
	// drive this through the exact same graceful-shutdown path as
	// SIGTERM/connLost below — drain, flush, exit cleanly — and let
	// Kubernetes' restart policy bring the container back up, which re-runs
	// config.LoadInfraConfig from scratch with the new file.
	infraChanged := make(chan struct{}, 1)
	go func() {
		ticker := time.NewTicker(time.Duration(INFRA_CONFIG_REFRESH_SEC) * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			next, reloadErr := config.LoadInfraConfig(INFRA_CONFIG_FILE)
			if reloadErr != nil {
				log.Printf("[infra] reload check of %s failed, keeping current process: %v", INFRA_CONFIG_FILE, reloadErr)
				continue
			}
			if next != infraCfg {
				log.Printf("[infra] %s changed — shutting down so Kubernetes restarts the pod with the new config...", INFRA_CONFIG_FILE)
				select {
				case infraChanged <- struct{}{}:
				default:
				}
				return
			}
		}
	}()

	// connLost signals the shutdown goroutine below instead of calling
	// os.Exit directly (as this used to) — an immediate exit would now skip
	// draining the decode workers and flushing the batch writers, abandoning
	// up to a full flush interval's worth of buffered points. Buffered size
	// 1 + non-blocking send: only the first loss matters, Paho may report
	// more before the process actually exits.
	connLost := make(chan error, 1)

	id := uuid.New().String()
	mqttOpts := MQTT.NewClientOptions().
		AddBroker(infraCfg.MQTT.Broker).
		SetClientID("parse-lns-sub-" + id).
		SetUsername(infraCfg.MQTT.Username).
		SetPassword(infraCfg.MQTT.Password).
		SetConnectionLostHandler(func(_ MQTT.Client, err error) {
			log.Printf("MQTT connection lost: %v", err)
			select {
			case connLost <- err:
			default:
			}
		})

	incoming := make(chan [2]string, 256)
	mqttOpts.SetDefaultPublishHandler(func(_ MQTT.Client, msg MQTT.Message) {
		incoming <- [2]string{msg.Topic(), string(msg.Payload())}
	})

	mqttClient := MQTT.NewClient(mqttOpts)
	if tok := mqttClient.Connect(); tok.Wait() && tok.Error() != nil {
		panic(tok.Error())
	}
	log.Printf("MQTT: connected to %s", infraCfg.MQTT.Broker)

	// Subscribe to  device/DEVICE_ID/telemetry  or the broader wildcard below.
	subTopic := "device/+/telemetry"
	if tok := mqttClient.Subscribe(subTopic, 0, nil); tok.Wait() && tok.Error() != nil {
		log.Fatal(tok.Error())
	}
	log.Printf("MQTT: subscribed to %s", subTopic)

	// Graceful shutdown: on SIGINT/SIGTERM (or an unexpected MQTT connection
	// loss, see connLost above), stop new MQTT messages from arriving, let
	// already-queued ones drain through the decode workers, then flush
	// whatever the batch writers still have buffered — so neither a clean
	// shutdown (e.g. `docker stop`) nor a dropped connection ever silently
	// discards buffered points the way an untracked in-flight write would.
	//
	// shutdownDueToConnLoss is written here before close(incoming) and only
	// read after workerWG.Wait() returns below — safe without atomics, since
	// closing a channel is itself a happens-before synchronization point in
	// Go's memory model, and that close-then-drain chain is what makes
	// workerWG.Wait() return in the first place.
	var shutdownDueToConnLoss bool
	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case sig := <-shutdownSig:
			log.Printf("[main] received %s, shutting down: disconnecting MQTT, draining workers...", sig)
		case err := <-connLost:
			shutdownDueToConnLoss = true
			log.Printf("[main] MQTT connection lost (%v) — shutting down so the process can be restarted...", err)
		case <-infraChanged:
			log.Printf("[main] infra.json changed, shutting down: disconnecting MQTT, draining workers...")
		}
		mqttClient.Disconnect(1000)
		close(incoming)
	}()

	// WRITE_WORKERS controls how many goroutines process MQTT messages in parallel.
	// Each worker handles the full decode+write pipeline for one message.
	// Defaults to 2× GOMAXPROCS so InfluxDB3 HTTP latency is hidden behind concurrency.
	numWorkers := runtime.GOMAXPROCS(0) * 2
	if raw := os.Getenv("WRITE_WORKERS"); raw != "" {
		if v, convErr := strconv.Atoi(raw); convErr == nil && v > 0 {
			numWorkers = v
		}
	}
	log.Printf("[main] starting %d worker goroutines", numWorkers)

	// Worker pool ─────────────────────────────────────────────────────────────
	// Each worker drains from the shared incoming channel. The MQTT publish
	// handler never blocks as long as the channel has capacity (256 slots).
	// Workers share read-only access to registry maps (protected by registryMu)
	// and the influxdb3 clients (which are goroutine-safe).
	var workerWG sync.WaitGroup
	for range numWorkers {
		workerWG.Add(1)
		go func() {
			defer workerWG.Done()
			for msg := range incoming {
				topic, payload := msg[0], msg[1]

				// Extract topic identifier from device/IDENTIFIER/telemetry.
				// For custom provider IDENTIFIER is deviceID; for LNS providers it is devEUI.
				parts := strings.SplitN(topic, "/", 3)
				if len(parts) < 2 {
					log.Printf("[main] unexpected topic format: %s", topic)
					continue
				}
				topicIdentifier := parts[1]

				provider := mqtt.DetectProvider(payload)
				if provider == "" {
					registryMu.RLock()
					configFromTopic, exists := deviceMap[topicIdentifier]
					registryMu.RUnlock()
					if exists {
						provider = "custom"
						log.Printf("[provider] fallback to custom for known device_id=%s model=%s", topicIdentifier, configFromTopic.Model)
					} else if dev, undecrypted := mqtt.DiagnoseUndetectedEverynet(payload); undecrypted {
						log.Printf("[drop] everynet device=%s sent an uplink Everynet itself didn't decrypt (encrypted_payload present, no payload) — check this device's session keys/activation on Everynet's console; topic=%s", dev, topic)
						continue
					} else {
						log.Printf("[drop] cannot detect provider topic=%s", topic)
						continue
					}
				}

				registryMu.RLock()
				currentDeviceMap := deviceMap
				currentDevEUIToDeviceID := devEUIToDeviceID
				currentReg := mqtt.RegistrySnapshot{SensorIndex: sensorIndex, SensorMap: sensorMap, DeviceChannels: deviceChannels}
				registryMu.RUnlock()

				deviceID, devEUI, deviceConfig, known := mqtt.ResolveIncomingDevice(provider, topicIdentifier, currentDeviceMap, currentDevEUIToDeviceID)
				if !known {
					if provider == "custom" {
						log.Printf("[drop] unknown custom device_id=%s topic=%s", topicIdentifier, topic)
					} else {
						log.Printf("[drop] unknown LNS mapping dev_eui=%s provider=%s topic=%s", topicIdentifier, provider, topic)
					}
					continue
				}

				if devEUI != "" {
					log.Printf("[recv] topic=%s provider=%s device_id=%s dev_eui=%s model=%s", topic, provider, deviceID, devEUI, deviceConfig.Model)
				} else {
					log.Printf("[recv] topic=%s provider=%s device_id=%s model=%s", topic, provider, deviceID, deviceConfig.Model)
				}

				// Audit log and sensor decode+write both just enqueue onto their
				// own data.BatchWriter now (see its doc comment) — fast,
				// non-blocking channel sends, no longer worth a dedicated
				// goroutine each like when WriteAuditLog did a direct blocking
				// WritePoints call. That also avoids a real shutdown race: an
				// untracked `go` here could still be enqueueing after
				// workerWG.Wait() returns, racing against auditWriter.Close().
				auditPayload, auditDeviceID, auditAssetID, auditNow := payload, deviceID, deviceConfig.AssetID, time.Now()
				mqtt.WriteAuditLog(auditWriter, auditDeviceID, auditAssetID, auditPayload, auditNow)

				mqtt.ParseMsg(sensorWriter, provider, deviceID, devEUI, deviceConfig, payload, currentReg)
			}
		}()
	}
	workerWG.Wait()

	// All decode workers have exited (incoming is closed and drained), so
	// nothing can still be calling Enqueue — safe to close the batch writers
	// now. Close blocks until each has flushed whatever it still had buffered.
	log.Printf("[main] all workers drained, flushing batch writers...")
	sensorWriter.Close()
	auditWriter.Close()
	if shutdownDueToConnLoss {
		log.Printf("[main] shutdown complete (connection loss) — exiting non-zero so the process gets restarted")
		os.Exit(1)
	}
	log.Printf("[main] shutdown complete")
}
