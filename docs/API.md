# MatchMind HTTP API

The public API listens on `http://localhost:8080` by default. JSON field names
use `snake_case`. Internal service-to-service communication remains gRPC.

Every response includes an `X-Trace-ID` header. A caller may supply its own
value in the request header. Error responses use this shape:

```json
{
  "error": {
    "code": "not_found",
    "message": "ticket not found"
  }
}
```

## Players

Player creation is provided for local demos and API clients:

```http
POST /api/v1/players
Content-Type: application/json

{
  "id": "player-1001",
  "name": "Nova",
  "initial_rating": 1500,
  "preferred_roles": ["core", "support"],
  "home_region": "hongkong",
  "region_latency": {
    "hongkong": 32,
    "singapore": 48
  },
  "behavior_score": 95
}
```

Query the player's current rating and complete rating history:

```http
GET /api/v1/players/player-1001/rating
```

## Match tickets

Create an idempotent ticket:

```http
POST /api/v1/tickets
Content-Type: application/json
Idempotency-Key: create-player-1001-001

{
  "player_id": "player-1001",
  "mode": "ranked_5v5",
  "client_version": "1.0.0",
  "preferred_roles": ["core", "support"],
  "region_latency": {
    "hongkong": 32,
    "singapore": 48
  }
}
```

The key may alternatively be supplied as `idempotency_key` in the body.

```http
GET /api/v1/tickets/{ticket_id}
```

Cancellation requires the owning player and another idempotency key:

```http
DELETE /api/v1/tickets/{ticket_id}
X-Player-ID: player-1001
Idempotency-Key: cancel-player-1001-001
```

The same values may be passed as the `player_id` and `idempotency_key` query
parameters.

## Matches and simulation

```http
GET /api/v1/matches/{match_id}
```

Run a reproducible simulation by supplying a non-zero seed:

```http
POST /api/v1/matches/{match_id}/simulate
Content-Type: application/json

{
  "random_seed": 42
}
```

When omitted, the gateway generates a seed and returns it in the response.
Simulation is idempotent per match: a repeated call returns the stored result.

Run an offline batch without updating live Matches or player ratings:

```http
POST /api/v1/simulations/batch
Content-Type: application/json

{
  "concurrency": 8,
  "inputs": [
    {
      "case_id": "balanced-001",
      "random_seed": 42,
      "rating_a": 1500,
      "rating_b": 1500,
      "predicted_win_rate_a": 0.5,
      "role_score": 95,
      "latency_score": 90,
      "party_score": 100
    }
  ]
}
```

The batch accepts 1 to 10,000 cases, preserves input order, and returns every
simulation plus aggregate win-rate, duration, actual-quality, one-sided, AFK,
and surrender statistics. `concurrency` defaults to the available CPU count
and is capped at 64.

## Operations

The public gateway exposes:

- `GET /health` for process liveness;
- `GET /ready` for Player, Matchmaking, and Simulation gRPC readiness;
- `GET /metrics` for API Prometheus metrics.

The matchmaking process exposes its operational endpoint on port `8082`:

- `GET /health`;
- `GET /ready`;
- `GET /metrics`.

Its metrics include all required core names: `match_queue_size`,
`match_wait_seconds`, `match_attempt_total`, `match_success_total`,
`match_failure_total`, `match_quality_score`,
`ticket_reservation_conflict_total`, and `match_worker_duration_seconds`.
Greedy-versus-Beam comparison is exposed through
`match_team_formation_greedy_duration_seconds`,
`match_team_formation_beam_duration_seconds`,
`match_team_formation_greedy_quality_score`, and
`match_team_formation_beam_quality_score`.

Player and Simulation expose the same operational routes on ports `8081` and
`8083` respectively. Their gRPC health services remain available for internal
dependency checks.
