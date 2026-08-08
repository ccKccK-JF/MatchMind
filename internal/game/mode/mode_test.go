package mode

import (
	"errors"
	"testing"
)

func TestModeRules(t *testing.T) {
	ranked, err := Get(" RANKED_5V5 ")
	if err != nil || ranked.ID != Ranked5v5 || !ranked.Rated || ranked.AllowsBots {
		t.Fatalf("ranked rules = %+v, %v", ranked, err)
	}
	normal, err := Get(string(Normal5v5))
	if err != nil || normal.Rated || normal.AllowsBots {
		t.Fatalf("normal rules = %+v, %v", normal, err)
	}
	training, err := Get(string(Training5v5))
	if err != nil || training.Rated || !training.AllowsBots {
		t.Fatalf("training rules = %+v, %v", training, err)
	}
	if _, err := Get("custom"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("unsupported mode error = %v", err)
	}
	if len(All()) != 3 {
		t.Fatalf("mode count = %d", len(All()))
	}
}
