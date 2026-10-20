package main

import (
	"strings"
	"testing"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
)

func TestResolveID(t *testing.T) {
	ids := []string{"aaaa1111-0000", "aaaa2222-0000", "bbbb3333-0000"}
	if got, err := resolveID("bbb", ids, "fleet"); err != nil || got != "bbbb3333-0000" {
		t.Errorf("unique prefix: %q %v", got, err)
	}
	if got, err := resolveID("aaaa1111-0000", ids, "fleet"); err != nil || got != "aaaa1111-0000" {
		t.Errorf("exact id: %q %v", got, err)
	}
	if _, err := resolveID("aaaa", ids, "fleet"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("ambiguous prefix should error, got %v", err)
	}
	if _, err := resolveID("zzz", ids, "fleet"); err == nil {
		t.Error("unknown prefix should error")
	}
	if _, err := resolveID("", ids, "fleet"); err == nil {
		t.Error("empty prefix should error")
	}
}

func TestRenderMap(t *testing.T) {
	g := &protoversev1.GalaxyState{
		Planets: []*protoversev1.Planet{
			{OwnerId: "me", Position: &protoversev1.Position{X: 0, Y: 0}},
			{OwnerId: "them", Position: &protoversev1.Position{X: 100, Y: 100}},
			{Position: &protoversev1.Position{X: 50, Y: 50}},
		},
		Fleets: []*protoversev1.Fleet{
			{OwnerId: "me", Position: &protoversev1.Position{X: 0, Y: 0}}, // on top of my planet
			{OwnerId: "them", Position: &protoversev1.Position{X: 100, Y: 0}},
		},
	}
	out := renderMap(g, "me", 100)
	for _, want := range []string{"f", "X", "o", "e"} {
		if !strings.Contains(out, want) {
			t.Errorf("map missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(strings.Split(out, "\n")[25], "P") {
		t.Error("fleet should be drawn over the planet it is parked on")
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 27 || len(lines[1]) != 52 {
		t.Errorf("unexpected map dimensions: %d lines, first row %d wide", len(lines), len(lines[1]))
	}
}

func TestShipSummary(t *testing.T) {
	f := &protoversev1.Fleet{Ships: []*protoversev1.Ship{{ShipType: "scout"}, {ShipType: "fighter"}, {ShipType: "scout"}}}
	if got := shipSummary(f); got != "2× scout, 1× fighter" {
		t.Errorf("got %q", got)
	}
}
