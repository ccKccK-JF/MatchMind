# MatchMind

MatchMind is a Go-based 5v5 matchmaking and match-quality optimization
platform. The first version uses small, independently runnable services and
gRPC/Protocol Buffers for internal APIs.

## Services

| Service | Default address | Responsibility |
|---|---:|---|
| `player-service` | gRPC `:50051`, HTTP `:8081` | Player profiles and ratings |
| `matchmaking-service` | gRPC `:50052`, HTTP `:8082` | Tickets, queues, team formation, and matches |
| `simulation-service` | gRPC `:50053`, HTTP `:8083` | Deterministic match simulation |
| `agent-service` | gRPC `:50054`, HTTP `:8084` | Offline policy advice, risk review, approval audit, and controlled rollout |
| `api-service` | HTTP `:8080` | Public REST API and downstream readiness |

The four internal gRPC services expose standard gRPC health and reflection.
Every process exposes HTTP liveness/readiness/metrics endpoints.

## Development status

- The service process skeleton, Protobuf contracts, generation workflow,
  health checks, reflection, and graceful shutdown are complete.
- `player-service` supports player creation/query, configurable Elo updates,
  regional latency replacement, rating history, result idempotency, a
  concurrency-safe memory store, and an optional transactional PostgreSQL
  repository. Player rating views include recent Match IDs and current
  matchmaking status.
- `matchmaking-service` supports idempotent ticket create/cancel/query, strict
  ticket and match state machines, partitioned queues, dynamic rating windows,
  bounded wait-driven latency windows, time-based non-preferred-role
  relaxation, party-safe candidate selection, deterministic 5v5 team/role
  assignment, five-part quality scoring, atomic reservation, automatic
  workers, and match connection details. Ticket queues and Matches can use
  memory or PostgreSQL;
  the PostgreSQL path includes batch reservation, expiry recovery, durable
  Match snapshots, optimistic revisions, and atomic Match-ready/Ticket-assigned
  commits. Finishing a Match atomically releases each player's active-Ticket
  guard and local game-server capacity so the next session can start. Server
  selection scores every available region using average/max latency, team
  latency difference, variance, and live capacity. The local allocator is
  concurrency-safe, and the Agones adapter reads Fleet ready replicas and
  creates atomic `GameServerAllocation` resources through the Kubernetes API.
  The production queue adapter uses
  Redis sorted-set queues and Lua all-or-nothing reservation while PostgreSQL
  remains durable and can rebuild missing Redis state at startup. Team
  formation supports both the baseline greedy strategy and a deterministic
  Beam Search strategy, with stable player-level A/B assignment and persisted
  policy versions for later comparison. Finished Matches can be analyzed by
  policy, region, mode, and time range; historical Ticket snapshots can be
  replayed read-only through multiple policies to compare counterfactual team
  formation and predicted quality.
- `simulation-service` runs reproducible seeded matches, records process
  metrics, updates ranked Elo through `player-service`, and completes matches
  through `matchmaking-service`. Its offline batch API evaluates up to 10,000
  seeded cases with bounded concurrency without changing live Match or Elo
  state.
- `agent-service` has a fixed five-tool allowlist and cannot issue Shell or
  arbitrary SQL. It reads queue/policy/quality data, generates a structured
  candidate policy, replays historical Matches offline, and records fairness,
  latency, role-fill, sample-size, and high-rating-player risk findings. A
  separate reviewer must approve a passing proposal before an administrator
  can start a guarded rollout; activation and rollback are audited and
  retry-safe. Runs and proposals support memory or PostgreSQL storage.
- A full-service integration test proves the complete in-memory flow from ten
  players entering the queue through match completion, rating history,
  predicted-versus-actual quality analysis, Greedy/Beam historical replay, and
  an audited Agent proposal.
- `api-service` exposes the required REST routes, trace IDs, JSON error
  mapping, Ticket owner checks, role-gated Agent operations, health/readiness
  checks, and Prometheus-format API metrics.
- `matchmaking-service` exposes the required queue, wait, attempt, success,
  failure, quality, reservation-conflict, and worker-duration metrics on
  `:8082`.

The local process demo defaults to memory. Docker Compose runs Player, Elo,
Ticket, and Match durability on PostgreSQL plus Redis coordination. An external
message broker, distributed tracing backend, and durable telemetry storage
remain later milestones.

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

