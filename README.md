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

Run a service:

```powershell
go run .\cmd\player-service
go run .\cmd\matchmaking-service
go run .\cmd\simulation-service
```
