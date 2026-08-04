package queue

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const PendingActionsKey = "actions:pending"

type Queue struct {
	Client *redis.Client
}

func New(addr string) *Queue {
	return &Queue{
		Client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

// Enqueue adds an action ID to the pending sorted set, scored by its
// execution time (unix seconds). The tick engine will pull members whose
// score is <= now(). The action's actual data lives in Postgres (actions
// table) — this set is purely the scheduling index.
func (q *Queue) Enqueue(ctx context.Context, actionID uuid.UUID, executeAt time.Time) error {
	return q.Client.ZAdd(ctx, PendingActionsKey, redis.Z{
		Score:  float64(executeAt.Unix()),
		Member: actionID.String(),
	}).Err()
}

func (q *Queue) Close() error {
	return q.Client.Close()
}