Install the pinned portable compiler and run the Windows race detector:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\install-race-tools.ps1
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\race.ps1
```

Run the complete five-process demo:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

Run the services in separate terminals, in dependency order:

```powershell
go run .\cmd\player-service
go run .\cmd\matchmaking-service
go run .\cmd\simulation-service
go run .\cmd\agent-service
go run .\cmd\api-service
```

The HTTP API is then available at `http://localhost:8080`. See
[`docs/API.md`](docs/API.md) for request examples. Matchmaking Prometheus
metrics are available at `http://localhost:8082/metrics`.

Runtime configuration:

| Variable | Default |
|---|---|
| `PLAYER_GRPC_ADDRESS` | `:50051` |
| `PLAYER_HTTP_ADDRESS` | `:8081` |
| `PLAYER_ELO_K_FACTOR` | `32` |
| `PLAYER_STORAGE_BACKEND` | `memory` |
| `POSTGRES_DSN` | `postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable` |
| `MATCHMAKING_GRPC_ADDRESS` | `:50052` |
| `MATCHMAKING_HTTP_ADDRESS` | `:8082` |
| `MATCHMAKING_WORKER_COUNT` | `1` |
| `MATCHMAKING_POLICY_MODE` | `beam` (`greedy`, `beam`, or `ab`) |
| `MATCHMAKING_BEAM_WIDTH` | `64` |
| `MATCHMAKING_AB_TREATMENT_BPS` | `5000` |
| `MATCHMAKING_AB_SALT` | `matchmind-team-formation-v2` |
| `MATCHMAKING_ALLOCATOR_BACKEND` | `local` (`local` or `agones`) |
| `MATCHMAKING_LOCAL_REGION_CAPACITIES` | `hongkong=100,singapore=100,tokyo=100` |
| `AGONES_API_URL` | `https://kubernetes.default.svc` |
| `AGONES_NAMESPACE` | `matchmind` |
| `AGONES_CA_FILE` | in-cluster service-account CA file |
| `AGONES_BEARER_TOKEN_FILE` | in-cluster service-account token file |
| `AGONES_HTTP_TIMEOUT_SECONDS` | `5` |
| `AGONES_GAME_LABEL_KEY` / `AGONES_GAME_LABEL_VALUE` | `matchmind.dev/game` / `matchmind` |
| `AGONES_REGION_LABEL_KEY` | `matchmind.dev/region` |
| `AGENT_CONTROL_TOKEN` | `matchmind-local-agent-control` (must match Agent and Matchmaking) |
| `MATCHMAKING_TICKET_STORAGE_BACKEND` | `memory` |
| `MATCHMAKING_MATCH_STORAGE_BACKEND` | Ticket backend (`postgres` when Ticket backend is `redis`) |
| `REDIS_ADDRESS` | `localhost:6379` |
| `REDIS_PASSWORD` | empty |
| `REDIS_DB` | `0` |
| `REDIS_KEY_PREFIX` | `matchmind` |
| `PLAYER_GRPC_TARGET` | `localhost:50051` |
| `SIMULATION_GRPC_ADDRESS` | `:50053` |
| `SIMULATION_HTTP_ADDRESS` | `:8083` |
| `AGENT_GRPC_ADDRESS` | `:50054` |
| `AGENT_HTTP_ADDRESS` | `:8084` |
| `AGENT_STORAGE_BACKEND` | `memory` |
| `AGENT_NAME` | `matchmind-policy-advisor` |
| `AGENT_MODEL` | `deterministic-policy-advisor-v1` |
| `AGENT_PROMPT_VERSION` | `matchmind-agent-v1` |
| `AGENT_DEFAULT_BASE_POLICY` | `v2-beam` |
| `MATCHMAKING_GRPC_TARGET` | `localhost:50052` |
| `SIMULATION_GRPC_TARGET` | `localhost:50053` |
| `AGENT_GRPC_TARGET` | `localhost:50054` |
| `API_HTTP_ADDRESS` | `:8080` |

## Documentation

- [System architecture](docs/ARCHITECTURE.md)
- [HTTP API](docs/API.md)
- [Deployment](docs/DEPLOYMENT.md)
- [Testing and load testing](docs/TESTING.md)
- [Reproducible demo](docs/DEMO.md)
- [Database design](docs/DATABASE.md)
- [Requirements traceability](docs/REQUIREMENTS_TRACEABILITY.md)
