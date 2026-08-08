# API contracts

Versioned API source files live under `proto/matchmind`. Generated Go clients
and servers live under `gen/go/matchmind`; the public REST mapping is in
`internal/api/transport/http`. Keeping this directory as an index preserves
the delivery layout without duplicating the source-of-truth contracts.
