package server_test

import (
	"context"
	"math/rand"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/auth"
	"github.com/Atul-Koundal/protoverse/internal/cache"
	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
	"github.com/Atul-Koundal/protoverse/internal/server"
	"github.com/Atul-Koundal/protoverse/internal/tickengine"
)

// harness boots the real gRPC server (with auth interceptors) on a local
// port, backed by real Postgres and Redis. It skips when they aren't
// configured, like the other integration tests.
type harness struct {
	t      *testing.T
	repo   *repository.Repository
	engine *tickengine.Engine
	client protoversev1.GameServiceClient
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dbURL, redisAddr := os.Getenv("DB_URL"), os.Getenv("REDIS_ADDR")
	if dbURL == "" || redisAddr == "" {
		t.Skip("DB_URL and REDIS_ADDR not set, skipping integration test")
	}
	ctx := context.Background()

	repo, err := repository.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(repo.Close)
	q := queue.New(redisAddr)
	t.Cleanup(func() { _ = q.Close() })
	rc := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = rc.Close() })

	public := auth.DefaultPublic()
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(auth.UnaryInterceptor(repo, public)),
		grpc.StreamInterceptor(auth.StreamInterceptor(repo, public)),
	)
	protoversev1.RegisterGameServiceServer(srv, server.New(repo, q, cache.New(rc)))
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &harness{t: t, repo: repo, engine: tickengine.New(repo, q), client: protoversev1.NewGameServiceClient(conn)}
}

func withKey(ctx context.Context, key string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+key)
}

type user struct {
	name, key, id string
}

func (h *harness) register(prefix string) user {
	h.t.Helper()
	name := prefix + "_" + uuid.NewString()[:6]
	p, err := h.client.CreateAccount(context.Background(), &protoversev1.CreateAccountRequest{DisplayName: name})
	if err != nil {
		h.t.Fatalf("CreateAccount(%s): %v", name, err)
	}
	if p.GetApiKey() == "" {
		h.t.Fatal("CreateAccount must return the API key")
	}
	return user{name: name, key: p.GetApiKey(), id: p.GetId()}
}

func (h *harness) state(u user) *protoversev1.PlayerState {
	h.t.Helper()
	s, err := h.client.GetMyState(withKey(context.Background(), u.key), &protoversev1.GetMyStateRequest{})
	if err != nil {
		h.t.Fatalf("GetMyState: %v", err)
	}
	return s
}

// advance runs one tick far enough in the future that every queued order is
// due, with a tick number no other run has used.
func (h *harness) advance() {
	h.engine.ProcessTick(context.Background(), time.Now().Add(time.Duration(2+rand.Intn(200_000))*time.Hour))
}

func (h *harness) setCredits(u user, n int64) {
	h.t.Helper()
	if _, err := h.repo.Pool.Exec(context.Background(), `UPDATE players SET credits = $1 WHERE id = $2`, n, u.id); err != nil {
		h.t.Fatal(err)
	}
}

func wantCode(t *testing.T, err error, want codes.Code, what string) {
	t.Helper()
	if status.Code(err) != want {
		t.Errorf("%s: got %v, want code %v", what, err, want)
	}
}

