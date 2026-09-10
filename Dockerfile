FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/weazltunes ./cmd/weazltunes
COPY internal/server ./internal/server
COPY web ./web
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/weazltunes ./cmd/weazltunes

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 weazl && adduser -D -u 10001 -G weazl weazl && mkdir /data && chown weazl:weazl /data
COPY --from=build /out/weazltunes /usr/local/bin/weazltunes
USER weazl
ENV LISTEN_ADDR=0.0.0.0:4000 DATA_DIR=/data
EXPOSE 4000
VOLUME /data
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s CMD wget -q -O /dev/null http://127.0.0.1:4000/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/weazltunes"]
