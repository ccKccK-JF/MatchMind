package memory

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type TicketStore struct {
	mu                sync.RWMutex
	tickets           map[string]*domain.MatchTicket
	activeByPlayer    map[string]string
	createIdempotency map[string]string
	cancelIdempotency map[string]string
	pools             map[domain.PoolKey][]string
	reservations      map[string][]string
}

func NewTicketStore() *TicketStore {
	return &TicketStore{
		tickets:           make(map[string]*domain.MatchTicket),
		activeByPlayer:    make(map[string]string),
		createIdempotency: make(map[string]string),
		cancelIdempotency: make(map[string]string),
		pools:             make(map[domain.PoolKey][]string),
		reservations:      make(map[string][]string),
	}
}

func (s *TicketStore) CreateQueued(ctx context.Context, ticket *domain.MatchTicket, idempotencyKey string) (*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	idempotencyKey = scopedKey(ticket.PlayerID(), idempotencyKey)
	if ticketID, exists := s.createIdempotency[idempotencyKey]; exists {
		return s.tickets[ticketID].Clone(), nil
	}
	if _, exists := s.tickets[ticket.ID()]; exists {
		return nil, application.ErrActiveTicketExists
	}
	if activeTicketID, exists := s.activeByPlayer[ticket.PlayerID()]; exists {
		active := s.tickets[activeTicketID]
		if active != nil && active.IsActive() {
			return nil, application.ErrActiveTicketExists
		}
	}
	if ticket.State() != domain.TicketStateQueued {
		return nil, domain.ErrInvalidTicket
	}

	stored := ticket.Clone()
	s.tickets[stored.ID()] = stored
	s.activeByPlayer[stored.PlayerID()] = stored.ID()
	s.createIdempotency[idempotencyKey] = stored.ID()
	key := poolKey(stored)
	s.pools[key] = append(s.pools[key], stored.ID())
	sort.SliceStable(s.pools[key], func(i, j int) bool {
		left := s.tickets[s.pools[key][i]]
		right := s.tickets[s.pools[key][j]]
		if left.CreatedAt().Equal(right.CreatedAt()) {
			return left.ID() < right.ID()
		}
		return left.CreatedAt().Before(right.CreatedAt())
	})
	return stored.Clone(), nil
}

func (s *TicketStore) Get(ctx context.Context, ticketID string) (*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	ticket, exists := s.tickets[strings.TrimSpace(ticketID)]
	s.mu.RUnlock()
	if !exists {
		return nil, application.ErrTicketNotFound
	}
	return ticket.Clone(), nil
}

func (s *TicketStore) GetActiveByPlayer(ctx context.Context, playerID string) (*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	playerID = strings.TrimSpace(playerID)
	s.mu.RLock()
	ticketID, exists := s.activeByPlayer[playerID]
	ticket := s.tickets[ticketID]
	s.mu.RUnlock()
	if !exists || ticket == nil || !ticket.IsActive() {
		return nil, application.ErrTicketNotFound
	}
	return ticket.Clone(), nil
}

func (s *TicketStore) Cancel(ctx context.Context, ticketID, playerID, idempotencyKey string, now time.Time) (*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ticketID = strings.TrimSpace(ticketID)
	playerID = strings.TrimSpace(playerID)

	s.mu.Lock()
	defer s.mu.Unlock()
	idempotencyKey = scopedKey(playerID, idempotencyKey)
	if previousTicketID, exists := s.cancelIdempotency[idempotencyKey]; exists {
		if previousTicketID != ticketID {
			return nil, application.ErrTicketForbidden
		}
		return s.tickets[previousTicketID].Clone(), nil
	}
	ticket, exists := s.tickets[ticketID]
	if !exists {
		return nil, application.ErrTicketNotFound
	}
	if ticket.PlayerID() != playerID {
		return nil, application.ErrTicketForbidden
	}
	previousKey := poolKey(ticket)
	if err := ticket.Cancel(now); err != nil {
		return nil, err
	}
	s.cancelIdempotency[idempotencyKey] = ticketID
	if s.activeByPlayer[playerID] == ticketID {
		delete(s.activeByPlayer, playerID)
	}
	s.removeFromPool(previousKey, ticketID)
	return ticket.Clone(), nil
}

func (s *TicketStore) QueueSnapshot(ctx context.Context, key domain.PoolKey, limit int) ([]*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := s.pools[key]
	if limit <= 0 || limit > len(ids) {
		limit = len(ids)
	}
	result := make([]*domain.MatchTicket, 0, limit)
	for _, ticketID := range ids {
		ticket := s.tickets[ticketID]
		if ticket != nil && ticket.IsQueueCandidate() {
			result = append(result, ticket.Clone())
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (s *TicketStore) PoolKeys(ctx context.Context) ([]domain.PoolKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	keys := make([]domain.PoolKey, 0, len(s.pools))
	for key, ticketIDs := range s.pools {
		for _, ticketID := range ticketIDs {
			if ticket := s.tickets[ticketID]; ticket != nil && ticket.IsQueueCandidate() {
				keys = append(keys, key)
				break
			}
		}
	}
	s.mu.RUnlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Mode != keys[j].Mode {
			return keys[i].Mode < keys[j].Mode
		}
		if keys[i].ClientVersion != keys[j].ClientVersion {
			return keys[i].ClientVersion < keys[j].ClientVersion
		}
		return keys[i].Region < keys[j].Region
	})
	return keys, nil
}

func (s *TicketStore) QueueSize(ctx context.Context) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, ticket := range s.tickets {
		if ticket != nil && ticket.IsQueueCandidate() {
			count++
		}
	}
	return count, nil
}

