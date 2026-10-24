package controllers

import (
	"context"
	"errors"
	"testing"
	"time"

	"account-service/internal/domain"

	"github.com/shopspring/decimal"
	appErrors "github.com/whagency/core-common/errorsx"
	"github.com/whagency/core-common/services/currency"
	"github.com/whagency/core-common/testkit/dbtest"
	"github.com/whagency/core-common/testkit/errorxtest"
)

// ============================================================
// calculateBlockchainTxStatus — table-driven unit tests
// ============================================================

func TestCalculateBlockchainTxStatus(t *testing.T) {
	t.Parallel()

	dust := decimal.NewFromInt(1000)
	normal := decimal.NewFromInt(5000)
	zero := decimal.Zero

	cases := []struct {
		name   string
		params calculateBlockchainTxStatusParams
		want   domain.BlockchainTxStatus
	}{
		{
			name: "flagged_address_takes_precedence",
			params: calculateBlockchainTxStatusParams{
				Type:       domain.BlockchainTxTypeInbound,
				Status:     domain.BlockchainTxStatusFailed,
				KYTVerdict: domain.KYTVerdictFlagged,
			},
			want: domain.BlockchainTxStatusBlocked,
		},
		{
			// An undecided verdict must not reach Confirmed: that is the status
			// the deposit flow credits on.
			name: "inconclusive_kyt_holds_confirmed_at_pending",
			params: calculateBlockchainTxStatusParams{
				Type:             domain.BlockchainTxTypeInbound,
				Status:           domain.BlockchainTxStatusConfirmed,
				Amount:           normal,
				DustThreshold:    zero,
				Confirmations:    20,
				MinConfirmations: 20,
				KYTVerdict:       domain.KYTVerdictInconclusive,
			},
			want: domain.BlockchainTxStatusPending,
		},
		{
			// Holding applies only to the crediting path; a chain-level failure
			// stays terminal because it never credits anyone.
			name: "inconclusive_kyt_leaves_failed_terminal",
			params: calculateBlockchainTxStatusParams{
				Type:       domain.BlockchainTxTypeInbound,
				Status:     domain.BlockchainTxStatusFailed,
				KYTVerdict: domain.KYTVerdictInconclusive,
			},
			want: domain.BlockchainTxStatusFailed,
		},
		{
			name: "clean_kyt_confirms_normally",
			params: calculateBlockchainTxStatusParams{
				Type:             domain.BlockchainTxTypeInbound,
				Status:           domain.BlockchainTxStatusConfirmed,
				Amount:           normal,
				DustThreshold:    zero,
				Confirmations:    20,
				MinConfirmations: 20,
				KYTVerdict:       domain.KYTVerdictClean,
			},
			want: domain.BlockchainTxStatusConfirmed,
		},
		{
			name: "failed_status",
			params: calculateBlockchainTxStatusParams{
				Type:   domain.BlockchainTxTypeInbound,
				Status: domain.BlockchainTxStatusFailed,
			},
			want: domain.BlockchainTxStatusFailed,
		},
		{
			name: "inbound_below_dust_threshold",
			params: calculateBlockchainTxStatusParams{
				Type:          domain.BlockchainTxTypeInbound,
				Status:        domain.BlockchainTxStatusNew,
				Amount:        decimal.NewFromInt(500), // < dust(1000)
				DustThreshold: dust,
			},
			want: domain.BlockchainTxStatusIgnored,
		},
		{
			name: "outbound_below_dust_not_ignored",
			params: calculateBlockchainTxStatusParams{
				Type:          domain.BlockchainTxTypeOutbound,
				Status:        domain.BlockchainTxStatusNew,
				Amount:        decimal.NewFromInt(500),
				DustThreshold: dust,
			},
			want: domain.BlockchainTxStatusNew,
		},
		{
			name: "no_confirmations_returns_new",
			params: calculateBlockchainTxStatusParams{
				Type:          domain.BlockchainTxTypeInbound,
				Status:        domain.BlockchainTxStatusNew,
				Amount:        normal,
				DustThreshold: zero,
				Confirmations: 0,
			},
			want: domain.BlockchainTxStatusNew,
		},
		{
			name: "some_confirmations_below_min",
			params: calculateBlockchainTxStatusParams{
				Type:             domain.BlockchainTxTypeInbound,
				Status:           domain.BlockchainTxStatusNew,
				Amount:           normal,
				DustThreshold:    zero,
				Confirmations:    5,
				MinConfirmations: 20,
			},
			want: domain.BlockchainTxStatusPending,
		},
		{
			name: "confirmed_status_with_enough_confirmations",
			params: calculateBlockchainTxStatusParams{
				Type:             domain.BlockchainTxTypeInbound,
				Status:           domain.BlockchainTxStatusConfirmed,
				Amount:           normal,
				DustThreshold:    zero,
				Confirmations:    20,
				MinConfirmations: 20,
				KYTVerdict:       domain.KYTVerdictClean,
			},
			want: domain.BlockchainTxStatusConfirmed,
		},
		{
			// Outbound transactions are screened before the funds are sent, so
			// tracking their on-chain status must still be able to confirm.
			name: "not_checked_confirms_normally",
			params: calculateBlockchainTxStatusParams{
				Type:             domain.BlockchainTxTypeOutbound,
				Status:           domain.BlockchainTxStatusConfirmed,
				Amount:           normal,
				DustThreshold:    zero,
				Confirmations:    20,
				MinConfirmations: 20,
				KYTVerdict:       domain.KYTVerdictNotChecked,
			},
			want: domain.BlockchainTxStatusConfirmed,
		},
		{
			// The whole point of the allow-list: a verdict nobody set holds
			// instead of passing for a clean check.
			name: "unset_verdict_holds_confirmed_at_pending",
			params: calculateBlockchainTxStatusParams{
				Type:             domain.BlockchainTxTypeInbound,
				Status:           domain.BlockchainTxStatusConfirmed,
				Amount:           normal,
				DustThreshold:    zero,
				Confirmations:    20,
				MinConfirmations: 20,
			},
			want: domain.BlockchainTxStatusPending,
		},
		{
			name: "confirmed_status_but_confirmations_below_min",
			params: calculateBlockchainTxStatusParams{
				Type:             domain.BlockchainTxTypeInbound,
				Status:           domain.BlockchainTxStatusConfirmed,
				Amount:           normal,
				DustThreshold:    zero,
				Confirmations:    5,
				MinConfirmations: 20,
			},
			want: domain.BlockchainTxStatusPending,
		},
		{
			name: "inbound_exact_dust_threshold_not_ignored",
			params: calculateBlockchainTxStatusParams{
				Type:          domain.BlockchainTxTypeInbound,
				Status:        domain.BlockchainTxStatusNew,
				Amount:        dust, // equal — not less than
				DustThreshold: dust,
			},
			want: domain.BlockchainTxStatusNew,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := calculateBlockchainTxStatus(tc.params)
			if got != tc.want {
				t.Fatalf("calculateBlockchainTxStatus: got %v, want %v", got, tc.want)
			}
		})
	}
}

