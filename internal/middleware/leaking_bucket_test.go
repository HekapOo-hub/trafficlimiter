package middleware

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"net/http"
	"sync"
	"testing"
	"time"
)

var (
	requestPath1       = "/1"
	leakingHostAndPort = "http://127.0.0.1:1124"
	perRequest         = time.Millisecond * 200
)

func TestLeakingBucketLimiter_LimitTraffic(t *testing.T) {
	e := setEchoServerWithLeakingBucketLimiter(NewLeakingBucketLimiter())
	req1, err := http.NewRequest(http.MethodGet, leakingHostAndPort+requestPath1, nil)
	client := http.Client{}
	require.NoError(t, err)
	wg := sync.WaitGroup{}
	for i := 0; i < leakingBucketCapacity; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Do(req1)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		}()
	}
	time.Sleep(perRequest / 2)
	resp, err := client.Do(req1)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)

	wg.Wait()
	require.NoError(t, e.Shutdown(context.Background()))
}

func TestLimitTraffic(t *testing.T) {
	e := setEchoServerWithLeakingBucketLimiter(NewLeakingBucketLimiter())
	req1, err := http.NewRequest(http.MethodGet, leakingHostAndPort+requestPath1, nil)
	client := http.Client{}
	require.NoError(t, err)
	wg := sync.WaitGroup{}
	for i := 0; i < leakingBucketCapacity; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Do(req1)
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
		}()
	}
	delay := perRequest / 2
	time.Sleep(perRequest + delay)
	resp, err := client.Do(req1)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	wg.Wait()
	require.NoError(t, e.Shutdown(context.Background()))
}

func setEchoServerWithLeakingBucketLimiter(limiter *LeakingBucketLimiter) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	e.GET(requestPath1, func(c echo.Context) error {
		time.Sleep(perRequest)
		return c.String(http.StatusOK, "1")
	}, limiter.LimitTraffic)
	go func() {
		e.Logger.Error(e.Start(":1124"))
	}()
	time.Sleep(time.Second)
	return e
}