func (s *TicketStore) ReserveAll(
	ctx context.Context,
	ticketIDs []string,
	reservationID string,
	expiresAt, now time.Time,
) ([]*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" || len(ticketIDs) == 0 {
		return nil, application.ErrReservationConflict
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existingIDs, exists := s.reservations[reservationID]; exists {
		if !sameTicketIDs(existingIDs, ticketIDs) {
			return nil, application.ErrReservationConflict
		}
		return s.cloneTickets(existingIDs), nil
	}

	updated := make(map[string]*domain.MatchTicket, len(ticketIDs))
	seenPlayers := make(map[string]struct{}, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		if _, duplicate := updated[ticketID]; duplicate {
			return nil, application.ErrReservationConflict
		}
		ticket, exists := s.tickets[ticketID]
		if !exists || !ticket.IsQueueCandidate() {
			return nil, application.ErrReservationConflict
		}
		if _, duplicate := seenPlayers[ticket.PlayerID()]; duplicate {
			return nil, application.ErrReservationConflict
		}
		seenPlayers[ticket.PlayerID()] = struct{}{}
		clone := ticket.Clone()
		if err := clone.Reserve(reservationID, expiresAt, now); err != nil {
			return nil, application.ErrReservationConflict
		}
		updated[ticketID] = clone
	}
	for ticketID, ticket := range updated {
		s.tickets[ticketID] = ticket
	}
	s.reservations[reservationID] = append([]string(nil), ticketIDs...)
	return s.cloneTickets(ticketIDs), nil
}

func (s *TicketStore) ReleaseAll(ctx context.Context, reservationID string, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ticketIDs, exists := s.reservations[reservationID]
	if !exists {
		return application.ErrReservationConflict
	}
	updated := make(map[string]*domain.MatchTicket, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		ticket := s.tickets[ticketID].Clone()
		if err := ticket.ReleaseReservation(reservationID, now); err != nil {
			return application.ErrReservationConflict
		}
		updated[ticketID] = ticket
	}
	for ticketID, ticket := range updated {
		s.tickets[ticketID] = ticket
	}
	delete(s.reservations, reservationID)
	return nil
}

func (s *TicketStore) AssignAll(ctx context.Context, reservationID, matchID string, now time.Time) ([]*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ticketIDs, exists := s.reservations[reservationID]
	if !exists {
		return nil, application.ErrReservationConflict
	}
	updated := make(map[string]*domain.MatchTicket, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		ticket := s.tickets[ticketID].Clone()
		if err := ticket.Assign(reservationID, matchID, now); err != nil {
			return nil, application.ErrReservationConflict
		}
		updated[ticketID] = ticket
	}
	for ticketID, ticket := range updated {
		s.tickets[ticketID] = ticket
		s.removeFromPool(poolKey(ticket), ticketID)
	}
	delete(s.reservations, reservationID)
	return s.cloneTickets(ticketIDs), nil
}

func (s *TicketStore) CompleteAssignedTickets(ctx context.Context, matchID string, _ time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return domain.ErrInvalidMatch
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ticket := range s.tickets {
		if ticket.State() != domain.TicketStateAssigned || ticket.MatchID() != matchID {
			continue
		}
		if s.activeByPlayer[ticket.PlayerID()] == ticket.ID() {
			delete(s.activeByPlayer, ticket.PlayerID())
		}
	}
	return nil
}

func (s *TicketStore) ActiveTickets(ctx context.Context) ([]*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*domain.MatchTicket, 0)
	for _, ticket := range s.tickets {
		if ticket.State() == domain.TicketStateQueued || ticket.State() == domain.TicketStateReserved {
			result = append(result, ticket.Clone())
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt().Equal(result[j].CreatedAt()) {
			return result[i].ID() < result[j].ID()
		}
		return result[i].CreatedAt().Before(result[j].CreatedAt())
	})
	return result, nil
}

func (s *TicketStore) RecoverExpiredReservations(ctx context.Context, now time.Time) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recovered := 0
	for reservationID, ticketIDs := range s.reservations {
		if len(ticketIDs) == 0 {
			delete(s.reservations, reservationID)
			continue
		}
		first := s.tickets[ticketIDs[0]]
		if first == nil || first.ReservationExpiresAt().After(now) {
			continue
		}
		for _, ticketID := range ticketIDs {
			ticket := s.tickets[ticketID].Clone()
			if err := ticket.ReleaseExpiredReservation(now); err != nil {
				return recovered, application.ErrReservationConflict
			}
			s.tickets[ticketID] = ticket
			recovered++
		}
		delete(s.reservations, reservationID)
	}
	return recovered, nil
}

func (s *TicketStore) cloneTickets(ticketIDs []string) []*domain.MatchTicket {
	result := make([]*domain.MatchTicket, 0, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		if ticket := s.tickets[ticketID]; ticket != nil {
			result = append(result, ticket.Clone())
		}
	}
	return result
}

func (s *TicketStore) removeFromPool(key domain.PoolKey, ticketID string) {
	ids := s.pools[key]
	for index, id := range ids {
		if id == ticketID {
			s.pools[key] = append(ids[:index], ids[index+1:]...)
			return
		}
	}
}

func poolKey(ticket *domain.MatchTicket) domain.PoolKey {
	return domain.PoolKey{Mode: ticket.Mode(), ClientVersion: ticket.ClientVersion(), Region: ticket.Region()}
}

func scopedKey(ownerID, idempotencyKey string) string {
	return strings.TrimSpace(ownerID) + "\x00" + strings.TrimSpace(idempotencyKey)
}

func sameTicketIDs(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[string]int, len(left))
	for _, ticketID := range left {
		counts[ticketID]++
	}
	for _, ticketID := range right {
		counts[ticketID]--
		if counts[ticketID] < 0 {
			return false
		}
	}
	return true
}
