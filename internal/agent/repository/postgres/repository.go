package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentapp "github.com/ccKccK-JF/MatchMind/internal/agent/application"
	agentdomain "github.com/ccKccK-JF/MatchMind/internal/agent/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const runColumns = `
	id, agent_name, model, prompt_version, requested_by, input_json, output_json,
	policy_version, status, error_message, tool_calls, started_at, finished_at
`

const proposalColumns = `
	id, run_id, requested_by, base_policy_version, candidate_policy, rationale,
	risk_report, state, reviewer_id, review_reason, reviewed_at, experiment_id,
	activated_by, treatment_basis_points, assignment_salt, activated_at,
	rolled_back_by, rolled_back_at, created_at, updated_at
`

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) CreateRun(ctx context.Context, run agentdomain.AuditRun) error {
	toolCalls, err := json.Marshal(run.ToolCalls)
	if err != nil {
		return fmt.Errorf("marshal agent tool calls: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO agent_runs (
			id, agent_name, model, prompt_version, requested_by, input_json,
			status, tool_calls, started_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, run.ID, run.AgentName, run.Model, run.PromptVersion, run.RequestedBy,
		[]byte(run.InputJSON), string(run.Status), toolCalls, run.StartedAt)
	if err != nil {
		return repositoryWriteError("create agent run", err)
	}
	return nil
}

func (r *Repository) FailRun(ctx context.Context, run agentdomain.AuditRun) error {
	toolCalls, err := json.Marshal(run.ToolCalls)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE agent_runs SET status = $1, error_message = $2, tool_calls = $3, finished_at = $4
		WHERE id = $5 AND status = 'RUNNING'
	`, string(run.Status), run.ErrorMessage, toolCalls, run.FinishedAt, run.ID)
	if err != nil {
		return repositoryWriteError("fail agent run", err)
	}
	if tag.RowsAffected() != 1 {
		return agentapp.ErrRepositoryConflict
	}
	return nil
}

func (r *Repository) CompleteRun(
	ctx context.Context,
	run agentdomain.AuditRun,
	proposal agentdomain.PolicyProposal,
) error {
	toolCalls, err := json.Marshal(run.ToolCalls)
	if err != nil {
		return err
	}
	candidate, rationale, risk, err := encodeProposal(proposal)
	if err != nil {
		return err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin complete agent run: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE agent_runs SET output_json = $1, policy_version = $2, status = $3,
			tool_calls = $4, finished_at = $5
		WHERE id = $6 AND status = 'RUNNING'
	`, []byte(run.OutputJSON), run.PolicyVersion, string(run.Status), toolCalls, run.FinishedAt, run.ID)
	if err != nil {
		return repositoryWriteError("complete agent run", err)
	}
	if tag.RowsAffected() != 1 {
		return agentapp.ErrRepositoryConflict
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO policy_proposals (
			id, run_id, requested_by, base_policy_version, candidate_policy,
			rationale, risk_report, state, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, proposal.ID, proposal.RunID, proposal.RequestedBy, proposal.BasePolicyVersion,
		candidate, rationale, risk, string(proposal.State), proposal.CreatedAt, proposal.UpdatedAt)
	if err != nil {
		return repositoryWriteError("create policy proposal", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return repositoryWriteError("commit agent run and proposal", err)
	}
	return nil
}

func (r *Repository) GetRun(ctx context.Context, runID string) (agentdomain.AuditRun, error) {
	run, err := scanRun(r.pool.QueryRow(ctx, `SELECT `+runColumns+` FROM agent_runs WHERE id = $1`, strings.TrimSpace(runID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentdomain.AuditRun{}, agentapp.ErrRunNotFound
	}
	if err != nil {
		return agentdomain.AuditRun{}, fmt.Errorf("get agent run: %w", err)
	}
	return run, nil
}

func (r *Repository) ListRuns(ctx context.Context, limit int) ([]agentdomain.AuditRun, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+runColumns+` FROM agent_runs ORDER BY started_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list agent runs: %w", err)
	}
	defer rows.Close()
	var result []agentdomain.AuditRun
	for rows.Next() {
		run, scanErr := scanRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, run)
	}
	return result, rows.Err()
}

