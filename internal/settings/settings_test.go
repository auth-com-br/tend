package settings

import (
	"testing"

	"github.com/auth-com-br/tend/internal/config"
)

// TestAChoiceStepsRoundItsRow: left and right go through a row's choices
// and wrap at either end, from the value the file has. If it regresses, the
// TUI and the desktop step the same setting to different values.
func TestAChoiceStepsRoundItsRow(t *testing.T) {
	row := Row{Choices: []Choice{{"a", "1"}, {"b", "2"}, {"c", "3"}}}
	for _, c := range []struct {
		current string
		delta   int
		want    string
	}{
		{"1", 1, "2"}, {"3", 1, "1"}, {"1", -1, "3"}, {"2", -1, "1"}, {"unknown", 1, "2"},
	} {
		if got := Next(row, c.current, c.delta); got.TOML != c.want {
			t.Errorf("from %s by %d: %s, want %s", c.current, c.delta, got.TOML, c.want)
		}
	}
}

// TestEveryRowReadsADefaultItOffers: each setting's current value, on a
// file that says nothing, is one of its own choices — otherwise the screen
// shows none selected and the first step jumps somewhere unexpected.
func TestEveryRowReadsADefaultItOffers(t *testing.T) {
	cfg := config.Defaults()
	for _, row := range Base {
		v := row.Value(cfg)
		found := false
		for _, c := range row.Choices {
			found = found || c.TOML == v
		}
		if !found {
			t.Errorf("%s reads %s by default, which is not one of its choices", row.Label, v)
		}
	}
}
