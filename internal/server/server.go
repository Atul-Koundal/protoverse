package server

import (
	"context"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/cache"
	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

const TickInterval = 10 * time.Second

// galaxyStateCacheKey and TTL are short and deliberately named — 5s means a
// burst of clients calling GetGalaxyState at once hits Postgres once, not
// once per request, while still staying close to real-time for a 10s tick.
const (
	galaxyStateCacheKey = "cache:galaxy_state"
	galaxyStateCacheTTL = 5 * time.Second
)

type GameServer struct {
	protoversev1.UnimplementedGameServiceServer
	Repo  *repository.Repository
	Queue *queue.Queue
	Cache *cache.Cache
}

func New(repo *repository.Repository, q *queue.Queue, c *cache.Cache) *GameServer {
	return &GameServer{Repo: repo, Queue: q, Cache: c}
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
	var cached protoversev1.GalaxyState
	found, err := s.Cache.GetJSON(ctx, galaxyStateCacheKey, &cached)
	if err != nil {
		// Cache errors shouldn't break the request — fall through to Postgres.
		found = false
	}
	if found {
		return &cached, nil
	}

	state, err := s.buildGalaxyStateFromDB(ctx)
	if err != nil {
		return nil, err
	}

	if err := s.Cache.SetJSON(ctx, galaxyStateCacheKey, state, galaxyStateCacheTTL); err != nil {
		// Log-worthy but not fatal — worth adding real logging here later.
		_ = err
	}

	return state, nil
}

func (s *GameServer) buildGalaxyStateFromDB(ctx context.Context) (*protoversev1.GalaxyState, error) {
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
