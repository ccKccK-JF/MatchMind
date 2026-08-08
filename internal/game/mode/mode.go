package mode

import (
	"errors"
	"strings"
)

var ErrUnsupported = errors.New("unsupported game mode")

type ID string

const (
	Ranked5v5   ID = "ranked_5v5"
	Normal5v5   ID = "normal_5v5"
	Training5v5 ID = "training_5v5"
)

type Rules struct {
	ID         ID
	Rated      bool
	AllowsBots bool
}

var rulesByID = map[ID]Rules{
	Ranked5v5:   {ID: Ranked5v5, Rated: true},
	Normal5v5:   {ID: Normal5v5},
	Training5v5: {ID: Training5v5, AllowsBots: true},
}

func Parse(value string) (ID, error) {
	id := ID(strings.ToLower(strings.TrimSpace(value)))
	if _, exists := rulesByID[id]; !exists {
		return "", ErrUnsupported
	}
	return id, nil
}

func Get(value string) (Rules, error) {
	id, err := Parse(value)
	if err != nil {
		return Rules{}, err
	}
	return rulesByID[id], nil
}

func All() []Rules {
	return []Rules{rulesByID[Ranked5v5], rulesByID[Normal5v5], rulesByID[Training5v5]}
}
