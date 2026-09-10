.PHONY: dev mockup test build docker
-include .env
export COOKIE_SECURE

dev:
	go run ./cmd/weazltunes
mockup:
	python3 -m http.server 4001 --bind 127.0.0.1 --directory mockup-ui
test:
	go test -race ./...
build:
	go build -trimpath -o bin/weazltunes ./cmd/weazltunes
docker:
	docker compose up --build -d
