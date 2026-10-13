# syntax=docker/dockerfile:1
#
# Build context is the monorepo root, invoked as
# `docker build -f services/bridge/chirpstackv4/Dockerfile -t telemetry-services-bridge-chirpstackv4 .`
# — this service depends on packages/go-config via the repo's go.work, so every
# COPY below is relative to the repo root, not this directory.
#
# Rather than COPYing the repo's own go.work (which `use`s every module in the
# monorepo, including services this image has nothing to do with — go build
# fails to load a workspace if any `use`d directory is missing), this stage
# writes its own minimal go.work listing only the two local modules this
# service actually imports. Keeps the build context — and the image's build
# cache invalidation — scoped to what this service needs.
ARG BUILDER_IMAGE=golang:1.24-alpine

############################
# STEP 1 build executable binary
############################
FROM ${BUILDER_IMAGE} AS builder

# ca-certificates for HTTPS (registry API), tzdata for timezone support.
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
COPY services/bridge/chirpstackv4 services/bridge/chirpstackv4

RUN printf 'go 1.26.5\n\nuse (\n\t./packages/go-config\n\t./services/bridge/chirpstackv4\n)\n' > go.work

WORKDIR /build/services/bridge/chirpstackv4

# Pure-Go build — no CGO needed.
RUN CGO_ENABLED=0 GOOS=linux go build \
  -ldflags='-w -s' \
  -o /go/bin/chirpstackv4 ./cmd/chirpstackv4/

############################
# STEP 2 minimal scratch image
############################
FROM scratch

COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /etc/passwd /etc/passwd
COPY --from=builder /etc/group /etc/group

COPY --from=builder /go/bin/chirpstackv4 /go/bin/chirpstackv4

# infra.json (MQTT broker creds + registry API URL) is not baked into the
# image — same resolution as the other Go services here: INFRA_CONFIG_FILE
# defaults to the relative path "infra.json", resolved against the
# container's working directory, and must be supplied at runtime (bind
# mount / k8s Secret volume). Override via the INFRA_CONFIG_FILE env var.

USER appuser:appuser

ENTRYPOINT ["/go/bin/chirpstackv4"]
