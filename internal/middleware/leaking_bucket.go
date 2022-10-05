package middleware

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"net/http"
	"strconv"
	"time"
)

var (
	leakingBucketCapacity         = 10
	pauseBetweenRequestProcessing = time.Second
)

type LeakingBucketLimiter struct {
	activeRequestQueue chan struct{}
	ticker             *time.Ticker
}

func NewLeakingBucketLimiter() *LeakingBucketLimiter {
	return &LeakingBucketLimiter{
		ticker:             time.NewTicker(pauseBetweenRequestProcessing),
		activeRequestQueue: make(chan struct{}, leakingBucketCapacity),
	}
}

func (l *LeakingBucketLimiter) LimitTraffic(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		select {
		case l.activeRequestQueue <- struct{}{}:
			c.Response().Header().Set("X-Ratelimit-Limit", strconv.Itoa(leakingBucketCapacity))
			c.Response().Header().Set("X-Ratelimit-Remaining", strconv.Itoa(leakingBucketCapacity-len(l.activeRequestQueue)))
		default:
			c.Response().Header().Set("X-Ratelimit-Limit", strconv.Itoa(leakingBucketCapacity))
			c.Response().Header().Set("X-Ratelimit-Remaining", "0")
			c.Response().Header().Set("X-Ratelimit-Retry-After", fmt.Sprintf("%f", pauseBetweenRequestProcessing.Seconds()))
			return echo.NewHTTPError(http.StatusTooManyRequests)
		}
		requestsInQueueWithoutCurrent := len(l.activeRequestQueue) - 1
		timeToSleep := time.Duration(requestsInQueueWithoutCurrent) * pauseBetweenRequestProcessing
		time.Sleep(timeToSleep)
		err := next(c)
		<-l.activeRequestQueue
		return err
	}
}