// ============================================================
// GetChainConfigs
// ============================================================

func TestBlockchainGetChainConfigs_Success(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	expected := domain.ChainConfigs{
		currency.DefaultAccountAssetCode: {domain.ChainNetworkTRX: testBlockchainChainConfig()},
	}
	f.chainConfigs.getAllFunc = func(context.Context) (domain.ChainConfigs, error) {
		return expected, nil
	}

	result, err := f.ctrl.GetChainConfigs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != len(expected) {
		t.Fatalf("unexpected chain configs count: got %d, want %d", len(result), len(expected))
	}
}

func TestBlockchainGetChainConfigs_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.chainConfigs.getAllFunc = func(context.Context) (domain.ChainConfigs, error) {
		return nil, errors.New("db error")
	}

	_, err := f.ctrl.GetChainConfigs(context.Background())
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// AllocateAddress
// ============================================================

func TestBlockchainAllocateAddress_Success(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	ownerID := domain.NewIDFromUint64(42)
	f.node.generateAddressFunc = func(_ context.Context, params domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error) {
		return domain.GenerateBlockchainAddressResult{Address: "TNEWADDR"}, nil
	}
	f.addresses.createFunc = func(_ context.Context, a *domain.AddressAssignment) error {
		a.ID = domain.NewIDFromUint64(1)
		return nil
	}

	result, err := f.ctrl.AllocateAddress(context.Background(), domain.AllocateAddressParams{
		OwnerID:     &ownerID,
		Purpose:     domain.PurposeAddressDeposit,
		ChainConfig: testBlockchainChainConfig(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AddressAssignment.Address != "TNEWADDR" {
		t.Fatalf("unexpected address: got %s, want TNEWADDR", result.AddressAssignment.Address)
	}
	if result.AddressAssignment.Status != domain.AddressStatusAllocated {
		t.Fatalf("unexpected address status: got %v, want Allocated", result.AddressAssignment.Status)
	}
}

func TestBlockchainAllocateAddress_NodeError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.generateAddressFunc = func(context.Context, domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error) {
		return domain.GenerateBlockchainAddressResult{}, errors.New("node down")
	}

	_, err := f.ctrl.AllocateAddress(context.Background(), domain.AllocateAddressParams{
		ChainConfig: testBlockchainChainConfig(),
	})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestBlockchainAllocateAddress_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.generateAddressFunc = func(context.Context, domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error) {
		return domain.GenerateBlockchainAddressResult{Address: "TADDR"}, nil
	}
	f.addresses.createFunc = func(context.Context, *domain.AddressAssignment) error {
		return errors.New("db error")
	}

	_, err := f.ctrl.AllocateAddress(context.Background(), domain.AllocateAddressParams{
		ChainConfig: testBlockchainChainConfig(),
	})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// RefreshAddress
// ============================================================

func TestBlockchainRefreshAddress_Success(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	called := false
	f.node.generateAddressFunc = func(_ context.Context, params domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error) {
		called = true
		if params.RefreshAddress == nil || *params.RefreshAddress != "TOLDADDR" {
			t.Errorf("expected RefreshAddress=TOLDADDR, got %v", params.RefreshAddress)
		}
		return domain.GenerateBlockchainAddressResult{Address: "TOLDADDR"}, nil
	}

	err := f.ctrl.RefreshAddress(context.Background(), domain.RefreshAddressParams{
		ChainConfig: testBlockchainChainConfig(),
		Address:     "TOLDADDR",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected node.GenerateAddress to be called")
	}
}

func TestBlockchainRefreshAddress_NodeError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.generateAddressFunc = func(context.Context, domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error) {
		return domain.GenerateBlockchainAddressResult{}, errors.New("node down")
	}

	err := f.ctrl.RefreshAddress(context.Background(), domain.RefreshAddressParams{
		ChainConfig: testBlockchainChainConfig(),
		Address:     "TOLDADDR",
	})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

// ============================================================
// ProcessTransaction
// ============================================================

func TestBlockchainProcessTransaction_NewInbound_CreatesAndPublishesEvent(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	var createdTx domain.BlockchainTx
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(10)
		createdTx = *tx
		return nil
	}
	publishedIDs := make([]uint64, 0)
	f.event.publishBlockchainTransactionUpdateFn = func(_ context.Context, ev domain.BlockchainTransactionUpdateEvent) error {
		publishedIDs = append(publishedIDs, ev.ID)
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-inbound-1",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		ToAddress:         "TRECEIVER",
		Amount:            decimal.NewFromInt(1),
		Confirmations:     0,
		Status:            domain.BlockchainTxStatusNew,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createdTx.Status != domain.BlockchainTxStatusNew {
		t.Fatalf("unexpected tx status: got %v, want New", createdTx.Status)
	}
	if createdTx.Type != domain.BlockchainTxTypeInbound {
		t.Fatalf("unexpected tx type: got %v, want Inbound", createdTx.Type)
	}
	if len(publishedIDs) != 1 {
		t.Fatalf("expected 1 event published, got %d", len(publishedIDs))
	}
}

func TestBlockchainProcessTransaction_NewInboundConfirmed_SetsConfirmedAt(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		cfg := testBlockchainChainConfig()
		cfg.MinConfirmations = 20
		return cfg, nil
	}
	var createdTx domain.BlockchainTx
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(10)
		confirmedAt := time.Now().UTC()
		tx.ConfirmedAt = &confirmedAt
		createdTx = *tx
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-confirmed",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		ToAddress:         "TRECEIVER",
		Amount:            decimal.NewFromInt(1),
		Confirmations:     20,
		Status:            domain.BlockchainTxStatusConfirmed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createdTx.Status != domain.BlockchainTxStatusConfirmed {
		t.Fatalf("unexpected tx status: got %v, want Confirmed", createdTx.Status)
	}
	if createdTx.ConfirmedAt == nil {
		t.Fatal("expected ConfirmedAt to be set for confirmed tx")
	}
}

func TestBlockchainProcessTransaction_InboundKYTBlocked_CreatesWithBlockedStatus(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return domain.KYTCheckAddressResult{Status: domain.KYTStatusSuccess, IsClean: false}, nil
	}
	var createdStatus domain.BlockchainTxStatus
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(1)
		createdStatus = tx.Status
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-blocked",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TBLOCKED",
		ToAddress:         "TRECEIVER",
		Amount:            decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createdStatus != domain.BlockchainTxStatusBlocked {
		t.Fatalf("unexpected status: got %v, want Blocked", createdStatus)
	}
}

func TestBlockchainProcessTransaction_InboundKYTError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return domain.KYTCheckAddressResult{}, errors.New("kyt service unavailable")
	}
	f.txs.createFunc = func(context.Context, *domain.BlockchainTx) error {
		t.Fatal("Create must not be called when KYT service errors")
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-kyterror",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		ToAddress:         "TRECEIVER",
		Amount:            decimal.NewFromInt(1),
	})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestBlockchainProcessTransaction_InboundBelowDust_CreatesIgnored(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		cfg := testBlockchainChainConfig()
		cfg.DustThreshold = decimal.NewFromInt(200_000_000) // 2 USDT in quants
		return cfg, nil
	}
	var createdStatus domain.BlockchainTxStatus
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(1)
		createdStatus = tx.Status
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-dust",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		ToAddress:         "TRECEIVER",
		Amount:            decimal.NewFromFloat(0.5), // 0.5 raw → after Shift(8) = 50_000_000 < 200_000_000
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if createdStatus != domain.BlockchainTxStatusIgnored {
		t.Fatalf("unexpected status: got %v, want Ignored", createdStatus)
	}
}

