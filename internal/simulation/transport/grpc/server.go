package simulationgrpc

import (
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
)

// Server is the gRPC adapter for the simulation application.
type Server struct {
	simulationv1.UnimplementedSimulationServiceServer
}

func NewServer() *Server {
	return &Server{}
}
