.PHONY: run build clean

run:
	ALPACA_API_KEY=$(ALPACA_API_KEY) ALPACA_API_SECRET=$(ALPACA_API_SECRET) go run ./cmd/bot/

build:
	go build -o bin/bot ./cmd/bot/

clean:
	rm -rf bin/
