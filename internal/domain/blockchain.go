package domain

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/tools"
)

// FeeTier represents fee priority levels
type FeeTier uint8

const (
	FeeTierSlow FeeTier = iota + 1
	FeeTierStandard
	FeeTierFast
)

var feeTierToString = map[FeeTier]string{
	FeeTierSlow:     "slow",
	FeeTierStandard: "standard",
	FeeTierFast:     "fast",
}

var _feeTierByString = map[string]FeeTier{
	"slow":     FeeTierSlow,
	"standard": FeeTierStandard,
	"fast":     FeeTierFast,
}

// String returns the string representation of the FeeTier
func (ft FeeTier) String() string {
	if str, ok := feeTierToString[ft]; ok {
		return str
	}
	return "unknown"
}

// NewFeeTierFromString converts a transport string to FeeTier.
func NewFeeTierFromString(value string) (FeeTier, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	feeTier, ok := _feeTierByString[normalized]
	if ok {
		return feeTier, nil
	}

	return 0, fmt.Errorf("[domain][NewFeeTierFromString][err: unsupported fee tier %q]", value)
}

// IsSupportedFeeTier reports whether the fee tier is supported.
func IsSupportedFeeTier(feeTier FeeTier) bool {
	_, ok := feeTierToString[feeTier]
	return ok
}

// SupportedFeeTierStrings returns transport strings for supported fee tiers.
func SupportedFeeTierStrings() []string {
	return slices.Sorted(maps.Keys(_feeTierByString))
}

// NewFeeTier creates a FeeTier from a uint8 value
func NewFeeTier(value uint8) FeeTier {
	return FeeTier(value)
}

// Uint8 converts FeeTier to uint8
func (ft FeeTier) Uint8() uint8 {
	return uint8(ft)
}

type FeeType uint8

const (
	FeeTypePercent FeeType = iota + 1
	FeeTypeFixAmount
)

var feeTypeToString = map[FeeType]string{
	FeeTypePercent:   "percent",
	FeeTypeFixAmount: "fix_amount",
}

// NewFeeType creates a new FeeType from a uint8 value.
func NewFeeType(s uint8) FeeType {
	return FeeType(s)
}

// String returns the string representation of the FeeType.
func (f FeeType) String() string {
	if str, ok := feeTypeToString[f]; ok {
		return str
	}
	return "unknown"
}

// Uint8 converts FeeType to uint8.
func (f FeeType) Uint8() uint8 {
	return uint8(f)
}

// AddressStatus represents the lifecycle status of a deposit address
type AddressStatus uint8

const (
	AddressStatusAllocated AddressStatus = iota + 1
	AddressStatusInUse
	AddressStatusRetired
	AddressStatusBlocked
)

// NewAddressStatus creates a new AddressStatus from a uint8 value.
func NewAddressStatus(s uint8) AddressStatus {
	return AddressStatus(s)
}

// Uint8 converts AddressStatus to uint8.
func (a AddressStatus) Uint8() uint8 {
	return uint8(a)
}

var _addressStatusToString = map[AddressStatus]string{
	AddressStatusAllocated: "allocated",
	AddressStatusInUse:     "in_use",
	AddressStatusRetired:   "retired",
	AddressStatusBlocked:   "blocked",
}

// String returns the string representation of the AddressStatus.
func (a AddressStatus) String() string {
	if str, ok := _addressStatusToString[a]; ok {
		return str
	}
	return "unknown"
}

// ChainNetwork represents supported blockchain networks
type ChainNetwork uint8

const ChainNetworkTRX ChainNetwork = iota + 1

// TRXAddressRegex matches a standard TRON base58 address.
const TRXAddressRegex = `^T[1-9A-HJ-NP-Za-km-z]{33}$`

// Uint8 converts ChainNetwork to uint8.
func (c ChainNetwork) Uint8() uint8 {
	return uint8(c)
}

// NewChainNetwork creates a new ChainNetwork from a uint8 value.
func NewChainNetwork(n uint8) ChainNetwork {
	return ChainNetwork(n)
}

var _chainNetworkToString = map[ChainNetwork]string{
	ChainNetworkTRX: "trx",
}

var _chainNetworkByString = map[string]ChainNetwork{
	"trx": ChainNetworkTRX,
}

