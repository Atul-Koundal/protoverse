package server

import (
	"log"

	"google.golang.org/protobuf/proto"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/auth"
	"github.com/Atul-Koundal/protoverse/internal/events"
)

// toProtoEvent converts an internal event into its gRPC form. Unknown event
// types return nil so a newer engine can't break an older API server.
func toProtoEvent(ev events.Event) *protoversev1.GameEvent {
	switch ev.Type {
	case events.TypeFleetMoved:
		return &protoversev1.GameEvent{Event: &protoversev1.GameEvent_FleetMoved{FleetMoved: &protoversev1.FleetMoved{
			FleetId:     ev.FleetID,
			NewPosition: &protoversev1.Position{X: ev.X, Y: ev.Y},
		}}}
	case events.TypeCombatResolved:
		return &protoversev1.GameEvent{Event: &protoversev1.GameEvent_CombatResolved{CombatResolved: &protoversev1.CombatResolved{
			AttackingFleetId:  ev.AttackingFleetID,
			TargetFleetId:     ev.TargetFleetID,
			DamageDealt:       ev.DamageDealt,
			TargetDestroyed:   ev.TargetDestroyed,
			CounterDamage:     ev.CounterDamage,
			AttackerDestroyed: ev.AttackerDestroyed,
			Salvage:           ev.Salvage,
			CounterSalvage:    ev.CounterSalvage,
		}}}
	case events.TypeShipBuilt:
		return &protoversev1.GameEvent{Event: &protoversev1.GameEvent_ShipBuilt{ShipBuilt: &protoversev1.ShipBuilt{
			PlanetId: ev.PlanetID,
			ShipType: ev.ShipType,
			Quantity: ev.Quantity,
			FleetId:  ev.FleetID,
		}}}
	case events.TypePlanetColonized:
		return &protoversev1.GameEvent{Event: &protoversev1.GameEvent_PlanetColonized{PlanetColonized: &protoversev1.PlanetColonized{
			PlanetId:   ev.PlanetID,
			PlanetName: ev.PlanetName,
		}}}
	case events.TypeIncome:
		return &protoversev1.GameEvent{Event: &protoversev1.GameEvent_IncomeEarned{IncomeEarned: &protoversev1.IncomeEarned{
			Credits: ev.Credits,
		}}}
	case events.TypeActionFailed:
		return &protoversev1.GameEvent{Event: &protoversev1.GameEvent_ActionFailed{ActionFailed: &protoversev1.ActionFailed{
			ActionId: ev.ActionID,
			Reason:   ev.Reason,
		}}}
	case events.TypeTick:
		return &protoversev1.GameEvent{Event: &protoversev1.GameEvent_Tick{Tick: &protoversev1.Tick{Tick: ev.Tick}}}
	}
	return nil
}

// parseGameEvent decodes a Redis payload and converts it, returning nil for
// events the given player should not see (or doesn't understand).
func parseGameEvent(payload, playerID string) (*protoversev1.GameEvent, error) {
	ev, err := events.Parse(payload)
	if err != nil {
		return nil, err
	}
	if !ev.VisibleTo(playerID) {
		return nil, nil
	}
	return toProtoEvent(ev), nil
}

// StreamGameState relays the tick engine's Redis pub/sub events to the
// authenticated player, filtered so they only see events about themselves
// (their fleets, fights, income, orders) plus the per-tick heartbeat.
func (s *GameServer) StreamGameState(_ *protoversev1.StreamGameStateRequest, stream protoversev1.GameService_StreamGameStateServer) error {
	ctx := stream.Context()
	player, err := auth.RequirePlayer(ctx)
	if err != nil {
		return err
	}
	playerID := player.ID.String()

	pubsub := s.Queue.Client.Subscribe(ctx, events.Channel)
	defer func() { _ = pubsub.Close() }()

	ch := pubsub.Channel()
	log.Printf("stream: %s subscribed", player.DisplayName)

	for {
		select {
		case <-ctx.Done():
			log.Printf("stream: %s disconnected", player.DisplayName)
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			event, err := parseGameEvent(msg.Payload, playerID)
			if err != nil {
				log.Printf("stream: failed to parse event: %v", err)
				continue
			}
			if event == nil {
				continue
			}
			if err := stream.Send(event); err != nil {
				return err
			}
		}
	}
}

var _ proto.Message = (*protoversev1.GameEvent)(nil)
