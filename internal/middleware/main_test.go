package middleware

import (
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/sirupsen/logrus"
	"os"
	"testing"
)

var (
	redisClient redis.UniversalClient
	tbLimiter   *SlidingWindowLogLimiter
)

func TestMain(m *testing.M) {
	dockerPool, err := dockertest.NewPool("")
	if err != nil {
		logrus.Fatalf("Could not connect to docker: %s", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	redisResource := initializeRedis(ctx, dockerPool, newRedisConfig())
	tbLimiter = NewTokenBucket(redisClient)
	code := m.Run()

	purgeResources(dockerPool, redisResource)

	os.Exit(code)
}

func initializeRedis(ctx context.Context, dockerPool *dockertest.Pool, rConfig *redisConfig) *dockertest.Resource {
	redisResource, err := dockerPool.RunWithOptions(&dockertest.RunOptions{
		Repository: rConfig.Repository,
		Tag:        rConfig.Version,
	}, func(cfg *docker.HostConfig) {
		cfg.AutoRemove = rConfig.AutoRemove
		cfg.RestartPolicy = docker.RestartPolicy{
			Name: rConfig.RestartPolicy,
		}
	})
	if err != nil {
		logrus.Fatal(err)
	}

	if err = dockerPool.Retry(func() error {
		cs := rConfig.getConnectionString(redisResource.GetHostPort(rConfig.PortID))

		opts, err := redis.ParseURL(cs)
		if err != nil {
			return err
		}
		redisClient = redis.NewClient(opts)

		_, err = redisClient.Ping(ctx).Result()
		if err != nil {
			return fmt.Errorf("can't connect to redis: %v", err)
		}
		return nil
	}); err != nil {
		logrus.Fatal(err)
	}
	return redisResource
}

func purgeResources(dockerPool *dockertest.Pool, resources ...*dockertest.Resource) {
	for i := range resources {
		if err := dockerPool.Purge(resources[i]); err != nil {
			logrus.Fatalf("Could not purge resource: %s", err)
		}

		err := resources[i].Expire(1)
		if err != nil {
			logrus.Fatal(err)
		}
	}

}

type redisConfig struct {
	Repository    string
	DB            int
	Version       string
	PortID        string
	RestartPolicy string
	AutoRemove    bool
}

func newRedisConfig() *redisConfig {
	return &redisConfig{
		Repository:    "redis",
		DB:            0,
		Version:       "7-alpine",
		PortID:        "6379/tcp",
		RestartPolicy: "no",
		AutoRemove:    true,
	}
}

func (r *redisConfig) getConnectionString(dbHostAndPort string) string {
	return fmt.Sprintf("%s://%s/%d", r.Repository,
		dbHostAndPort,
		r.DB,
	)
}
