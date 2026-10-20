package events

import "testing"

func TestRoundTrip(t *testing.T) {
	in := Event{Type: TypeCombatResolved, Players: []string{"a", "b"}, AttackingFleetID: "f1", TargetFleetID: "f2", DamageDealt: 12, Salvage: 75}
	b, err := in.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	out, err := Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != in.Type || out.DamageDealt != 12 || out.Salvage != 75 || len(out.Players) != 2 {
		t.Errorf("round trip mismatch: %+v", out)
	}
	if _, err := Parse("not json"); err == nil {
		t.Error("expected error for malformed payload")
	}
}

func TestVisibleTo(t *testing.T) {
	private := Event{Type: TypeIncome, Players: []string{"alice"}}
	if !private.VisibleTo("alice") || private.VisibleTo("bob") {
		t.Error("private event visibility wrong")
	}
	if !(Event{Type: TypeTick}).VisibleTo("anyone") {
		t.Error("broadcast event should be visible to everyone")
	}
}
