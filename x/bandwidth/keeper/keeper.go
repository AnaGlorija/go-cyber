package keeper

import (
	"fmt"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/tendermint/tendermint/libs/log"

	"github.com/cybercongress/go-cyber/x/bandwidth/types"
	gtypes "github.com/cybercongress/go-cyber/x/graph/types"
)

type BandwidthMeter struct {
	stakeProvider types.AccountStakeProvider
	cdc           codec.BinaryMarshaler
	storeKey      sdk.StoreKey
	paramSpace    paramstypes.Subspace

	currentBlockSpentBandwidth uint64
	currentCreditPrice         sdk.Dec
	bandwidthSpentByBlock      map[uint64]uint64
	totalSpentForSlidingWindow uint64
}

func NewBandwidthMeter(
	cdc codec.BinaryMarshaler,
	key sdk.StoreKey,
	asp types.AccountStakeProvider,
	paramSpace paramstypes.Subspace,
) *BandwidthMeter {

	if !paramSpace.HasKeyTable() {
		paramSpace = paramSpace.WithKeyTable(types.ParamKeyTable())
	}

	return &BandwidthMeter{
		cdc:                   cdc,
		storeKey:              key,
		stakeProvider:         asp,
		paramSpace:            paramSpace,
		bandwidthSpentByBlock: make(map[uint64]uint64),
	}
}

func (bm BandwidthMeter) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

func (bm BandwidthMeter) GetParams(ctx sdk.Context) (params types.Params) {
	bm.paramSpace.GetParamSet(ctx, &params)
	return params
}

func (bm BandwidthMeter) SetParams(ctx sdk.Context, params types.Params) {
	bm.paramSpace.SetParamSet(ctx, &params)
}

func (bm *BandwidthMeter) LoadState(ctx sdk.Context) {
	params := bm.GetParams(ctx)
	bm.totalSpentForSlidingWindow = 0
	// TODO test case when period parameter is increased
	bm.bandwidthSpentByBlock = bm.GetValuesForPeriod(ctx, params.RecoveryPeriod)
	for _, spentBandwidth := range bm.bandwidthSpentByBlock {
		bm.totalSpentForSlidingWindow += spentBandwidth
	}
	bm.currentCreditPrice = bm.GetBandwidthPrice(ctx, params.BasePrice)
	bm.currentBlockSpentBandwidth = 0
}

func (bm BandwidthMeter) GetBandwidthPrice(ctx sdk.Context, basePrice sdk.Dec) sdk.Dec {
	store := ctx.KVStore(bm.storeKey)
	priceAsBytes := store.Get(types.LastBandwidthPrice)
	if priceAsBytes == nil {
		return basePrice
	}
	var price types.Price
	bm.cdc.MustUnmarshalBinaryBare(priceAsBytes, &price)
	return price.Price
}

func (bm BandwidthMeter) StoreBandwidthPrice(ctx sdk.Context, price sdk.Dec) {
	store := ctx.KVStore(bm.storeKey)
	store.Set(types.LastBandwidthPrice, bm.cdc.MustMarshalBinaryBare(&types.Price{Price: price}))
}

func (bm BandwidthMeter) GetDesirableBandwidth(ctx sdk.Context) uint64 {
	store := ctx.KVStore(bm.storeKey)
	bandwidthAsBytes := store.Get(types.DesirableBandwidth)
	if bandwidthAsBytes == nil {
		return 0
	}
	return sdk.BigEndianToUint64(bandwidthAsBytes)
}

func (bm BandwidthMeter) AddToDesirableBandwidth(ctx sdk.Context, toAdd uint64) {
	current := bm.GetDesirableBandwidth(ctx)
	store := ctx.KVStore(bm.storeKey)
	store.Set(types.DesirableBandwidth, sdk.Uint64ToBigEndian(current+toAdd))
}

func (bm *BandwidthMeter) AddToBlockBandwidth(value uint64) {
	bm.currentBlockSpentBandwidth += value
}

