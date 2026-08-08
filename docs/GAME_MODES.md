# Game modes

MatchMind accepts exactly three versioned 5v5 modes. Mode identifiers are
normalized at the Ticket boundary and validated again when a Match is
restored, preventing unknown strings from bypassing rating or matchmaking
rules.

| Mode ID | Purpose | Matchmaking behavior | Ranked rating |
|---|---|---|---|
| `ranked_5v5` | Competitive queue | Uses the selected policy unchanged and keeps the strictest initial quality target | Updated after a finished Match |
| `normal_5v5` | Faster unranked play | 1.5x initial/maximum rating range, 2x rating expansion, half the role-relaxation delay, and a quality threshold reduced by 10 points | Never updated |
| `training_5v5` | Test and simulation sandbox | Accepts the full validated rating/latency range, immediately relaxes roles, and has no minimum quality threshold | Never updated |

All three modes still require ten unique eligible players for the live Ticket
flow, preserve parties, enforce one active Ticket per player, and use atomic
reservation. Training cases that need synthetic ratings, latency, seeds, or
large batches use the offline batch-simulation API; it never creates live
Tickets or changes player ratings. The mode registry marks training as the
only bot-capable mode, but the first-version backend does not silently invent
bots in a live player queue.

The policy version stored on a Match remains the selected algorithm version.
Mode tuning is derived deterministically from the stored Match mode in both
the real-time Worker and historical replay, so replay does not need a hidden
copy of runtime configuration.
