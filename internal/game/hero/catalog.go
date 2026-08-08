package hero

import (
	"errors"
	"sort"
	"strings"
)

var ErrUnknownRole = errors.New("unknown hero role")

type Hero struct {
	ID         string
	Name       string
	Role       string
	Difficulty int
	Offense    int
	Defense    int
	Control    int
	Teamwork   int
}

var catalog = []Hero{
	{ID: "ironwall", Name: "Ironwall", Role: "vanguard", Difficulty: 35, Offense: 45, Defense: 95, Control: 80, Teamwork: 85},
	{ID: "stormguard", Name: "Stormguard", Role: "vanguard", Difficulty: 65, Offense: 65, Defense: 85, Control: 75, Teamwork: 80},
	{ID: "shadowstep", Name: "Shadowstep", Role: "roamer", Difficulty: 80, Offense: 80, Defense: 35, Control: 70, Teamwork: 75},
	{ID: "pathfinder", Name: "Pathfinder", Role: "roamer", Difficulty: 55, Offense: 60, Defense: 55, Control: 75, Teamwork: 90},
	{ID: "starblade", Name: "Starblade", Role: "core", Difficulty: 75, Offense: 95, Defense: 40, Control: 45, Teamwork: 60},
	{ID: "emberlord", Name: "Emberlord", Role: "core", Difficulty: 60, Offense: 90, Defense: 50, Control: 55, Teamwork: 65},
	{ID: "windshot", Name: "Windshot", Role: "ranged", Difficulty: 50, Offense: 90, Defense: 30, Control: 50, Teamwork: 65},
	{ID: "arcweaver", Name: "Arcweaver", Role: "ranged", Difficulty: 85, Offense: 85, Defense: 35, Control: 75, Teamwork: 75},
	{ID: "lifebloom", Name: "Lifebloom", Role: "support", Difficulty: 45, Offense: 30, Defense: 50, Control: 65, Teamwork: 98},
	{ID: "chronowarden", Name: "Chronowarden", Role: "support", Difficulty: 80, Offense: 45, Defense: 45, Control: 95, Teamwork: 95},
}

var byID = func() map[string]Hero {
	result := make(map[string]Hero, len(catalog))
	for _, entry := range catalog {
		result[entry.ID] = entry
	}
	return result
}()

func All() []Hero {
	return append([]Hero(nil), catalog...)
}

func Get(id string) (Hero, bool) {
	entry, exists := byID[strings.ToLower(strings.TrimSpace(id))]
	return entry, exists
}

func BestForRole(proficiency map[string]float64, role string) (Hero, float64, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	candidates := make([]Hero, 0, 2)
	for _, entry := range catalog {
		if entry.Role == role {
			candidates = append(candidates, entry)
		}
	}
	if len(candidates) == 0 {
		return Hero{}, 0, ErrUnknownRole
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ID < candidates[j].ID })
	best := candidates[0]
	bestScore := 50.0
	if len(proficiency) > 0 {
		bestScore = proficiency[best.ID]
		for _, candidate := range candidates[1:] {
			score := proficiency[candidate.ID]
			if score > bestScore {
				best = candidate
				bestScore = score
			}
		}
	}
	return best, bestScore, nil
}
