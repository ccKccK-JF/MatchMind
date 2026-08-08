# Deployment

## Local Windows processes

Install Go 1.25 or newer, then run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\generate.ps1
go test ./...
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

The demo script builds all four executables, starts them in hidden processes,
waits for readiness, runs one complete 5v5 match, prints the result, and always
stops the processes. Logs are written under `.cache/demo`.

To start processes manually, use this order:

```powershell
go run .\cmd\player-service
go run .\cmd\matchmaking-service
go run .\cmd\simulation-service
go run .\cmd\api-service
```

## Docker Compose

```powershell
docker compose config
docker compose up --build -d
docker compose ps
Invoke-RestMethod http://localhost:8080/ready
```

The Compose stack runs PostgreSQL migrations, a PostgreSQL-backed Player
service, Redis-backed active matchmaking queues, ten matching workers, and
Prometheus. It enables a 50/50 greedy-versus-Beam A/B experiment by default;
override `MATCHMAKING_POLICY_MODE` with `greedy` or `beam` to pin one strategy.
Public endpoints:

- API: `http://localhost:8080`
- matchmaking metrics: `http://localhost:8082/metrics`
- Prometheus: `http://localhost:9090`

Stop the stack with:

```powershell
docker compose down
```

To remove the development database as well:

```powershell
docker compose down --volumes
```

Containers run as a non-root user, include health checks, and communicate on a
private Compose network. Configuration keys and defaults are listed in
`configs/.env.example`.