func TestBlockchainProcessTransaction_OutboundSkipsKYT(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	// kyt.checkAddressFunc is nil — if called, it panics
	f.kyt.checkAddressFunc = nil
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(1)
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-outbound",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeOutbound,
		ToAddress:         "TRECEIVER",
		Amount:            decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBlockchainProcessTransaction_ExistingTxNoChange_NoEventPublished(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	existing := domain.BlockchainTx{
		ID:            domain.NewIDFromUint64(5),
		Status:        domain.BlockchainTxStatusPending,
		Confirmations: 10,
	}
	existing = testStoredTxIdentity(existing, domain.BlockchainTxTypeInbound, "TSENDER", "TRECEIVER")
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return existing, nil
	}
	published := false
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		published = true
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-existing",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		ToAddress:         "TRECEIVER",
		Amount:            decimal.NewFromInt(1),
		Confirmations:     10,
		Status:            domain.BlockchainTxStatusPending, // same as existing
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if published {
		t.Fatal("expected no event for unchanged tx")
	}
}

func TestBlockchainProcessTransaction_ExistingTxStatusChanged_UpdatesAndPublishes(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	existing := domain.BlockchainTx{
		ID:            domain.NewIDFromUint64(5),
		Status:        domain.BlockchainTxStatusPending,
		Confirmations: 10,
	}
	existing = testStoredTxIdentity(existing, domain.BlockchainTxTypeInbound, "TSENDER", "")
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return existing, nil
	}
	var updatedID domain.ID
	var updatedStatus domain.BlockchainTxStatus
	f.txs.updateStatusFunc = func(_ context.Context, id domain.ID, status domain.BlockchainTxStatus, _ uint8, _ *string) error {
		updatedID = id
		updatedStatus = status
		return nil
	}
	publishCount := 0
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		publishCount++
		return nil
	}
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		cfg := testBlockchainChainConfig()
		cfg.MinConfirmations = 20
		return cfg, nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-existing",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		Amount:            decimal.NewFromInt(1),
		Confirmations:     20,
		Status:            domain.BlockchainTxStatusConfirmed,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedID != existing.ID {
		t.Fatalf("unexpected updated id: got %v, want %v", updatedID, existing.ID)
	}
	if updatedStatus != domain.BlockchainTxStatusConfirmed {
		t.Fatalf("unexpected updated status: got %v, want Confirmed", updatedStatus)
	}
	if publishCount != 1 {
		t.Fatalf("expected 1 event, got %d", publishCount)
	}
}

func TestBlockchainProcessTransaction_ExistingTxIDAppeared_UpdatesAndPublishes(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	existing := domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(5),
		ExternalRequestID: "req-existing",
		Status:            domain.BlockchainTxStatusPending,
		Confirmations:     3,
	}
	existing = testStoredTxIdentity(existing, domain.BlockchainTxTypeOutbound, "", "")
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return existing, nil
	}
	var updatedTxID *string
	f.txs.updateStatusFunc = func(_ context.Context, _ domain.ID, _ domain.BlockchainTxStatus, _ uint8, txID *string) error {
		updatedTxID = txID
		return nil
	}
	publishCount := 0
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		publishCount++
		return nil
	}

	txID := "broadcasted-hash"
	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-existing",
		TxID:              &txID,
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeOutbound,
		Amount:            decimal.NewFromInt(1),
		Confirmations:     3,
		Status:            domain.BlockchainTxStatusPending,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if updatedTxID == nil || *updatedTxID != txID {
		t.Fatalf("expected txID update %q, got %v", txID, updatedTxID)
	}
	if publishCount != 1 {
		t.Fatalf("expected 1 event, got %d", publishCount)
	}
}

// TestBlockchainProcessTransaction_LockerCacheError_ReturnsCacheError verifies
// that a locker failure other than KeyAlreadyLockedErr is surfaced as
// CacheError (not InternalServerError), so callers and dashboards can
// distinguish a cache-layer problem from an unexpected internal fault.
func TestBlockchainProcessTransaction_LockerCacheError_ReturnsCacheError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.processingLocker.tryLockFunc = func(context.Context, string) error {
		return errors.New("hazelcast connection refused")
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-cache-err",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
	})
	errorxtest.AssertErrorCode(t, err, appErrors.CacheError)
}

// TestBlockchainProcessTransaction_ExistingTxIDNotOverwrittenByNewTxID
// verifies that when an existing BlockchainTx already has a tx_id set, and
// params carries a different TxID but nothing else changed (status and
// confirmations are unchanged), the record is treated as no-op. This is the
// idempotency guard that prevents a second poll result from silently replacing
// the canonical on-chain hash.
func TestBlockchainProcessTransaction_ExistingTxIDNotOverwrittenByNewTxID(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	existingHash := "original-hash"
	existing := domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(3),
		ExternalRequestID: "req-existing",
		TxID:              &existingHash,
		Status:            domain.BlockchainTxStatusPending,
		Confirmations:     5,
	}
	existing = testStoredTxIdentity(existing, domain.BlockchainTxTypeOutbound, "", "")
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return existing, nil
	}
	// UpdateStatus must not be called — any call would panic.

	differentHash := "different-hash"
	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-existing",
		TxID:              &differentHash,
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeOutbound,
		Amount:            decimal.NewFromInt(1),
		Confirmations:     5,                                // unchanged
		Status:            domain.BlockchainTxStatusPending, // unchanged
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBlockchainProcessTransaction_GetChainConfigError_ReturnsError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return domain.ChainConfig{}, errors.New("cache miss")
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-err",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestBlockchainProcessTransaction_GetByExternalRequestIDDBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, errors.New("db error")
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-err",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		Amount:            decimal.NewFromInt(1),
	})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestBlockchainProcessTransaction_CreateDBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.txs.createFunc = func(context.Context, *domain.BlockchainTx) error {
		return errors.New("db error")
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-err",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		Amount:            decimal.NewFromInt(1),
	})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestBlockchainProcessTransaction_UpdateStatusDBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	existing := domain.BlockchainTx{
		ID:            domain.NewIDFromUint64(5),
		Status:        domain.BlockchainTxStatusNew,
		Confirmations: 0,
	}
	existing = testStoredTxIdentity(existing, domain.BlockchainTxTypeInbound, "TSENDER", "")
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return existing, nil
	}
	f.txs.updateStatusFunc = func(context.Context, domain.ID, domain.BlockchainTxStatus, uint8, *string) error {
		return errors.New("db error")
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-err",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		Amount:            decimal.NewFromInt(1),
		Confirmations:     10, // changed from 0
		Status:            domain.BlockchainTxStatusPending,
	})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

