package tickengine

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sync"
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

// ---- planetary conquest & defense ----------------------------------------

func setDefense(t *testing.T, repo *repository.Repository, planet uuid.UUID, level, shield int) {
	t.Helper()
	_, err := repo.Pool.Exec(context.Background(),
		`UPDATE planets SET defense_level = $1, shield = $2 WHERE id = $3`, level, shield, planet)
	if err != nil {
		t.Fatalf("setDefense: %v", err)
	}
}

func getPlanet(t *testing.T, repo *repository.Repository, id uuid.UUID) *domain.Planet {
	t.Helper()
	p, err := repo.GetPlanetByID(context.Background(), id)
	if err != nil || p == nil {
		t.Fatalf("GetPlanetByID: %v", err)
	}
	return p
}

// invadeSetup creates an attacker with a fleet next to a defended enemy planet.
// The defender owns two planets so the sanctuary rule doesn't apply.
type invadeSetup struct {
	attacker, defender *uuid.UUID
	fleet              uuid.UUID
	target, spare      uuid.UUID
}

func newInvadeSetup(t *testing.T, repo *repository.Repository, level, shield int, ships ...[4]int) invadeSetup {
	t.Helper()
	ctx := context.Background()
	s := invadeSetup{attacker: newPlayer(t, repo, "inv_a"), defender: newPlayer(t, repo, "inv_d")}
	s.target = insertPlanet(t, repo, s.defender, 40, 40, 5000, 10)
	s.spare = insertPlanet(t, repo, s.defender, 90, 10, 5000, 10)
	setDefense(t, repo, s.target, level, shield)
	f, err := repo.CreateFleet(ctx, *s.attacker, 42, 42) // ~2.8 from the planet
	if err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}
	s.fleet = f.ID
	for _, sh := range ships { // {attack, defense, hp, isTrooper}
		typ := "cruiser"
		if sh[3] == 1 {
			typ = "trooper"
		}
		if _, err := repo.AddShip(ctx, f.ID, typ, sh[0], sh[1], sh[2]); err != nil {
			t.Fatalf("AddShip: %v", err)
		}
	}
	return s
}

func (s invadeSetup) order(t *testing.T, repo *repository.Repository) uuid.UUID {
	t.Helper()
	payload := []byte(fmt.Sprintf(`{"fleet_id": "%s", "planet_id": "%s"}`, s.fleet, s.target))
	a, err := repo.CreateAction(context.Background(), *s.attacker, "invade", payload, time.Now())
	if err != nil {
		t.Fatalf("CreateAction(invade): %v", err)
	}
	return a.ID
}

