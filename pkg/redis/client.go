package redis

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

type Client struct {
	*redis.Client
}

// NewClient connects to redis on DB 0 (production). Server code uses this.
func NewClient(addr string) (*Client, error) {
	return NewClientDB(addr, 0)
}

// NewClientDB connects to a specific redis DB index. Tests use a non-zero DB
// (see pkg/api TestMain) so they never touch production data on DB 0 (issue 39).
func NewClientDB(addr string, db int) (*Client, error) {
	c := redis.NewClient(&redis.Options{
		Addr: addr,
		DB:   db,
	})

	if err := c.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &Client{c}, nil
}
