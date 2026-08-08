package application

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

var (
	ErrOperationsUnauthorized = errors.New("matchmaking operations authorization failed")
	ErrApprovalRequired       = errors.New("approved proposal id is required")
)

type QueueSizer interface {
	QueueSize(ctx context.Context) (int, error)
}

type OperationalSnapshot struct {
	QueueSize        int
	Policies         []domain.MatchPolicy
	ActiveExperiment *PolicyExperiment
}

type PolicyOperationsService struct {
	queue        QueueSizer
	policies     *PolicyManager
	controlToken string
	clock        func() time.Time
}

func NewPolicyOperationsService(
	queue QueueSizer,
	policies *PolicyManager,
	controlToken string,
	clock func() time.Time,
) (*PolicyOperationsService, error) {
	controlToken = strings.TrimSpace(controlToken)
	if queue == nil || policies == nil || controlToken == "" {
		return nil, ErrOperationsUnauthorized
	}
	if clock == nil {
		clock = time.Now
	}
	return &PolicyOperationsService{queue: queue, policies: policies, controlToken: controlToken, clock: clock}, nil
}

func (s *PolicyOperationsService) Snapshot(ctx context.Context) (OperationalSnapshot, error) {
	queueSize, err := s.queue.QueueSize(ctx)
	if err != nil {
		return OperationalSnapshot{}, err
	}
	result := OperationalSnapshot{QueueSize: queueSize, Policies: s.policies.Policies()}
	if experiment, active := s.policies.ActiveExperiment(); active {
		result.ActiveExperiment = &experiment
	}
	return result, nil
}

func (s *PolicyOperationsService) ActivateApprovedPolicy(
	_ context.Context,
	controlToken, approvalID string,
	policy domain.MatchPolicy,
	treatmentBasisPoints int,
	assignmentSalt string,
) (PolicyExperiment, error) {
	if !s.authorized(controlToken) {
		return PolicyExperiment{}, ErrOperationsUnauthorized
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return PolicyExperiment{}, ErrApprovalRequired
	}
	experiment := PolicyExperiment{
		ID: "approved-" + approvalID, ControlVersion: s.policies.DefaultPolicy().Version,
		TreatmentVersion: policy.Version, TreatmentBasisPoints: treatmentBasisPoints,
		AssignmentSalt: strings.TrimSpace(assignmentSalt), StartedAt: s.clock(),
	}
	if err := s.policies.ActivateApprovedPolicy(policy, experiment); err != nil {
		return PolicyExperiment{}, err
	}
	active, _ := s.policies.ActiveExperiment()
	return active, nil
}

func (s *PolicyOperationsService) RollbackExperiment(
	_ context.Context,
	controlToken, experimentID string,
) error {
	if !s.authorized(controlToken) {
		return ErrOperationsUnauthorized
	}
	err := s.policies.StopExperiment(experimentID)
	if errors.Is(err, ErrExperimentNotActive) {
		if _, active := s.policies.ActiveExperiment(); !active {
			return nil
		}
	}
	return err
}

func (s *PolicyOperationsService) authorized(candidate string) bool {
	expected := []byte(s.controlToken)
	actual := []byte(strings.TrimSpace(candidate))
	return len(expected) == len(actual) && subtle.ConstantTimeCompare(expected, actual) == 1
}
