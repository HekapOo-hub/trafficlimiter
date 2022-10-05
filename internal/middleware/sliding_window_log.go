package middleware

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"net/http"
	"strconv"
	"time"
)

var (
	ErrRequestBlocked = fmt.Errorf("request is blocked")
	capacityOfBucket  = 10
	refillFrequency   = time.Second
)

type TrafficLimiter interface {
	LimitTraffic(next echo.HandlerFunc) echo.HandlerFunc
}

type SlidingWindowLogLimiter struct {
	cli             redis.UniversalClient
	capacity        int
	refillFrequency time.Duration
}

func NewTokenBucket(cli redis.UniversalClient) *SlidingWindowLogLimiter {
	// should probably read some file on disk to configure capacity of bucket and refill frequency

	return &SlidingWindowLogLimiter{cli: cli,
		capacity:        capacityOfBucket,
		refillFrequency: refillFrequency,
	}
}

func (tb *SlidingWindowLogLimiter) LimitTraffic(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		ip := c.RealIP()
		requestURL := c.Request().URL
		setName := fmt.Sprintf("%s:%s", ip, requestURL)
		ctx := context.Background()
		var retryAfter float64
		var count int
		err := tb.cli.Watch(ctx, func(tx *redis.Tx) error {
			err := tb.removeOldTimestamps(ctx, setName, tx)
			if err != nil {
				return fmt.Errorf("token bucket limiter: limit traffic: %w", err)
			}
			count, err = tb.countAllByKey(ctx, setName, tx)
			if err != nil {
				return fmt.Errorf("token bucket limiter: limit traffic: %w", err)
			}
			err = tb.resetExpirationOnSet(ctx, setName, tx)
			if err != nil {
				return fmt.Errorf("token bucket limiter: limit traffic: %w", err)
			}
			if count == tb.capacity {
				firstTimestampStr, err := tb.getFirstByKey(ctx, setName, tx)
				if err != nil {
					return fmt.Errorf("token bucket limiter: limit traffic: %w", err)
				}
				firstTimestamp, err := strconv.Atoi(firstTimestampStr)
				if err != nil {
					return fmt.Errorf("token bucket limiter: limit traffic: %w", err)
				}
				retryAfterInMicroseconds := int64(firstTimestamp) + tb.refillFrequency.Microseconds() - time.Now().UTC().UnixMicro()
				retryAfter = float64(retryAfterInMicroseconds) / 1000000
				return fmt.Errorf("token: bucket limiter: limit traffic: %w", ErrRequestBlocked)
			}
			err = tb.cli.ZAdd(ctx, setName, &redis.Z{Score: float64(time.Now().UTC().UnixMicro()),
				Member: time.Now().UTC().UnixMicro()}).Err()
			count++
			if err != nil {
				return fmt.Errorf("token bucket limiter: limit traffic: %w", err)
			}
			return nil
		}, setName)
		c.Response().Header().Set("X-Ratelimit-Limit", strconv.Itoa(tb.capacity))
		c.Response().Header().Set("X-Ratelimit-Remaining", strconv.Itoa(tb.capacity-count))
		if err != nil {
			logrus.Error(err)
			if errors.Is(err, ErrRequestBlocked) {
				c.Response().Header().Set("X-Ratelimit-Retry-After", fmt.Sprintf("%f", retryAfter))
				return echo.NewHTTPError(http.StatusTooManyRequests, err)
			} else {
				return echo.NewHTTPError(http.StatusInternalServerError, err)
			}
		}
		return next(c)
	}
}

func (tb *SlidingWindowLogLimiter) removeOldTimestamps(ctx context.Context, key string, tx *redis.Tx) error {
	min := "-inf"
	max := time.Now().UTC().Add(-1 * tb.refillFrequency).UnixMicro()
	maxStr := strconv.Itoa(int(max))
	err := tx.ZRemRangeByScore(ctx, key, min, maxStr).Err()
	if err != nil {
		return fmt.Errorf("remove old timestamps: %w", err)
	}
	return nil
}

func (tb *SlidingWindowLogLimiter) countAllByKey(ctx context.Context, key string, tx *redis.Tx) (int, error) {
	timestamps, err := tx.ZRange(ctx, key, 0, -1).Result()
	if err != nil {
		return -1, fmt.Errorf("count all by key: %w", err)
	}
	return len(timestamps), nil
}

func (tb *SlidingWindowLogLimiter) resetExpirationOnSet(ctx context.Context, key string, tx *redis.Tx) error {
	err := tx.Expire(ctx, key, tb.refillFrequency).Err()
	if err != nil {
		return fmt.Errorf("reset expiration on set: %w", err)
	}
	return nil
}

func (tb *SlidingWindowLogLimiter) getFirstByKey(ctx context.Context, key string, tx *redis.Tx) (string, error) {
	timestamp, err := tx.ZRange(ctx, key, 0, 0).Result()
	if err != nil {
		return "", fmt.Errorf("get first by key: %w", err)
	}
	return timestamp[0], nil
}
