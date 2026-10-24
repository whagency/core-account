package domain

import (
	"context"
	"time"

	"github.com/whagency/core-common/cache"
	"github.com/whagency/core-common/services/currency"
)

const (
	DepositLockerConfigKey              = "deposit_locker"
	DepositProcessingLockerConfigKey    = "deposit_processing_locker"
	WithdrawalLockerConfigKey           = "withdrawal_locker"
	WithdrawalProcessingLockerConfigKey = "withdrawal_processing_locker"
	TransferLockerConfigKey             = "transfer_locker"
	BlockchainLockerConfigKey           = "blockchain_locker"
)

const (
	AssetsCacheKeyID cache.KeyID = iota + 1
	ChainConfigsCacheKeyID
)

const (
	AssetsConfigCacheKey = "assets"
	ChainConfigsCacheKey = "chain_configs"
)

var CacheConfigKeyIDs = map[string]cache.KeyID{
	AssetsConfigCacheKey: AssetsCacheKeyID,
	ChainConfigsCacheKey: ChainConfigsCacheKeyID,
}

var CacheKeys = map[cache.KeyID]*cache.Key{
	AssetsCacheKeyID: {
		ID:   AssetsCacheKeyID,
		Name: "assets",
	},
	ChainConfigsCacheKeyID: {
		ID:   ChainConfigsCacheKeyID,
		Name: "chain_configs",
	},
}

// CacheRepository defines the operations for a cacheable repository
type CacheRepository[T cache.Cacheable] interface {
	Get(ctx context.Context, key string) (T, error)
	Exists(ctx context.Context, key string) (bool, error)
	Update(ctx context.Context, key string, data T) error
	UpdateWithTTL(ctx context.Context, key string, data T, ttl time.Duration) error
	GetWithRefresh(ctx context.Context, key string) (T, error)
	RefreshAndGet(ctx context.Context, key string) (T, error)
	DeleteKey(ctx context.Context, key string) error
}

// Locker defines the operations for a distributed lock manager
type Locker interface {
	TryLock(context.Context, string) error
	Unlock(context.Context, string) error
}

// CacheController defines the interface for cache-related operations.
type CacheController interface {
	GetAsset(context.Context, currency.AssetCode) (currency.Asset, error)
	GetAllAssets(ctx context.Context) (currency.AssetMap, error)
	GetChainConfig(context.Context, currency.AssetCode, ChainNetwork) (ChainConfig, error)
}