func TestBlockchainProcessTransaction_PublishEventError_IsLoggedOnly(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(1)
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		return errors.New("queue down")
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-event-fail",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		Amount:            decimal.NewFromInt(1),
	})
	if err != nil {
		t.Fatalf("expected publish failure to be logged only, got %v", err)
	}
	if f.txManager.BeginCalls != 0 {
		t.Fatalf("expected no DB transaction on publish failure path, got %d", f.txManager.BeginCalls)
	}
	if f.txManager.CommitCalls != 0 {
		t.Fatalf("expected no commit on publish failure path, got %d", f.txManager.CommitCalls)
	}
	if f.txManager.RollbackCalls != 0 {
		t.Fatalf("expected no rollback on publish failure path, got %d", f.txManager.RollbackCalls)
	}
}

func TestBlockchainProcessTransaction_NoChange_NoCommitNoPublish(t *testing.T) {
	t.Parallel()

	existing := domain.BlockchainTx{
		ID:            domain.NewIDFromUint64(7),
		Status:        domain.BlockchainTxStatusPending,
		Confirmations: 5,
	}
	existing = testStoredTxIdentity(existing, domain.BlockchainTxTypeInbound, "TSENDER", "")

	f := newBlockchainFixture()
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return existing, nil
	}
	publishCalled := false
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		publishCalled = true
		return nil
	}

	err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
		ExternalRequestID: "req-no-change",
		AssetCode:         currency.DefaultAccountAssetCode,
		Network:           domain.ChainNetworkTRX,
		Type:              domain.BlockchainTxTypeInbound,
		FromAddress:       "TSENDER",
		Amount:            decimal.NewFromInt(1),
		Status:            domain.BlockchainTxStatusPending,
		Confirmations:     5, // same as existing — no update
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if f.txManager.BeginCalls != 0 {
		t.Fatalf("expected no DB transaction opened for no-change path, got %d", f.txManager.BeginCalls)
	}
	if f.txManager.CommitCalls != 0 {
		t.Fatalf("expected no commit for no-change path, got %d", f.txManager.CommitCalls)
	}
	if publishCalled {
		t.Fatal("expected no publish for no-change path")
	}
}

// ============================================================
// Withdrawal
// ============================================================

func TestBlockchainWithdrawal_NodeReturnsNew_StoredAsNew(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.withdrawalFunc = func(_ context.Context, params domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
		return domain.WithdrawalBlockchainTxResult{
			ExternalRequestID:     "ext-001",
			FromAddress:           "TFROM",
			Status:                domain.BlockchainTxStatusNew,
			Confirmations:         0,
			RequiredConfirmations: 20,
		}, nil
	}
	var storedTx domain.BlockchainTx
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(1)
		storedTx = *tx
		return nil
	}

	result, err := f.ctrl.Withdrawal(context.Background(), domain.WithdrawalBlockchainTxParams{
		ToAddress: "TRECEIVER",
		Amount:    decimal.NewFromInt(1),
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.BlockchainTxStatusNew {
		t.Fatalf("unexpected result status: got %v, want New", result.Status)
	}
	if storedTx.Type != domain.BlockchainTxTypeOutbound {
		t.Fatalf("unexpected stored tx type: got %v, want Outbound", storedTx.Type)
	}
	if storedTx.ConfirmedAt != nil {
		t.Fatal("expected ConfirmedAt to be nil for New tx")
	}
}

func TestBlockchainWithdrawal_NodeReturnsConfirmed_SetsConfirmedAt(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
		return domain.WithdrawalBlockchainTxResult{
			ExternalRequestID:     "ext-002",
			FromAddress:           "TFROM",
			Status:                domain.BlockchainTxStatusConfirmed,
			Confirmations:         20,
			RequiredConfirmations: 20,
		}, nil
	}
	var storedTx domain.BlockchainTx
	f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
		tx.ID = domain.NewIDFromUint64(2)
		confirmedAt := time.Now().UTC()
		tx.ConfirmedAt = &confirmedAt
		storedTx = *tx
		return nil
	}

	result, err := f.ctrl.Withdrawal(context.Background(), domain.WithdrawalBlockchainTxParams{
		ToAddress: "TRECEIVER",
		Amount:    decimal.NewFromInt(1),
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.BlockchainTxStatusConfirmed {
		t.Fatalf("unexpected result status: got %v, want Confirmed", result.Status)
	}
	if storedTx.ConfirmedAt == nil {
		t.Fatal("expected ConfirmedAt to be set for confirmed tx")
	}
}

func TestBlockchainWithdrawal_NodeError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
		return domain.WithdrawalBlockchainTxResult{}, errors.New("node down")
	}

	_, err := f.ctrl.Withdrawal(context.Background(), domain.WithdrawalBlockchainTxParams{
		ToAddress: "TRECEIVER",
		Amount:    decimal.NewFromInt(1),
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestBlockchainWithdrawal_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
		return domain.WithdrawalBlockchainTxResult{
			ExternalRequestID: "ext-003",
			FromAddress:       "TFROM",
			Status:            domain.BlockchainTxStatusNew,
		}, nil
	}
	f.txs.createFunc = func(context.Context, *domain.BlockchainTx) error {
		return errors.New("db error")
	}

	_, err := f.ctrl.Withdrawal(context.Background(), domain.WithdrawalBlockchainTxParams{
		ToAddress: "TRECEIVER",
		Amount:    decimal.NewFromInt(1),
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// GetTransactionByID
// ============================================================

// ============================================================
// Withdrawal() — resend against an already-persisted request id
// ============================================================

// testOutboundWithdrawalResult is the node answer for the withdrawal built by
// sendWithdrawal below.
func testOutboundWithdrawalResult() domain.WithdrawalBlockchainTxResult {
	return domain.WithdrawalBlockchainTxResult{
		ExternalRequestID:     "ext-resend",
		FromAddress:           "TFROM",
		Status:                domain.BlockchainTxStatusNew,
		Confirmations:         0,
		RequiredConfirmations: 20,
	}
}

func sendWithdrawal(f *blockchainFixture) (domain.BlockchainTx, error) {
	return f.ctrl.Withdrawal(context.Background(), domain.WithdrawalBlockchainTxParams{
		ToAddress: "TRECEIVER",
		Amount:    decimal.NewFromInt(1),
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
}

// A withdrawal whose first attempt persisted the blockchain tx but never
// committed its own transaction stays at New, and the reconcile pass resends
// it. node-service answers idempotently with the original request id, so a
// blind insert would hit the unique index on external_request_id and the
// withdrawal would be stuck at New forever, failing identically on every tick.
func TestBlockchainWithdrawal_KnownRequestID_ReusesExistingTx(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
		return testOutboundWithdrawalResult(), nil
	}
	existing := domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(42),
		ExternalRequestID: "ext-resend",
		Type:              domain.BlockchainTxTypeOutbound,
		ToAddress:         "TRECEIVER",
		Status:            domain.BlockchainTxStatusPending,
	}
	f.txs.getByExternalRequestIDFunc = func(_ context.Context, id string) (domain.BlockchainTx, error) {
		if id != "ext-resend" {
			t.Fatalf("unexpected external request id: %s", id)
		}
		return existing, nil
	}
	f.txs.createFunc = func(context.Context, *domain.BlockchainTx) error {
		t.Fatal("Create must not run when the request id is already persisted")
		return nil
	}

	result, err := sendWithdrawal(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != existing.ID {
		t.Fatalf("expected the existing tx %d, got %d", existing.ID, result.ID)
	}
	if result.Status != domain.BlockchainTxStatusPending {
		t.Fatalf("expected the persisted status to be preserved, got %v", result.Status)
	}
}

// The lookup and the insert are not atomic, so a concurrent sender can insert
// the same request id in between. The unique index catches it and the caller
// falls back to the row that won.
func TestBlockchainWithdrawal_DuplicateOnCreate_ReusesExistingTx(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
		return testOutboundWithdrawalResult(), nil
	}
	existing := domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(43),
		ExternalRequestID: "ext-resend",
		Type:              domain.BlockchainTxTypeOutbound,
		ToAddress:         "TRECEIVER",
		Status:            domain.BlockchainTxStatusNew,
	}
	lookups := 0
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		lookups++
		if lookups == 1 {
			return domain.BlockchainTx{}, domain.ErrRecordNotFound
		}
		return existing, nil
	}
	f.txs.createFunc = func(context.Context, *domain.BlockchainTx) error {
		return domain.ErrRecordAlreadyExists
	}

	result, err := sendWithdrawal(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != existing.ID {
		t.Fatalf("expected the existing tx %d, got %d", existing.ID, result.ID)
	}
	if lookups != 2 {
		t.Fatalf("expected a second lookup after the duplicate, got %d lookups", lookups)
	}
}

// Adopting a row settles the withdrawal against the transaction that row
// describes, so a row belonging to something else must surface instead.
func TestBlockchainWithdrawal_ExistingTxNotOurs_ReturnsInternalError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		existing domain.BlockchainTx
	}{
		{
			name: "inbound row",
			existing: domain.BlockchainTx{
				ID:        domain.NewIDFromUint64(44),
				Type:      domain.BlockchainTxTypeInbound,
				ToAddress: "TRECEIVER",
			},
		},
		{
			name: "different destination",
			existing: domain.BlockchainTx{
				ID:        domain.NewIDFromUint64(45),
				Type:      domain.BlockchainTxTypeOutbound,
				ToAddress: "TSOMEONEELSE",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newBlockchainFixture()
			f.node.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
				return testOutboundWithdrawalResult(), nil
			}
			f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
				return tc.existing, nil
			}
			f.txs.createFunc = func(context.Context, *domain.BlockchainTx) error {
				t.Fatal("Create must not run for a mismatched existing row")
				return nil
			}

			_, err := sendWithdrawal(f)
			errorxtest.AssertErrorCode(t, err, appErrors.InternalServerError)
		})
	}
}

