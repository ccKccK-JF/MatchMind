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

## Historical analysis and replay

Analyze finished Matches, optionally filtering by policy, mode, server region,
and a half-open RFC3339 time range (`from <= created_at < to`):

```http
GET /api/v1/analytics/match-quality?policy_version=v2-beam&mode=ranked_5v5&server_region=hongkong&from=2026-08-01T00%3A00%3A00Z&to=2026-08-08T00%3A00%3A00Z&limit=100
```

The response includes per-Match predicted and actual quality, signed and
absolute error, predicted Team A win probability, observed outcome, and Brier
score. Policy summaries include mean quality error, win-probability Brier
score, win rate, duration, and one-sided/AFK/surrender rates. The default limit
is 100 finished Matches and the maximum is 1,000.

Replay a finished Match without reserving Tickets, creating a Match, or
changing Elo:

```http
POST /api/v1/matches/{match_id}/replay
Content-Type: application/json

{
  "policy_versions": ["v1-greedy", "v2-beam"]
}
```

With no `ticket_ids`, replay reconstructs the ten Ticket snapshots used by the
source Match. Supplying 10 to the selected policies' candidate limit allows a
larger historical candidate snapshot to be replayed. Each outcome reports
accepted/rejected counts, selected teams and roles, predicted quality and its
delta from the source, search diagnostics, and whether the team split and role
assignments match history. A failed counterfactual is returned as an outcome
with `matched: false`; replay never mutates persisted state.

## Agent policy workflow

Agent endpoints require `X-Operator-ID` and `X-Operator-Role`. The supported
reference roles are `analyst`, `reviewer`, and `admin`; the API derives every
audit identity from these headers rather than accepting it in JSON.

Create an offline analysis as an analyst or administrator:

```http
POST /api/v1/agent/runs
X-Operator-ID: analyst-1
X-Operator-Role: analyst
Content-Type: application/json

{
  "base_policy_version": "v2-beam",
  "mode": "ranked_5v5",
  "server_region": "hongkong",
  "historical_limit": 20
}
```

The response contains the completed run audit, every allowlisted tool call,
candidate policy and rationale, plus exactly five risk findings: fairness,
latency cap, role fill, sample size, and high-rating-player experience. This
operation only reads snapshots and runs historical replay; it cannot change
live matchmaking.

Runs and proposals can be inspected by all three roles:

```http
GET /api/v1/agent/runs?limit=20
GET /api/v1/agent/runs/{run_id}
GET /api/v1/agent/proposals?limit=20
GET /api/v1/agent/proposals/{proposal_id}
```

A reviewer or administrator who is not the requester may approve a proposal
only when no risk finding blocks it, or may reject it:

```http
POST /api/v1/agent/proposals/{proposal_id}/review
X-Operator-ID: reviewer-1
X-Operator-Role: reviewer
Content-Type: application/json

{
  "decision": "approve",
  "reason": "offline checks passed"
}
```

Only an administrator may activate an approved proposal or roll it back. Basis
points range from 1 to 10,000 and are persisted with the assignment salt so a
retry cannot change rollout semantics:

```http
POST /api/v1/agent/proposals/{proposal_id}/activate
X-Operator-ID: admin-1
X-Operator-Role: admin
Content-Type: application/json

{
  "treatment_basis_points": 1000,
  "assignment_salt": "guarded-rollout-2026-08"
}
```

```http
POST /api/v1/agent/proposals/{proposal_id}/rollback
X-Operator-ID: admin-1
X-Operator-Role: admin
```

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

Player, Simulation, and Agent expose the same operational routes on ports
`8081`, `8083`, and `8084` respectively. Agent metrics include run outcome and
duration plus proposal approval, rejection, activation, and rollback counters.
Their gRPC health services remain available for internal dependency checks.
