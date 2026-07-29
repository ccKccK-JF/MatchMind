package matchmakinggrpc

import (
	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
)

// Server is the gRPC adapter for the matchmaking application.
type Server struct {
	matchmakingv1.UnimplementedMatchmakingServiceServer
}

func NewServer() *Server {
	return &Server{}
}