func (r *Repository) GetProposal(ctx context.Context, proposalID string) (agentdomain.PolicyProposal, error) {
	proposal, err := scanProposal(r.pool.QueryRow(ctx, `SELECT `+proposalColumns+` FROM policy_proposals WHERE id = $1`, strings.TrimSpace(proposalID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return agentdomain.PolicyProposal{}, agentapp.ErrProposalNotFound
	}
	if err != nil {
		return agentdomain.PolicyProposal{}, fmt.Errorf("get policy proposal: %w", err)
	}
	return proposal, nil
}

func (r *Repository) ListProposals(ctx context.Context, limit int) ([]agentdomain.PolicyProposal, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+proposalColumns+` FROM policy_proposals ORDER BY created_at DESC, id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list policy proposals: %w", err)
	}
	defer rows.Close()
	var result []agentdomain.PolicyProposal
	for rows.Next() {
		proposal, scanErr := scanProposal(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, proposal)
	}
	return result, rows.Err()
}

func (r *Repository) UpdateProposal(
	ctx context.Context,
	proposal agentdomain.PolicyProposal,
	expectedState agentdomain.ProposalState,
) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE policy_proposals SET
			state = $1, reviewer_id = $2, review_reason = $3, reviewed_at = $4,
			experiment_id = $5, activated_by = $6, treatment_basis_points = $7,
			assignment_salt = $8, activated_at = $9, rolled_back_by = $10,
			rolled_back_at = $11, updated_at = $12
		WHERE id = $13 AND state = $14
	`, string(proposal.State), nullableString(proposal.ReviewerID), nullableString(proposal.ReviewReason), nullableTime(proposal.ReviewedAt),
		nullableString(proposal.ExperimentID), nullableString(proposal.ActivatedBy), nullablePositiveInt(proposal.TreatmentBasisPoints),
		nullableString(proposal.AssignmentSalt), nullableTime(proposal.ActivatedAt), nullableString(proposal.RolledBackBy),
		nullableTime(proposal.RolledBackAt), proposal.UpdatedAt,
		proposal.ID, string(expectedState))
	if err != nil {
		return repositoryWriteError("update policy proposal", err)
	}
	if tag.RowsAffected() != 1 {
		return agentapp.ErrRepositoryConflict
	}
	return nil
}

func (r *Repository) RecoverIncompleteRuns(ctx context.Context, now time.Time) (int, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE agent_runs SET status = 'FAILED',
			error_message = 'Agent service restarted before the run completed',
			finished_at = $1
		WHERE status = 'RUNNING'
	`, now)
	if err != nil {
		return 0, repositoryWriteError("recover incomplete Agent runs", err)
	}
	return int(tag.RowsAffected()), nil
}

type rowScanner interface{ Scan(...any) error }

func scanRun(row rowScanner) (agentdomain.AuditRun, error) {
	var run agentdomain.AuditRun
	var input, output, toolCalls []byte
	var status string
	var policyVersion, errorMessage *string
	var finishedAt *time.Time
	if err := row.Scan(
		&run.ID, &run.AgentName, &run.Model, &run.PromptVersion, &run.RequestedBy,
		&input, &output, &policyVersion, &status, &errorMessage, &toolCalls,
		&run.StartedAt, &finishedAt,
	); err != nil {
		return agentdomain.AuditRun{}, err
	}
	run.InputJSON = string(input)
	run.OutputJSON = string(output)
	run.Status = agentdomain.RunStatus(status)
	if policyVersion != nil {
		run.PolicyVersion = *policyVersion
	}
	if errorMessage != nil {
		run.ErrorMessage = *errorMessage
	}
	if finishedAt != nil {
		run.FinishedAt = finishedAt.UTC()
	}
	if len(toolCalls) > 0 {
		if err := json.Unmarshal(toolCalls, &run.ToolCalls); err != nil {
			return agentdomain.AuditRun{}, fmt.Errorf("decode tool calls: %w", err)
		}
	}
	return run.Clone(), nil
}

func scanProposal(row rowScanner) (agentdomain.PolicyProposal, error) {
	var proposal agentdomain.PolicyProposal
	var candidate, rationale, risk []byte
	var state string
	var reviewerID, reviewReason, experimentID, activatedBy, assignmentSalt, rolledBackBy *string
	var treatmentBasisPoints *int
	var reviewedAt, activatedAt, rolledBackAt *time.Time
	if err := row.Scan(
		&proposal.ID, &proposal.RunID, &proposal.RequestedBy, &proposal.BasePolicyVersion,
		&candidate, &rationale, &risk, &state, &reviewerID, &reviewReason, &reviewedAt,
		&experimentID, &activatedBy, &treatmentBasisPoints, &assignmentSalt, &activatedAt, &rolledBackBy, &rolledBackAt,
		&proposal.CreatedAt, &proposal.UpdatedAt,
	); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	if err := json.Unmarshal(candidate, &proposal.CandidatePolicy); err != nil {
		return agentdomain.PolicyProposal{}, fmt.Errorf("decode candidate policy: %w", err)
	}
	if err := json.Unmarshal(rationale, &proposal.Rationale); err != nil {
		return agentdomain.PolicyProposal{}, fmt.Errorf("decode proposal rationale: %w", err)
	}
	if err := json.Unmarshal(risk, &proposal.RiskReport); err != nil {
		return agentdomain.PolicyProposal{}, fmt.Errorf("decode risk report: %w", err)
	}
	proposal.State = agentdomain.ProposalState(state)
	assignString(&proposal.ReviewerID, reviewerID)
	assignString(&proposal.ReviewReason, reviewReason)
	assignString(&proposal.ExperimentID, experimentID)
	assignString(&proposal.ActivatedBy, activatedBy)
	if treatmentBasisPoints != nil {
		proposal.TreatmentBasisPoints = *treatmentBasisPoints
	}
	assignString(&proposal.AssignmentSalt, assignmentSalt)
	assignString(&proposal.RolledBackBy, rolledBackBy)
	assignTime(&proposal.ReviewedAt, reviewedAt)
	assignTime(&proposal.ActivatedAt, activatedAt)
	assignTime(&proposal.RolledBackAt, rolledBackAt)
	return proposal.Clone(), nil
}

func encodeProposal(proposal agentdomain.PolicyProposal) ([]byte, []byte, []byte, error) {
	candidate, err := json.Marshal(proposal.CandidatePolicy)
	if err != nil {
		return nil, nil, nil, err
	}
	rationale, err := json.Marshal(proposal.Rationale)
	if err != nil {
		return nil, nil, nil, err
	}
	risk, err := json.Marshal(proposal.RiskReport)
	return candidate, rationale, risk, err
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullablePositiveInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func assignString(target *string, value *string) {
	if value != nil {
		*target = *value
	}
}

func assignTime(target *time.Time, value *time.Time) {
	if value != nil {
		*target = value.UTC()
	}
}

func repositoryWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "23505" || postgresError.Code == "40001" || postgresError.Code == "40P01") {
		return agentapp.ErrRepositoryConflict
	}
	return fmt.Errorf("%s: %w", operation, err)
}

var _ agentapp.Repository = (*Repository)(nil)
