package tickengine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/domain"
	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

// setupTestEngine connects to whatever DB_URL/REDIS_ADDR point at (same
// pattern as the repository package's integration tests) and skips cleanly
// if they're not set, so `go test ./...` still passes with no services
// running — only `go test ./... ` with env vars set exercises this.
func setupTestEngine(t *testing.T) (*Engine, *repository.Repository) {
	t.Helper()
	dbURL := os.Getenv("DB_URL")
	redisAddr := os.Getenv("REDIS_ADDR")
	if dbURL == "" || redisAddr == "" {
		t.Skip("DB_URL and REDIS_ADDR not set, skipping integration test")
	}

	ctx := context.Background()
	repo, err := repository.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	t.Cleanup(repo.Close)

	q := queue.New(redisAddr)
	t.Cleanup(func() { _ = q.Close() })

	return New(repo, q), repo
}

func newPlayer(t *testing.T, repo *repository.Repository, prefix string) *uuid.UUID {
	t.Helper()
	p, err := repo.CreatePlayer(context.Background(), prefix+"_"+uuid.NewString()[:8], uuid.NewString())
	if err != nil {
		t.Fatalf("CreatePlayer: %v", err)
	}
	return &p.ID
}

func credits(t *testing.T, repo *repository.Repository, id uuid.UUID) int64 {
	t.Helper()
	p, err := repo.GetPlayerByID(context.Background(), id)
	if err != nil || p == nil {
		t.Fatalf("GetPlayerByID: %v", err)
	}
	return p.Credits
}

func giveCredits(t *testing.T, repo *repository.Repository, id uuid.UUID, amount int64) {
	t.Helper()
	if _, err := repo.Pool.Exec(context.Background(), `UPDATE players SET credits = $1 WHERE id = $2`, amount, id); err != nil {
		t.Fatalf("giveCredits: %v", err)
	}
}

func insertPlanet(t *testing.T, repo *repository.Repository, owner *uuid.UUID, x, y float64, resources int64, production int) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := repo.Pool.QueryRow(context.Background(),
		`INSERT INTO planets (owner_id, name, pos_x, pos_y, resources, production)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		owner, "test-"+uuid.NewString()[:8], x, y, resources, production).Scan(&id)
	if err != nil {
		t.Fatalf("insertPlanet: %v", err)
	}
	return id
}

func actionStatus(t *testing.T, repo *repository.Repository, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := repo.Pool.QueryRow(context.Background(), `SELECT status FROM actions WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("actionStatus: %v", err)
	}
	return s
}

func TestEngine_ResolvesMoveAction(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	player := newPlayer(t, repo, "move_test")
	fleet, err := repo.CreateFleet(ctx, *player, 0, 0)
	if err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}

	payload := []byte(`{"fleet_id": "` + fleet.ID.String() + `", "dest_x": 42, "dest_y": 99}`)
	action, err := repo.CreateAction(ctx, *player, "move", payload, time.Now())
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}

	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	updated, err := repo.GetFleetWithShips(ctx, fleet.ID)
	if err != nil {
		t.Fatalf("GetFleetWithShips: %v", err)
	}
	if updated.PosX != 42 || updated.PosY != 99 {
		t.Errorf("expected fleet at (42, 99), got (%v, %v)", updated.PosX, updated.PosY)
	}
	if updated.DestX != nil || updated.ArrivesAt != nil {
		t.Error("arrived fleet should no longer have a destination")
	}
}

func TestEngine_MoveOfDestroyedFleetFails(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	player := newPlayer(t, repo, "ghost")
	payload := []byte(`{"fleet_id": "` + uuid.NewString() + `", "dest_x": 1, "dest_y": 1}`)
	action, err := repo.CreateAction(ctx, *player, "move", payload, time.Now())
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	if s := actionStatus(t, repo, action.ID); s != "failed" {
		t.Errorf("status = %q, want failed", s)
	}
}

