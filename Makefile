.PHONY: run build test test-integration notify-test report clean

REPORT_DAYS ?= 30

-include .env
export

run: build
	@go run ./cmd/bot/

build:
	go build -o bin/bot ./cmd/bot/

test:
	@go test ./...

test-integration:
	@go test -v -tags integration -timeout 60s ./internal/trading/

notify-test:
	@go run ./cmd/notify-test/

report:
	@go run ./cmd/report/ --days=$(REPORT_DAYS)

clean:
	@rm -rf bin/
