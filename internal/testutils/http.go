package testutils

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net"
	"os"
	"runtime"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/http"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type TestDB struct {
	client         *redis.Client
	database       *gorm.DB
	redisContainer *dockertest.Resource
}

func (t *TestDB) createRedis() *redis.Client {
	if t.client != nil {
		return t.client
	}
	pool, err := dockertest.NewPool("")
	if err != nil {
		klog.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		klog.Fatalf("Could not connect to Docker: %s", err)
	}

	// Start ports at a random number above 10000
	// Check if that port is in use, and if so, increment it.
	// This is to avoid conflicts with other running tests
	bigPort, err := rand.Int(rand.Reader, big.NewInt(55534))
	port := uint16(bigPort.Uint64())
	if err != nil {
		klog.Fatalf("Could not generate random port: %s", err)
	}

	for {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			port++
			continue
		} else {
			listener.Close()
			break
		}
	}

	// pulls an image, creates a container based on it and runs it
	t.redisContainer, err = pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "redis",
		Tag:        "7-alpine",
		Cmd:        []string{"--requirepass", "password"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"6379/tcp": {
				{
					HostIP:   "127.0.0.1",
					HostPort: fmt.Sprintf("%d", port),
				},
			},
		},
	})
	if err != nil {
		klog.Fatalf("Could not start resource: %s", err)
	}

	t.client = redis.NewClient(&redis.Options{
		Addr:            t.redisContainer.GetHostPort("6379/tcp"),
		Password:        "password",
		PoolFIFO:        true,
		PoolSize:        runtime.GOMAXPROCS(0) * 10,
		MinIdleConns:    runtime.GOMAXPROCS(0),
		ConnMaxIdleTime: 10 * time.Minute,
	})
	_, err = t.client.Ping(context.Background()).Result()
	if err != nil {
		_ = t.redisContainer.Close()
		klog.Fatalf("Failed to connect to redis: %s", err)
	}
	return t.client
}

func (t *TestDB) CloseRedis() {
	if t.client != nil {
		_ = t.client.Close()
	}
	if t.redisContainer != nil {
		_ = t.redisContainer.Close()
	}
	t.redisContainer = nil
	t.client = nil
}

func (t *TestDB) CloseDB() {
	if t.database != nil {
		sqlDB, _ := t.database.DB()
		_ = sqlDB.Close()
	}
	t.database = nil
}

func CreateTestDBRouter() (*gin.Engine, *TestDB) {
	os.Setenv("TEST", "test")
	var t TestDB
	t.database = db.MakeDB()
	return http.CreateRouter(db.MakeDB(), t.createRedis()), &t
}