func TestBlockchainWithdrawal_LookupDBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.node.withdrawalFunc = func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
		return testOutboundWithdrawalResult(), nil
	}
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, errors.New("db down")
	}
	f.txs.createFunc = func(context.Context, *domain.BlockchainTx) error {
		t.Fatal("Create must not run when the lookup failed")
		return nil
	}

	_, err := sendWithdrawal(f)
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// ProcessTransaction() — inconclusive KYT verdict
// ============================================================

// AMLBot answers HTTP 200 with result:false on its own internal errors, which
// decodes to a non-success status. Treating that as clean is what let an
// unchecked deposit reach Confirmed and be credited.
func TestBlockchainProcessTransaction_InboundKYTInconclusive_HoldsAtPending(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		kytStatus domain.KYTStatus
	}{
		{name: "provider_error", kytStatus: domain.KYTStatusError},
		{name: "check_still_running", kytStatus: domain.KYTStatusPending},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newBlockchainFixture()
			f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
				// IsClean is true here on purpose: the provider never rendered a
				// usable verdict, so the flag carries no information.
				return domain.KYTCheckAddressResult{Status: tc.kytStatus, IsClean: true}, nil
			}
			f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
				return domain.BlockchainTx{}, domain.ErrRecordNotFound
			}
			var storedTx domain.BlockchainTx
			f.txs.createFunc = func(_ context.Context, tx *domain.BlockchainTx) error {
				tx.ID = domain.NewIDFromUint64(7)
				storedTx = *tx
				return nil
			}
			f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
				return nil
			}

			err := f.ctrl.ProcessTransaction(context.Background(), domain.ProcessBlockchainTxParams{
				ExternalRequestID:     "ext-kyt-hold",
				AssetCode:             currency.DefaultAccountAssetCode,
				Network:               domain.ChainNetworkTRX,
				Type:                  domain.BlockchainTxTypeInbound,
				FromAddress:           "TSENDER",
				ToAddress:             "TDEPOSIT",
				Amount:                decimal.NewFromInt(100),
				Status:                domain.BlockchainTxStatusConfirmed,
				Confirmations:         20,
				RequiredConfirmations: 20,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if storedTx.Status != domain.BlockchainTxStatusPending {
				t.Fatalf("expected the tx to be held at Pending, got %v", storedTx.Status)
			}
		})
	}
}

func TestBlockchainGetTransactionByID_Success(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	txID := domain.NewIDFromUint64(77)
	f.txs.getByIDFunc = func(_ context.Context, id domain.ID) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{ID: id, Status: domain.BlockchainTxStatusPending}, nil
	}

	result, err := f.ctrl.GetTransactionByID(context.Background(), txID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != txID {
		t.Fatalf("unexpected tx id: got %v, want %v", result.ID, txID)
	}
}

func TestBlockchainGetTransactionByID_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.txs.getByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, errors.New("db error")
	}

	_, err := f.ctrl.GetTransactionByID(context.Background(), domain.NewIDFromUint64(1))
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// PollTransactionStatus
// ============================================================

func TestBlockchainPollTransactionStatus_MissingTxIDUsesExternalRequestID(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	bcID := domain.NewIDFromUint64(49)
	f.txs.getByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{
			ID:                bcID,
			ExternalRequestID: "node-request-1",
			Network:           domain.ChainNetworkTRX,
		}, nil
	}
	txID := "broadcasted-hash"
	f.node.getTransactionStatusByRequestIDFunc = func(_ context.Context, network domain.ChainNetwork, requestID string) (domain.TransactionStatusResult, error) {
		if network != domain.ChainNetworkTRX {
			t.Fatalf("unexpected network: %v", network)
		}
		if requestID != "node-request-1" {
			t.Fatalf("unexpected requestID: %q", requestID)
		}
		return domain.TransactionStatusResult{
			TxID:                  &txID,
			Status:                domain.BlockchainTxStatusConfirmed,
			Confirmations:         20,
			RequiredConfirmations: 20,
		}, nil
	}

	result, err := f.ctrl.PollTransactionStatus(context.Background(), bcID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TxID == nil || *result.TxID != txID {
		t.Fatalf("expected txID %q, got %v", txID, result.TxID)
	}
	if result.Status != domain.BlockchainTxStatusConfirmed {
		t.Fatalf("unexpected status: %v", result.Status)
	}
}

