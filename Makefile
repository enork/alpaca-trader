.PHONY: run build clean

-include .env
export

run:
	@go run ./cmd/bot/

build:
	@go build -o bin/bot ./cmd/bot/

clean:
	@rm -rf bin/
