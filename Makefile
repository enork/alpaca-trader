.PHONY: run build test test-integration notify-test clean

-include .env
export

run:
	@go run ./cmd/bot/

build:
	@go build -o bin/bot ./cmd/bot/

test:
	@go test ./...

test-integration:
	@go test -v -tags integration -timeout 60s ./internal/trading/

notify-test:
	@go run ./cmd/notify-test/

clean:
	@rm -rf bin/
