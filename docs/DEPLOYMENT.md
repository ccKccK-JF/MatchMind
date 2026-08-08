# Deployment

## Local Windows processes

Install Go 1.25 or newer, then run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\generate.ps1
go test ./...
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

The demo script builds all five executables, starts them in hidden processes,
waits for readiness, runs one complete 5v5 match, prints the result, and always
stops and waits for the processes. Logs and an assertion-backed JSON acceptance
report are written under `.cache/demo`.

To start processes manually, use this order:

```powershell
go run .\cmd\player-service
go run .\cmd\matchmaking-service
go run .\cmd\simulation-service
go run .\cmd\agent-service
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
service, Redis-backed active matchmaking queues, a PostgreSQL-backed Agent,
ten matching workers, and Prometheus. It enables a 50/50
greedy-versus-Beam A/B experiment by default;
override `MATCHMAKING_POLICY_MODE` with `greedy` or `beam` to pin one strategy.
Public endpoints:

- API: `http://localhost:8080`
- matchmaking metrics: `http://localhost:8082/metrics`
- Agent metrics: `http://localhost:8084/metrics`
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

Use a randomly generated `AGENT_CONTROL_TOKEN` outside local development and
provide the same value only to Matchmaking and Agent. The REST role headers are
a local/reference authorization boundary; place the API behind authenticated
identity middleware or a trusted gateway before production exposure.

## Agones allocation

The local backend is the default and advertises capacity from
`MATCHMAKING_LOCAL_REGION_CAPACITIES`. For Kubernetes/Agones, apply the
least-privilege service account and namespace-scoped permissions:

```powershell
kubectl apply -f .\deployments\agones\matchmaking-rbac.yaml
```

Run the Matchmaking Pod with `serviceAccountName: matchmind-matchmaking` and:

```text
MATCHMAKING_ALLOCATOR_BACKEND=agones
AGONES_NAMESPACE=matchmind
AGONES_API_URL=https://kubernetes.default.svc
```

Each Agones Fleet must carry `matchmind.dev/game=matchmind` and one regional
label such as `matchmind.dev/region=hongkong`, `tokyo`, or `singapore`.
MatchMind sums each region's `status.readyReplicas`, rejects zero-capacity or
latency-inadmissible regions, then creates an
`allocation.agones.dev/v1 GameServerAllocation` using the selected labels.
The in-cluster CA and bearer-token files are used by default; never enable
`AGONES_INSECURE_SKIP_TLS_VERIFY` outside disposable local clusters.
See the official [Agones GameServerAllocation specification](https://agones.dev/site/docs/reference/gameserverallocation/)
for Fleet selectors and returned connection fields.
