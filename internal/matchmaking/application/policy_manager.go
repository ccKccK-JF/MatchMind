package application

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

var (
	ErrPolicyNotFound      = errors.New("match policy not found")
	ErrInvalidExperiment   = errors.New("invalid policy experiment")
	ErrExperimentActive    = errors.New("a policy experiment is already active")
	ErrExperimentNotActive = errors.New("policy experiment is not active")
)

type PolicyExperiment struct {
	ID                   string
	ControlVersion       string
	TreatmentVersion     string
	TreatmentBasisPoints int
	AssignmentSalt       string
	StartedAt            time.Time
}

type PolicySelection struct {
	Policy       domain.MatchPolicy
	ExperimentID string
	Variant      string
	Bucket       int
}

type PolicySelector interface {
	SelectPolicy(assignmentKey string) PolicySelection
	MaxCandidateLimit() int
}

type PolicyManager struct {
	mu             sync.RWMutex
	policies       map[string]domain.MatchPolicy
	defaultVersion string
	experiment     *PolicyExperiment
}

func NewPolicyManager(policies []domain.MatchPolicy, defaultVersion string) (*PolicyManager, error) {
	manager := &PolicyManager{policies: make(map[string]domain.MatchPolicy)}
	for _, policy := range policies {
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := manager.policies[policy.Version]; duplicate {
			return nil, domain.ErrInvalidPolicy
		}
		manager.policies[policy.Version] = policy
	}
	defaultVersion = strings.TrimSpace(defaultVersion)
	if _, exists := manager.policies[defaultVersion]; !exists {
		return nil, ErrPolicyNotFound
	}
	manager.defaultVersion = defaultVersion
	return manager, nil
}

func (m *PolicyManager) StartExperiment(experiment PolicyExperiment) error {
	experiment.ID = strings.TrimSpace(experiment.ID)
	experiment.ControlVersion = strings.TrimSpace(experiment.ControlVersion)
	experiment.TreatmentVersion = strings.TrimSpace(experiment.TreatmentVersion)
	experiment.AssignmentSalt = strings.TrimSpace(experiment.AssignmentSalt)
	if experiment.ID == "" || experiment.ControlVersion == experiment.TreatmentVersion ||
		experiment.TreatmentBasisPoints < 0 || experiment.TreatmentBasisPoints > 10000 ||
		experiment.AssignmentSalt == "" || experiment.StartedAt.IsZero() {
		return ErrInvalidExperiment
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.experiment != nil {
		return ErrExperimentActive
	}
	if _, exists := m.policies[experiment.ControlVersion]; !exists {
		return ErrPolicyNotFound
	}
	if _, exists := m.policies[experiment.TreatmentVersion]; !exists {
		return ErrPolicyNotFound
	}
	experiment.StartedAt = experiment.StartedAt.UTC()
	m.experiment = &experiment
	return nil
}

func (m *PolicyManager) StopExperiment(experimentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.experiment == nil || m.experiment.ID != strings.TrimSpace(experimentID) {
		return ErrExperimentNotActive
	}
	m.experiment = nil
	return nil
}

func (m *PolicyManager) SelectPolicy(assignmentKey string) PolicySelection {
	m.mu.RLock()
	defaultPolicy := m.policies[m.defaultVersion]
	if m.experiment == nil {
		m.mu.RUnlock()
		return PolicySelection{Policy: defaultPolicy, Variant: "default", Bucket: -1}
	}
	experiment := *m.experiment
	control := m.policies[experiment.ControlVersion]
	treatment := m.policies[experiment.TreatmentVersion]
	m.mu.RUnlock()

	bucket := experimentBucket(experiment, assignmentKey)
	if bucket < experiment.TreatmentBasisPoints {
		return PolicySelection{Policy: treatment, ExperimentID: experiment.ID, Variant: "treatment", Bucket: bucket}
	}
	return PolicySelection{Policy: control, ExperimentID: experiment.ID, Variant: "control", Bucket: bucket}
}

func (m *PolicyManager) MaxCandidateLimit() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	maximum := 0
	for _, policy := range m.policies {
		if policy.CandidateLimit > maximum {
			maximum = policy.CandidateLimit
		}
	}
	return maximum
}

func (m *PolicyManager) Policies() []domain.MatchPolicy {
	m.mu.RLock()
	result := make([]domain.MatchPolicy, 0, len(m.policies))
	for _, policy := range m.policies {
		result = append(result, policy)
	}
	m.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result
}

func (m *PolicyManager) ActiveExperiment() (PolicyExperiment, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.experiment == nil {
		return PolicyExperiment{}, false
	}
	return *m.experiment, true
}

func experimentBucket(experiment PolicyExperiment, assignmentKey string) int {
	digest := sha256.Sum256([]byte(experiment.AssignmentSalt + "\x00" + experiment.ID + "\x00" + strings.TrimSpace(assignmentKey)))
	return int(binary.BigEndian.Uint64(digest[:8]) % 10000)
}

var _ PolicySelector = (*PolicyManager)(nil)
