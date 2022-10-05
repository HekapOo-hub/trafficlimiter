package middleware

import (
	"context"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/require"
	"net/http"
	"strconv"
	"testing"
	"time"
)

var (
	limitHostAndPort = "http://127.0.0.1:1123"
	limitAddr        = ":1123"
	limitReqPath     = "/"
)

func TestSlidingWindowLogLimiter_LimitTraffic(t *testing.T) {
	e := setEchoServerWithTokenBucketLimiter()
	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, limitHostAndPort+limitReqPath, nil)
	require.NoError(t, err)

	for i := 0; i < capacityOfBucket; i++ {
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		expectedRemaining := capacityOfBucket - i - 1
		actualRemaining := resp.Header.Get("X-Ratelimit-Remaining")
		require.Equal(t, strconv.Itoa(expectedRemaining), actualRemaining)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "bucket has no capacity for another request")
	require.NoError(t, e.Shutdown(context.Background()))
}

func TestSlidingWindowLogLimiter_LimitTrafficRefill(t *testing.T) {
	e := setEchoServerWithTokenBucketLimiter()
	client := http.Client{}
	req, err := http.NewRequest(http.MethodGet, limitHostAndPort+limitReqPath, nil)
	require.NoError(t, err)

	for i := 0; i < capacityOfBucket; i++ {
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		expectedRemaining := capacityOfBucket - i - 1
		actualRemaining := resp.Header.Get("X-Ratelimit-Remaining")
		require.Equal(t, strconv.Itoa(expectedRemaining), actualRemaining)
		time.Sleep(refillFrequency / time.Duration(capacityOfBucket))
	}

	resp, err := client.Do(req)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "there should be space in bucket because first request timestamp is no longer in refillFrequency window")
	expectedRemaining := 0
	actualRemaining := resp.Header.Get("X-Ratelimit-Remaining")
	require.Equal(t, strconv.Itoa(expectedRemaining), actualRemaining)
	require.NoError(t, e.Shutdown(context.Background()))
}

func setEchoServerWithTokenBucketLimiter() *echo.Echo {
	e := echo.New()
	e.IPExtractor = echo.ExtractIPDirect()
	e.HideBanner = true

	e.GET(limitReqPath, func(c echo.Context) error {
		return c.String(http.StatusOK, "")
	}, tbLimiter.LimitTraffic)
	go func() {
		e.Logger.Error(e.Start(limitAddr))
	}()
	time.Sleep(time.Second)
	return e
}
