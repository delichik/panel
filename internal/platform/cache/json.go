package cache

import (
	"context"
	"encoding/json"
	"time"
)

// GetJSON decodes a cached JSON value. A missing key or an invalid payload
// returns the zero value and false.
func GetJSON[T any](c *Cache, key string) (T, bool) {
	var zero T
	raw, ok := c.Get(key)
	if !ok {
		return zero, false
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, false
	}
	return value, true
}

// SetJSON encodes value as JSON and stores it.
func SetJSON[T any](c *Cache, key string, value T, ttl time.Duration) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return c.Set(key, raw, ttl)
}

// GetOrLoadJSON is GetOrLoad with JSON encoding of the loaded value.
func GetOrLoadJSON[T any](ctx context.Context, c *Cache, key string, ttl time.Duration, load func(context.Context) (T, error)) (T, error) {
	var zero T
	raw, err := c.GetOrLoad(ctx, key, ttl, func(ctx context.Context) ([]byte, error) {
		value, err := load(ctx)
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	})
	if err != nil {
		return zero, err
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, err
	}
	return value, nil
}
