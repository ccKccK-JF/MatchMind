.PHONY: generate test race vet build demo load compose-up compose-down

generate:
	powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/generate.ps1

test:
	go test -count=1 ./...

race:
	powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/race.ps1

vet:
	go vet ./...

build:
	go build -o bin/player-service ./cmd/player-service
	go build -o bin/matchmaking-service ./cmd/matchmaking-service
	go build -o bin/simulation-service ./cmd/simulation-service
	go build -o bin/api-service ./cmd/api-service

demo:
	powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/demo.ps1

load:
	go run ./cmd/loadtest -rate 500 -duration 10m -max-tickets 100000 -concurrency 256

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down