func TestBlockchainPollTransactionStatus_WithTxIDUsesTxHash(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	bcID := domain.NewIDFromUint64(49)
	txHash := "existing-hash"
	f.txs.getByIDFunc = func(context.Context, domain.ID) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{
			ID:      bcID,
			TxID:    &txHash,
			Network: domain.ChainNetworkTRX,
		}, nil
	}
	f.node.getTransactionStatusFunc = func(_ context.Context, network domain.ChainNetwork, gotTxHash string) (domain.TransactionStatusResult, error) {
		if network != domain.ChainNetworkTRX {
			t.Fatalf("unexpected network: %v", network)
		}
		if gotTxHash != txHash {
			t.Fatalf("unexpected txHash: %q", gotTxHash)
		}
		return domain.TransactionStatusResult{
			Status:                domain.BlockchainTxStatusPending,
			Confirmations:         3,
			RequiredConfirmations: 20,
		}, nil
	}

	result, err := f.ctrl.PollTransactionStatus(context.Background(), bcID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != domain.BlockchainTxStatusPending {
		t.Fatalf("unexpected status: %v", result.Status)
	}
}

// ============================================================
// CreateDefaultChainConfigs
// ============================================================

func TestBlockchainCreateDefaultChainConfigs_Success(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	createCount := 0
	f.chainConfigs.createFunc = func(context.Context, *domain.ChainConfig) error {
		createCount++
		return nil
	}

	err := f.ctrl.CreateDefaultChainConfigs(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := len(domain.DefaultChainConfigs())
	if createCount != expected {
		t.Fatalf("unexpected create calls: got %d, want %d", createCount, expected)
	}
}

func TestBlockchainCreateDefaultChainConfigs_DBError_ReturnsDatabaseError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.chainConfigs.createFunc = func(context.Context, *domain.ChainConfig) error {
		return errors.New("db error")
	}

	err := f.ctrl.CreateDefaultChainConfigs(context.Background())
	errorxtest.AssertErrorCode(t, err, appErrors.DatabaseError)
}

// ============================================================
// GetFees
// ============================================================