func TestEngine_ResolvesAttackAction(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	attackerPlayer := newPlayer(t, repo, "atk_test")
	defenderPlayer := newPlayer(t, repo, "def_test")

	attackerFleet, _ := repo.CreateFleet(ctx, *attackerPlayer, 0, 0)
	defenderFleet, _ := repo.CreateFleet(ctx, *defenderPlayer, 5, 5)

	if _, err := repo.AddShip(ctx, attackerFleet.ID, "cruiser", 20, 0, 50); err != nil {
		t.Fatalf("AddShip (attacker): %v", err)
	}
	if _, err := repo.AddShip(ctx, defenderFleet.ID, "cruiser", 0, 5, 30); err != nil {
		t.Fatalf("AddShip (defender): %v", err)
	}

	payload := []byte(`{"attacking_fleet_id": "` + attackerFleet.ID.String() + `", "target_fleet_id": "` + defenderFleet.ID.String() + `"}`)
	action, err := repo.CreateAction(ctx, *attackerPlayer, "attack", payload, time.Now())
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}

	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	updatedDefender, err := repo.GetFleetWithShips(ctx, defenderFleet.ID)
	if err != nil {
		t.Fatalf("GetFleetWithShips: %v", err)
	}
	if len(updatedDefender.Ships) != 1 {
		t.Fatalf("expected defender fleet to still exist with 1 ship, got %d ships", len(updatedDefender.Ships))
	}
	if updatedDefender.Ships[0].HP >= 30 {
		t.Errorf("expected defender ship HP to have dropped below 30, got %d", updatedDefender.Ships[0].HP)
	}
}

func TestEngine_AttackOutOfRangeFailsWithoutDamage(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	a, d := newPlayer(t, repo, "far_a"), newPlayer(t, repo, "far_d")
	af, _ := repo.CreateFleet(ctx, *a, 0, 0)
	df, _ := repo.CreateFleet(ctx, *d, 50, 50)
	_, _ = repo.AddShip(ctx, af.ID, "cruiser", 50, 0, 100)
	_, _ = repo.AddShip(ctx, df.ID, "cruiser", 0, 0, 30)

	payload := []byte(`{"attacking_fleet_id": "` + af.ID.String() + `", "target_fleet_id": "` + df.ID.String() + `"}`)
	action, _ := repo.CreateAction(ctx, *a, "attack", payload, time.Now())
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	if s := actionStatus(t, repo, action.ID); s != "failed" {
		t.Errorf("status = %q, want failed", s)
	}
	after, _ := repo.GetFleetWithShips(ctx, df.ID)
	if after.Ships[0].HP != 30 {
		t.Errorf("out-of-range attack must not deal damage, defender HP = %d", after.Ships[0].HP)
	}
}

func TestEngine_AttackCannotTargetOwnFleet(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "self")
	f1, _ := repo.CreateFleet(ctx, *p, 0, 0)
	f2, _ := repo.CreateFleet(ctx, *p, 1, 1)
	_, _ = repo.AddShip(ctx, f1.ID, "cruiser", 50, 0, 100)
	_, _ = repo.AddShip(ctx, f2.ID, "cruiser", 0, 0, 30)

	payload := []byte(`{"attacking_fleet_id": "` + f1.ID.String() + `", "target_fleet_id": "` + f2.ID.String() + `"}`)
	action, _ := repo.CreateAction(ctx, *p, "attack", payload, time.Now())
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	if s := actionStatus(t, repo, action.ID); s != "failed" {
		t.Errorf("status = %q, want failed", s)
	}
}

