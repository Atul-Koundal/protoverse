package server

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Atul-Koundal/protoverse/internal/domain"
	"github.com/Atul-Koundal/protoverse/internal/events"
)

func TestValidateDisplayName(t *testing.T) {
	good := []string{"abc", "Alice", "star_lord", "Space Cadet 7", "a-b", "x_y_z", "ABCDEFGHIJKLMNOPQRSTUVWX"}
	bad := []string{"", "ab", " alice", "alice ", "-bob", "bob-", "has/slash", "emoji🚀name", "ABCDEFGHIJKLMNOPQRSTUVWXY", "semi;colon"}
	for _, n := range good {
		if err := ValidateDisplayName(n); err != nil {
			t.Errorf("%q should be valid: %v", n, err)
		}
	}
	for _, n := range bad {
		err := ValidateDisplayName(n)
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%q should be rejected with InvalidArgument, got %v", n, err)
		}
	}
}

func TestParseGameEventFiltersByPlayer(t *testing.T) {
	private := `{"type":"income","players":["alice"],"credits":42}`
	ev, err := parseGameEvent(private, "alice")
	if err != nil || ev == nil || ev.GetIncomeEarned().GetCredits() != 42 {
		t.Fatalf("alice should receive her income event: ev=%v err=%v", ev, err)
	}
	if ev, err := parseGameEvent(private, "bob"); err != nil || ev != nil {
		t.Errorf("bob must not receive alice's event: ev=%v err=%v", ev, err)
	}
	if ev, _ := parseGameEvent(`{"type":"tick","tick":7}`, "bob"); ev == nil || ev.GetTick().GetTick() != 7 {
		t.Error("tick heartbeat should reach everyone")
	}
	if ev, err := parseGameEvent(`{"type":"from_the_future"}`, "bob"); err != nil || ev != nil {
		t.Errorf("unknown event types are skipped silently: ev=%v err=%v", ev, err)
	}
	if _, err := parseGameEvent("garbage", "bob"); err == nil {
		t.Error("malformed payload should error")
	}
}

func TestToProtoEventCoversEveryType(t *testing.T) {
	for _, typ := range []string{
		events.TypeFleetMoved, events.TypeCombatResolved, events.TypeShipBuilt,
		events.TypePlanetColonized, events.TypeIncome, events.TypeActionFailed, events.TypeTick,
		events.TypePlanetBattle, events.TypePlanetFortified,
	} {
		if toProtoEvent(events.Event{Type: typ}) == nil {
			t.Errorf("event type %q has no gRPC mapping", typ)
		}
	}
}

func TestDescribeAction(t *testing.T) {
	cases := map[string]string{
		"move":     `{"fleet_id":"12345678-aaaa","dest_x":1.5,"dest_y":2}`,
		"attack":   `{"attacking_fleet_id":"12345678-aaaa","target_fleet_id":"87654321-bbbb"}`,
		"build":    `{"planet_id":"12345678-aaaa","ship_type":"scout","quantity":2}`,
		"colonize": `{"fleet_id":"12345678-aaaa","planet_id":"87654321-bbbb"}`,
		"invade":   `{"fleet_id":"12345678-aaaa","planet_id":"87654321-bbbb"}`,
		"fortify":  `{"planet_id":"87654321-bbbb"}`,
	}
	for typ, payload := range cases {
		got := describeAction(domain.Action{ActionType: typ, Payload: []byte(payload)})
		if got == "" || got == typ {
			t.Errorf("%s: description %q is not informative", typ, got)
		}
	}
	if got := describeAction(domain.Action{ActionType: "move", Payload: []byte("not json")}); got != "move" {
		t.Errorf("bad payload should fall back to the type, got %q", got)
	}
}
