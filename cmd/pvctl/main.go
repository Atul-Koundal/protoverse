// Command pvctl is a command-line client for Protoverse.
//
//	pvctl register <name>             create an account (saves your API key)
//	pvctl status                      credits, planets, fleets, pending orders
//	pvctl map                         ASCII map of the galaxy
//	pvctl catalog                     ship types, costs and game rules
//	pvctl build <planet> <type> [n]   build ships at one of your planets
//	pvctl move <fleet> <x> <y>        send a fleet somewhere
//	pvctl attack <fleet> <target>     attack an enemy fleet within range
//	pvctl colonize <fleet> <planet>   claim a neutral planet with a colony ship
//	pvctl scan <x> <y> <radius>       list everything near a point
//	pvctl leaderboard [n]             top players
//	pvctl watch                       live event feed
//
// IDs may be abbreviated to any unique prefix (the 8 characters shown by
// `status` are enough). The API address comes from -addr or PROTOVERSE_ADDR
// (default localhost:50051); the key from PROTOVERSE_KEY or ~/.protoverse/key.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
)

func main() {
	addr := flag.String("addr", envOr("PROTOVERSE_ADDR", "localhost:50051"), "API server address")
	flag.Usage = usage
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fatal(err)
	}
	defer conn.Close()

	c := &cli{client: protoversev1.NewGameServiceClient(conn)}
	if err := c.run(args[0], args[1:]); err != nil {
		fatal(err)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: pvctl [-addr host:port] <register|status|map|catalog|build|move|attack|colonize|scan|leaderboard|watch> [args]")
	fmt.Fprintln(os.Stderr, "see `go doc ./cmd/pvctl` or the README for details")
}

func fatal(err error) {
	if st, ok := status.FromError(err); ok {
		fmt.Fprintf(os.Stderr, "error: %s\n", st.Message())
	} else {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	os.Exit(1)
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

type cli struct {
	client protoversev1.GameServiceClient
}

func keyPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".protoverse", "key"), nil
}

func loadKey() (string, error) {
	if k := os.Getenv("PROTOVERSE_KEY"); k != "" {
		return k, nil
	}
	p, err := keyPath()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", errors.New("no API key: run `pvctl register <name>` or set PROTOVERSE_KEY")
	}
	return strings.TrimSpace(string(b)), nil
}

func (c *cli) ctx() (context.Context, context.CancelFunc, error) {
	key, err := loadKey()
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+key), cancel, nil
}

func (c *cli) run(cmd string, args []string) error {
	switch cmd {
	case "register":
		return c.register(args)
	case "catalog":
		return c.catalog()
	case "leaderboard":
		return c.leaderboard(args)
	case "status":
		return c.status()
	case "map":
		return c.showMap()
	case "build":
		return c.build(args)
	case "move":
		return c.move(args)
	case "attack":
		return c.attack(args)
	case "colonize":
		return c.colonize(args)
	case "scan":
		return c.scan(args)
	case "watch":
		return c.watch()
	}
	usage()
	return fmt.Errorf("unknown command %q", cmd)
}

func (c *cli) register(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: pvctl register <name>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	p, err := c.client.CreateAccount(ctx, &protoversev1.CreateAccountRequest{DisplayName: args[0]})
	if err != nil {
		return err
	}
	path, err := keyPath()
	if err == nil {
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			err = os.WriteFile(path, []byte(p.GetApiKey()+"\n"), 0o600)
		}
	}
	fmt.Printf("Welcome, commander %s!\n", p.GetDisplayName())
	if err != nil {
		fmt.Printf("Could not save your key (%v). Save it yourself — it is shown only once:\n  %s\n", err, p.GetApiKey())
	} else {
		fmt.Printf("API key saved to %s (keep it secret; it is shown only once).\n", path)
	}
	fmt.Println("Next: `pvctl status`, then `pvctl map`.")
	return nil
}

