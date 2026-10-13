# syntax=docker/dockerfile:1
#
# Build context is the monorepo root, invoked as
# `docker build -f services/input/mqtt/Dockerfile -t telemetry-services-input-mqtt .`
# — this service depends on packages/go-config, packages/go-data, and
# packages/go-parse via the repo's go.work, so every COPY below is relative
# to the repo root, not this directory.
#
# Rather than COPYing the repo's own go.work (which `use`s every module in
# the monorepo, including services this image has nothing to do with — go
# build fails to load a workspace if any `use`d directory is missing), this
# stage writes its own minimal go.work listing only the local modules this
# service actually imports. Keeps the build context — and the image's build
# cache invalidation — scoped to what this service needs.
ARG BUILDER_IMAGE=golang:1.24-alpine

############################
# STEP 1 build executable binary
############################
FROM ${BUILDER_IMAGE} AS builder

# ca-certificates for HTTPS, tzdata for timezone support.
RUN apk add --no-cache ca-certificates tzdata && update-ca-certificates

# golang:1.24-alpine pins GOTOOLCHAIN=local (the exact Go version baked into
# the image, for build reproducibility) — but the generated go.work below
# declares `go 1.26.5` to match this monorepo's modules. auto lets `go build`
# download and use that newer toolchain instead of refusing to build.
ENV GOTOOLCHAIN=auto

ENV USER=appuser
ENV UID=10001
RUN adduser \
  --disabled-password \
  --gecos "" \
  --home "/nonexistent" \
  --shell "/sbin/nologin" \
  --no-create-home \
  --uid "${UID}" \
  "${USER}"

WORKDIR /build

COPY packages/go-config packages/go-config
COPY packages/go-data packages/go-data
COPY packages/go-parse packages/go-parse
COPY services/input/mqtt services/input/mqtt

RUN printf 'go 1.26.5\n\nuse (\n\t./packages/go-config\n\t./packages/go-data\n\t./packages/go-parse\n\t./services/input/mqtt\n)\n' > go.work

WORKDIR /build/services/input/mqtt

# Pure-Go build — no CGO needed.
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags='-w -s' \
  -o /go/bin/mqtt ./cmd/mqtt/

############################
# STEP 2 minimal scratch image
############################
FROM scratch

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

COPY --from=builder /go/bin/mqtt /go/bin/mqtt

USER appuser:appuser

# REGISTRY_API_URL is read from infra.json (registry.api_url), not baked in
# here — see packages/go-config.
#
# infra.json (InfluxDB3 host/token, MQTT broker/credentials) is NOT baked
# into the image either — a fixed, well-known absolute path baked in via ENV
# below, with the actual file supplied at runtime (a volume mount — see
# infra/docker/docker-compose.yml, or a k8s ConfigMap/Secret volume). This is
# the one thing that IS baked in: a Kubernetes Deployment manifest never
# needs its own env/configMap entry for this path, only the volume mount
# itself — and that volume must be mounted as a whole directory, never
# subPath, which never updates in place on a ConfigMap change.
ENV INFRA_CONFIG_FILE=/etc/mqtt/infra.json

ENTRYPOINT ["/go/bin/mqtt"]
