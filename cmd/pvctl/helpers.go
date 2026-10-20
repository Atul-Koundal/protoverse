package main

import (
	"fmt"
	"strings"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
)

// resolveID expands a unique ID prefix (as printed by pvctl) into a full ID.
// kind is only used in error messages ("fleet", "planet").
func resolveID(prefix string, ids []string, kind string) (string, error) {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	if prefix == "" {
		return "", fmt.Errorf("missing %s id", kind)
	}
	var match []string
	for _, id := range ids {
		if id == prefix {
			return id, nil
		}
		if strings.HasPrefix(id, prefix) {
			match = append(match, id)
		}
	}
	switch len(match) {
	case 0:
		return "", fmt.Errorf("no %s matches %q", kind, prefix)
	case 1:
		return match[0], nil
	}
	return "", fmt.Errorf("%q is ambiguous: matches %d %ss, type more characters", prefix, len(match), kind)
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// renderMap draws the galaxy as ASCII. Legend:
//
//	. empty   o neutral planet   P your planet   X enemy planet
//	f your fleet   e enemy fleet (fleets draw over planets)
func renderMap(g *protoversev1.GalaxyState, myID string, size float64) string {
	const cols, rows = 50, 25
	grid := make([][]rune, rows)
	for r := range grid {
		grid[r] = []rune(strings.Repeat(".", cols))
	}
	cell := func(p *protoversev1.Position) (int, int) {
		c := int(p.GetX() / size * float64(cols))
		r := rows - 1 - int(p.GetY()/size*float64(rows))
		if c < 0 {
			c = 0
		}
		if c >= cols {
			c = cols - 1
		}
		if r < 0 {
			r = 0
		}
		if r >= rows {
			r = rows - 1
		}
		return r, c
	}
	for _, p := range g.GetPlanets() {
		r, c := cell(p.GetPosition())
		switch {
		case p.GetOwnerId() == myID:
			grid[r][c] = 'P'
		case p.GetOwnerId() != "":
			grid[r][c] = 'X'
		default:
			grid[r][c] = 'o'
		}
	}
	for _, f := range g.GetFleets() {
		r, c := cell(f.GetPosition())
		if f.GetOwnerId() == myID {
			grid[r][c] = 'f'
		} else if grid[r][c] != 'f' {
			grid[r][c] = 'e'
		}
	}
	var sb strings.Builder
	sb.WriteString("+" + strings.Repeat("-", cols) + "+\n")
	for _, row := range grid {
		sb.WriteString("|" + string(row) + "|\n")
	}
	sb.WriteString("+" + strings.Repeat("-", cols) + "+\n")
	sb.WriteString(". empty   o neutral planet   P your planet   X enemy planet   f your fleet   e enemy fleet\n")
	return sb.String()
}

// shipSummary turns a fleet's ships into e.g. "2× scout, 1× fighter".
func shipSummary(f *protoversev1.Fleet) string {
	counts := map[string]int{}
	var order []string
	for _, s := range f.GetShips() {
		if counts[s.GetShipType()] == 0 {
			order = append(order, s.GetShipType())
		}
		counts[s.GetShipType()]++
	}
	parts := make([]string, 0, len(order))
	for _, t := range order {
		parts = append(parts, fmt.Sprintf("%d× %s", counts[t], t))
	}
	return strings.Join(parts, ", ")
}