// String returns the string representation of the ChainNetwork.
func (c ChainNetwork) String() string {
	if str, ok := _chainNetworkToString[c]; ok {
		return str
	}
	return "unknown"
}

// NewChainNetworkFromString converts a transport string to ChainNetwork.
func NewChainNetworkFromString(value string) (ChainNetwork, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	network, ok := _chainNetworkByString[normalized]
	if ok {
		return network, nil
	}

	return 0, fmt.Errorf("[domain][NewChainNetworkFromString][err: unsupported chain network %q]", value)
}

// IsSupportedChainNetwork reports whether the chain network is supported.
func IsSupportedChainNetwork(network ChainNetwork) bool {
	_, ok := _chainNetworkToString[network]
	return ok
}

// SupportedChainNetworkStrings returns transport strings for supported chain networks.
func SupportedChainNetworkStrings() []string {
	return slices.Sorted(maps.Keys(_chainNetworkByString))
}

// ChainConfigStatus represents the status of a chain config
type ChainConfigStatus uint8

const (
	ChainConfigStatusAll ChainConfigStatus = iota + 1
	ChainConfigStatusDeposit
	ChainConfigStatusWithdrawal
	ChainConfigStatusDisable
)

var chainConfigStatusToString = map[ChainConfigStatus]string{
	ChainConfigStatusAll:        "all",
	ChainConfigStatusDeposit:    "deposit",
	ChainConfigStatusWithdrawal: "withdrawal",
	ChainConfigStatusDisable:    "disable",
}

// String returns the string representation of the ChainConfigStatus
func (cs ChainConfigStatus) String() string {
	if str, ok := chainConfigStatusToString[cs]; ok {
		return str
	}
	return "unknown"
}

// NewChainConfigStatus creates a ChainConfigStatus from a ChainConfigStatus value
func NewChainConfigStatus(value uint8) ChainConfigStatus {
	return ChainConfigStatus(value)
}

// Uint8 converts ChainConfigStatus to uint8
func (cs ChainConfigStatus) Uint8() uint8 {
	return uint8(cs)
}

type ChainConfigs map[currency.AssetCode]map[ChainNetwork]ChainConfig

var _defaultUSDTMinAmountWithdrawalReview = decimal.NewFromInt(1000) // 1000 USDT

// ChainConfig holds runtime configuration for a specific chain/asset pair
type ChainConfig struct {
	ID                        ID
	AssetCode                 currency.AssetCode
	Network                   ChainNetwork
	Decimals                  uint8
	MinConfirmations          uint8
	DustThreshold             decimal.Decimal
	ConfirmFinality           uint8
	AssetName                 string
	BlockUrl                  *string
	TxUrl                     *string
	AddressUrl                *string
	ContractAddressUrl        *string
	AddressRegex              *string
	WithdrawFee               decimal.Decimal
	DepositFee                decimal.Decimal
	MinAmountWithdrawalReview decimal.Decimal
	WithdrawFeeType           FeeType
	DepositFeeType            FeeType
	EstimatedArrivalTime      int
	Status                    ChainConfigStatus
	CreatedAt                 time.Time
	UpdatedAt                 *time.Time
}

// ChainConfigRepository defines operations for chain configurations
type ChainConfigRepository interface {
	Create(context.Context, *ChainConfig) error
	GetAll(context.Context) (ChainConfigs, error)
}

// DefaultChainConfigs returns the default chain configurations
func DefaultChainConfigs() []ChainConfig {
	return []ChainConfig{
		{
			AssetCode:        currency.DefaultAccountAssetCode,
			Network:          ChainNetworkTRX,
			Decimals:         6,
			MinConfirmations: 20,
			DustThreshold:    decimal.Zero,
			ConfirmFinality:  20,
			AssetName:        currency.DefaultAccountAssetCode.String(),
			AddressRegex:     tools.ToPtr(TRXAddressRegex),
			WithdrawFee:      decimal.Zero,
			DepositFee:       decimal.Zero,
			WithdrawFeeType:  FeeTypePercent,
			DepositFeeType:   FeeTypePercent,
			// Amounts below this threshold are sent to node-service automatically.
			MinAmountWithdrawalReview: _defaultUSDTMinAmountWithdrawalReview,
			Status:                    ChainConfigStatusAll,
		},
	}
}

