package server

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

// GameServer implements the GameService gRPC interface.
type GameServer struct {
	protoversev1.UnimplementedGameServiceServer
	Repo *repository.Repository
}

func New(repo *repository.Repository) *GameServer {
	return &GameServer{Repo: repo}
}

func (s *GameServer) CreateAccount(ctx context.Context, req *protoversev1.CreateAccountRequest) (*protoversev1.Player, error) {
	apiKey := uuid.NewString()
	player, err := s.Repo.CreatePlayer(ctx, req.GetDisplayName(), apiKey)
	if err != nil {
		return nil, err
	}
	return &protoversev1.Player{
		Id:          player.ID.String(),
		DisplayName: player.DisplayName,
		CreatedAt:   timestamppb.New(player.CreatedAt),
	}, nil
}

func (s *GameServer) GetGalaxyState(ctx context.Context, req *protoversev1.GetGalaxyStateRequest) (*protoversev1.GalaxyState, error) {
	planets, err := s.Repo.ListPlanets(ctx)
	if err != nil {
		return nil, err
	}
	fleets, err := s.Repo.ListFleetsWithShips(ctx)
	if err != nil {
		return nil, err
	}

	state := &protoversev1.GalaxyState{}

	for _, p := range planets {
		ownerID := ""
		if p.OwnerID != nil {
			ownerID = p.OwnerID.String()
		}
		state.Planets = append(state.Planets, &protoversev1.Planet{
			Id:        p.ID.String(),
			OwnerId:   ownerID,
			Name:      p.Name,
			Position:  &protoversev1.Position{X: p.PosX, Y: p.PosY},
			Resources: p.Resources,
		})
	}

	for _, f := range fleets {
		pbFleet := &protoversev1.Fleet{
			Id:       f.ID.String(),
			OwnerId:  f.OwnerID.String(),
			Position: &protoversev1.Position{X: f.PosX, Y: f.PosY},
		}
		if f.DestX != nil && f.DestY != nil {
			pbFleet.Destination = &protoversev1.Position{X: *f.DestX, Y: *f.DestY}
		}
		for _, sh := range f.Ships {
			pbFleet.Ships = append(pbFleet.Ships, &protoversev1.Ship{
				Id:       sh.ID.String(),
				FleetId:  sh.FleetID.String(),
				ShipType: sh.ShipType,
				Attack:   int32(sh.Attack),
				Defense:  int32(sh.Defense),
				Hp:       int32(sh.HP),
			})
		}
		state.Fleets = append(state.Fleets, pbFleet)
	}

	return state, nil
}
