package queue

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const PendingActionsKey = "actions:pending"

// popDueScript atomically reads all actions due by `now` and removes them
// from the set in one round-trip. Doing this as two separate Redis calls
// (ZRANGEBYSCORE then ZREM) would race if you ever run more than one
// tick-engine instance — both could read the same batch before either
// removes it.
var popDueScript = redis.NewScript(`
local key = KEYS[1]
local now = ARGV[1]
local limit = ARGV[2]
local due = redis.call('ZRANGEBYSCORE', key, '-inf', now, 'LIMIT', 0, limit)
if #due > 0 then
  redis.call('ZREM', key, unpack(due))
end
return due
`)

type Queue struct {
	Client *redis.Client
}

func New(addr string) *Queue {
	return &Queue{
		Client: redis.NewClient(&redis.Options{Addr: addr}),
	}
}

func (q *Queue) Enqueue(ctx context.Context, actionID uuid.UUID, executeAt time.Time) error {
	return q.Client.ZAdd(ctx, PendingActionsKey, redis.Z{
		Score:  float64(executeAt.Unix()),
		Member: actionID.String(),
	}).Err()
}

// PopDueActions returns up to `limit` action IDs whose scheduled time has
// passed, and removes them from the pending set atomically.
func (q *Queue) PopDueActions(ctx context.Context, now time.Time, limit int64) ([]uuid.UUID, error) {
	result, err := popDueScript.Run(ctx, q.Client, []string{PendingActionsKey}, now.Unix(), limit).StringSlice()
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(result))
	for _, s := range result {
		id, err := uuid.Parse(s)
		if err != nil {
			continue // skip malformed entries rather than fail the whole batch
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (q *Queue) Publish(ctx context.Context, channel, payload string) error {
	return q.Client.Publish(ctx, channel, payload).Err()
}

func (q *Queue) Close() error {
	return q.Client.Close()
}