func (c *cli) catalog() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cat, err := c.client.GetCatalog(ctx, &protoversev1.GetCatalogRequest{})
	if err != nil {
		return err
	}
	fmt.Printf("Galaxy %.0f×%.0f · tick every %ds · attack range %.0f · colonize range %.0f\n\n",
		cat.GetGalaxySize(), cat.GetGalaxySize(), cat.GetTickIntervalSeconds(), cat.GetAttackRange(), cat.GetColonizeRange())
	fmt.Printf("%-9s %6s %5s %5s %5s %6s %6s  %s\n", "SHIP", "COST", "ATK", "DEF", "HP", "SPEED", "TICKS", "")
	for _, s := range cat.GetShips() {
		fmt.Printf("%-9s %6d %5d %5d %5d %6.0f %6d  %s\n", s.GetShipType(), s.GetCost(), s.GetAttack(), s.GetDefense(), s.GetHp(), s.GetSpeed(), s.GetBuildTicks(), s.GetDescription())
	}
	return nil
}

func (c *cli) leaderboard(args []string) error {
	limit := 10
	if len(args) > 0 {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return errors.New("usage: pvctl leaderboard [count]")
		}
		limit = n
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lb, err := c.client.GetLeaderboard(ctx, &protoversev1.GetLeaderboardRequest{Limit: int32(limit)})
	if err != nil {
		return err
	}
	fmt.Printf("%3s  %-24s %7s %7s %6s %6s\n", "#", "COMMANDER", "SCORE", "PLANETS", "SHIPS", "KILLS")
	for _, e := range lb.GetEntries() {
		fmt.Printf("%3d  %-24s %7d %7d %6d %6d\n", e.GetRank(), e.GetDisplayName(), e.GetScore(), e.GetPlanets(), e.GetShips(), e.GetKills())
	}
	return nil
}

func (c *cli) myState() (*protoversev1.PlayerState, context.Context, context.CancelFunc, error) {
	ctx, cancel, err := c.ctx()
	if err != nil {
		return nil, nil, nil, err
	}
	st, err := c.client.GetMyState(ctx, &protoversev1.GetMyStateRequest{})
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return st, ctx, cancel, nil
}

func (c *cli) status() error {
	st, _, cancel, err := c.myState()
	if err != nil {
		return err
	}
	defer cancel()
	fmt.Printf("Commander %s · %d credits · tick %d\n", st.GetPlayer().GetDisplayName(), st.GetCredits(), st.GetTick())
	fmt.Println("\nPLANETS")
	for _, p := range st.GetPlanets() {
		fmt.Printf("  %s  %-22s (%.1f, %.1f)  +%d/tick  deposit %d\n", shortID(p.GetId()), p.GetName(), p.GetPosition().GetX(), p.GetPosition().GetY(), p.GetProduction(), p.GetResources())
	}
	fmt.Println("\nFLEETS")
	for _, f := range st.GetFleets() {
		where := fmt.Sprintf("at (%.1f, %.1f)", f.GetPosition().GetX(), f.GetPosition().GetY())
		if d := f.GetDestination(); d != nil {
			where = fmt.Sprintf("→ (%.1f, %.1f) arriving %s", d.GetX(), d.GetY(), f.GetArrivesAt().AsTime().Local().Format("15:04:05"))
		}
		fmt.Printf("  %s  %s  %s\n", shortID(f.GetId()), where, shipSummary(f))
	}
	if len(st.GetPendingActions()) > 0 {
		fmt.Println("\nPENDING ORDERS")
		for _, a := range st.GetPendingActions() {
			fmt.Printf("  %s  %s  (resolves %s)\n", shortID(a.GetId()), a.GetDescription(), a.GetExecuteAt().AsTime().Local().Format("15:04:05"))
		}
	}
	return nil
}

func (c *cli) showMap() error {
	st, ctx, cancel, err := c.myState()
	if err != nil {
		return err
	}
	defer cancel()
	g, err := c.client.GetGalaxyState(ctx, &protoversev1.GetGalaxyStateRequest{})
	if err != nil {
		return err
	}
	cat, err := c.client.GetCatalog(ctx, &protoversev1.GetCatalogRequest{})
	if err != nil {
		return err
	}
	fmt.Print(renderMap(g, st.GetPlayer().GetId(), cat.GetGalaxySize()))
	fmt.Println("\nNEUTRAL PLANETS (colonize targets)")
	for _, p := range g.GetPlanets() {
		if p.GetOwnerId() == "" {
			fmt.Printf("  %s  %-14s (%5.1f, %5.1f)  +%d/tick\n", shortID(p.GetId()), p.GetName(), p.GetPosition().GetX(), p.GetPosition().GetY(), p.GetProduction())
		}
	}
	return nil
}

