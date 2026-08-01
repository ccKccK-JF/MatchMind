package engine

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

var (
	ErrNoQueuedTickets     = errors.New("no queued tickets")
	ErrInsufficientPlayers = errors.New("insufficient eligible players")
	ErrNoValidTeamSplit    = errors.New("no valid 5v5 team split")
)

type CandidateDecision struct {
	TicketID string
	Accepted bool
	Reason   string
}

type CandidateResult struct {
	Anchor      *domain.MatchTicket
	Tickets     []*domain.MatchTicket
	Decisions   []CandidateDecision
	RatingRange float64
}

type ticketGroup struct {
	key     string
	tickets []*domain.MatchTicket
}

func GenerateCandidates(tickets []*domain.MatchTicket, now time.Time, policy domain.MatchPolicy) (CandidateResult, error) {
	if err := policy.Validate(); err != nil {
		return CandidateResult{}, err
	}
	ordered := cloneAndSortTickets(tickets)
	anchor := oldestQueued(ordered)
	if anchor == nil {
		return CandidateResult{}, ErrNoQueuedTickets
	}
	ratingRange := policy.RatingRange(now.Sub(anchor.CreatedAt()))
	groups := groupTickets(ordered)
	moveAnchorGroupFirst(groups, anchor)

	result := CandidateResult{Anchor: anchor.Clone(), RatingRange: ratingRange}
	decisions := make(map[string]CandidateDecision, len(ordered))
	seenPlayers := make(map[string]string, len(ordered))
	for _, ticket := range ordered {
		if !ticket.IsQueueCandidate() {
			decisions[ticket.ID()] = rejected(ticket, "ticket_not_queued")
			continue
		}
		if previousTicketID, duplicate := seenPlayers[ticket.PlayerID()]; duplicate {
			decisions[ticket.ID()] = rejected(ticket, "duplicate_player")
			decisions[previousTicketID] = CandidateDecision{TicketID: previousTicketID, Reason: "duplicate_player"}
		} else {
			seenPlayers[ticket.PlayerID()] = ticket.ID()
		}
	}

	for _, group := range groups {
		reason := groupRejectionReason(group, anchor, ratingRange, decisions)
		if reason == "" && len(result.Tickets)+len(group.tickets) > policy.CandidateLimit {
			reason = "candidate_limit"
		}
		if reason != "" {
			for _, ticket := range group.tickets {
				if _, decided := decisions[ticket.ID()]; !decided {
					decisions[ticket.ID()] = rejected(ticket, reason)
				}
			}
			continue
		}
		for _, ticket := range group.tickets {
			result.Tickets = append(result.Tickets, ticket.Clone())
			decisions[ticket.ID()] = CandidateDecision{TicketID: ticket.ID(), Accepted: true, Reason: "accepted"}
		}
	}

	for _, ticket := range ordered {
		decision, exists := decisions[ticket.ID()]
		if !exists {
			decision = rejected(ticket, "not_selected")
		}
		result.Decisions = append(result.Decisions, decision)
	}
	return result, nil
}

func groupRejectionReason(
	group ticketGroup,
	anchor *domain.MatchTicket,
	ratingRange float64,
	decisions map[string]CandidateDecision,
) string {
	for _, ticket := range group.tickets {
		if decision, exists := decisions[ticket.ID()]; exists && !decision.Accepted {
			return decision.Reason
		}
		switch {
		case ticket.Mode() != anchor.Mode():
			return "mode_mismatch"
		case ticket.ClientVersion() != anchor.ClientVersion():
			return "client_version_mismatch"
		case ticket.Region() != anchor.Region():
			return "region_mismatch"
		case math.Abs(ticket.Rating()-anchor.Rating()) > ratingRange:
			return "rating_outside_window"
		}
	}
	return ""
}

func cloneAndSortTickets(tickets []*domain.MatchTicket) []*domain.MatchTicket {
	result := make([]*domain.MatchTicket, 0, len(tickets))
	for _, ticket := range tickets {
		if ticket != nil {
			result = append(result, ticket.Clone())
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].CreatedAt().Equal(result[j].CreatedAt()) {
			return result[i].ID() < result[j].ID()
		}
		return result[i].CreatedAt().Before(result[j].CreatedAt())
	})
	return result
}

func oldestQueued(tickets []*domain.MatchTicket) *domain.MatchTicket {
	for _, ticket := range tickets {
		if ticket.IsQueueCandidate() {
			return ticket
		}
	}
	return nil
}

func groupTickets(tickets []*domain.MatchTicket) []ticketGroup {
	groupIndexes := make(map[string]int)
	groups := make([]ticketGroup, 0, len(tickets))
	for _, ticket := range tickets {
		key := "ticket:" + ticket.ID()
		if ticket.PartyID() != "" {
			key = "party:" + ticket.PartyID()
		}
		index, exists := groupIndexes[key]
		if !exists {
			index = len(groups)
			groupIndexes[key] = index
			groups = append(groups, ticketGroup{key: key})
		}
		groups[index].tickets = append(groups[index].tickets, ticket)
	}
	return groups
}

func moveAnchorGroupFirst(groups []ticketGroup, anchor *domain.MatchTicket) {
	for index, group := range groups {
		for _, ticket := range group.tickets {
			if ticket.ID() == anchor.ID() {
				groups[0], groups[index] = groups[index], groups[0]
				return
			}
		}
	}
}

func rejected(ticket *domain.MatchTicket, reason string) CandidateDecision {
	return CandidateDecision{TicketID: ticket.ID(), Accepted: false, Reason: reason}
}
