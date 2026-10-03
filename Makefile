.PHONY: dev mockup test build docker
-include .env
export COOKIE_SECURE CAPTURE_BUDGET_BYTES CAPTURE_ACCOUNT_BUDGET_BYTES CAPTURE_RESERVE_BYTES CAPTURE_RETENTION_DAYS

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
