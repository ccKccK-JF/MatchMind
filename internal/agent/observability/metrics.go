package observability

import (
	agentapp "github.com/ccKccK-JF/MatchMind/internal/agent/application"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
)

type Metrics struct {
	runSucceeded *platformmetrics.Counter
	runFailed    *platformmetrics.Counter
	runDuration  *platformmetrics.Histogram
	approved     *platformmetrics.Counter
	rejected     *platformmetrics.Counter
	activated    *platformmetrics.Counter
	rolledBack   *platformmetrics.Counter
}

func NewMetrics(registry *platformmetrics.Registry) *Metrics {
	return &Metrics{
		runSucceeded: registry.NewCounter("agent_run_success_total", "Successful Agent analysis runs."),
		runFailed:    registry.NewCounter("agent_run_failure_total", "Failed Agent analysis runs."),
		runDuration:  registry.NewHistogram("agent_run_duration_seconds", "Agent analysis run duration.", []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120}),
		approved:     registry.NewCounter("agent_proposal_approved_total", "Approved Agent policy proposals."),
		rejected:     registry.NewCounter("agent_proposal_rejected_total", "Rejected Agent policy proposals."),
		activated:    registry.NewCounter("agent_proposal_activated_total", "Activated approved policy proposals."),
		rolledBack:   registry.NewCounter("agent_proposal_rolled_back_total", "Rolled back policy proposals."),
	}
}

func (m *Metrics) IncRunSucceeded()             { m.runSucceeded.Inc() }
func (m *Metrics) IncRunFailed()                { m.runFailed.Inc() }
func (m *Metrics) ObserveRunDuration(v float64) { m.runDuration.Observe(v) }
func (m *Metrics) IncProposalApproved()         { m.approved.Inc() }
func (m *Metrics) IncProposalRejected()         { m.rejected.Inc() }
func (m *Metrics) IncProposalActivated()        { m.activated.Inc() }
func (m *Metrics) IncProposalRolledBack()       { m.rolledBack.Inc() }

var _ agentapp.Metrics = (*Metrics)(nil)
