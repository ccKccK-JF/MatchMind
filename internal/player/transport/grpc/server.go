package playergrpc

import (
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
)

// Server is the gRPC adapter for the player application.
// Business methods will be added after the domain and repository layers exist.
type Server struct {
	playerv1.UnimplementedPlayerServiceServer
}

func NewServer() *Server {
	return &Server{}
}
