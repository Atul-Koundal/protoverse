package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/auth"
	"github.com/Atul-Koundal/protoverse/internal/domain"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

// loadOwnedFleet fetches a fleet and verifies the caller owns it.
func (s *GameServer) loadOwnedFleet(ctx context.Context, player *domain.Player, rawID, label string) (*domain.Fleet, error) {
	id, err := uuid.Parse(rawID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid %s: %v", label, err)
	}
	fleet, err := s.Repo.GetFleetWithShips(ctx, id)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load fleet: %v", err)
	}
	if fleet == nil {
		return nil, status.Errorf(codes.NotFound, "%s not found", label)
	}
	// Don't reveal that someone else's fleet exists: report "not found".
	if fleet.OwnerID != player.ID {
		return nil, status.Errorf(codes.NotFound, "%s not found", label)
	}
	return fleet, nil
}

// enqueue pushes an already-persisted action onto the Redis schedule. The
// action is safe in Postgres either way: if Redis is down the tick engine's
// stale-action recovery will still resolve it, just a few ticks late.
func (s *GameServer) enqueue(ctx context.Context, action *domain.Action) {
	_ = s.Queue.Enqueue(ctx, action.ID, action.ExecuteAt)
}

func ack(action *domain.Action) *protoversev1.ActionAck {
	return &protoversev1.ActionAck{
		ActionId:     action.ID.String(),
		ScheduledFor: timestamppb.New(action.ExecuteAt),
	}
}

// mapActionErr converts repository sentinel errors into gRPC statuses.
func mapActionErr(err error, what string) error {
	switch {
	case errors.Is(err, repository.ErrInsufficientCredits):
		return status.Error(codes.FailedPrecondition, "not enough credits")
	case errors.Is(err, repository.ErrConflict), repository.IsUniqueViolation(err):
		return status.Errorf(codes.FailedPrecondition, "%s", what)
	}
	return status.Errorf(codes.Internal, "failed to create action: %v", err)
}