func TestBlockchainGetFees_Success_RateFound(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	direction := "TRX_USDT"
	f.currency.getRateMapFunc = func(context.Context) (currency.RateMap, error) {
		return currency.RateMap{
			direction: {Rate: decimal.NewFromFloat(1.5)},
		}, nil
	}
	f.node.getFeesFunc = func(context.Context, domain.ChainNetwork) (domain.GetBlockchainFeeResult, error) {
		return domain.GetBlockchainFeeResult{Network: domain.ChainNetworkTRX.String()}, nil
	}

	result, err := f.ctrl.GetFees(context.Background(), domain.GetFeesParams{
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.AssetRate.Cmp(decimal.NewFromFloat(1.5)) != 0 {
		t.Fatalf("unexpected asset rate: got %s, want 1.5", result.AssetRate.String())
	}
}

func TestBlockchainGetFees_RateNotFound_ReturnsZeroRate(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.currency.getRateMapFunc = func(context.Context) (currency.RateMap, error) {
		return currency.RateMap{}, nil // empty — no rate
	}
	f.node.getFeesFunc = func(context.Context, domain.ChainNetwork) (domain.GetBlockchainFeeResult, error) {
		return domain.GetBlockchainFeeResult{}, nil
	}

	result, err := f.ctrl.GetFees(context.Background(), domain.GetFeesParams{
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.AssetRate.IsZero() {
		t.Fatalf("expected zero rate, got %s", result.AssetRate.String())
	}
}

func TestBlockchainGetFees_CurrencyError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.currency.getRateMapFunc = func(context.Context) (currency.RateMap, error) {
		return nil, errors.New("currency service down")
	}

	_, err := f.ctrl.GetFees(context.Background(), domain.GetFeesParams{
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

func TestBlockchainGetFees_NodeError_ReturnsExternalServiceError(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.currency.getRateMapFunc = func(context.Context) (currency.RateMap, error) {
		return currency.RateMap{}, nil
	}
	f.node.getFeesFunc = func(context.Context, domain.ChainNetwork) (domain.GetBlockchainFeeResult, error) {
		return domain.GetBlockchainFeeResult{}, errors.New("node down")
	}

	_, err := f.ctrl.GetFees(context.Background(), domain.GetFeesParams{
		AssetCode: currency.DefaultAccountAssetCode,
		Network:   domain.ChainNetworkTRX,
	})
	errorxtest.AssertErrorCode(t, err, appErrors.ExternalServiceError)
}

// testStoredTxIdentity fills the fields that identify which on-chain
// transaction a row describes. ProcessTransaction now refuses a callback whose
// identity disagrees with the stored row, so a stub that leaves them at their
// zero values no longer stands in for a real row — Type would read as "unknown"
// and the amount as zero.
func testStoredTxIdentity(tx domain.BlockchainTx, txType domain.BlockchainTxType, from, to string) domain.BlockchainTx {
	tx.AssetCode = currency.DefaultAccountAssetCode
	tx.Network = domain.ChainNetworkTRX
	tx.Type = txType
	tx.FromAddress = from
	tx.ToAddress = to
	tx.Amount = decimal.NewFromInt(1).Shift(currency.DefaultAssetScale)
	return tx
}

// ============================================================
// Fixture and helpers
// ============================================================

type blockchainFixture struct {
	ctrl             *BlockchainCtrl
	txManager        *dbtest.ObservableTxManager
	processingLocker *mockLocker
	event            *mockEventController
	cache            *mockCacheController
	chainConfigs     *mockChainConfigRepo
	addresses        *mockAddressAssignmentRepo
	txs              *mockBlockchainTxRepo
	node             *mockNodeService
	kyt              *mockKYTService
	currency         *mockCurrencyService
}

func newBlockchainFixture() *blockchainFixture {
	txManager := &dbtest.ObservableTxManager{}
	locker := &mockLocker{}
	f := &blockchainFixture{
		txManager:        txManager,
		processingLocker: locker,
		event:            &mockEventController{},
		cache:            &mockCacheController{},
		chainConfigs:     &mockChainConfigRepo{},
		addresses:        &mockAddressAssignmentRepo{},
		txs:              &mockBlockchainTxRepo{},
		node:             &mockNodeService{},
		kyt:              &mockKYTService{},
		currency:         &mockCurrencyService{},
	}

	f.cache.getChainConfigFunc = func(context.Context, currency.AssetCode, domain.ChainNetwork) (domain.ChainConfig, error) {
		return testBlockchainChainConfig(), nil
	}
	f.kyt.checkAddressFunc = func(context.Context, string, string) (domain.KYTCheckAddressResult, error) {
		return domain.KYTCheckAddressResult{Status: domain.KYTStatusSuccess, IsClean: true}, nil
	}
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return domain.BlockchainTx{}, domain.ErrRecordNotFound
	}
	f.txs.withTxFromContextFunc = func(_ context.Context) domain.BlockchainTxRepository {
		return f.txs
	}

	f.ctrl = NewBlockchainController(
		f.event,
		f.cache,
		f.addresses,
		f.txs,
		f.chainConfigs,
		f.node,
		f.kyt,
		f.currency,
		locker,
		txManager,
	)

	return f
}

func testBlockchainChainConfig() domain.ChainConfig {
	return domain.ChainConfig{
		ID:               domain.NewIDFromUint64(1),
		AssetCode:        currency.DefaultAccountAssetCode,
		Network:          domain.ChainNetworkTRX,
		Decimals:         6,
		MinConfirmations: 20,
		DustThreshold:    decimal.Zero,
		Status:           domain.ChainConfigStatusAll,
	}
}

// ============================================================
// New mocks (node, kyt, currency, chainConfigRepo, blockchainTxRepo)
// ============================================================

type mockNodeService struct {
	generateAddressFunc                 func(context.Context, domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error)
	withdrawalFunc                      func(context.Context, domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error)
	getFeesFunc                         func(context.Context, domain.ChainNetwork) (domain.GetBlockchainFeeResult, error)
	getTransactionStatusFunc            func(context.Context, domain.ChainNetwork, string) (domain.TransactionStatusResult, error)
	getTransactionStatusByRequestIDFunc func(context.Context, domain.ChainNetwork, string) (domain.TransactionStatusResult, error)
}

func (m *mockNodeService) GenerateAddress(ctx context.Context, params domain.GenerateBlockchainAddressParams) (domain.GenerateBlockchainAddressResult, error) {
	if m.generateAddressFunc != nil {
		return m.generateAddressFunc(ctx, params)
	}
	panic("unexpected call: NodeService.GenerateAddress")
}

func (m *mockNodeService) Withdrawal(ctx context.Context, params domain.WithdrawalBlockchainTxParams) (domain.WithdrawalBlockchainTxResult, error) {
	if m.withdrawalFunc != nil {
		return m.withdrawalFunc(ctx, params)
	}
	panic("unexpected call: NodeService.Withdrawal")
}

func (m *mockNodeService) GetFees(ctx context.Context, network domain.ChainNetwork) (domain.GetBlockchainFeeResult, error) {
	if m.getFeesFunc != nil {
		return m.getFeesFunc(ctx, network)
	}
	panic("unexpected call: NodeService.GetFees")
}

func (m *mockNodeService) GetTransactionStatus(ctx context.Context, network domain.ChainNetwork, txHash string) (domain.TransactionStatusResult, error) {
	if m.getTransactionStatusFunc != nil {
		return m.getTransactionStatusFunc(ctx, network, txHash)
	}
	panic("unexpected call: NodeService.GetTransactionStatus")
}

func (m *mockNodeService) GetTransactionStatusByRequestID(ctx context.Context, network domain.ChainNetwork, requestID string) (domain.TransactionStatusResult, error) {
	if m.getTransactionStatusByRequestIDFunc != nil {
		return m.getTransactionStatusByRequestIDFunc(ctx, network, requestID)
	}
	panic("unexpected call: NodeService.GetTransactionStatusByRequestID")
}

type mockCurrencyService struct {
	getRateMapFunc func(context.Context) (currency.RateMap, error)
}

func (m *mockCurrencyService) GetRateMap(ctx context.Context) (currency.RateMap, error) {
	if m.getRateMapFunc != nil {
		return m.getRateMapFunc(ctx)
	}
	panic("unexpected call: CurrencyService.GetRateMap")
}

func (m *mockCurrencyService) GetAssetMap(context.Context) (currency.AssetMap, error) {
	panic("unexpected call: CurrencyService.GetAssetMap")
}

type mockChainConfigRepo struct {
	createFunc func(context.Context, *domain.ChainConfig) error
	getAllFunc func(context.Context) (domain.ChainConfigs, error)
}

func (m *mockChainConfigRepo) Create(ctx context.Context, config *domain.ChainConfig) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, config)
	}
	panic("unexpected call: ChainConfigRepo.Create")
}

func (m *mockChainConfigRepo) GetAll(ctx context.Context) (domain.ChainConfigs, error) {
	if m.getAllFunc != nil {
		return m.getAllFunc(ctx)
	}
	panic("unexpected call: ChainConfigRepo.GetAll")
}

type mockBlockchainTxRepo struct {
	createFunc                       func(context.Context, *domain.BlockchainTx) error
	getByParamsFunc                  func(context.Context, domain.GetBlockchainTxParams) (domain.BlockchainTx, error)
	getByExternalRequestIDFunc       func(context.Context, string) (domain.BlockchainTx, error)
	getByIDFunc                      func(context.Context, domain.ID) (domain.BlockchainTx, error)
	updateStatusFunc                 func(context.Context, domain.ID, domain.BlockchainTxStatus, uint8, *string) error
	findStuckInboundForReconcileFunc func(context.Context, time.Duration, int) ([]domain.BlockchainTx, error)
	withTxFromContextFunc            func(context.Context) domain.BlockchainTxRepository
}

func (m *mockBlockchainTxRepo) Create(ctx context.Context, tx *domain.BlockchainTx) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, tx)
	}
	panic("unexpected call: BlockchainTxRepo.Create")
}

func (m *mockBlockchainTxRepo) GetByParams(ctx context.Context, params domain.GetBlockchainTxParams) (domain.BlockchainTx, error) {
	if m.getByParamsFunc != nil {
		return m.getByParamsFunc(ctx, params)
	}
	panic("unexpected call: BlockchainTxRepo.GetByParams")
}

func (m *mockBlockchainTxRepo) GetByExternalRequestID(ctx context.Context, id string) (domain.BlockchainTx, error) {
	if m.getByExternalRequestIDFunc != nil {
		return m.getByExternalRequestIDFunc(ctx, id)
	}
	panic("unexpected call: BlockchainTxRepo.GetByExternalRequestID")
}

func (m *mockBlockchainTxRepo) GetByID(ctx context.Context, id domain.ID) (domain.BlockchainTx, error) {
	if m.getByIDFunc != nil {
		return m.getByIDFunc(ctx, id)
	}
	panic("unexpected call: BlockchainTxRepo.GetByID")
}

func (m *mockBlockchainTxRepo) UpdateStatus(ctx context.Context, id domain.ID, status domain.BlockchainTxStatus, confirmations uint8, txID *string) error {
	if m.updateStatusFunc != nil {
		return m.updateStatusFunc(ctx, id, status, confirmations, txID)
	}
	panic("unexpected call: BlockchainTxRepo.UpdateStatus")
}

func (m *mockBlockchainTxRepo) FindStuckInboundForReconcile(ctx context.Context, olderThan time.Duration, limit int) ([]domain.BlockchainTx, error) {
	if m.findStuckInboundForReconcileFunc != nil {
		return m.findStuckInboundForReconcileFunc(ctx, olderThan, limit)
	}
	panic("unexpected call: BlockchainTxRepo.FindStuckInboundForReconcile")
}

