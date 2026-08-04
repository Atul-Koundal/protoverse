package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
)

func (s *GameServer) MoveFleet(ctx context.Context, req *protoversev1.MoveFleetRequest) (*protoversev1.ActionAck, error) {
	fleetID, err := uuid.Parse(req.GetFleetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid fleet_id: %v", err)
	}

	fleet, err := s.Repo.GetFleetWithShips(ctx, fleetID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load fleet: %v", err)
	}
	if fleet == nil {
		return nil, status.Errorf(codes.NotFound, "fleet not found")
	}

	dest := req.GetDestination()
	payload, err := json.Marshal(map[string]interface{}{
		"fleet_id": fleetID.String(),
		"dest_x":   dest.GetX(),
		"dest_y":   dest.GetY(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode payload: %v", err)
	}

	executeAt := time.Now().Add(TickInterval)
	action, err := s.Repo.CreateAction(ctx, fleet.OwnerID, "move", payload, executeAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create action: %v", err)
	}

	if err := s.Queue.Enqueue(ctx, action.ID, executeAt); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to enqueue action: %v", err)
	}

	return &protoversev1.ActionAck{
		ActionId:     action.ID.String(),
		ScheduledFor: timestamppb.New(executeAt),
	}, nil
}

func (s *GameServer) Attack(ctx context.Context, req *protoversev1.AttackRequest) (*protoversev1.ActionAck, error) {
	attackingFleetID, err := uuid.Parse(req.GetAttackingFleetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid attacking_fleet_id: %v", err)
	}
	targetFleetID, err := uuid.Parse(req.GetTargetFleetId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid target_fleet_id: %v", err)
	}

	attackingFleet, err := s.Repo.GetFleetWithShips(ctx, attackingFleetID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load attacking fleet: %v", err)
	}
	if attackingFleet == nil {
		return nil, status.Errorf(codes.NotFound, "attacking fleet not found")
	}

	payload, err := json.Marshal(map[string]interface{}{
		"attacking_fleet_id": attackingFleetID.String(),
		"target_fleet_id":    targetFleetID.String(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to encode payload: %v", err)
	}

	executeAt := time.Now().Add(TickInterval)
	action, err := s.Repo.CreateAction(ctx, attackingFleet.OwnerID, "attack", payload, executeAt)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create action: %v", err)
	}

	if err := s.Queue.Enqueue(ctx, action.ID, executeAt); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to enqueue action: %v", err)
	}

	return &protoversev1.ActionAck{
		ActionId:     action.ID.String(),
		ScheduledFor: timestamppb.New(executeAt),
	}, nil
}
