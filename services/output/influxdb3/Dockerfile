# Builds cmd/main.go (the JetStream -> InfluxDB3 telemetry writer) as a
# static binary, then ships it in a minimal runtime image. Two stages so the
# final image doesn't carry the Go toolchain or module cache.
FROM golang:1.26.5-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/influxdb3-writer ./cmd

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/influxdb3-writer /usr/local/bin/influxdb3-writer
ENTRYPOINT ["influxdb3-writer"]