// AddressAssignment represents a deposit address assigned to a user
type AddressAssignment struct {
	ID        ID
	OwnerID   *ID // nil for system-owned addresses
	AssetCode currency.AssetCode
	Network   ChainNetwork
	Purpose   PurposeAddress
	Address   string
	Status    AddressStatus
	CreatedAt time.Time
	UpdatedAt *time.Time
}

type PurposeAddress uint8

const PurposeAddressDeposit PurposeAddress = iota + 1

// NewPurposeAddress creates a new PurposeAddress from a uint8 value.
func NewPurposeAddress(p uint8) PurposeAddress {
	return PurposeAddress(p)
}

// Uint8 converts PurposeAddress to uint8.
func (p PurposeAddress) Uint8() uint8 {
	return uint8(p)
}

var _purposeAddressToString = map[PurposeAddress]string{
	PurposeAddressDeposit: "deposit",
}

// String returns the string representation of the PurposeAddress.
func (p PurposeAddress) String() string {
	if str, ok := _purposeAddressToString[p]; ok {
		return str
	}
	return "unknown"
}

// AllocateAddressParams contains parameters for address allocation
type AllocateAddressParams struct {
	OwnerID     *ID
	Purpose     PurposeAddress
	ChainConfig ChainConfig
}

type AllocateAddressResult struct {
	AddressAssignment AddressAssignment
}

// RefreshAddressParams contains parameters for refreshing an address
type RefreshAddressParams struct {
	ChainConfig ChainConfig
	Address     string
}

// ProcessBlockchainTxParams contains parameters for processing blockchain transactions
type ProcessBlockchainTxParams struct {
	ExternalRequestID     string
	TxID                  *string
	AssetCode             currency.AssetCode
	Network               ChainNetwork
	Type                  BlockchainTxType
	FromAddress           string
	ToAddress             string
	Amount                decimal.Decimal //
	Confirmations         uint8
	RequiredConfirmations uint8
	Status                BlockchainTxStatus
}

// GetQuantizedAmount returns the amount shifted to the default asset scale.
func (p ProcessBlockchainTxParams) GetQuantizedAmount() decimal.Decimal {
	return p.Amount.Shift(currency.DefaultAssetScale)
}

type GetFeesParams struct {
	AssetCode currency.AssetCode
	Network   ChainNetwork
}

type GetActiveAddressParams struct {
	OwnerID   *ID
	Address   *string
	IsSystem  bool
	Purpose   PurposeAddress
	AssetCode currency.AssetCode
	Network   ChainNetwork
}

type GetBlockchainFeeResult struct {
	Network         string
	Unit            string
	EnergyPriceSun  decimal.Decimal
	EstimatedEnergy decimal.Decimal
	Slow            GetBlockchainFeeItem
	Standard        GetBlockchainFeeItem
	Fast            GetBlockchainFeeItem
	AssetRate       decimal.Decimal
}

// GetSlowFee calculates the slow fee in asset terms.
func (r GetBlockchainFeeResult) GetSlowFee() decimal.Decimal {
	return r.Slow.EstimatedFee.Mul(r.AssetRate)
}

// GetStandardFee calculates the standard fee in asset terms.
func (r GetBlockchainFeeResult) GetStandardFee() decimal.Decimal {
	return r.Standard.EstimatedFee.Mul(r.AssetRate)
}

// GetFastFee calculates the fast fee in asset terms.
func (r GetBlockchainFeeResult) GetFastFee() decimal.Decimal {
	return r.Fast.EstimatedFee.Mul(r.AssetRate)
}

type GetBlockchainFeeItem struct {
	GasPrice      decimal.Decimal
	EstimatedTime int64
	EstimatedFee  decimal.Decimal
}

type GenerateBlockchainAddressParams struct {
	AssetCode      currency.AssetCode
	Network        ChainNetwork
	RefreshAddress *string
}

type GenerateBlockchainAddressResult struct {
	Address string
}

type WithdrawalBlockchainTxParams struct {
	ToAddress      string
	Amount         decimal.Decimal
	FeeTier        FeeTier
	AssetCode      currency.AssetCode
	Network        ChainNetwork
	IdempotencyKey string
}

// GetQuantizedAmount returns the amount shifted to the default asset scale.
func (p WithdrawalBlockchainTxParams) GetQuantizedAmount() decimal.Decimal {
	return p.Amount.Shift(currency.DefaultAssetScale)
}

