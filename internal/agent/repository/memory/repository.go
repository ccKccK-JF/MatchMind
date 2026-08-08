package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	agentapp "github.com/ccKccK-JF/MatchMind/internal/agent/application"
	agentdomain "github.com/ccKccK-JF/MatchMind/internal/agent/domain"
)

type Repository struct {
	mu        sync.RWMutex
	runs      map[string]agentdomain.AuditRun
	proposals map[string]agentdomain.PolicyProposal
}

func NewRepository() *Repository {
	return &Repository{
		runs:      make(map[string]agentdomain.AuditRun),
		proposals: make(map[string]agentdomain.PolicyProposal),
	}
}

func (r *Repository) CreateRun(ctx context.Context, run agentdomain.AuditRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runs[run.ID]; exists {
		return agentapp.ErrRepositoryConflict
	}
	r.runs[run.ID] = run.Clone()
	return nil
}

func (r *Repository) FailRun(ctx context.Context, run agentdomain.AuditRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.runs[run.ID]
	if !exists {
		return agentapp.ErrRunNotFound
	}
	if current.Status != agentdomain.RunStatusRunning || run.Status != agentdomain.RunStatusFailed {
		return agentapp.ErrRepositoryConflict
	}
	r.runs[run.ID] = run.Clone()
	return nil
}

func (r *Repository) CompleteRun(
	ctx context.Context,
	run agentdomain.AuditRun,
	proposal agentdomain.PolicyProposal,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.runs[run.ID]
	if !exists {
		return agentapp.ErrRunNotFound
	}
	if current.Status != agentdomain.RunStatusRunning || run.Status != agentdomain.RunStatusSucceeded ||
		proposal.RunID != run.ID {
		return agentapp.ErrRepositoryConflict
	}
	if _, duplicate := r.proposals[proposal.ID]; duplicate {
		return agentapp.ErrRepositoryConflict
	}
	r.runs[run.ID] = run.Clone()
	r.proposals[proposal.ID] = proposal.Clone()
	return nil
}

func (r *Repository) GetRun(ctx context.Context, runID string) (agentdomain.AuditRun, error) {
	if err := ctx.Err(); err != nil {
		return agentdomain.AuditRun{}, err
	}
	r.mu.RLock()
	run, exists := r.runs[strings.TrimSpace(runID)]
	r.mu.RUnlock()
	if !exists {
		return agentdomain.AuditRun{}, agentapp.ErrRunNotFound
	}
	return run.Clone(), nil
}

func (r *Repository) ListRuns(ctx context.Context, limit int) ([]agentdomain.AuditRun, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	result := make([]agentdomain.AuditRun, 0, len(r.runs))
	for _, run := range r.runs {
		result = append(result, run.Clone())
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].StartedAt.Equal(result[j].StartedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].StartedAt.After(result[j].StartedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *Repository) GetProposal(ctx context.Context, proposalID string) (agentdomain.PolicyProposal, error) {
	if err := ctx.Err(); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	r.mu.RLock()
	proposal, exists := r.proposals[strings.TrimSpace(proposalID)]
	r.mu.RUnlock()
	if !exists {
		return agentdomain.PolicyProposal{}, agentapp.ErrProposalNotFound
	}
	return proposal.Clone(), nil
}

func (r *Repository) ListProposals(ctx context.Context, limit int) ([]agentdomain.PolicyProposal, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	result := make([]agentdomain.PolicyProposal, 0, len(r.proposals))
	for _, proposal := range r.proposals {
		result = append(result, proposal.Clone())
	}
	r.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *Repository) UpdateProposal(
	ctx context.Context,
	proposal agentdomain.PolicyProposal,
	expectedState agentdomain.ProposalState,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	current, exists := r.proposals[proposal.ID]
	if !exists {
		return agentapp.ErrProposalNotFound
	}
	if current.State != expectedState {
		return agentapp.ErrRepositoryConflict
	}
	r.proposals[proposal.ID] = proposal.Clone()
	return nil
}

func (r *Repository) RecoverIncompleteRuns(ctx context.Context, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	recovered := 0
	for runID, run := range r.runs {
		if run.Status != agentdomain.RunStatusRunning {
			continue
		}
		if err := run.Fail("Agent service restarted before the run completed", nil, now); err != nil {
			return recovered, err
		}
		r.runs[runID] = run.Clone()
		recovered++
	}
	return recovered, nil
}

var _ agentapp.Repository = (*Repository)(nil)
