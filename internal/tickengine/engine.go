package tickengine

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

const (
	TickInterval  = 10 * time.Second
	BatchLimit    = 50
	EventsChannel = "tick.events"
)

type Engine struct {
	Repo  *repository.Repository
	Queue *queue.Queue
}

func New(repo *repository.Repository, q *queue.Queue) *Engine {
	return &Engine{Repo: repo, Queue: q}
}

// Run blocks, ticking every TickInterval until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()

	log.Println("tick engine started, interval:", TickInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("tick engine shutting down")
			return
		case now := <-ticker.C:
			e.processTick(ctx, now)
		}
	}
}

func (e *Engine) processTick(ctx context.Context, now time.Time) {
	actionIDs, err := e.Queue.PopDueActions(ctx, now, BatchLimit)
	if err != nil {
		log.Printf("tick: failed to pop due actions: %v", err)
		return
	}
	if len(actionIDs) == 0 {
		return
	}
	log.Printf("tick: resolving %d due action(s)", len(actionIDs))

	for _, id := range actionIDs {
		if err := e.resolveAction(ctx, id); err != nil {
			log.Printf("tick: failed to resolve action %s: %v", id, err)
		}
	}
}

func (e *Engine) resolveAction(ctx context.Context, actionID uuid.UUID) error {
	return e.Repo.WithTx(ctx, func(tx pgxTx) error {
		action, err := e.Repo.TxGetActionByID(ctx, tx, actionID)
		if err != nil {
			return err
		}
		if action == nil || action.Status != "pending" {
			return nil // vanished or already resolved — nothing to do
		}

		switch action.ActionType {
		case "move":
			err = e.resolveMove(ctx, tx, action)
		case "attack":
			err = e.resolveAttack(ctx, tx, action)
		default:
			log.Printf("tick: unknown action type %q, marking failed", action.ActionType)
			return e.Repo.TxMarkActionResolved(ctx, tx, action.ID, "failed")
		}
		if err != nil {
			return err
		}

		return e.Repo.TxMarkActionResolved(ctx, tx, action.ID, "resolved")
	})
}