func TestAccountsAndAuth(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	alice := h.register("alice")

	// Sign-up rules.
	_, err := h.client.CreateAccount(ctx, &protoversev1.CreateAccountRequest{DisplayName: alice.name})
	wantCode(t, err, codes.AlreadyExists, "duplicate name")
	_, err = h.client.CreateAccount(ctx, &protoversev1.CreateAccountRequest{DisplayName: "x"})
	wantCode(t, err, codes.InvalidArgument, "bad name")

	// Private calls need a valid key.
	_, err = h.client.GetMyState(ctx, &protoversev1.GetMyStateRequest{})
	wantCode(t, err, codes.Unauthenticated, "no key")
	_, err = h.client.GetMyState(withKey(ctx, "pv_nope"), &protoversev1.GetMyStateRequest{})
	wantCode(t, err, codes.Unauthenticated, "wrong key")
	_, err = h.client.MoveFleet(ctx, &protoversev1.MoveFleetRequest{})
	wantCode(t, err, codes.Unauthenticated, "move without key")

	// Public calls don't.
	if _, err := h.client.GetCatalog(ctx, &protoversev1.GetCatalogRequest{}); err != nil {
		t.Errorf("GetCatalog should be public: %v", err)
	}
	if _, err := h.client.GetGalaxyState(ctx, &protoversev1.GetGalaxyStateRequest{}); err != nil {
		t.Errorf("GetGalaxyState should be public: %v", err)
	}

	// A fresh account has everything needed to play.
	s := h.state(alice)
	if s.GetCredits() != 500 || len(s.GetPlanets()) != 1 || len(s.GetFleets()) != 1 || len(s.GetFleets()[0].GetShips()) != 3 {
		t.Fatalf("unexpected starting state: credits=%d planets=%d fleets=%d", s.GetCredits(), len(s.GetPlanets()), len(s.GetFleets()))
	}
	if s.GetPlanets()[0].GetProduction() != 20 {
		t.Errorf("home production = %d, want 20", s.GetPlanets()[0].GetProduction())
	}
	// The stored key is a hash, never the raw key.
	var stored string
	if err := h.repo.Pool.QueryRow(ctx, `SELECT api_key FROM players WHERE id = $1`, alice.id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == alice.key || stored != auth.HashKey(alice.key) {
		t.Error("API key must be stored hashed")
	}
}

func TestBuildMoveAndOwnership(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.register("alice"), h.register("bob")
	actx := withKey(context.Background(), alice.key)
	bctx := withKey(context.Background(), bob.key)

	st := h.state(alice)
	home, fleet := st.GetPlanets()[0], st.GetFleets()[0]

	// --- building ---
	_, err := h.client.BuildShip(actx, &protoversev1.BuildShipRequest{PlanetId: home.GetId(), ShipType: "cruiser", Quantity: 2})
	wantCode(t, err, codes.FailedPrecondition, "cruisers x2 cost 1000 > 500 credits")
	_, err = h.client.BuildShip(actx, &protoversev1.BuildShipRequest{PlanetId: home.GetId(), ShipType: "death-star"})
	wantCode(t, err, codes.InvalidArgument, "unknown ship")
	_, err = h.client.BuildShip(actx, &protoversev1.BuildShipRequest{PlanetId: home.GetId(), ShipType: "scout", Quantity: 99})
	wantCode(t, err, codes.InvalidArgument, "quantity cap")
	_, err = h.client.BuildShip(bctx, &protoversev1.BuildShipRequest{PlanetId: home.GetId(), ShipType: "scout"})
	wantCode(t, err, codes.NotFound, "building at someone else's planet")
	if got := h.state(alice).GetCredits(); got != 500 {
		t.Fatalf("failed purchases must not cost anything, credits = %d", got)
	}

	if _, err := h.client.BuildShip(actx, &protoversev1.BuildShipRequest{PlanetId: home.GetId(), ShipType: "fighter", Quantity: 2}); err != nil {
		t.Fatalf("BuildShip: %v", err)
	}
	st = h.state(alice)
	if st.GetCredits() != 200 || len(st.GetPendingActions()) != 1 {
		t.Fatalf("after ordering: credits=%d pending=%d, want 200 and 1", st.GetCredits(), len(st.GetPendingActions()))
	}
	h.advance()
	st = h.state(alice)
	if got := len(st.GetFleets()[0].GetShips()); got != 5 {
		t.Errorf("fleet has %d ships after the build resolved, want 5", got)
	}
	if len(st.GetPendingActions()) != 0 {
		t.Error("resolved orders should leave the pending list")
	}
	if st.GetCredits() < 200+20 {
		t.Errorf("home planet income missing: credits = %d", st.GetCredits())
	}

	// --- moving ---
	_, err = h.client.MoveFleet(bctx, &protoversev1.MoveFleetRequest{FleetId: fleet.GetId(), Destination: &protoversev1.Position{X: 1, Y: 1}})
	wantCode(t, err, codes.NotFound, "moving someone else's fleet")
	_, err = h.client.MoveFleet(actx, &protoversev1.MoveFleetRequest{FleetId: fleet.GetId(), Destination: &protoversev1.Position{X: 500, Y: 1}})
	wantCode(t, err, codes.InvalidArgument, "destination outside galaxy")
	_, err = h.client.MoveFleet(actx, &protoversev1.MoveFleetRequest{FleetId: "nope", Destination: &protoversev1.Position{X: 1, Y: 1}})
	wantCode(t, err, codes.InvalidArgument, "bad fleet id")

	dest := &protoversev1.Position{X: 95, Y: 95}
	ackMove, err := h.client.MoveFleet(actx, &protoversev1.MoveFleetRequest{FleetId: fleet.GetId(), Destination: dest})
	if err != nil {
		t.Fatalf("MoveFleet: %v", err)
	}
	// Travel takes real time: the order must not resolve on the very next tick
	// for a long trip, and the fleet is visibly under way meanwhile.
	if !ackMove.GetScheduledFor().AsTime().After(time.Now().Add(2 * time.Second)) {
		t.Error("a cross-galaxy trip should be scheduled in the future")
	}
	_, err = h.client.MoveFleet(actx, &protoversev1.MoveFleetRequest{FleetId: fleet.GetId(), Destination: &protoversev1.Position{X: 2, Y: 2}})
	wantCode(t, err, codes.FailedPrecondition, "second move while under way")
	moving := h.state(alice).GetFleets()[0]
	if moving.GetDestination() == nil || moving.GetArrivesAt() == nil {
		t.Error("moving fleet should expose destination and arrival time")
	}

	h.advance()
	arrived := h.state(alice).GetFleets()[0]
	if arrived.GetPosition().GetX() != 95 || arrived.GetPosition().GetY() != 95 || arrived.GetDestination() != nil {
		t.Errorf("fleet should have arrived at (95,95), got %v dest=%v", arrived.GetPosition(), arrived.GetDestination())
	}
}

func TestAttackValidation(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.register("alice"), h.register("bob")
	actx := withKey(context.Background(), alice.key)

	af := h.state(alice).GetFleets()[0]
	bf := h.state(bob).GetFleets()[0]

	_, err := h.client.Attack(actx, &protoversev1.AttackRequest{AttackingFleetId: af.GetId(), TargetFleetId: af.GetId()})
	wantCode(t, err, codes.InvalidArgument, "attacking yourself")
	_, err = h.client.Attack(actx, &protoversev1.AttackRequest{AttackingFleetId: bf.GetId(), TargetFleetId: af.GetId()})
	wantCode(t, err, codes.NotFound, "attacking with someone else's fleet")
	_, err = h.client.Attack(actx, &protoversev1.AttackRequest{AttackingFleetId: af.GetId(), TargetFleetId: uuid.NewString()})
	wantCode(t, err, codes.NotFound, "unknown target")

	// Put Bob right next to Alice so the range check passes.
	if _, err := h.repo.Pool.Exec(context.Background(),
		`UPDATE fleets SET pos_x = $1, pos_y = $2 WHERE id = $3`, af.GetPosition().GetX()+1, af.GetPosition().GetY(), bf.GetId()); err != nil {
		t.Fatal(err)
	}
	if _, err := h.client.Attack(actx, &protoversev1.AttackRequest{AttackingFleetId: af.GetId(), TargetFleetId: bf.GetId()}); err != nil {
		t.Fatalf("in-range attack should be accepted: %v", err)
	}
	// One pending attack per fleet — otherwise damage could be multiplied by spamming.
	_, err = h.client.Attack(actx, &protoversev1.AttackRequest{AttackingFleetId: af.GetId(), TargetFleetId: bf.GetId()})
	wantCode(t, err, codes.FailedPrecondition, "second attack from the same fleet in one tick")

	h.advance()

	// The attack resolved: it's in the combat log and the defender was hurt.
	var logs, damage int
	err = h.repo.Pool.QueryRow(context.Background(),
		`SELECT count(*), COALESCE(sum(damage_dealt), 0) FROM combat_logs WHERE attacker_player_id = $1`, alice.id).Scan(&logs, &damage)
	if err != nil {
		t.Fatal(err)
	}
	if logs != 1 || damage <= 0 {
		t.Errorf("expected exactly one combat log with damage, got %d logs / %d damage", logs, damage)
	}
	hp := int32(0)
	for _, s := range h.state(bob).GetFleets()[0].GetShips() {
		hp += s.GetHp()
	}
	if hp >= 80 { // two scouts + a fighter at full health = 20+20+40
		t.Errorf("bob's fleet should have taken damage, total HP = %d", hp)
	}
}

func TestColonizeFlowAndLeaderboard(t *testing.T) {
	h := newHarness(t)
	alice := h.register("alice")
	actx := withKey(context.Background(), alice.key)
	ctx := context.Background()

	st := h.state(alice)
	home, fleet := st.GetPlanets()[0], st.GetFleets()[0]

	// A neutral planet for her to claim.
	var neutral uuid.UUID
	err := h.repo.Pool.QueryRow(ctx,
		`INSERT INTO planets (name, pos_x, pos_y, resources, production) VALUES ($1, 50, 50, 5000, 15) RETURNING id`,
		"colony-target-"+uuid.NewString()[:6]).Scan(&neutral)
	if err != nil {
		t.Fatal(err)
	}

	// Needs a colony ship first.
	_, err = h.client.Colonize(actx, &protoversev1.ColonizeRequest{FleetId: fleet.GetId(), PlanetId: neutral.String()})
	wantCode(t, err, codes.FailedPrecondition, "colonize without colony ship")

	h.setCredits(alice, 1000)
	if _, err := h.client.BuildShip(actx, &protoversev1.BuildShipRequest{PlanetId: home.GetId(), ShipType: "colony"}); err != nil {
		t.Fatalf("build colony ship: %v", err)
	}
	h.advance()

	// Too far away until she flies there.
	_, err = h.client.Colonize(actx, &protoversev1.ColonizeRequest{FleetId: fleet.GetId(), PlanetId: neutral.String()})
	wantCode(t, err, codes.FailedPrecondition, "colonize out of range")
	if _, err := h.client.MoveFleet(actx, &protoversev1.MoveFleetRequest{FleetId: fleet.GetId(), Destination: &protoversev1.Position{X: 50, Y: 50}}); err != nil {
		t.Fatalf("move: %v", err)
	}
	h.advance()
	if _, err := h.client.Colonize(actx, &protoversev1.ColonizeRequest{FleetId: fleet.GetId(), PlanetId: neutral.String()}); err != nil {
		t.Fatalf("colonize: %v", err)
	}
	h.advance()

	st = h.state(alice)
	if len(st.GetPlanets()) != 2 {
		t.Fatalf("alice owns %d planets, want 2", len(st.GetPlanets()))
	}
	_, err = h.client.Colonize(actx, &protoversev1.ColonizeRequest{FleetId: fleet.GetId(), PlanetId: neutral.String()})
	wantCode(t, err, codes.FailedPrecondition, "colonizing an owned planet")

	// Leaderboard: she scores at least 2 planets' worth and ranks consistently.
	lb, err := h.client.GetLeaderboard(ctx, &protoversev1.GetLeaderboardRequest{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var mine *protoversev1.LeaderboardEntry
	for i, e := range lb.GetEntries() {
		if e.GetRank() != int32(i+1) {
			t.Errorf("rank %d at position %d", e.GetRank(), i)
		}
		if i > 0 && e.GetScore() > lb.GetEntries()[i-1].GetScore() {
			t.Error("leaderboard not sorted by score")
		}
		if e.GetPlayerId() == alice.id {
			mine = e
		}
	}
	if mine == nil {
		// Cache may be a few seconds old only if no tick invalidated it; advance() does.
		t.Fatal("alice missing from leaderboard")
	}
	if mine.GetPlanets() != 2 || mine.GetScore() < 200 {
		t.Errorf("alice: planets=%d score=%d, want 2 planets and score >= 200", mine.GetPlanets(), mine.GetScore())
	}
	if top, _ := h.client.GetLeaderboard(ctx, &protoversev1.GetLeaderboardRequest{Limit: 1}); len(top.GetEntries()) != 1 {
		t.Error("limit not honoured")
	}
}

func TestScan(t *testing.T) {
	h := newHarness(t)
	alice := h.register("alice")
	actx := withKey(context.Background(), alice.key)
	home := h.state(alice).GetPlanets()[0].GetPosition()

	res, err := h.client.Scan(actx, &protoversev1.ScanRequest{Center: home, Radius: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.GetVisiblePlanets()) < 1 || len(res.GetVisibleFleets()) < 1 {
		t.Errorf("scan at home should see her planet and fleet, got %d/%d", len(res.GetVisiblePlanets()), len(res.GetVisibleFleets()))
	}
	_, err = h.client.Scan(actx, &protoversev1.ScanRequest{Center: home, Radius: 9999})
	wantCode(t, err, codes.InvalidArgument, "radius too large")
	_, err = h.client.Scan(context.Background(), &protoversev1.ScanRequest{Center: home, Radius: 5})
	wantCode(t, err, codes.Unauthenticated, "scan without key")
}

func TestStreamIsPerPlayer(t *testing.T) {
	h := newHarness(t)
	alice, bob := h.register("alice"), h.register("bob")
	actx := withKey(context.Background(), alice.key)

	sctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	open := func(u user) chan *protoversev1.GameEvent {
		stream, err := h.client.StreamGameState(withKey(sctx, u.key), &protoversev1.StreamGameStateRequest{})
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		ch := make(chan *protoversev1.GameEvent, 64)
		go func() {
			defer close(ch)
			for {
				ev, err := stream.Recv()
				if err != nil {
					return
				}
				ch <- ev
			}
		}()
		return ch
	}
	aliceEvents, bobEvents := open(alice), open(bob)

	// Streams need a valid key.
	bad, err := h.client.StreamGameState(context.Background(), &protoversev1.StreamGameStateRequest{})
	if err == nil {
		_, err = bad.Recv()
	}
	wantCode(t, err, codes.Unauthenticated, "stream without key")

	time.Sleep(500 * time.Millisecond) // let both Redis subscriptions establish

	home := h.state(alice).GetPlanets()[0]
	if _, err := h.client.BuildShip(actx, &protoversev1.BuildShipRequest{PlanetId: home.GetId(), ShipType: "scout"}); err != nil {
		t.Fatal(err)
	}
	h.advance()
	time.Sleep(500 * time.Millisecond)

	drain := func(ch chan *protoversev1.GameEvent) (built, tick, income bool) {
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return
				}
				switch ev.GetEvent().(type) {
				case *protoversev1.GameEvent_ShipBuilt:
					built = true
				case *protoversev1.GameEvent_Tick:
					tick = true
				case *protoversev1.GameEvent_IncomeEarned:
					income = true
				}
			default:
				return
			}
		}
	}
	aBuilt, aTick, aIncome := drain(aliceEvents)
	bBuilt, bTick, bIncome := drain(bobEvents)

	if !aBuilt || !aTick || !aIncome {
		t.Errorf("alice should see her ship_built, tick and income events: %v %v %v", aBuilt, aTick, aIncome)
	}
	if bBuilt {
		t.Error("bob must NOT see alice's ship_built event")
	}
	if !bTick || !bIncome {
		t.Errorf("bob should still get the broadcast tick and his own income: tick=%v income=%v", bTick, bIncome)
	}
}