// Here we move bandwidth window:
// Remove first block of window and add new block to window end
func (bm *BandwidthMeter) CommitBlockBandwidth(ctx sdk.Context) {
	params := bm.GetParams(ctx)
	defer func() {
		if bm.currentBlockSpentBandwidth > 0 {
			bm.Logger(ctx).Info("Block", "bandwidth", bm.currentBlockSpentBandwidth)
			bm.Logger(ctx).Info("Window", "bandwidth", bm.totalSpentForSlidingWindow)
		}

		telemetry.SetGauge(float32(bm.currentBlockSpentBandwidth), types.ModuleName, "block_bandwidth")
		telemetry.SetGauge(float32(bm.totalSpentForSlidingWindow), types.ModuleName, "window_bandwidth")
		bm.currentBlockSpentBandwidth = 0
	}()

	bm.totalSpentForSlidingWindow += bm.currentBlockSpentBandwidth

	newWindowEnd := ctx.BlockHeight()
	windowStart := newWindowEnd - int64(params.RecoveryPeriod)
	if windowStart < 0 {
		windowStart = 0
	}

	// clean window slot in in-memory
	windowStartValue, exists := bm.bandwidthSpentByBlock[uint64(windowStart)]
	if exists {
		bm.totalSpentForSlidingWindow -= windowStartValue
		delete(bm.bandwidthSpentByBlock, uint64(windowStart))
	}

	// clean window slot in storage
	store := ctx.KVStore(bm.storeKey)
	if store.Has(types.BlockStoreKey(uint64(windowStart))) {
		store.Delete(types.BlockStoreKey(uint64(windowStart)))
	}

	bm.SetBlockBandwidth(ctx, uint64(ctx.BlockHeight()), bm.currentBlockSpentBandwidth)
	bm.bandwidthSpentByBlock[uint64(newWindowEnd)] = bm.currentBlockSpentBandwidth
}

func (bm *BandwidthMeter) GetCurrentBlockSpentBandwidth(ctx sdk.Context) uint64 {
	return bm.currentBlockSpentBandwidth
}

func (bm *BandwidthMeter) GetCurrentNetworkLoad(ctx sdk.Context) sdk.Dec {
	return sdk.NewDec(int64(bm.totalSpentForSlidingWindow)).QuoInt64(int64(bm.GetDesirableBandwidth(ctx)))
}

func (bm *BandwidthMeter) GetMaxBlockBandwidth(ctx sdk.Context) uint64 {
	params := bm.GetParams(ctx)
	maxBlockBandwidth := params.MaxBlockBandwidth
	return maxBlockBandwidth
}

func (bm *BandwidthMeter) GetCurrentCreditPrice() sdk.Dec {
	return bm.currentCreditPrice
}

func (bm *BandwidthMeter) AdjustPrice(ctx sdk.Context) {
	params := bm.GetParams(ctx)

	desirableBandwidth := bm.GetDesirableBandwidth(ctx)
	if desirableBandwidth != 0 {
		telemetry.SetGauge(float32(bm.totalSpentForSlidingWindow)/float32(desirableBandwidth), types.ModuleName, "load")

		newPrice := sdk.NewDec(int64(bm.totalSpentForSlidingWindow)).QuoInt64(int64(desirableBandwidth))
		bm.Logger(ctx).Info("Load", "value", newPrice.String())
		if newPrice.LT(params.BasePrice) {
			newPrice = params.BasePrice
		}
		bm.Logger(ctx).Info("Price", "value", newPrice.String())

		bm.currentCreditPrice = newPrice
		bm.StoreBandwidthPrice(ctx, newPrice)
	}
}

func (bm *BandwidthMeter) GetTotalCyberlinksCost(ctx sdk.Context, tx sdk.Tx) (uint64) {
	bandwidthForTx := uint64(0)
	for _, msg := range tx.GetMsgs() {
		linkMsg := msg.(*gtypes.MsgCyberlink)
		bandwidthForTx = bandwidthForTx + uint64(len(linkMsg.Links)) * 1000
	}
	return bandwidthForTx
}

func (bm *BandwidthMeter) GetPricedTotalCyberlinksCost(ctx sdk.Context, tx sdk.Tx) uint64 {
	return uint64(bm.currentCreditPrice.Mul(sdk.NewDec(int64(bm.GetTotalCyberlinksCost(ctx, tx)))).RoundInt64())
}

func (bm *BandwidthMeter) ConsumeAccountBandwidth(ctx sdk.Context, bw types.AccountBandwidth, amt uint64) error {
	err := bw.Consume(amt); if err != nil {
		return err
	}
	bm.SetAccountBandwidth(ctx, bw)
	return nil
}

func (bm *BandwidthMeter) GetCurrentAccountBandwidth(ctx sdk.Context, address sdk.AccAddress) types.AccountBandwidth {
	accBw := bm.GetAccountBandwidth(ctx, address)
	accMaxBw := bm.GetAccountMaxBandwidth(ctx, address)
	params := bm.GetParams(ctx)
	accBw.UpdateMax(accMaxBw, uint64(ctx.BlockHeight()), params.RecoveryPeriod)
	return accBw
}

func (bm *BandwidthMeter) GetAccountMaxBandwidth(ctx sdk.Context, addr sdk.AccAddress) uint64 {
	accStakePercentage := bm.stakeProvider.GetAccountStakePercentageVolt(ctx, addr)
	return uint64(accStakePercentage * float64(bm.GetDesirableBandwidth(ctx)))
}

func (bm *BandwidthMeter) UpdateAccountMaxBandwidth(ctx sdk.Context, address sdk.AccAddress) {
	bw := bm.GetCurrentAccountBandwidth(ctx, address)
	bm.SetAccountBandwidth(ctx, bw)
}
