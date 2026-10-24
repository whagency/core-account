package app

import (
	"context"
	"fmt"

	"account-service/config"
	"account-service/internal/domain"

	"github.com/dgraph-io/ristretto"
	"github.com/hazelcast/hazelcast-go-client"
	"github.com/hazelcast/hazelcast-go-client/cluster"
	hzLogger "github.com/hazelcast/hazelcast-go-client/logger"
	"github.com/hazelcast/hazelcast-go-client/types"
	"github.com/whagency/core-common/cache"
	"github.com/whagency/core-common/cache/hzcast"
	"github.com/whagency/core-common/logger"
)

// setupRemoteCacheManager sets the cron jobs for the application and runs them
func (app *Application) setupRemoteCacheManager() error {
	app.remoteCacheManager = hzcast.NewRemoteCacheManager(config.Commons().AppName, app.remoteCache, cache.Config().TTL)
	if err := cache.LoadCacheOptions(domain.CacheKeys, domain.CacheConfigKeyIDs); err != nil {
		return fmt.Errorf("[setupRemoteCacheManager]%w", err)
	}
	return nil
}

// setupRemoteCache sets up the cache connection.
func (app *Application) setupRemoteCache() error {
	logger.Log().Infof("[Application:setupRemoteCache][host:%s][cluster:%s]", cache.Config().Host, cache.Config().ClusterName)

	cacheConfig := hazelcast.Config{
		Logger: hzLogger.Config{
			Level: hzLogger.InfoLevel,
		},
	}

	if cache.Config().ClusterName != "" {
		cacheConfig.Cluster = cluster.Config{
			Name:              cache.Config().ClusterName,
			InvocationTimeout: types.Duration(cache.Config().InvocationTimeout),
		}
	}

	cacheConfig.Cluster.Network.SetAddresses(cache.Config().Host)

	cacheClient, err := hazelcast.StartNewClientWithConfig(context.Background(), cacheConfig)
	if err != nil {
		return fmt.Errorf("[setupRemoteCache][err: %w]", err)
	}

	app.remoteCache = cacheClient

	logger.Log().Info("[Application:setupRemoteCache][msg: completed]")

	return nil
}

// setupGlobalLocalCache sets up the global local cache.
func (app *Application) setupGlobalLocalCache() error {
	logger.Log().Infof("[Application:setupGlobalLocalCache][size:%d][max_keys:%d]", cache.Config().CacheManager.GlobalLocalCacheConfig.Size, cache.Config().CacheManager.GlobalLocalCacheConfig.MaxKeys)

	localCache, err := ristretto.NewCache(&ristretto.Config{
		NumCounters: cache.Config().CacheManager.GlobalLocalCacheConfig.MaxKeys,
		MaxCost:     cache.Config().CacheManager.GlobalLocalCacheConfig.Size,
		BufferItems: cache.Config().DefaultBufferSizeLocalCache,
	})
	if err != nil {
		return fmt.Errorf("[setupGlobalLocalCache][err: %w]", err)
	}

	app.globalLocalCache = localCache

	logger.Log().Info("[Application:setupGlobalLocalCache][msg: completed]")

	return nil
}
