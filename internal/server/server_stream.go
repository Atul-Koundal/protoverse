package server

import (
	"encoding/json"
	"log"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
	"github.com/Atul-Koundal/protoverse/internal/tickengine"
)

// rawEvent mirrors the JSON shape the tick engine publishes in
// internal/tickengine/resolvers.go — kept as one flat struct since a single
// tick event is only ever one of the two shapes, distinguished by Type.
type rawEvent struct {
	Type             string  `json:"type"`
	FleetID          string  `json:"fleet_id"`
	X                float64 `json:"x"`
	Y                float64 `json:"y"`
	AttackingFleetID string  `json:"attacking_fleet_id"`
	TargetFleetID    string  `json:"target_fleet_id"`
	DamageDealt      int32   `json:"damage_dealt"`
	TargetDestroyed  bool    `json:"target_destroyed"`
}

func parseGameEvent(payload string) (*protoversev1.GameEvent, error) {
	var raw rawEvent
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return nil, err
	}

	switch raw.Type {
	case "fleet_moved":
		return &protoversev1.GameEvent{
			Event: &protoversev1.GameEvent_FleetMoved{
				FleetMoved: &protoversev1.FleetMoved{
					FleetId:     raw.FleetID,
					NewPosition: &protoversev1.Position{X: raw.X, Y: raw.Y},
				},
			},
		}, nil
	case "combat_resolved":
		return &protoversev1.GameEvent{
			Event: &protoversev1.GameEvent_CombatResolved{
				CombatResolved: &protoversev1.CombatResolved{
					AttackingFleetId: raw.AttackingFleetID,
					TargetFleetId:    raw.TargetFleetID,
					DamageDealt:      raw.DamageDealt,
					TargetDestroyed:  raw.TargetDestroyed,
				},
			},
		}, nil
	default:
		return nil, nil // unknown event type — skip rather than fail the whole stream
	}
}

// StreamGameState subscribes to the tick engine's Redis pub/sub channel and
// forwards every event to the connected gRPC client as it arrives. Note:
// this is a v1 simplification — it broadcasts every event to every
// connected client regardless of player_id. Proper per-player filtering
// (only send events relevant to that player's fleets) is a good next
// improvement once this is working end to end.
func (s *GameServer) StreamGameState(req *protoversev1.StreamGameStateRequest, stream protoversev1.GameService_StreamGameStateServer) error {
	ctx := stream.Context()

	pubsub := s.Queue.Client.Subscribe(ctx, tickengine.EventsChannel)
	defer pubsub.Close()

	ch := pubsub.Channel()
	log.Printf("stream: client subscribed (player_id=%s)", req.GetPlayerId())

	for {
		select {
		case <-ctx.Done():
			log.Printf("stream: client disconnected (player_id=%s)", req.GetPlayerId())
			return nil
		case msg, ok := <-ch:
			if !ok {
				return nil
			}
			event, err := parseGameEvent(msg.Payload)
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
