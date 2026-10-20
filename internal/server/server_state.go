package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/auth"
	"github.com/Atul-Koundal/protoverse/internal/cache"
	"github.com/Atul-Koundal/protoverse/internal/domain"
)

const (
	defaultLeaderboardLimit = 10
	maxLeaderboardLimit     = 50
	maxScanRadius           = 40.0
	leaderboardCacheTTL     = 5 * time.Second
)

// GetMyState is the player's private dashboard.
func (s *GameServer) GetMyState(ctx context.Context, _ *protoversev1.GetMyStateRequest) (*protoversev1.PlayerState, error) {
	caller, err := auth.RequirePlayer(ctx)
	if err != nil {
		return nil, err
	}
	// Reload: the player attached by the interceptor may be a few ms old, and
	// credits change every tick.
	player, err := s.Repo.GetPlayerByID(ctx, caller.ID)
	if err != nil || player == nil {
		return nil, status.Error(codes.Internal, "failed to load player")
	}
	planets, err := s.Repo.ListPlanetsByOwner(ctx, player.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load planets: %v", err)
	}
	fleets, err := s.Repo.ListFleetsByOwner(ctx, player.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load fleets: %v", err)
	}
	pending, err := s.Repo.ListPendingActionsByPlayer(ctx, player.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load orders: %v", err)
	}

	now := time.Now()
	names := map[uuid.UUID]string{player.ID: player.DisplayName}
	state := &protoversev1.PlayerState{
		Player: &protoversev1.Player{
			Id:          player.ID.String(),
			DisplayName: player.DisplayName,
			CreatedAt:   timestamppb.New(player.CreatedAt),
		},
		Credits: player.Credits,
		Tick:    domain.TickNumber(now),
	}
	for i := range planets {
		state.Planets = append(state.Planets, planetToProto(&planets[i], names))
	}
	for i := range fleets {
		state.Fleets = append(state.Fleets, fleetToProto(&fleets[i], names, now))
	}
	for _, a := range pending {
		state.PendingActions = append(state.PendingActions, &protoversev1.PendingAction{
			Id:          a.ID.String(),
			ActionType:  a.ActionType,
			Description: describeAction(a),
			ExecuteAt:   timestamppb.New(a.ExecuteAt),
		})
	}
	return state, nil
}

// describeAction renders a stored order as a short human-readable string.
func describeAction(a domain.Action) string {
	var p map[string]any
	if err := json.Unmarshal(a.Payload, &p); err != nil {
		return a.ActionType
	}
	short := func(key string) string {
		v, _ := p[key].(string)
		if len(v) > 8 {
			return v[:8]
		}
		return v
	}
	switch a.ActionType {
	case "move":
		return fmt.Sprintf("move fleet %s to (%.1f, %.1f)", short("fleet_id"), p["dest_x"], p["dest_y"])
	case "attack":
		return fmt.Sprintf("fleet %s attacks fleet %s", short("attacking_fleet_id"), short("target_fleet_id"))
	case "build":
		return fmt.Sprintf("build %v x %v at planet %s", p["quantity"], p["ship_type"], short("planet_id"))
	case "colonize":
		return fmt.Sprintf("fleet %s colonizes planet %s", short("fleet_id"), short("planet_id"))
	}
	return a.ActionType
}

// GetCatalog returns the static rules of the game.
func (s *GameServer) GetCatalog(_ context.Context, _ *protoversev1.GetCatalogRequest) (*protoversev1.Catalog, error) {
	cat := &protoversev1.Catalog{
		TickIntervalSeconds: int32(TickInterval / time.Second),
		GalaxySize:          domain.GalaxySize,
		AttackRange:         domain.AttackRange,
		ColonizeRange:       domain.ColonizeRange,
		CurrentTick:         domain.TickNumber(time.Now()),
		MaxBuildQuantity:    domain.MaxBuildQuantity,
	}
	for _, sp := range domain.ShipCatalog() {
		cat.Ships = append(cat.Ships, &protoversev1.ShipSpec{
			ShipType:    sp.Type,
			Description: sp.Description,
			Attack:      int32(sp.Attack),
			Defense:     int32(sp.Defense),
			Hp:          int32(sp.HP),
			Speed:       sp.Speed,
			Cost:        sp.Cost,
			BuildTicks:  int32(sp.BuildTicks),
		})
	}
	return cat, nil
}

// GetLeaderboard ranks players by score. The full top-50 is cached briefly;
// the requested limit just slices it.
func (s *GameServer) GetLeaderboard(ctx context.Context, req *protoversev1.GetLeaderboardRequest) (*protoversev1.Leaderboard, error) {
	limit := int(req.GetLimit())
	if limit <= 0 {
		limit = defaultLeaderboardLimit
	}
	if limit > maxLeaderboardLimit {
		limit = maxLeaderboardLimit
	}

	var full protoversev1.Leaderboard
	found, err := s.Cache.GetProto(ctx, cache.LeaderboardKey, &full)
	if err != nil {
		log.Printf("GetLeaderboard: cache read error: %v", err)
		found = false
	}
	if !found {
		standings, err := s.Repo.LeaderboardStandings(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute leaderboard: %v", err)
		}
		for i, st := range standings {
			if i >= maxLeaderboardLimit {
				break
			}
			full.Entries = append(full.Entries, &protoversev1.LeaderboardEntry{
				Rank:        int32(i + 1),
				PlayerId:    st.PlayerID.String(),
				DisplayName: st.DisplayName,
				Score:       st.Score,
				Planets:     int32(st.Planets),
				Ships:       int32(st.Ships),
				Kills:       int32(st.Kills),
				Credits:     st.Credits,
			})
		}
		if err := s.Cache.SetProto(ctx, cache.LeaderboardKey, &full, leaderboardCacheTTL); err != nil {
			log.Printf("GetLeaderboard: failed to cache: %v", err)
		}
	}

	out := &protoversev1.Leaderboard{}
	for i, e := range full.Entries {
		if i >= limit {
			break
		}
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

// Scan returns everything within `radius` of `center`.
func (s *GameServer) Scan(ctx context.Context, req *protoversev1.ScanRequest) (*protoversev1.ScanResult, error) {
	if _, err := auth.RequirePlayer(ctx); err != nil {
		return nil, err
	}
	c := req.GetCenter()
	if !domain.InBounds(c.GetX(), c.GetY()) {
		return nil, status.Error(codes.InvalidArgument, "center is outside the galaxy")
	}
	radius := req.GetRadius()
	if radius <= 0 || radius > maxScanRadius {
		return nil, status.Errorf(codes.InvalidArgument, "radius must be between 0 and %.0f", maxScanRadius)
	}

	planets, err := s.Repo.ListPlanets(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load planets: %v", err)
	}
	fleets, err := s.Repo.ListFleetsWithShips(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load fleets: %v", err)
	}
	names, err := s.Repo.PlayerNames(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load players: %v", err)
	}

	now := time.Now()
	out := &protoversev1.ScanResult{}
	for i := range planets {
		if domain.Distance(c.GetX(), c.GetY(), planets[i].PosX, planets[i].PosY) <= radius {
			out.VisiblePlanets = append(out.VisiblePlanets, planetToProto(&planets[i], names))
		}
	}
	for i := range fleets {
		x, y := fleets[i].PositionAt(now)
		if domain.Distance(c.GetX(), c.GetY(), x, y) <= radius {
			out.VisibleFleets = append(out.VisibleFleets, fleetToProto(&fleets[i], names, now))
		}
	}
	return out, nil
}
