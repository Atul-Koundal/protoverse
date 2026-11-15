package server

import (
	"context"
	"log"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/auth"
	"github.com/Atul-Koundal/protoverse/internal/cache"
	"github.com/Atul-Koundal/protoverse/internal/domain"
	"github.com/Atul-Koundal/protoverse/internal/galaxy"
	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

const TickInterval = domain.TickInterval

const galaxyStateCacheTTL = 5 * time.Second

// displayNameRe: 3-24 chars, letters/digits/underscore/hyphen/space, and it
// must start and end with a letter, digit or underscore.
var displayNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_ -]{1,22}[A-Za-z0-9_]$`)

type GameServer struct {
	protoversev1.UnimplementedGameServiceServer
	Repo  *repository.Repository
	Queue *queue.Queue
	Cache *cache.Cache

	rngMu sync.Mutex
	rng   *rand.Rand
}

func New(repo *repository.Repository, q *queue.Queue, c *cache.Cache) *GameServer {
	return &GameServer{
		Repo:  repo,
		Queue: q,
		Cache: c,
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ValidateDisplayName reports whether name is acceptable for a new account.
func ValidateDisplayName(name string) error {
	if !displayNameRe.MatchString(name) {
		return status.Error(codes.InvalidArgument,
			"display_name must be 3-24 characters: letters, digits, spaces, '_' or '-' (starting and ending with a letter, digit or '_')")
	}
	return nil
}

func (s *GameServer) CreateAccount(ctx context.Context, req *protoversev1.CreateAccountRequest) (*protoversev1.Player, error) {
	name := strings.TrimSpace(req.GetDisplayName())
	if err := ValidateDisplayName(name); err != nil {
		return nil, err
	}

	rawKey, keyHash, err := auth.NewKey()
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to generate API key")
	}

	planets, err := s.Repo.ListPlanets(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to load galaxy: %v", err)
	}
	s.rngMu.Lock()
	homeX, homeY := galaxy.PickHome(s.rng, planets)
	s.rngMu.Unlock()

	player, planet, _, err := s.Repo.RegisterPlayer(ctx, name, keyHash, homeX, homeY)
	if err != nil {
		if repository.IsUniqueViolation(err) {
			return nil, status.Errorf(codes.AlreadyExists, "display name %q is already taken", name)
		}
		return nil, status.Errorf(codes.Internal, "failed to create account: %v", err)
	}
	log.Printf("account created: %s (home planet %s at %.1f,%.1f)", player.DisplayName, planet.Name, planet.PosX, planet.PosY)

	// New planet + new player name: drop the cached public map.
	if err := s.Cache.Invalidate(ctx, cache.GalaxyStateKey, cache.LeaderboardKey); err != nil {
		log.Printf("CreateAccount: failed to invalidate caches: %v", err)
	}

	return &protoversev1.Player{
		Id:          player.ID.String(),
		DisplayName: player.DisplayName,
		CreatedAt:   timestamppb.New(player.CreatedAt),
		ApiKey:      rawKey,
	}, nil
}

func (s *GameServer) GetGalaxyState(ctx context.Context, _ *protoversev1.GetGalaxyStateRequest) (*protoversev1.GalaxyState, error) {
	var cached protoversev1.GalaxyState
	found, err := s.Cache.GetProto(ctx, cache.GalaxyStateKey, &cached)
	if err != nil {
		log.Printf("GetGalaxyState: cache read error (falling back to DB): %v", err)
		found = false
	}
	if found {
		return &cached, nil
	}

	state, err := s.buildGalaxyStateFromDB(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Cache.SetProto(ctx, cache.GalaxyStateKey, state, galaxyStateCacheTTL); err != nil {
		log.Printf("GetGalaxyState: failed to cache result: %v", err)
	}
	return state, nil
}

func (s *GameServer) buildGalaxyStateFromDB(ctx context.Context) (*protoversev1.GalaxyState, error) {
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
	return buildState(planets, fleets, names, time.Now()), nil
}

func buildState(planets []domain.Planet, fleets []domain.Fleet, names map[uuid.UUID]string, now time.Time) *protoversev1.GalaxyState {
	state := &protoversev1.GalaxyState{}
	for i := range planets {
		state.Planets = append(state.Planets, planetToProto(&planets[i], names))
	}
	for i := range fleets {
		state.Fleets = append(state.Fleets, fleetToProto(&fleets[i], names, now))
	}
	return state
}

func planetToProto(p *domain.Planet, names map[uuid.UUID]string) *protoversev1.Planet {
	out := &protoversev1.Planet{
		Id:         p.ID.String(),
		Name:       p.Name,
		Position:   &protoversev1.Position{X: p.PosX, Y: p.PosY},
		Resources:  p.Resources,
		Production: int32(p.Production),

		DefenseLevel: int32(p.DefenseLevel),
		Shield:       int32(p.Shield),
		MaxShield:    int32(p.MaxShield()),
	}
	if p.OwnerID != nil {
		out.OwnerId = p.OwnerID.String()
		out.OwnerName = names[*p.OwnerID]
	}
	return out
}

func fleetToProto(f *domain.Fleet, names map[uuid.UUID]string, now time.Time) *protoversev1.Fleet {
	x, y := f.PositionAt(now)
	out := &protoversev1.Fleet{
		Id:        f.ID.String(),
		OwnerId:   f.OwnerID.String(),
		OwnerName: names[f.OwnerID],
		Position:  &protoversev1.Position{X: x, Y: y},
		Speed:     f.Speed(),
	}
	if f.DestX != nil && f.DestY != nil {
		out.Destination = &protoversev1.Position{X: *f.DestX, Y: *f.DestY}
	}
	if f.DepartedAt != nil {
		out.DepartedAt = timestamppb.New(*f.DepartedAt)
	}
	if f.ArrivesAt != nil {
		out.ArrivesAt = timestamppb.New(*f.ArrivesAt)
	}
	for _, sh := range f.Ships {
		out.Ships = append(out.Ships, &protoversev1.Ship{
			Id:       sh.ID.String(),
			FleetId:  sh.FleetID.String(),
			ShipType: sh.ShipType,
			Attack:   int32(sh.Attack),
			Defense:  int32(sh.Defense),
			Hp:       int32(sh.HP),
			MaxHp:    int32(sh.MaxHP),
		})
	}
	return out
}
