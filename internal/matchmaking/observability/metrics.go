package observability

import "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"

type MatchmakingMetrics struct {
	queueSize           *metrics.Gauge
	waitSeconds         *metrics.Histogram
	matchAttempt        *metrics.Counter
	matchSuccess        *metrics.Counter
	matchFailure        *metrics.Counter
	qualityScore        *metrics.Histogram
	reservationConflict *metrics.Counter
	workerDuration      *metrics.Histogram
}

func NewMatchmakingMetrics(registry *metrics.Registry) *MatchmakingMetrics {
	return &MatchmakingMetrics{
		queueSize:           registry.NewGauge("match_queue_size", "Number of tickets currently eligible for matchmaking."),
		waitSeconds:         registry.NewHistogram("match_wait_seconds", "Ticket wait time for successful matches.", []float64{1, 5, 10, 20, 30, 60, 120, 300}),
		matchAttempt:        registry.NewCounter("match_attempt_total", "Total matchmaking formation attempts."),
		matchSuccess:        registry.NewCounter("match_success_total", "Total successfully created matches."),
		matchFailure:        registry.NewCounter("match_failure_total", "Total matchmaking attempts that did not create a match."),
		qualityScore:        registry.NewHistogram("match_quality_score", "Calculated match quality score on the 0-100 scale.", []float64{50, 60, 70, 75, 80, 85, 90, 95, 100}),
		reservationConflict: registry.NewCounter("ticket_reservation_conflict_total", "Total atomic ticket reservation conflicts."),
		workerDuration:      registry.NewHistogram("match_worker_duration_seconds", "Duration of one matchmaking worker iteration.", []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1}),
	}
}

func (m *MatchmakingMetrics) SetQueueSize(value int)              { m.queueSize.Set(float64(value)) }
func (m *MatchmakingMetrics) IncMatchAttempt()                    { m.matchAttempt.Inc() }
func (m *MatchmakingMetrics) IncMatchSuccess()                    { m.matchSuccess.Inc() }
func (m *MatchmakingMetrics) IncMatchFailure()                    { m.matchFailure.Inc() }
func (m *MatchmakingMetrics) IncReservationConflict()             { m.reservationConflict.Inc() }
func (m *MatchmakingMetrics) ObserveWaitSeconds(value float64)    { m.waitSeconds.Observe(value) }
func (m *MatchmakingMetrics) ObserveQualityScore(value float64)   { m.qualityScore.Observe(value) }
func (m *MatchmakingMetrics) ObserveWorkerDuration(value float64) { m.workerDuration.Observe(value) }