func ackLine(what string, a *protoversev1.ActionAck) {
	fmt.Printf("%s Order %s resolves around %s.\n", what, shortID(a.GetActionId()), a.GetScheduledFor().AsTime().Local().Format("15:04:05"))
}

func (c *cli) build(args []string) error {
	if len(args) < 2 || len(args) > 3 {
		return errors.New("usage: pvctl build <planet> <ship-type> [quantity]")
	}
	qty := 1
	if len(args) == 3 {
		n, err := strconv.Atoi(args[2])
		if err != nil {
			return errors.New("quantity must be a number")
		}
		qty = n
	}
	st, ctx, cancel, err := c.myState()
	if err != nil {
		return err
	}
	defer cancel()
	var ids []string
	for _, p := range st.GetPlanets() {
		ids = append(ids, p.GetId())
	}
	planet, err := resolveID(args[0], ids, "planet of yours")
	if err != nil {
		return err
	}
	a, err := c.client.BuildShip(ctx, &protoversev1.BuildShipRequest{PlanetId: planet, ShipType: args[1], Quantity: int32(qty)})
	if err != nil {
		return err
	}
	ackLine(fmt.Sprintf("Building %d × %s.", qty, args[1]), a)
	return nil
}

func (c *cli) move(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: pvctl move <fleet> <x> <y>")
	}
	x, err1 := strconv.ParseFloat(args[1], 64)
	y, err2 := strconv.ParseFloat(args[2], 64)
	if err1 != nil || err2 != nil {
		return errors.New("x and y must be numbers")
	}
	st, ctx, cancel, err := c.myState()
	if err != nil {
		return err
	}
	defer cancel()
	var ids []string
	for _, f := range st.GetFleets() {
		ids = append(ids, f.GetId())
	}
	fleet, err := resolveID(args[0], ids, "fleet of yours")
	if err != nil {
		return err
	}
	a, err := c.client.MoveFleet(ctx, &protoversev1.MoveFleetRequest{FleetId: fleet, Destination: &protoversev1.Position{X: x, Y: y}})
	if err != nil {
		return err
	}
	ackLine(fmt.Sprintf("Fleet %s departing for (%.1f, %.1f).", shortID(fleet), x, y), a)
	return nil
}

func (c *cli) attack(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: pvctl attack <your-fleet> <target-fleet>")
	}
	st, ctx, cancel, err := c.myState()
	if err != nil {
		return err
	}
	defer cancel()
	g, err := c.client.GetGalaxyState(ctx, &protoversev1.GetGalaxyStateRequest{})
	if err != nil {
		return err
	}
	var mine, all []string
	for _, f := range st.GetFleets() {
		mine = append(mine, f.GetId())
	}
	for _, f := range g.GetFleets() {
		all = append(all, f.GetId())
	}
	attacker, err := resolveID(args[0], mine, "fleet of yours")
	if err != nil {
		return err
	}
	target, err := resolveID(args[1], all, "fleet")
	if err != nil {
		return err
	}
	a, err := c.client.Attack(ctx, &protoversev1.AttackRequest{AttackingFleetId: attacker, TargetFleetId: target})
	if err != nil {
		return err
	}
	ackLine("Attack launched.", a)
	return nil
}

func (c *cli) colonize(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: pvctl colonize <fleet> <planet>")
	}
	st, ctx, cancel, err := c.myState()
	if err != nil {
		return err
	}
	defer cancel()
	g, err := c.client.GetGalaxyState(ctx, &protoversev1.GetGalaxyStateRequest{})
	if err != nil {
		return err
	}
	var fleets, planets []string
	for _, f := range st.GetFleets() {
		fleets = append(fleets, f.GetId())
	}
	for _, p := range g.GetPlanets() {
		planets = append(planets, p.GetId())
	}
	fleet, err := resolveID(args[0], fleets, "fleet of yours")
	if err != nil {
		return err
	}
	planet, err := resolveID(args[1], planets, "planet")
	if err != nil {
		return err
	}
	a, err := c.client.Colonize(ctx, &protoversev1.ColonizeRequest{FleetId: fleet, PlanetId: planet})
	if err != nil {
		return err
	}
	ackLine("Colonization ordered.", a)
	return nil
}

