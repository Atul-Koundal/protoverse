package server_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/domain"
)

// giveShip inserts a ship straight into a fleet (bypassing building) so the
// test controls exactly how strong the assault force is.
func (h *harness) giveShip(fleetID, shipType string, attack, defense, hp int) {
	h.t.Helper()
	_, err := h.repo.Pool.Exec(context.Background(),
		`INSERT INTO ships (fleet_id, ship_type, attack, defense, hp, max_hp) VALUES ($1, $2, $3, $4, $5, $5)`,
		fleetID, shipType, attack, defense, hp)
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) placeFleet(fleetID string, x, y float64) {
	h.t.Helper()
	if _, err := h.repo.Pool.Exec(context.Background(), `UPDATE fleets SET pos_x = $1, pos_y = $2 WHERE id = $3`, x, y, fleetID); err != nil {
		h.t.Fatal(err)
	}
}

func TestInvadeAndCapturePlanetEndToEnd(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice, bob := h.register("alice"), h.register("bob")
	actx, bctx := withKey(ctx, alice.key), withKey(ctx, bob.key)

	aFleet := h.state(alice).GetFleets()[0]
	bobHome := h.state(bob).GetPlanets()[0]
	aliceHome := h.state(alice).GetPlanets()[0]

	// A neutral planet, for the "unowned" check.
	var neutral uuid.UUID
	if err := h.repo.Pool.QueryRow(ctx,
		`INSERT INTO planets (name, pos_x, pos_y, resources, production) VALUES ($1, 5, 5, 100, 1) RETURNING id`,
		"neutral-"+uuid.NewString()[:6]).Scan(&neutral); err != nil {
		t.Fatal(err)
	}

	// --- rules enforced when the order is placed ---
	_, err := h.client.InvadePlanet(actx, &protoversev1.InvadePlanetRequest{FleetId: aFleet.GetId(), PlanetId: aliceHome.GetId()})
	wantCode(t, err, codes.InvalidArgument, "invading your own planet")
	_, err = h.client.InvadePlanet(actx, &protoversev1.InvadePlanetRequest{FleetId: aFleet.GetId(), PlanetId: neutral.String()})
	wantCode(t, err, codes.FailedPrecondition, "invading a neutral planet")
	_, err = h.client.InvadePlanet(withKey(ctx, bob.key), &protoversev1.InvadePlanetRequest{FleetId: aFleet.GetId(), PlanetId: aliceHome.GetId()})
	wantCode(t, err, codes.NotFound, "using someone else's fleet")
	_, err = h.client.InvadePlanet(ctx, &protoversev1.InvadePlanetRequest{})
	wantCode(t, err, codes.Unauthenticated, "no key")

	// Sanctuary: Bob has exactly one planet, so it can't be invaded — even in range.
	h.placeFleet(aFleet.GetId(), bobHome.GetPosition().GetX()+1, bobHome.GetPosition().GetY())
	_, err = h.client.InvadePlanet(actx, &protoversev1.InvadePlanetRequest{FleetId: aFleet.GetId(), PlanetId: bobHome.GetId()})
	wantCode(t, err, codes.FailedPrecondition, "a last planet is protected")

	// Give Bob a second planet so the first becomes fair game.
	if _, err := h.repo.Pool.Exec(ctx,
		`INSERT INTO planets (owner_id, name, pos_x, pos_y, resources, production) VALUES ($1, $2, 99, 99, 1000, 5)`,
		bob.id, "bob-colony-"+uuid.NewString()[:6]); err != nil {
		t.Fatal(err)
	}

	// Out of range is rejected up front.
	h.placeFleet(aFleet.GetId(), 0, 0)
	if dx := bobHome.GetPosition().GetX(); dx > 12 || bobHome.GetPosition().GetY() > 12 {
		_, err = h.client.InvadePlanet(actx, &protoversev1.InvadePlanetRequest{FleetId: aFleet.GetId(), PlanetId: bobHome.GetId()})
		wantCode(t, err, codes.FailedPrecondition, "planet out of range")
	}

	// --- a real assault: overwhelming force plus a trooper ---
	h.placeFleet(aFleet.GetId(), bobHome.GetPosition().GetX()+1, bobHome.GetPosition().GetY())
	h.giveShip(aFleet.GetId(), "cruiser", 600, 40, 900)
	h.giveShip(aFleet.GetId(), "trooper", 1, 1, 30)

	if _, err := h.client.InvadePlanet(actx, &protoversev1.InvadePlanetRequest{FleetId: aFleet.GetId(), PlanetId: bobHome.GetId()}); err != nil {
		t.Fatalf("InvadePlanet: %v", err)
	}
	// One invasion per fleet at a time.
	_, err = h.client.InvadePlanet(actx, &protoversev1.InvadePlanetRequest{FleetId: aFleet.GetId(), PlanetId: bobHome.GetId()})
	wantCode(t, err, codes.FailedPrecondition, "second invasion from the same fleet")
	if got := len(h.state(alice).GetPendingActions()); got != 1 {
		t.Errorf("pending orders = %d, want 1", got)
	}

	h.advance()

	// The planet changed hands, weakened.
	var captured *protoversev1.Planet
	for _, p := range h.state(alice).GetPlanets() {
		if p.GetId() == bobHome.GetId() {
			captured = p
		}
	}
	if captured == nil {
		t.Fatalf("alice should now own bob's former homeworld; she owns %d planets", len(h.state(alice).GetPlanets()))
	}
	if captured.GetDefenseLevel() != int32(domain.CapturedLevel(domain.HomeDefenseLevel)) {
		t.Errorf("captured planet defense level = %d, want %d", captured.GetDefenseLevel(), domain.CapturedLevel(domain.HomeDefenseLevel))
	}
	if captured.GetShield() >= captured.GetMaxShield() {
		t.Error("a freshly captured planet should be below full shield")
	}
	for _, p := range h.state(bob).GetPlanets() {
		if p.GetId() == bobHome.GetId() {
			t.Error("bob should have lost the planet")
		}
	}
	if n := len(h.state(bob).GetPlanets()); n != 1 {
		t.Errorf("bob should keep his other planet, has %d", n)
	}

	// Captures count towards the leaderboard.
	lb, err := h.client.GetLeaderboard(ctx, &protoversev1.GetLeaderboardRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range lb.GetEntries() {
		if e.GetPlayerId() == alice.id {
			found = true
			if e.GetCaptures() != 1 {
				t.Errorf("alice captures = %d, want 1", e.GetCaptures())
			}
		}
	}
	if !found {
		t.Error("alice missing from leaderboard")
	}

	// Bob's fleet can't be given orders by Alice, and Bob can't invade back
	// into his own (former) planet — it's Alice's now, so it IS invadable. Just
	// confirm the API treats it as an enemy planet.
	bFleet := h.state(bob).GetFleets()[0]
	h.placeFleet(bFleet.GetId(), bobHome.GetPosition().GetX()+1, bobHome.GetPosition().GetY())
	if _, err := h.client.InvadePlanet(bctx, &protoversev1.InvadePlanetRequest{FleetId: bFleet.GetId(), PlanetId: bobHome.GetId()}); err != nil {
		t.Errorf("bob should be able to counter-invade the planet he lost: %v", err)
	}
}

func TestFortifyPlanetEndToEnd(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	alice, bob := h.register("alice"), h.register("bob")
	actx := withKey(ctx, alice.key)
	home := h.state(alice).GetPlanets()[0]

	if home.GetDefenseLevel() != domain.HomeDefenseLevel || home.GetShield() != int32(domain.MaxShield(domain.HomeDefenseLevel)) {
		t.Fatalf("homeworld should start at level %d with a full shield, got level %d shield %d",
			domain.HomeDefenseLevel, home.GetDefenseLevel(), home.GetShield())
	}

	cat, _ := h.client.GetCatalog(ctx, &protoversev1.GetCatalogRequest{})
	if cat.GetMaxDefenseLevel() != domain.MaxDefenseLevel || cat.GetFortifyBaseCost() != domain.FortifyCost(0) {
		t.Errorf("catalog defense info wrong: %v", cat)
	}
	hasTrooper := false
	for _, s := range cat.GetShips() {
		hasTrooper = hasTrooper || s.GetShipType() == "trooper"
	}
	if !hasTrooper {
		t.Error("catalog should offer troopers")
	}

	cost := domain.FortifyCost(domain.HomeDefenseLevel) // level 2 -> 3
	h.setCredits(alice, 0)
	_, err := h.client.FortifyPlanet(actx, &protoversev1.FortifyPlanetRequest{PlanetId: home.GetId()})
	wantCode(t, err, codes.FailedPrecondition, "fortify with no credits")

	_, err = h.client.FortifyPlanet(withKey(ctx, bob.key), &protoversev1.FortifyPlanetRequest{PlanetId: home.GetId()})
	wantCode(t, err, codes.NotFound, "fortifying someone else's planet")

	h.setCredits(alice, cost+50)
	if _, err := h.client.FortifyPlanet(actx, &protoversev1.FortifyPlanetRequest{PlanetId: home.GetId()}); err != nil {
		t.Fatalf("FortifyPlanet: %v", err)
	}
	if got := h.state(alice).GetCredits(); got != 50 {
		t.Errorf("credits after ordering = %d, want 50 (cost charged immediately)", got)
	}
	h.setCredits(alice, cost)
	_, err = h.client.FortifyPlanet(actx, &protoversev1.FortifyPlanetRequest{PlanetId: home.GetId()})
	wantCode(t, err, codes.FailedPrecondition, "second fortify while one is pending")
	if got := h.state(alice).GetCredits(); got != cost {
		t.Errorf("a rejected duplicate must not cost credits: %d != %d", got, cost)
	}

	h.advance()
	after := h.state(alice).GetPlanets()[0]
	if after.GetDefenseLevel() != int32(domain.HomeDefenseLevel+1) {
		t.Errorf("defense level = %d, want %d", after.GetDefenseLevel(), domain.HomeDefenseLevel+1)
	}
	if after.GetMaxShield() != int32(domain.MaxShield(domain.HomeDefenseLevel+1)) {
		t.Errorf("max shield = %d", after.GetMaxShield())
	}
	// The public map shows defenses too.
	g, _ := h.client.GetGalaxyState(ctx, &protoversev1.GetGalaxyStateRequest{})
	for _, p := range g.GetPlanets() {
		if p.GetId() == home.GetId() && p.GetDefenseLevel() != int32(domain.HomeDefenseLevel+1) {
			t.Errorf("galaxy map shows level %d", p.GetDefenseLevel())
		}
	}
}