func TestEngine_CombatReturnFireAndSalvage(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	a, d := newPlayer(t, repo, "rf_a"), newPlayer(t, repo, "rf_d")
	af, _ := repo.CreateFleet(ctx, *a, 0, 0)
	df, _ := repo.CreateFleet(ctx, *d, 3, 3)
	// Attacker: strong enough to kill the defender's weak scout outright.
	_, _ = repo.AddShip(ctx, af.ID, "cruiser", 100, 0, 500)
	_, _ = repo.AddShip(ctx, df.ID, "scout", 10, 0, 5)

	payload := []byte(`{"attacking_fleet_id": "` + af.ID.String() + `", "target_fleet_id": "` + df.ID.String() + `"}`)
	action, _ := repo.CreateAction(ctx, *a, "attack", payload, time.Now())
	before := credits(t, repo, *a)
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	if gone, _ := repo.GetFleetWithShips(ctx, df.ID); gone != nil {
		t.Fatal("defender fleet should have been destroyed")
	}
	// Salvage is half the scout's catalog cost (50 -> 25).
	if got := credits(t, repo, *a) - before; got != 25 {
		t.Errorf("attacker salvage = %d, want 25", got)
	}

	// A destroyed fleet fires no return shot; verify the log + leaderboard.
	var kills, counter int
	err := repo.Pool.QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE target_destroyed), COALESCE(sum(counter_damage),0)
		 FROM combat_logs WHERE attacker_player_id = $1`, *a).Scan(&kills, &counter)
	if err != nil {
		t.Fatalf("query combat log: %v", err)
	}
	if kills != 1 || counter != 0 {
		t.Errorf("kills=%d counter=%d, want 1 and 0", kills, counter)
	}
	standings, err := repo.LeaderboardStandings(ctx)
	if err != nil {
		t.Fatalf("LeaderboardStandings: %v", err)
	}
	found := false
	for _, s := range standings {
		if s.PlayerID == *a {
			found = true
			if s.Kills != 1 || s.Damage < 5 {
				t.Errorf("standing kills=%d damage=%d", s.Kills, s.Damage)
			}
		}
	}
	if !found {
		t.Error("attacker missing from leaderboard")
	}
}

func TestEngine_DefenderReturnsFire(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	a, d := newPlayer(t, repo, "ret_a"), newPlayer(t, repo, "ret_d")
	af, _ := repo.CreateFleet(ctx, *a, 0, 0)
	df, _ := repo.CreateFleet(ctx, *d, 2, 2)
	_, _ = repo.AddShip(ctx, af.ID, "cruiser", 30, 0, 500)
	_, _ = repo.AddShip(ctx, df.ID, "cruiser", 30, 0, 500) // survives the volley and shoots back

	payload := []byte(`{"attacking_fleet_id": "` + af.ID.String() + `", "target_fleet_id": "` + df.ID.String() + `"}`)
	action, _ := repo.CreateAction(ctx, *a, "attack", payload, time.Now())
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	after, _ := repo.GetFleetWithShips(ctx, af.ID)
	if after.Ships[0].HP >= 500 {
		t.Errorf("attacker should have taken return fire, HP = %d", after.Ships[0].HP)
	}
}

func TestEngine_BuildAddsShipsToStationaryFleet(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "builder")
	planet := insertPlanet(t, repo, p, 60, 60, 1000, 10)
	existing, _ := repo.CreateFleet(ctx, *p, 60, 60)

	payload := []byte(fmt.Sprintf(`{"planet_id": "%s", "ship_type": "fighter", "quantity": 3, "cost": 450}`, planet))
	action, _ := repo.CreateAction(ctx, *p, "build", payload, time.Now())
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	fleet, _ := repo.GetFleetWithShips(ctx, existing.ID)
	if len(fleet.Ships) != 3 || fleet.Ships[0].ShipType != "fighter" || fleet.Ships[0].Attack != 8 {
		t.Errorf("expected 3 catalog fighters in the existing fleet, got %+v", fleet.Ships)
	}
	if s := actionStatus(t, repo, action.ID); s != "resolved" {
		t.Errorf("status = %q, want resolved", s)
	}
}

func TestEngine_BuildOnLostPlanetRefunds(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p, other := newPlayer(t, repo, "refund"), newPlayer(t, repo, "thief")
	planet := insertPlanet(t, repo, other, 70, 70, 1000, 10) // not owned by p
	giveCredits(t, repo, *p, 0)

	payload := []byte(fmt.Sprintf(`{"planet_id": "%s", "ship_type": "scout", "quantity": 2, "cost": 100}`, planet))
	action, _ := repo.CreateAction(ctx, *p, "build", payload, time.Now())
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	if s := actionStatus(t, repo, action.ID); s != "failed" {
		t.Errorf("status = %q, want failed", s)
	}
	if got := credits(t, repo, *p); got != 100 {
		t.Errorf("credits after refund = %d, want 100", got)
	}
}

func TestEngine_ColonizeClaimsPlanetAndConsumesShip(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "settler")
	planet := insertPlanet(t, repo, nil, 30, 30, 5000, 12)
	fleet, _ := repo.CreateFleet(ctx, *p, 31, 31)
	_, _ = repo.AddShip(ctx, fleet.ID, "colony", 0, 2, 60)
	_, _ = repo.AddShip(ctx, fleet.ID, "scout", 2, 1, 20)

	payload := []byte(fmt.Sprintf(`{"fleet_id": "%s", "planet_id": "%s"}`, fleet.ID, planet))
	action, _ := repo.CreateAction(ctx, *p, "colonize", payload, time.Now())
	if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	got, _ := repo.GetPlanetByID(ctx, planet)
	if got.OwnerID == nil || *got.OwnerID != *p {
		t.Fatalf("planet should now belong to the colonizer, owner = %v", got.OwnerID)
	}
	after, _ := repo.GetFleetWithShips(ctx, fleet.ID)
	if after.CountShips("colony") != 0 || after.CountShips("scout") != 1 {
		t.Errorf("colony ship should be consumed, scout kept: %+v", after.Ships)
	}
}

func TestEngine_ColonizeTooFarOrOwnedFails(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p, other := newPlayer(t, repo, "far_settler"), newPlayer(t, repo, "owner")
	far := insertPlanet(t, repo, nil, 90, 90, 5000, 12)
	owned := insertPlanet(t, repo, other, 10, 10, 5000, 12)
	fleet, _ := repo.CreateFleet(ctx, *p, 10, 10)
	_, _ = repo.AddShip(ctx, fleet.ID, "colony", 0, 2, 60)

	for _, planet := range []uuid.UUID{far, owned} {
		payload := []byte(fmt.Sprintf(`{"fleet_id": "%s", "planet_id": "%s"}`, fleet.ID, planet))
		action, _ := repo.CreateAction(ctx, *p, "colonize", payload, time.Now())
		if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
			t.Fatalf("resolveAction: %v", err)
		}
		if s := actionStatus(t, repo, action.ID); s != "failed" {
			t.Errorf("status = %q, want failed", s)
		}
	}
	after, _ := repo.GetFleetWithShips(ctx, fleet.ID)
	if after.CountShips("colony") != 1 {
		t.Error("a failed colonization must not consume the colony ship")
	}
}

func TestEngine_IncomePaidOncePerTickAndDepletes(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "rich")
	giveCredits(t, repo, *p, 0)
	planet := insertPlanet(t, repo, p, 20, 20, 25, 10) // deposit runs out after 10+10+5

	now := time.Unix(4_000_000_000+rand.Int63n(1_000_000)*10, 0) // a tick number no other test uses
	if !engine.collectIncome(ctx, now) {
		t.Fatal("first collection of a tick should claim it")
	}
	if engine.collectIncome(ctx, now) {
		t.Fatal("a tick must only be claimed once")
	}
	if got := credits(t, repo, *p); got != 10 {
		t.Fatalf("credits = %d, want 10 (paid once for the tick)", got)
	}

	engine.collectIncome(ctx, now.Add(TickInterval))
	engine.collectIncome(ctx, now.Add(2*TickInterval))
	engine.collectIncome(ctx, now.Add(3*TickInterval)) // deposit exhausted: pays nothing
	if got := credits(t, repo, *p); got != 25 {
		t.Errorf("total credits = %d, want 25 (the whole deposit, no more)", got)
	}
	if pl, _ := repo.GetPlanetByID(ctx, planet); pl.Resources != 0 {
		t.Errorf("deposit = %d, want 0", pl.Resources)
	}
}

func TestEngine_RecoversStaleActionFromPostgres(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "stale")
	fleet, _ := repo.CreateFleet(ctx, *p, 0, 0)

	// An overdue pending action that was never put in (or was lost from) Redis.
	payload := []byte(`{"fleet_id": "` + fleet.ID.String() + `", "dest_x": 7, "dest_y": 8}`)
	action, err := repo.CreateAction(ctx, *p, "move", payload, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("CreateAction: %v", err)
	}

	engine.ProcessTick(ctx, time.Now())

	if s := actionStatus(t, repo, action.ID); s != "resolved" {
		t.Errorf("stale action status = %q, want resolved", s)
	}
	if f, _ := repo.GetFleetWithShips(ctx, fleet.ID); f.PosX != 7 || f.PosY != 8 {
		t.Errorf("fleet at (%v,%v), want (7,8)", f.PosX, f.PosY)
	}
}

func TestEngine_ResolvingTwiceIsHarmless(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "twice")
	planet := insertPlanet(t, repo, p, 80, 80, 1000, 10)
	payload := []byte(fmt.Sprintf(`{"planet_id": "%s", "ship_type": "scout", "quantity": 1, "cost": 50}`, planet))
	action, _ := repo.CreateAction(ctx, *p, "build", payload, time.Now())

	for i := 0; i < 2; i++ {
		if err := engine.resolveAction(ctx, action.ID, time.Now()); err != nil {
			t.Fatalf("resolveAction #%d: %v", i, err)
		}
	}
	fleets, _ := repo.ListFleetsByOwner(ctx, *p)
	total := 0
	for _, f := range fleets {
		total += len(f.Ships)
	}
	if total != 1 {
		t.Errorf("ships after double-resolve = %d, want 1 (idempotent)", total)
	}
}

func TestSalvageValue(t *testing.T) {
	if got := salvageValue([]string{domain.ShipScout, domain.ShipCruiser}); got != 25+250 {
		t.Errorf("salvage = %d, want 275", got)
	}
	if salvageValue(nil) != 0 || salvageValue([]string{"unknown"}) != 0 {
		t.Error("empty/unknown wrecks salvage nothing")
	}
}
