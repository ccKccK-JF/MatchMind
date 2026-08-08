# Reproducible demo

Run:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\demo.ps1
```

The script performs the following observable flow:

1. builds and starts Player, Matchmaking, Simulation, and API services;
2. waits until `GET /ready` confirms every gRPC dependency is serving;
3. creates ten players with balanced preferred roles;
4. creates ten idempotent ranked 5v5 Tickets;
5. waits for the workers to produce one READY Match;
6. queries predicted win rate and the five-part quality score;
7. simulates the Match with seed `42`;
8. queries a player's changed Elo and one rating-history entry;
9. compares predicted quality with the stored actual quality;
10. replays the historical Ticket snapshot through Greedy and Beam policies;
11. prints the Match, analysis, and replay summary, then terminates all demo
    processes.

Because the seed is fixed, the same input state produces the same simulation
result. API responses include an `X-Trace-ID`, and operational metrics remain
available during the run at ports `8080` and `8082`.
