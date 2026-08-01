# MatchMind

MatchMind is a Go-based 5v5 matchmaking and match-quality optimization
platform. The first version uses small, independently runnable services and
gRPC/Protocol Buffers for internal APIs.

## Services

| Service | Default gRPC address | Responsibility |
|---|---:|---|
| `player-service` | `:50051` | Player profiles and ratings |
| `matchmaking-service` | `:50052` | Tickets, queues, team formation, and matches |
| `simulation-service` | `:50053` | Deterministic match simulation |

All services expose the standard gRPC health service and server reflection.

## Development status

- The service process skeleton, Protobuf contracts, generation workflow,
  health checks, reflection, and graceful shutdown are complete.
- `player-service` supports player creation/query, configurable Elo updates,
  rating history, result idempotency, and a concurrency-safe memory store.
- `matchmaking-service` supports idempotent ticket create/cancel/query, strict
  ticket and match state machines, partitioned queues, dynamic rating windows,
  party-safe candidate selection, deterministic 5v5 team/role assignment,
  five-part quality scoring, atomic reservation, automatic workers, and match
  connection details.
- `simulation-service` runs reproducible seeded matches, records process
  metrics, updates ranked Elo through `player-service`, and completes matches
  through `matchmaking-service`.
- A three-service integration test proves the complete in-memory flow from ten
  players entering the queue through match completion and rating history.

Current persistence is intentionally in memory. PostgreSQL, Redis, an external
REST gateway, and production telemetry remain later milestones.

## Architecture

```text
cmd/                 process entry points
proto/               source-of-truth Protobuf contracts
gen/go/              generated Go messages and gRPC stubs
internal/*            domain, application, and transport implementation
internal/platform/    shared process infrastructure
scripts/              repeatable local development commands
```

Business logic must not depend directly on gRPC, a database, or a message
broker. Transport and persistence packages adapt the domain/application layer
to external systems.

## Local setup on Windows

Install the code-generation tools:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-tools.ps1
```

If Go module downloads require the local Clash proxy shown in the development
environment:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-tools.ps1 -Proxy http://127.0.0.1:7897
```

Lint Protobuf contracts and generate Go code:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\generate.ps1
```

Download Go module dependencies and run tests:

```powershell
go mod tidy
go test ./...
```

The full test suite includes domain tests, repository concurrency tests, gRPC
status-code tests, and an in-memory end-to-end gRPC test.

Run the services in separate terminals, in dependency order:

```powershell
go run .\cmd\player-service
go run .\cmd\matchmaking-service
go run .\cmd\simulation-service
```

Runtime configuration:

| Variable | Default |
|---|---|
| `PLAYER_GRPC_ADDRESS` | `:50051` |
| `PLAYER_ELO_K_FACTOR` | `32` |
| `MATCHMAKING_GRPC_ADDRESS` | `:50052` |
| `PLAYER_GRPC_TARGET` | `localhost:50051` |
| `SIMULATION_GRPC_ADDRESS` | `:50053` |
| `MATCHMAKING_GRPC_TARGET` | `localhost:50052` |
