package tickengine

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/Atul-Koundal/protoverse/internal/cache"
	"github.com/Atul-Koundal/protoverse/internal/domain"
	"github.com/Atul-Koundal/protoverse/internal/events"
	"github.com/Atul-Koundal/protoverse/internal/queue"
	"github.com/Atul-Koundal/protoverse/internal/repository"
)

const (
	TickInterval  = domain.TickInterval
	BatchLimit    = 200
	EventsChannel = events.Channel

	// staleAfter is how overdue a pending action must be before the engine
	// assumes Redis lost it and resolves it straight from Postgres.
	staleAfter = 3 * TickInterval

	// keepTicks is how many rows of the ticks table to retain.
	keepTicks = 10_000
)

type Engine struct {
	Repo  *repository.Repository
	Queue *queue.Queue
	Cache *cache.Cache
}

func New(repo *repository.Repository, q *queue.Queue) *Engine {
	return &Engine{Repo: repo, Queue: q, Cache: cache.New(q.Client)}
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
			e.ProcessTick(ctx, now)
		}
	}
}

// ProcessTick advances the world by one tick as of time `now`:
//
//  1. resolve every order that has come due,
//  2. pay out planet income (once per tick, even with several engine replicas),
//  3. tell connected players the tick happened.
//
// It is exported so tests (and tooling) can drive time by hand.
func (e *Engine) ProcessTick(ctx context.Context, now time.Time) {
	changed := false
	for _, id := range e.dueActionIDs(ctx, now) {
		if err := e.resolveAction(ctx, id, now); err != nil {
			log.Printf("tick: failed to resolve action %s: %v", id, err)
			continue
		}
		changed = true
	}

	claimed := e.collectIncome(ctx, now)

	if changed || claimed {
		if err := e.Cache.Invalidate(ctx, cache.GalaxyStateKey, cache.LeaderboardKey); err != nil {
			log.Printf("tick: failed to invalidate caches: %v", err)
		}
	}
	if claimed {
		e.publish(ctx, events.Event{Type: events.TypeTick, Tick: domain.TickNumber(now)})
	}
}

// dueActionIDs gathers the actions to resolve this tick: everything Redis says
// is due, plus anything Postgres says is badly overdue (a safety net for
// actions lost between "popped from Redis" and "committed to Postgres").
func (e *Engine) dueActionIDs(ctx context.Context, now time.Time) []uuid.UUID {
	seen := make(map[uuid.UUID]bool)
	var ids []uuid.UUID
	add := func(list []uuid.UUID) {
		for _, id := range list {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}

	popped, err := e.Queue.PopDueActions(ctx, now, BatchLimit)
	if err != nil {
		log.Printf("tick: failed to pop due actions: %v", err)
	}
	add(popped)

	stale, err := e.Repo.ListStalePendingActionIDs(ctx, now.Add(-staleAfter), BatchLimit)
	if err != nil {
		log.Printf("tick: failed to list stale actions: %v", err)
	}
	if len(stale) > 0 {
		log.Printf("tick: recovering %d overdue action(s) from postgres", len(stale))
	}
	add(stale)

	if len(ids) > 0 {
		log.Printf("tick: resolving %d due action(s)", len(ids))
	}
	return ids
}

// collectIncome pays planet income and regenerates planet shields for this tick. It reports whether this
// engine instance was the one that claimed the tick.
func (e *Engine) collectIncome(ctx context.Context, now time.Time) bool {
	tick := domain.TickNumber(now)
	var gains map[uuid.UUID]int64
	claimed := false

	err := e.Repo.WithTx(ctx, func(tx pgxTx) error {
		ok, err := e.Repo.TxClaimTick(ctx, tx, tick)
		if err != nil || !ok {
			return err
		}
		claimed = true
		gains, err = e.Repo.TxCollectIncome(ctx, tx)
		if err != nil {
			return err
		}
		if err := e.Repo.TxRegenShields(ctx, tx, domain.ShieldRegenPerTick); err != nil {
			return err
		}
		return e.Repo.TxPruneTicks(ctx, tx, tick-keepTicks)
	})
	if err != nil {
		log.Printf("tick: income failed: %v", err)
		return false
	}

	for playerID, credits := range gains {
		e.publish(ctx, events.Event{Type: events.TypeIncome, Players: []string{playerID.String()}, Credits: credits})
	}
	return claimed
}

// outcome is what resolving one action produced.
type outcome struct {
	failed bool
	reason string
	events []events.Event
}

func ok(evs ...events.Event) outcome { return outcome{events: evs} }
func fail(reason string) outcome     { return outcome{failed: true, reason: reason} }

// resolveAction resolves one action inside a single transaction. Events are
// collected during the transaction and published only after it commits, so a
// rolled-back resolution can never leak a ghost event to players.
func (e *Engine) resolveAction(ctx context.Context, actionID uuid.UUID, now time.Time) error {
	var evs []events.Event

	err := e.Repo.WithTx(ctx, func(tx pgxTx) error {
		action, err := e.Repo.TxGetActionByID(ctx, tx, actionID)
		if err != nil {
			return err
		}
		if action == nil || action.Status != "pending" {
			return nil // vanished or already resolved — nothing to do
		}

		var out outcome
		switch action.ActionType {
		case "move":
			out, err = e.resolveMove(ctx, tx, action)
		case "attack":
			out, err = e.resolveAttack(ctx, tx, action, now)
		case "build":
			out, err = e.resolveBuild(ctx, tx, action)
		case "colonize":
			out, err = e.resolveColonize(ctx, tx, action, now)
		case "invade":
			out, err = e.resolveInvade(ctx, tx, action, now)
		case "fortify":
			out, err = e.resolveFortify(ctx, tx, action)
		default:
			log.Printf("tick: unknown action type %q, marking failed", action.ActionType)
			out = fail("unknown action type")
		}
		if err != nil {
			return err
		}

		status := "resolved"
		if out.failed {
			status = "failed"
			out.events = append(out.events, events.Event{
				Type:     events.TypeActionFailed,
				Players:  []string{action.PlayerID.String()},
				ActionID: action.ID.String(),
				Reason:   out.reason,
			})
		}
		if err := e.Repo.TxMarkActionResolved(ctx, tx, action.ID, status); err != nil {
			return err
		}
		evs = out.events
		return nil
	})
	if err != nil {
		return err
	}

	e.publish(ctx, evs...)
	return nil
}

func (e *Engine) publish(ctx context.Context, evs ...events.Event) {
	for _, ev := range evs {
		payload, err := ev.Marshal()
		if err != nil {
			log.Printf("tick: failed to encode %s event: %v", ev.Type, err)
			continue
		}
		if err := e.Queue.Publish(ctx, EventsChannel, string(payload)); err != nil {
			log.Printf("tick: failed to publish %s event: %v", ev.Type, err)
		}
	}
}