func TestEngine_InvadeDrainsShieldAndPlanetFiresBack(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	// Level 1: armour 5, attack 18, shield 90. A 60-attack fleet does ~55 damage:
	// a big dent, but not a breach.
	s := newInvadeSetup(t, repo, 1, 90, [4]int{60, 0, 500, 0})
	action := s.order(t, repo)
	if err := engine.resolveAction(ctx, action, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	p := getPlanet(t, repo, s.target)
	if p.Shield <= 0 || p.Shield >= 90 {
		t.Errorf("shield = %d, want drained but not breached (0 < shield < 90)", p.Shield)
	}
	if *p.OwnerID != *s.defender {
		t.Error("planet must not change hands without a breach")
	}
	f, _ := repo.GetFleetWithShips(ctx, s.fleet)
	if f.Ships[0].HP >= 500 {
		t.Errorf("planet should have fired back, fleet HP = %d", f.Ships[0].HP)
	}
	var battles int
	_ = repo.Pool.QueryRow(ctx, `SELECT count(*) FROM planet_battles WHERE planet_id = $1 AND NOT captured`, s.target).Scan(&battles)
	if battles != 1 {
		t.Errorf("battle log rows = %d, want 1", battles)
	}
	if st := actionStatus(t, repo, action); st != "resolved" {
		t.Errorf("status = %q, want resolved", st)
	}
}

func TestEngine_InvadeWithTrooperCapturesBreachedPlanet(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	s := newInvadeSetup(t, repo, 2, 130, [4]int{500, 40, 800, 0}, [4]int{1, 1, 30, 1})
	if err := engine.resolveAction(ctx, s.order(t, repo), time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}

	p := getPlanet(t, repo, s.target)
	if p.OwnerID == nil || *p.OwnerID != *s.attacker {
		t.Fatalf("planet should now belong to the attacker, owner = %v", p.OwnerID)
	}
	if p.DefenseLevel != domain.CapturedLevel(2) || p.Shield != domain.CapturedShield(2) {
		t.Errorf("captured planet level/shield = %d/%d, want %d/%d",
			p.DefenseLevel, p.Shield, domain.CapturedLevel(2), domain.CapturedShield(2))
	}
	f, _ := repo.GetFleetWithShips(ctx, s.fleet)
	if f.CountShips("trooper") != 0 {
		t.Error("the trooper is consumed by the capture")
	}
	if f.CountShips("cruiser") != 1 {
		t.Error("the rest of the fleet survives")
	}

	standings, err := repo.LeaderboardStandings(ctx)
	if err != nil {
		t.Fatalf("LeaderboardStandings: %v", err)
	}
	for _, st := range standings {
		if st.PlayerID == *s.attacker && st.Captures != 1 {
			t.Errorf("attacker captures = %d, want 1", st.Captures)
		}
	}
}

func TestEngine_BreachWithoutTrooperDoesNotCapture(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	s := newInvadeSetup(t, repo, 1, 90, [4]int{500, 40, 800, 0}) // no trooper
	if err := engine.resolveAction(ctx, s.order(t, repo), time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	p := getPlanet(t, repo, s.target)
	if p.Shield != 0 {
		t.Errorf("shield = %d, want fully breached", p.Shield)
	}
	if *p.OwnerID != *s.defender {
		t.Error("without a trooper the planet stays with its owner")
	}
}

func TestEngine_LastPlanetIsProtected(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	s := newInvadeSetup(t, repo, 1, 90, [4]int{500, 40, 800, 0}, [4]int{1, 1, 30, 1})
	if _, err := repo.Pool.Exec(ctx, `DELETE FROM planets WHERE id = $1`, s.spare); err != nil {
		t.Fatal(err)
	}
	action := s.order(t, repo)
	if err := engine.resolveAction(ctx, action, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	if st := actionStatus(t, repo, action); st != "failed" {
		t.Errorf("status = %q, want failed (sanctuary)", st)
	}
	if p := getPlanet(t, repo, s.target); p.Shield != 90 || *p.OwnerID != *s.defender {
		t.Errorf("a protected planet must be untouched: shield=%d", p.Shield)
	}
}

func TestEngine_InvadeValidation(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	// out of range
	s := newInvadeSetup(t, repo, 1, 90, [4]int{500, 40, 800, 0})
	if _, err := repo.Pool.Exec(ctx, `UPDATE fleets SET pos_x = 0, pos_y = 0 WHERE id = $1`, s.fleet); err != nil {
		t.Fatal(err)
	}
	action := s.order(t, repo)
	_ = engine.resolveAction(ctx, action, time.Now())
	if st := actionStatus(t, repo, action); st != "failed" {
		t.Errorf("out of range: status = %q, want failed", st)
	}
	if getPlanet(t, repo, s.target).Shield != 90 {
		t.Error("an out-of-range assault must not damage the planet")
	}

	// your own planet, and an unowned one
	own := insertPlanet(t, repo, s.attacker, 42, 42, 100, 1)
	neutral := insertPlanet(t, repo, nil, 42, 43, 100, 1)
	_, _ = repo.Pool.Exec(ctx, `UPDATE fleets SET pos_x = 42, pos_y = 42 WHERE id = $1`, s.fleet)
	for name, planet := range map[string]uuid.UUID{"own": own, "unowned": neutral} {
		payload := []byte(fmt.Sprintf(`{"fleet_id": "%s", "planet_id": "%s"}`, s.fleet, planet))
		a, _ := repo.CreateAction(ctx, *s.attacker, "invade", payload, time.Now())
		_ = engine.resolveAction(ctx, a.ID, time.Now())
		if st := actionStatus(t, repo, a.ID); st != "failed" {
			t.Errorf("%s planet: status = %q, want failed", name, st)
		}
	}
}

func TestEngine_PlanetGunsCanDestroyAFleet(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	// Level 10: attack 90. A lone fragile ship has no chance.
	s := newInvadeSetup(t, repo, 10, 450, [4]int{2, 0, 20, 0})
	defBefore := credits(t, repo, *s.defender)
	if err := engine.resolveAction(ctx, s.order(t, repo), time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	if f, _ := repo.GetFleetWithShips(ctx, s.fleet); f != nil {
		t.Error("the fleet should have been destroyed by the planet's guns")
	}
	if got := credits(t, repo, *s.defender) - defBefore; got != 250 { // half a cruiser's 500
		t.Errorf("defender salvage = %d, want 250", got)
	}
	var destroyed bool
	_ = repo.Pool.QueryRow(ctx, `SELECT attacker_destroyed FROM planet_battles WHERE planet_id = $1`, s.target).Scan(&destroyed)
	if !destroyed {
		t.Error("battle log should record that the attacker was destroyed")
	}
}

func TestEngine_FortifyRaisesLevelAndShield(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "fort")
	planet := insertPlanet(t, repo, p, 20, 20, 5000, 10)
	setDefense(t, repo, planet, 1, 70)
	giveCredits(t, repo, *p, 0)

	payload := []byte(fmt.Sprintf(`{"planet_id": "%s", "cost": 400}`, planet))
	a, _ := repo.CreateAction(ctx, *p, "fortify", payload, time.Now())
	if err := engine.resolveAction(ctx, a.ID, time.Now()); err != nil {
		t.Fatalf("resolveAction: %v", err)
	}
	got := getPlanet(t, repo, planet)
	if got.DefenseLevel != 2 || got.Shield != 70+40 {
		t.Errorf("level/shield = %d/%d, want 2/110", got.DefenseLevel, got.Shield)
	}
	if credits(t, repo, *p) != 0 {
		t.Error("a successful fortify must not refund")
	}
}

func TestEngine_FortifyRefundsWhenImpossible(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p, other := newPlayer(t, repo, "fort_r"), newPlayer(t, repo, "fort_o")
	notMine := insertPlanet(t, repo, other, 30, 30, 5000, 10)
	maxed := insertPlanet(t, repo, p, 31, 31, 5000, 10)
	setDefense(t, repo, maxed, domain.MaxDefenseLevel, 450)

	for name, planet := range map[string]uuid.UUID{"not owner": notMine, "max level": maxed} {
		giveCredits(t, repo, *p, 0)
		payload := []byte(fmt.Sprintf(`{"planet_id": "%s", "cost": 300}`, planet))
		a, _ := repo.CreateAction(ctx, *p, "fortify", payload, time.Now())
		if err := engine.resolveAction(ctx, a.ID, time.Now()); err != nil {
			t.Fatalf("%s: resolveAction: %v", name, err)
		}
		if st := actionStatus(t, repo, a.ID); st != "failed" {
			t.Errorf("%s: status = %q, want failed", name, st)
		}
		if got := credits(t, repo, *p); got != 300 {
			t.Errorf("%s: credits = %d, want 300 refunded", name, got)
		}
	}
}

func TestEngine_ShieldRegenerationAndCapMatchesDomain(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	p := newPlayer(t, repo, "regen")
	// One planet per level, each a point below full: regen must cap at exactly
	// domain.MaxShield, proving the SQL formula and the Go formula agree.
	near := map[int]uuid.UUID{}
	for level := 0; level <= domain.MaxDefenseLevel; level++ {
		id := insertPlanet(t, repo, p, float64(level), 5, 100, 1)
		setDefense(t, repo, id, level, domain.MaxShield(level)-1)
		near[level] = id
	}
	empty := insertPlanet(t, repo, p, 50, 50, 100, 1)
	setDefense(t, repo, empty, 3, 0)
	neutral := insertPlanet(t, repo, nil, 60, 60, 100, 1)
	setDefense(t, repo, neutral, 3, 0)

	now := time.Unix(4_100_000_000+rand.Int63n(1_000_000)*10, 0)
	if !engine.collectIncome(ctx, now) {
		t.Fatal("tick should be claimed")
	}
	for level, id := range near {
		if got := getPlanet(t, repo, id).Shield; got != domain.MaxShield(level) {
			t.Errorf("level %d shield = %d, want capped at %d", level, got, domain.MaxShield(level))
		}
	}
	if got := getPlanet(t, repo, empty).Shield; got != domain.ShieldRegenPerTick {
		t.Errorf("empty shield regenerated to %d, want %d", got, domain.ShieldRegenPerTick)
	}
	if got := getPlanet(t, repo, neutral).Shield; got != 0 {
		t.Errorf("unowned planets don't regenerate, got %d", got)
	}
}

// Two attackers resolving at the same moment against the defender's last two
// planets: exactly one may capture, or the defender would be left with nothing.
func TestEngine_ConcurrentInvasionsRespectSanctuary(t *testing.T) {
	engine, repo := setupTestEngine(t)
	ctx := context.Background()

	defender := newPlayer(t, repo, "conc_d")
	p1 := insertPlanet(t, repo, defender, 10, 10, 5000, 10)
	p2 := insertPlanet(t, repo, defender, 80, 80, 5000, 10)
	setDefense(t, repo, p1, 0, 50)
	setDefense(t, repo, p2, 0, 50)

	var actions []uuid.UUID
	for _, target := range []struct {
		planet uuid.UUID
		x, y   float64
	}{{p1, 11, 11}, {p2, 81, 81}} {
		attacker := newPlayer(t, repo, "conc_a")
		fleet, _ := repo.CreateFleet(ctx, *attacker, target.x, target.y)
		_, _ = repo.AddShip(ctx, fleet.ID, "cruiser", 500, 40, 800)
		_, _ = repo.AddShip(ctx, fleet.ID, "trooper", 1, 1, 30)
		payload := []byte(fmt.Sprintf(`{"fleet_id": "%s", "planet_id": "%s"}`, fleet.ID, target.planet))
		a, err := repo.CreateAction(ctx, *attacker, "invade", payload, time.Now())
		if err != nil {
			t.Fatalf("CreateAction: %v", err)
		}
		actions = append(actions, a.ID)
	}

	var wg sync.WaitGroup
	errs := make([]error, len(actions))
	for i, id := range actions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = engine.resolveAction(ctx, id, time.Now())
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("resolve #%d: %v", i, err)
		}
	}

	n, err := repo.CountPlanetsOwned(ctx, *defender)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("defender owns %d planets after two simultaneous invasions, want exactly 1 (sanctuary)", n)
	}
	var resolved, failed int
	for _, id := range actions {
		switch actionStatus(t, repo, id) {
		case "resolved":
			resolved++
		case "failed":
			failed++
		}
	}
	if resolved != 1 || failed != 1 {
		t.Errorf("want one capture and one refusal, got %d resolved / %d failed", resolved, failed)
	}
}