type WithdrawalBlockchainTxResult struct {
	ExternalRequestID     string
	TxID                  *string
	FromAddress           string
	Amount                decimal.Decimal
	Confirmations         uint8
	RequiredConfirmations uint8
	Fee                   decimal.Decimal
	Status                BlockchainTxStatus
	BlockchainTx          BlockchainTx
}

// AddressAssignmentRepository defines operations for address assignments
type AddressAssignmentRepository interface {
	Create(context.Context, *AddressAssignment) error
	GetActive(context.Context, GetActiveAddressParams) (AddressAssignment, error)
	UpdateStatus(context.Context, ID, AddressStatus) error

	WithTxFromContext(context.Context) AddressAssignmentRepository
}

// BlockchainController defines the interface for blockchain operations
type BlockchainController interface {
	GetChainConfigs(context.Context) (ChainConfigs, error)
	AllocateAddress(context.Context, AllocateAddressParams) (AllocateAddressResult, error)
	RefreshAddress(context.Context, RefreshAddressParams) error
	ProcessTransaction(context.Context, ProcessBlockchainTxParams) error
	Withdrawal(context.Context, WithdrawalBlockchainTxParams) (BlockchainTx, error)
	GetTransactionByID(context.Context, ID) (BlockchainTx, error)
	CreateDefaultChainConfigs(context.Context) error
	GetFees(context.Context, GetFeesParams) (GetBlockchainFeeResult, error)
	PollTransactionStatus(context.Context, ID) (TransactionStatusResult, error)
	FindStuckInboundForReconcile(context.Context, time.Duration, int) ([]BlockchainTx, error)
}

// NodeService defines the interface for interacting with the node service
type NodeService interface {
	GenerateAddress(context.Context, GenerateBlockchainAddressParams) (GenerateBlockchainAddressResult, error)
	Withdrawal(context.Context, WithdrawalBlockchainTxParams) (WithdrawalBlockchainTxResult, error)
	GetFees(context.Context, ChainNetwork) (GetBlockchainFeeResult, error)
	GetTransactionStatus(context.Context, ChainNetwork, string) (TransactionStatusResult, error)
	GetTransactionStatusByRequestID(context.Context, ChainNetwork, string) (TransactionStatusResult, error)
}

// TransactionStatusResult is the snapshot returned by the node-service transaction-status endpoint.
type TransactionStatusResult struct {
	TxID                  *string
	Status                BlockchainTxStatus
	Confirmations         uint8
	RequiredConfirmations uint8
}

// BlockchainTxStatus represents the status of a blockchain transaction
type BlockchainTxStatus uint8

const (
	BlockchainTxStatusNew BlockchainTxStatus = iota + 1
	BlockchainTxStatusPending
	BlockchainTxStatusConfirmed
	BlockchainTxStatusFailed
	BlockchainTxStatusIgnored
	BlockchainTxStatusBlocked
	BlockchainTxPendingSignatures
)

// IsTerminal reports whether the chain has said its final word about this
// transaction.
//
// Note the numeric order of the constants is not a progression — PendingSignatures
// sits last but precedes Pending for a multisig withdrawal — so "has it moved
// forward" cannot be asked by comparing values. Terminality is the property
// that actually matters: a transaction that reached one of these has been
// settled, failed, dropped below the dust threshold, or blocked, and a later
// callback reporting it as New or Pending again is stale or reordered delivery.
func (s BlockchainTxStatus) IsTerminal() bool {
	switch s {
	case BlockchainTxStatusConfirmed,
		BlockchainTxStatusFailed,
		BlockchainTxStatusIgnored,
		BlockchainTxStatusBlocked:
		return true
	default:
		return false
	}
}

// Uint8 converts BlockchainTxStatus to uint8.
func (s BlockchainTxStatus) Uint8() uint8 {
	return uint8(s)
}

var _blockchainTxStatusToString = map[BlockchainTxStatus]string{
	BlockchainTxStatusNew:         "new",
	BlockchainTxStatusPending:     "pending",
	BlockchainTxStatusConfirmed:   "confirmed",
	BlockchainTxStatusFailed:      "failed",
	BlockchainTxStatusIgnored:     "ignored",
	BlockchainTxStatusBlocked:     "blocked",
	BlockchainTxPendingSignatures: "pending_signatures",
}