func (s *GameServer) MoveFleet(ctx context.Context, req *protoversev1.MoveFleetRequest) (*protoversev1.ActionAck, error) {
	player, err := auth.RequirePlayer(ctx)
	if err != nil {
		return nil, err
	}
	fleet, err := s.loadOwnedFleet(ctx, player, req.GetFleetId(), "fleet_id")
	if err != nil {
		return nil, err
	}

	dest := req.GetDestination()
	if dest == nil || !domain.InBounds(dest.GetX(), dest.GetY()) {
		return nil, status.Errorf(codes.InvalidArgument, "destination must be inside the galaxy (0..%.0f)", domain.GalaxySize)
	}
	speed := fleet.Speed()
	if speed <= 0 {
		return nil, status.Error(codes.FailedPrecondition, "fleet has no ships")
	}

	now := time.Now()
	dist := domain.Distance(fleet.PosX, fleet.PosY, dest.GetX(), dest.GetY())
	arrivesAt := domain.ExecuteAfterTicks(now, domain.TravelTicks(dist, speed))

	payload, err := json.Marshal(map[string]any{
		"fleet_id": fleet.ID.String(),
		"dest_x":   dest.GetX(),
		"dest_y":   dest.GetY(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode payload: %v", err)
	}

	action, err := s.Repo.CreateMoveAction(ctx, player.ID, fleet.ID, dest.GetX(), dest.GetY(), now, arrivesAt, payload)
	if err != nil {
		return nil, mapActionErr(err, "that fleet is already moving")
	}
	s.enqueue(ctx, action)
	return ack(action), nil
}

func (s *GameServer) Attack(ctx context.Context, req *protoversev1.AttackRequest) (*protoversev1.ActionAck, error) {
	player, err := auth.RequirePlayer(ctx)
	if err != nil {
		return nil, err
	}
	attacker, err := s.loadOwnedFleet(ctx, player, req.GetAttackingFleetId(), "attacking_fleet_id")
	if err != nil {
		return nil, err
	}
	targetID, err := uuid.Parse(req.GetTargetFleetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid target_fleet_id: %v", err)
	}
	target, err := s.Repo.GetFleetWithShips(ctx, targetID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load target fleet: %v", err)
	}
	if target == nil {
		return nil, status.Error(codes.NotFound, "target fleet not found")
	}
	if target.OwnerID == player.ID {
		return nil, status.Error(codes.InvalidArgument, "you cannot attack your own fleet")
	}

	// Early feedback only — the tick engine re-checks range when the attack
	// actually resolves, because fleets keep moving in between.
	now := time.Now()
	ax, ay := attacker.PositionAt(now)
	tx, ty := target.PositionAt(now)
	if dist := domain.Distance(ax, ay, tx, ty); dist > domain.AttackRange {
		return nil, status.Errorf(codes.FailedPrecondition,
			"target is out of range (%.1f away, attack range is %.0f)", dist, domain.AttackRange)
	}

	payload, err := json.Marshal(map[string]any{
		"attacking_fleet_id": attacker.ID.String(),
		"target_fleet_id":    target.ID.String(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode payload: %v", err)
	}
	action, err := s.Repo.CreateAction(ctx, player.ID, "attack", payload, domain.ExecuteAfterTicks(now, 1))
	if err != nil {
		return nil, mapActionErr(err, "that fleet already has an attack queued this tick")
	}
	s.enqueue(ctx, action)
	return ack(action), nil
}

func (s *GameServer) BuildShip(ctx context.Context, req *protoversev1.BuildShipRequest) (*protoversev1.ActionAck, error) {
	player, err := auth.RequirePlayer(ctx)
	if err != nil {
		return nil, err
	}
	planetID, err := uuid.Parse(req.GetPlanetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid planet_id: %v", err)
	}
	spec, ok := domain.LookupShip(req.GetShipType())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown ship_type %q (see GetCatalog)", req.GetShipType())
	}
	qty := int(req.GetQuantity())
	if qty == 0 {
		qty = 1
	}
	if qty < 1 || qty > domain.MaxBuildQuantity {
		return nil, status.Errorf(codes.InvalidArgument, "quantity must be between 1 and %d", domain.MaxBuildQuantity)
	}

	planet, err := s.Repo.GetPlanetByID(ctx, planetID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load planet: %v", err)
	}
	if planet == nil || planet.OwnerID == nil || *planet.OwnerID != player.ID {
		return nil, status.Error(codes.NotFound, "planet not found among your planets")
	}

	cost := spec.Cost * int64(qty)
	payload, err := json.Marshal(map[string]any{
		"planet_id": planet.ID.String(),
		"ship_type": spec.Type,
		"quantity":  qty,
		"cost":      cost,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode payload: %v", err)
	}

	executeAt := domain.ExecuteAfterTicks(time.Now(), spec.BuildTicks)
	action, err := s.Repo.CreateBuildAction(ctx, player.ID, cost, payload, executeAt)
	if err != nil {
		if errors.Is(err, repository.ErrInsufficientCredits) {
			return nil, status.Error(codes.FailedPrecondition, fmt.Sprintf("not enough credits: need %d for %d x %s", cost, qty, spec.Type))
		}
		return nil, mapActionErr(err, "could not queue build")
	}
	s.enqueue(ctx, action)
	return ack(action), nil
}

func (s *GameServer) Colonize(ctx context.Context, req *protoversev1.ColonizeRequest) (*protoversev1.ActionAck, error) {
	player, err := auth.RequirePlayer(ctx)
	if err != nil {
		return nil, err
	}
	fleet, err := s.loadOwnedFleet(ctx, player, req.GetFleetId(), "fleet_id")
	if err != nil {
		return nil, err
	}
	planetID, err := uuid.Parse(req.GetPlanetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid planet_id: %v", err)
	}
	planet, err := s.Repo.GetPlanetByID(ctx, planetID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load planet: %v", err)
	}
	if planet == nil {
		return nil, status.Error(codes.NotFound, "planet not found")
	}
	if planet.OwnerID != nil {
		return nil, status.Error(codes.FailedPrecondition, "that planet is already owned")
	}
	if fleet.CountShips(domain.ShipColony) == 0 {
		return nil, status.Error(codes.FailedPrecondition, "fleet has no colony ship (build one first)")
	}

	now := time.Now()
	fx, fy := fleet.PositionAt(now)
	if dist := domain.Distance(fx, fy, planet.PosX, planet.PosY); dist > domain.ColonizeRange {
		return nil, status.Errorf(codes.FailedPrecondition,
			"fleet is %.1f from the planet; move within %.0f first", dist, domain.ColonizeRange)
	}

	payload, err := json.Marshal(map[string]any{
		"fleet_id":  fleet.ID.String(),
		"planet_id": planet.ID.String(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode payload: %v", err)
	}
	action, err := s.Repo.CreateAction(ctx, player.ID, "colonize", payload, domain.ExecuteAfterTicks(now, 1))
	if err != nil {
		return nil, mapActionErr(err, "that planet is already being colonized")
	}
	s.enqueue(ctx, action)
	return ack(action), nil
}
