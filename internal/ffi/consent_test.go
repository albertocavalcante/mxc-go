package ffi

import (
	"errors"
	"testing"
)

func TestDispatchNeverEscapes(t *testing.T) {
	cases := []struct {
		name string
		p    Presenter
		want int
	}{
		{"yes", func(string) (int, error) { return DecisionYes, nil }, DecisionYes},
		{"no", func(string) (int, error) { return DecisionNo, nil }, DecisionNo},
		{"dismissed", func(string) (int, error) { return DecisionDismissed, nil }, DecisionDismissed},
		{"error", func(string) (int, error) { return DecisionYes, errors.New("boom") }, DecisionError},
		{"panic", func(string) (int, error) { panic("boom") }, DecisionError},
		{"out of range", func(string) (int, error) { return 7, nil }, DecisionError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dispatch(c.p, `{}`); got != c.want {
				t.Fatalf("got %d (%s), want %d (%s)", got, decisionName(got), c.want, decisionName(c.want))
			}
		})
	}
}

func TestDispatchPassesPromptThrough(t *testing.T) {
	var seen string
	dispatch(func(js string) (int, error) { seen = js; return DecisionNo, nil }, `{"title":"x"}`)
	if seen != `{"title":"x"}` {
		t.Fatalf("seen=%q", seen)
	}
}
