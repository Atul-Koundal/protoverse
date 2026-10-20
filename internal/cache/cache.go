package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// Cache keys shared between the API server (which fills them) and the tick
// engine (which invalidates them whenever the world changes).
const (
	GalaxyStateKey = "cache:galaxy_state"
	LeaderboardKey = "cache:leaderboard"
)

type Cache struct {
	Client *redis.Client
}

func New(client *redis.Client) *Cache {
	return &Cache{Client: client}
}

// GetJSON returns (found, err). found=false, err=nil means a clean cache miss.
func (c *Cache) GetJSON(ctx context.Context, key string, dest interface{}) (bool, error) {
	val, err := c.Client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal([]byte(val), dest); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cache) SetJSON(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Client.Set(ctx, key, data, ttl).Err()
}

// GetProto is GetJSON for protobuf messages, using protojson so well-known
// types such as timestamps survive the round trip.
func (c *Cache) GetProto(ctx context.Context, key string, dest proto.Message) (bool, error) {
	val, err := c.Client.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := protojson.Unmarshal([]byte(val), dest); err != nil {
		return false, err
	}
	return true, nil
}

func (c *Cache) SetProto(ctx context.Context, key string, value proto.Message, ttl time.Duration) error {
	data, err := protojson.Marshal(value)
	if err != nil {
		return err
	}
	return c.Client.Set(ctx, key, data, ttl).Err()
}

// Invalidate removes cached keys.
func (c *Cache) Invalidate(ctx context.Context, keys ...string) error {
	return c.Client.Del(ctx, keys...).Err()
}
