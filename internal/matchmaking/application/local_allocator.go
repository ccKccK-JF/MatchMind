package application

import (
	"context"
	"fmt"
	"strings"

	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
)

type LocalAllocator struct {
	tokenGenerator platformid.Generator
}

func NewLocalAllocator(tokenGenerator platformid.Generator) *LocalAllocator {
	if tokenGenerator == nil {
		tokenGenerator = platformid.UUID
	}
	return &LocalAllocator{tokenGenerator: tokenGenerator}
}

func (a *LocalAllocator) Allocate(ctx context.Context, matchID, region string) (Allocation, error) {
	if err := ctx.Err(); err != nil {
		return Allocation{}, err
	}
	token, err := a.tokenGenerator()
	if err != nil {
		return Allocation{}, err
	}
	region = strings.ToLower(strings.TrimSpace(region))
	return Allocation{
		Address: fmt.Sprintf("%s.game.matchmind.local:7000", region),
		Token:   token,
	}, nil
}