func (c *cli) scan(args []string) error {
	if len(args) != 3 {
		return errors.New("usage: pvctl scan <x> <y> <radius>")
	}
	var v [3]float64
	for i := range v {
		f, err := strconv.ParseFloat(args[i], 64)
		if err != nil {
			return errors.New("x, y and radius must be numbers")
		}
		v[i] = f
	}
	ctx, cancel, err := c.ctx()
	if err != nil {
		return err
	}
	defer cancel()
	res, err := c.client.Scan(ctx, &protoversev1.ScanRequest{Center: &protoversev1.Position{X: v[0], Y: v[1]}, Radius: v[2]})
	if err != nil {
		return err
	}
	for _, p := range res.GetVisiblePlanets() {
		owner := p.GetOwnerName()
		if owner == "" {
			owner = "neutral"
		}
		fmt.Printf("planet %s %-14s (%5.1f, %5.1f) %s\n", shortID(p.GetId()), p.GetName(), p.GetPosition().GetX(), p.GetPosition().GetY(), owner)
	}
	for _, f := range res.GetVisibleFleets() {
		fmt.Printf("fleet  %s %-14s (%5.1f, %5.1f) %s\n", shortID(f.GetId()), f.GetOwnerName(), f.GetPosition().GetX(), f.GetPosition().GetY(), shipSummary(f))
	}
	if len(res.GetVisiblePlanets())+len(res.GetVisibleFleets()) == 0 {
		fmt.Println("nothing in range")
	}
	return nil
}

func (c *cli) watch() error {
	key, err := loadKey()
	if err != nil {
		return err
	}
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+key)
	stream, err := c.client.StreamGameState(ctx, &protoversev1.StreamGameStateRequest{})
	if err != nil {
		return err
	}
	fmt.Println("Watching for events (Ctrl+C to stop)…")
	for {
		ev, err := stream.Recv()
		if err != nil {
			return err
		}
		ts := time.Now().Format("15:04:05")
		switch e := ev.GetEvent().(type) {
		case *protoversev1.GameEvent_Tick:
			fmt.Printf("%s · tick %d\n", ts, e.Tick.GetTick())
		case *protoversev1.GameEvent_IncomeEarned:
			fmt.Printf("%s · +%d credits\n", ts, e.IncomeEarned.GetCredits())
		case *protoversev1.GameEvent_FleetMoved:
			p := e.FleetMoved.GetNewPosition()
			fmt.Printf("%s · fleet %s arrived at (%.1f, %.1f)\n", ts, shortID(e.FleetMoved.GetFleetId()), p.GetX(), p.GetY())
		case *protoversev1.GameEvent_ShipBuilt:
			fmt.Printf("%s · built %d × %s\n", ts, e.ShipBuilt.GetQuantity(), e.ShipBuilt.GetShipType())
		case *protoversev1.GameEvent_PlanetColonized:
			fmt.Printf("%s · colonized %s!\n", ts, e.PlanetColonized.GetPlanetName())
		case *protoversev1.GameEvent_CombatResolved:
			cr := e.CombatResolved
			fmt.Printf("%s · combat %s → %s: %d damage (target destroyed: %v), return fire %d (attacker destroyed: %v), salvage %d/%d\n",
				ts, shortID(cr.GetAttackingFleetId()), shortID(cr.GetTargetFleetId()), cr.GetDamageDealt(), cr.GetTargetDestroyed(),
				cr.GetCounterDamage(), cr.GetAttackerDestroyed(), cr.GetSalvage(), cr.GetCounterSalvage())
		case *protoversev1.GameEvent_ActionFailed:
			fmt.Printf("%s · order failed: %s\n", ts, e.ActionFailed.GetReason())
		}
	}
}