var _blockchainTxStatusByString = map[string]BlockchainTxStatus{
	"new":                BlockchainTxStatusNew,
	"pending":            BlockchainTxStatusPending,
	"confirmed":          BlockchainTxStatusConfirmed,
	"failed":             BlockchainTxStatusFailed,
	"ignored":            BlockchainTxStatusIgnored,
	"blocked":            BlockchainTxStatusBlocked,
	"pending_signatures": BlockchainTxPendingSignatures,
}

// String returns the string representation of the BlockchainTxStatus.
func (s BlockchainTxStatus) String() string {
	if str, ok := _blockchainTxStatusToString[s]; ok {
		return str
	}
	return "unknown"
}

// NewBlockchainTxStatus creates a new BlockchainTxStatus from a uint8 value.
func NewBlockchainTxStatus(s uint8) BlockchainTxStatus {
	return BlockchainTxStatus(s)
}

// NewBlockchainTxStatusFromString converts a transport string to BlockchainTxStatus.
func NewBlockchainTxStatusFromString(value string) (BlockchainTxStatus, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	status, ok := _blockchainTxStatusByString[normalized]
	if ok {
		return status, nil
	}

	return 0, fmt.Errorf("[domain][NewBlockchainTxStatusFromString][err: unsupported blockchain transaction status %q]", value)
}

type BlockchainTxType uint8

const (
	BlockchainTxTypeInbound BlockchainTxType = iota + 1
	BlockchainTxTypeOutbound
)

var _blockchainTxTypeToString = map[BlockchainTxType]string{
	BlockchainTxTypeInbound:  "inbound",
	BlockchainTxTypeOutbound: "outbound",
}

var _blockchainTxTypeByString = map[string]BlockchainTxType{
	"incoming": BlockchainTxTypeInbound,
	"outgoing": BlockchainTxTypeOutbound,
}

// Uint8 converts BlockchainTxType to uint8.
func (t BlockchainTxType) Uint8() uint8 {
	return uint8(t)
}

// String returns the string representation of the BlockchainTxType.
func (t BlockchainTxType) String() string {
	if s, ok := _blockchainTxTypeToString[t]; ok {
		return s
	}
	return "unknown"
}

// NewBlockchainTxType creates a new BlockchainTxType from a uint8 value.
func NewBlockchainTxType(t uint8) BlockchainTxType {
	return BlockchainTxType(t)
}

// NewBlockchainTxTypeFromString converts a transport string to BlockchainTxType.
func NewBlockchainTxTypeFromString(value string) (BlockchainTxType, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	txType, ok := _blockchainTxTypeByString[normalized]
	if ok {
		return txType, nil
	}

	return 0, fmt.Errorf("[domain][NewBlockchainTxTypeFromString][err: unsupported blockchain transaction type %q]", value)
}

// BlockchainTx represents a blockchain transaction
type BlockchainTx struct {
	ID                ID
	ExternalRequestID string
	TxID              *string
	AssetCode         currency.AssetCode
	Network           ChainNetwork
	Type              BlockchainTxType
	FromAddress       string
	ToAddress         string
	Amount            decimal.Decimal
	Confirmations     uint8
	Status            BlockchainTxStatus
	CreatedAt         time.Time
	ConfirmedAt       *time.Time
	UpdatedAt         *time.Time
}

// GetAmountInBaseUnits converts the amount from quants to base units
func (bt BlockchainTx) GetAmountInBaseUnits() decimal.Decimal {
	return bt.Amount.Shift(-currency.DefaultAssetScale)
}

type GetBlockchainTxParams struct {
	TxID      string
	AssetCode currency.AssetCode
	Network   ChainNetwork
}

// BlockchainTxRepository defines operations for blockchain transactions
type BlockchainTxRepository interface {
	Create(context.Context, *BlockchainTx) error
	GetByParams(context.Context, GetBlockchainTxParams) (BlockchainTx, error)
	GetByExternalRequestID(context.Context, string) (BlockchainTx, error)
	GetByID(context.Context, ID) (BlockchainTx, error)
	UpdateStatus(context.Context, ID, BlockchainTxStatus, uint8, *string) error
	FindStuckInboundForReconcile(context.Context, time.Duration, int) ([]BlockchainTx, error)

	WithTxFromContext(context.Context) BlockchainTxRepository
}