func (m *mockBlockchainTxRepo) WithTxFromContext(ctx context.Context) domain.BlockchainTxRepository {
	if m.withTxFromContextFunc != nil {
		return m.withTxFromContextFunc(ctx)
	}
	return m
}

// ============================================================
// Stale callbacks and identity — ProcessTransaction guards
// ============================================================

// storedConfirmedInboundTx is a settled inbound transaction: the state a stale
// callback must not be able to undo.
func storedConfirmedInboundTx() domain.BlockchainTx {
	return testStoredTxIdentity(domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(5),
		ExternalRequestID: "req-settled",
		Status:            domain.BlockchainTxStatusConfirmed,
		Confirmations:     20,
	}, domain.BlockchainTxTypeInbound, "TSENDER", "TRECEIVER")
}

// staleCallbackParams replays what node-service sent before the transaction
// confirmed: no confirmations yet, status new.
func staleCallbackParams() domain.ProcessBlockchainTxParams {
	return domain.ProcessBlockchainTxParams{
		ExternalRequestID:     "req-settled",
		AssetCode:             currency.DefaultAccountAssetCode,
		Network:               domain.ChainNetworkTRX,
		Type:                  domain.BlockchainTxTypeInbound,
		FromAddress:           "TSENDER",
		ToAddress:             "TRECEIVER",
		Amount:                decimal.NewFromInt(1),
		Confirmations:         0,
		RequiredConfirmations: 20,
		Status:                domain.BlockchainTxStatusNew,
	}
}

// TestBlockchainProcessTransaction_StaleCallback_DoesNotUndoTerminalStatus is
// the regression. Queue and callback delivery is at-least-once and unordered, so
// the pre-confirmation callback can arrive after the confirming one. It used to
// rewrite the row back to new/0 and publish a lifecycle event for it, losing the
// record that the transaction ever confirmed.
func TestBlockchainProcessTransaction_StaleCallback_DoesNotUndoTerminalStatus(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return storedConfirmedInboundTx(), nil
	}
	f.txs.updateStatusFunc = func(_ context.Context, _ domain.ID, s domain.BlockchainTxStatus, c uint8, _ *string) error {
		t.Errorf("stale callback rewrote the row to status=%v confirmations=%d", s, c)
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		t.Error("stale callback published a lifecycle event")
		return nil
	}

	if err := f.ctrl.ProcessTransaction(context.Background(), staleCallbackParams()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBlockchainProcessTransaction_ReorgToFailed_IsRecorded keeps the guard from
// being a blanket freeze: terminal to terminal must still land, because a reorg
// turning a confirmed transaction into a failed one is real and an operator
// needs it on the row.
func TestBlockchainProcessTransaction_ReorgToFailed_IsRecorded(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return storedConfirmedInboundTx(), nil
	}
	var got domain.BlockchainTxStatus
	f.txs.updateStatusFunc = func(_ context.Context, _ domain.ID, s domain.BlockchainTxStatus, _ uint8, _ *string) error {
		got = s
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		return nil
	}

	params := staleCallbackParams()
	params.Status = domain.BlockchainTxStatusFailed

	if err := f.ctrl.ProcessTransaction(context.Background(), params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != domain.BlockchainTxStatusFailed {
		t.Fatalf("reorg was not recorded: status = %v, want failed", got)
	}
}

// TestBlockchainProcessTransaction_StaleConfirmations_DoNotDecrease covers the
// count on its own: a reordered callback carrying fewer confirmations must not
// lower what the row already recorded.
func TestBlockchainProcessTransaction_StaleConfirmations_DoNotDecrease(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	stored := testStoredTxIdentity(domain.BlockchainTx{
		ID:                domain.NewIDFromUint64(6),
		ExternalRequestID: "req-partial",
		Status:            domain.BlockchainTxStatusPending,
		Confirmations:     12,
	}, domain.BlockchainTxTypeInbound, "TSENDER", "TRECEIVER")

	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return stored, nil
	}
	f.txs.updateStatusFunc = func(_ context.Context, _ domain.ID, _ domain.BlockchainTxStatus, c uint8, _ *string) error {
		t.Errorf("confirmations were lowered to %d from the stored 12", c)
		return nil
	}
	f.event.publishBlockchainTransactionUpdateFn = func(context.Context, domain.BlockchainTransactionUpdateEvent) error {
		t.Error("a stale confirmation count published a lifecycle event")
		return nil
	}

	params := staleCallbackParams()
	params.ExternalRequestID = "req-partial"
	params.Confirmations = 4

	if err := f.ctrl.ProcessTransaction(context.Background(), params); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestBlockchainProcessTransaction_IdentityMismatch_IsRefused covers the second
// guard. UpdateStatus never writes these fields, so a callback describing a
// different transfer under a known request id used to have its status applied
// to the stored row regardless.
func TestBlockchainProcessTransaction_IdentityMismatch_IsRefused(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*domain.ProcessBlockchainTxParams)
	}{
		{"amount", func(p *domain.ProcessBlockchainTxParams) { p.Amount = decimal.NewFromInt(999) }},
		{"sender", func(p *domain.ProcessBlockchainTxParams) { p.FromAddress = "TSOMEONE-ELSE" }},
		{"recipient", func(p *domain.ProcessBlockchainTxParams) { p.ToAddress = "TSOMEWHERE-ELSE" }},
		{"direction", func(p *domain.ProcessBlockchainTxParams) { p.Type = domain.BlockchainTxTypeOutbound }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newBlockchainFixture()
			f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
				return storedConfirmedInboundTx(), nil
			}
			f.txs.updateStatusFunc = func(context.Context, domain.ID, domain.BlockchainTxStatus, uint8, *string) error {
				t.Error("a mismatched callback was applied to the stored row")
				return nil
			}

			params := staleCallbackParams()
			params.Status = domain.BlockchainTxStatusConfirmed
			params.Confirmations = 20
			tc.mutate(&params)

			err := f.ctrl.ProcessTransaction(context.Background(), params)
			errorxtest.AssertErrorCode(t, err, appErrors.InvalidRequestError)
		})
	}
}

// TestBlockchainProcessTransaction_EquivalentAmountIsNotAMismatch pins that the
// identity check compares amounts by value: decimal carries its exponent, so
// 1.50 and 1.5 are different structs and the same money.
func TestBlockchainProcessTransaction_EquivalentAmountIsNotAMismatch(t *testing.T) {
	t.Parallel()

	f := newBlockchainFixture()
	stored := storedConfirmedInboundTx()
	stored.Amount = decimal.RequireFromString("100000000.00")
	f.txs.getByExternalRequestIDFunc = func(context.Context, string) (domain.BlockchainTx, error) {
		return stored, nil
	}

	params := staleCallbackParams()
	params.Status = domain.BlockchainTxStatusConfirmed
	params.Confirmations = 20

	if err := f.ctrl.ProcessTransaction(context.Background(), params); err != nil {
		t.Fatalf("equivalent amount was treated as a mismatch: %v", err)
	}
}
