.PHONY: build test vet lint clean run docker-build docker-up docker-down

build:
	go build -ldflags="-s -w" -o bot ./cmd/bot

test:
	go test ./... -count=1

vet:
	go vet ./...

lint: vet
	@echo "Lint passed"

clean:
	rm -f bot

run: build
	./bot

docker-build:
	docker build -t polybot .

docker-up:
	docker compose up -d

docker-down:
	docker compose down
