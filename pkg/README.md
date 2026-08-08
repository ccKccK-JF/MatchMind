# Public packages

MatchMind currently exposes versioned generated clients from `gen/go` and has
no additional hand-written public Go library. Domain and application packages
remain under `internal` so external programs cannot bypass service contracts.
