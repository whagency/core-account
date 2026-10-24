package controllers

import (
	"context"
	"fmt"

	"account-service/internal/domain"
	svcErrors "account-service/internal/domain/errors"

	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
)

type CacheCtrl struct {
	assets       domain.CacheRepository[currency.AssetMap]
	chainConfigs domain.CacheRepository[domain.ChainConfigs]
}

func NewCacheController(
	assets domain.CacheRepository[currency.AssetMap],
	chainConfigs domain.CacheRepository[domain.ChainConfigs],
) *CacheCtrl {
	return &CacheCtrl{
		assets:       assets,
		chainConfigs: chainConfigs,
	}
}

// GetAsset retrieves the asset from the cache and validates the asset code
func (c CacheCtrl) GetAsset(ctx context.Context, assetCode currency.AssetCode) (currency.Asset, error) {
	logger.LogCtx(ctx).Debugf("[CacheCtrl:GetAsset][assetCode: %s]", assetCode)

	asset, errResp := c.getAsset(ctx, assetCode)
	if errResp != nil {
		errResp.SetCause(fmt.Errorf("[CacheCtrl:GetAsset]%w", errResp.Cause))
		return currency.Asset{}, errResp
	}

	return asset, nil
}

// getAsset retrieves the asset from the cache and validates the asset code
func (c CacheCtrl) getAsset(ctx context.Context, assetCode currency.AssetCode) (currency.Asset, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[CacheCtrl:getAsset][assetCode: %s]", assetCode)

	assets, err := c.assets.GetWithRefresh(ctx, "")
	if err != nil {
		return currency.Asset{}, svcErrors.New(
			appErrors.CacheError,
			appErrors.WithCause(fmt.Errorf("[getAsset]%w", err)),
		)
	}

	// Validate and get asset
	asset, ok := assets[assetCode]
	if !ok {
		return currency.Asset{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[getAsset][err: unsupported asset %s]", assetCode)),
		)
	}

	return asset, nil
}

// GetAllAssets retrieves the asset from the cache and validates the asset code
func (c CacheCtrl) GetAllAssets(ctx context.Context) (currency.AssetMap, error) {
	logger.LogCtx(ctx).Debugf("[CacheCtrl:GetAllAssets]")

	assets, err := c.assets.GetWithRefresh(ctx, "")
	if err != nil {
		return nil, svcErrors.New(
			appErrors.CacheError,
			appErrors.WithCause(fmt.Errorf("[CacheCtrl:GetAllAssets]%w", err)),
		)
	}

	return assets, nil
}

// GetChainConfig retrieves the chain configuration for a given asset and network
func (c CacheCtrl) GetChainConfig(ctx context.Context, assetCode currency.AssetCode, network domain.ChainNetwork) (domain.ChainConfig, error) {
	logger.LogCtx(ctx).Debugf("[CacheCtrl:GetChainConfig][assetCode:%s][network:%s]", assetCode, network)

	chainConfig, errResp := c.getChainConfig(ctx, assetCode, network)
	if errResp != nil {
		errResp.SetCause(fmt.Errorf("[CacheCtrl:GetChainConfig]%w", errResp.Cause))
		return domain.ChainConfig{}, errResp
	}

	return chainConfig, nil
}

// getChainConfig retrieves the chain configuration for a given asset and network from cache
func (c CacheCtrl) getChainConfig(ctx context.Context, assetCode currency.AssetCode, network domain.ChainNetwork) (domain.ChainConfig, *appErrors.ErrorResponse) {
	logger.LogCtx(ctx).Debugf("[CacheCtrl:getChainConfig][assetCode:%s][network:%s]", assetCode, network)

	chainConfigs, err := c.chainConfigs.GetWithRefresh(ctx, "")
	if err != nil {
		return domain.ChainConfig{}, svcErrors.New(
			appErrors.CacheError,
			appErrors.WithCause(fmt.Errorf("[getChainConfig]%w", err)),
		)
	}

	// Validate asset and network
	chainConfig, ok := chainConfigs[assetCode][network]
	if !ok {
		return domain.ChainConfig{}, svcErrors.New(
			appErrors.InternalServerError,
			appErrors.WithCause(fmt.Errorf("[getChainConfig][err: unsupported asset %s on network %s]", assetCode, network)),
		)
	}

	return chainConfig, nil
}
