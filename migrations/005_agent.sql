CREATE TABLE IF NOT EXISTS agent_runs (
    id TEXT PRIMARY KEY,
    agent_name TEXT NOT NULL,
    model TEXT NOT NULL,
    prompt_version TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    input_json JSONB NOT NULL,
    output_json JSONB,
    policy_version TEXT,
    status TEXT NOT NULL CHECK (status IN ('RUNNING', 'SUCCEEDED', 'FAILED')),
    error_message TEXT,
    tool_calls JSONB NOT NULL DEFAULT '[]'::jsonb,
    started_at TIMESTAMPTZ NOT NULL,
    finished_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS policy_proposals (
    id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL UNIQUE REFERENCES agent_runs(id),
    requested_by TEXT NOT NULL,
    base_policy_version TEXT NOT NULL,
    candidate_policy JSONB NOT NULL,
    rationale JSONB NOT NULL,
    risk_report JSONB NOT NULL,
    state TEXT NOT NULL CHECK (state IN (
        'PENDING_APPROVAL', 'APPROVED', 'REJECTED', 'ACTIVATING',
        'ACTIVE', 'ROLLING_BACK', 'ROLLED_BACK'
    )),
    reviewer_id TEXT,
    review_reason TEXT,
    reviewed_at TIMESTAMPTZ,
    experiment_id TEXT,
    activated_by TEXT,
    treatment_basis_points INTEGER CHECK (treatment_basis_points BETWEEN 1 AND 10000),
    assignment_salt TEXT,
    activated_at TIMESTAMPTZ,
    rolled_back_by TEXT,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS agent_runs_started_idx
    ON agent_runs (started_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS policy_proposals_state_created_idx
    ON policy_proposals (state, created_at DESC, id DESC);
