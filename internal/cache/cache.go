package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
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
