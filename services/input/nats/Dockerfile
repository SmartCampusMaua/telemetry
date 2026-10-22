# Builds cmd/main.go (the NATS Core -> JetStream adapter/decoder -- see
# internal/adapter.go and internal/signals.go) as a static binary, then ships
# it in a minimal runtime image. Two stages so the final image doesn't carry
# the Go toolchain or module cache. Deliberately builds ./cmd only, not
# ./cmd/simulator -- the simulator is a dev/test tool, not part of the
# production service this image runs.
FROM golang:1.26.5-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/nats-adapter ./cmd

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/nats-adapter /usr/local/bin/nats-adapter
ENTRYPOINT ["nats-adapter"]
