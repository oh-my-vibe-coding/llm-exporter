BINARY=llm-exporter
MODULE=github.com/taosun/llm-exporter

.PHONY: build run clean test docker

build:
	go build -ldflags="-s -w" -o $(BINARY) ./cmd/llm-exporter

run: build
	./$(BINARY) --config config.example.yaml

test:
	go test ./...

clean:
	rm -f $(BINARY)

docker:
	docker build -t llm-exporter .

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down
