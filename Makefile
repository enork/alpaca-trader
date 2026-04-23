.PHONY: run build clean

-include .env
export

run:
	@go run ./cmd/bot/

build:
	@go build -o bin/bot ./cmd/bot/

notify-test:
	@go run ./cmd/notify-test/

clean:
	@rm -rf bin/
