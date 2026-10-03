FROM golang:1.26-alpine@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/weazltunes ./cmd/weazltunes
COPY cmd/weazlfixture ./cmd/weazlfixture
COPY internal/testnav ./internal/testnav
COPY internal/testradio ./internal/testradio
COPY internal/server ./internal/server
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/weazltunes ./cmd/weazltunes
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/weazlfixture ./cmd/weazlfixture

FROM alpine:3.23@sha256:85fe1e81d6758c208f3e1eed4338a1997e19d4be002d4dd32d3100c9a8c010a0 AS runtime
# Pin the tested Alpine FFmpeg package; Go alone validates every outbound radio URL.
RUN apk add --no-cache ca-certificates ffmpeg=8.0.1-r1 fdkaac=1.0.6-r0 && addgroup -g 10001 weazl && adduser -D -u 10001 -G weazl weazl && mkdir /data && chown weazl:weazl /data
COPY --from=build /out/weazltunes /usr/local/bin/weazltunes
USER weazl
ENV LISTEN_ADDR=0.0.0.0:4000 DATA_DIR=/data
EXPOSE 4000
VOLUME /data
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD wget -q -O /dev/null http://127.0.0.1:4000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/weazltunes"]

FROM runtime AS fixture
COPY --from=build /out/weazlfixture /usr/local/bin/weazlfixture
ENTRYPOINT ["/usr/local/bin/weazlfixture"]

FROM runtime AS production
