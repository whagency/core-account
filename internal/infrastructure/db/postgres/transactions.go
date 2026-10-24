package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"account-service/internal/domain"
	"account-service/tools"

	"github.com/shopspring/decimal"
	"github.com/whagency/core-common/db"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
	"gorm.io/gorm"
)

type transactionSQL struct {
	ID        uint64          `gorm:"column:id"`
	JournalID uint64          `gorm:"column:journal_id"`
	AccountID uint64          `gorm:"column:account_id"`
	AssetCode string          `gorm:"column:asset_code"`
	Type      uint8           `gorm:"column:type"`
	Amount    decimal.Decimal `gorm:"column:amount"`
	CreatedAt time.Time       `gorm:"column:created_at"`
}

// decode converts transactionSQL to domain.Transaction
func (t transactionSQL) decode() domain.Transaction {
	return domain.Transaction{
		ID:        domain.NewIDFromUint64(t.ID),
		JournalID: domain.NewIDFromUint64(t.JournalID),
		AccountID: domain.NewIDFromUint64(t.AccountID),
		Type:      domain.NewTransactionType(t.Type),
		AssetCode: currency.NewAssetCode(t.AssetCode),
		Amount:    t.Amount,
		CreatedAt: t.CreatedAt,
	}
}

type transactionDetailsSQL struct {
	ID        uint64          `gorm:"column:id"`
	OwnerID   uint64          `gorm:"column:owner_id"`
	Type      uint8           `gorm:"column:type"`
	AssetCode string          `gorm:"column:asset_code"`
	Amount    decimal.Decimal `gorm:"column:amount"`
	Status    uint8           `gorm:"column:status"`
	BizType   uint8           `gorm:"column:biz_type"`
	AcctType  uint8           `gorm:"column:acct_type"`
	CreatedAt time.Time       `gorm:"column:created_at"`
}

// decode converts transactionDetailsSQL to domain.TransactionDetails
func (t transactionDetailsSQL) decode() domain.TransactionDetails {
	return domain.TransactionDetails{
		ID:        domain.NewIDFromUint64(t.ID),
		OwnerID:   domain.NewIDFromUint64(t.OwnerID),
		Type:      domain.NewTransactionType(t.Type),
		BizType:   domain.NewJournalBizType(t.BizType),
		AcctType:  domain.NewAccountType(t.AcctType),
		AssetCode: currency.NewAssetCode(t.AssetCode),
		Amount:    t.Amount,
		Status:    domain.NewJournalStatus(t.Status),
		CreatedAt: t.CreatedAt,
	}
}

func (transactionSQL) TableName() string { return "transactions" }

type TransactionRepo struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) *TransactionRepo {
	return &TransactionRepo{db: db}
}

// WithTxFromContext checks if there's an active transaction in the context and uses it for subsequent DB operations.
func (r TransactionRepo) WithTxFromContext(ctx context.Context) domain.TransactionRepository {
	if tx := db.GetTxFromContext(ctx); tx != nil {
		r.db = tx
	}
	return r
}

// CreateBatch creates multiple transaction records in a single batch operation.
func (r TransactionRepo) CreateBatch(ctx context.Context, postings []domain.Transaction) error {
	logger.LogCtx(ctx).Debugf("[TransactionRepo:CreateBatch][postings: %+v]", postings)
	defer tools.EndDBRequestTimer(time.Now())

	if err := r.db.WithContext(ctx).Create(encodeTransactionsForCreate(postings)).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return fmt.Errorf("[TransactionRepo:CreateBatch][err: %w]", err)
	}

	return nil
}

// TransactionList returns a filtered transaction list for the user.
func (r TransactionRepo) TransactionList(ctx context.Context, userID domain.ID, filter domain.TransactionFilter) ([]domain.TransactionDetails, int64, error) {
	logger.LogCtx(ctx).Debugf("[TransactionRepo:TransactionList][userID: %d][filter: %+v]", userID, filter)
	defer tools.EndDBRequestTimer(time.Now())
	var (
		rows  []transactionDetailsSQL
		total int64
	)

	// No DISTINCT: t.id is the primary key, and both joins are on primary keys
	// (t.account_id -> a.id, t.journal_id -> e.id), so a posting cannot be
	// multiplied into several rows. Deduplicating anyway cost a sort on every
	// list request and on its COUNT.
	query := r.db.WithContext(ctx).
		Select("t.id", "t.asset_code", "t.type", "t.amount", "t.created_at", "a.owner_id", "a.acct_type", "e.status", "e.biz_type").
		Table("transactions AS t").
		Where("a.owner_id = ?", userID.Uint64()).
		Joins("LEFT JOIN accounts AS a ON t.account_id = a.id").
		Joins("LEFT JOIN journal_entries AS e ON t.journal_id = e.id").
		Order("t.id DESC")

	if filter.AssetCode != nil {
		query = query.Where("t.asset_code = ?", strings.ToUpper(*filter.AssetCode))
	}

	if filter.Status != nil {
		query = query.Where("e.status = ?", filter.Status.Uint8())
	}

	if filter.Type != nil {
		query = query.Where("t.type = ?", filter.Type.Uint8())
	}

	if filter.EntryType != nil {
		query = query.Where("e.biz_type = ?", filter.EntryType.Uint8())
	}

	if filter.AccountType != nil {
		query = query.Where("a.acct_type = ?", filter.AccountType.Uint8())
	}

	countQuery := query.Session(&gorm.Session{})
	if err := countQuery.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("[TransactionRepo:TransactionList][err: %w]", err)
	}

	query = query.
		Limit(filter.Limit).
		Offset(filter.Offset)

	if err := query.Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, 0, fmt.Errorf("[TransactionRepo:TransactionList][err: %w]", err)
	}

	transactions := make([]domain.TransactionDetails, 0, len(rows))
	for _, row := range rows {
		transactions = append(transactions, row.decode())
	}

	return transactions, total, nil
}

// GetByJournalID retrieves transactions for the given journal ID.
func (r TransactionRepo) GetByJournalID(ctx context.Context, journalID domain.ID) ([]domain.Transaction, error) {
	logger.LogCtx(ctx).Debugf("[TransactionRepo:GetByJournalID][journalID: %d]", journalID)
	defer tools.EndDBRequestTimer(time.Now())

	var rows []transactionSQL
	if err := r.db.WithContext(ctx).
		Where("journal_id = ?", journalID.Uint64()).
		Find(&rows).Error; err != nil {
		tools.TrackMetricDBErrorAdd()
		return nil, fmt.Errorf("[TransactionRepo:GetByJournalID][err: %w]", err)
	}

	transactions := make([]domain.Transaction, 0, len(rows))
	for _, row := range rows {
		transactions = append(transactions, row.decode())
	}

	return transactions, nil
}

// encodeTransactionsForCreate converts []domain.Transaction to []transactionSQL for batch insertion
func encodeTransactionsForCreate(transactions []domain.Transaction) []transactionSQL {
	rows := make([]transactionSQL, len(transactions))
	now := time.Now().UTC()
	for i := range transactions {
		rows[i] = transactionSQL{
			JournalID: transactions[i].JournalID.Uint64(),
			AccountID: transactions[i].AccountID.Uint64(),
			AssetCode: transactions[i].AssetCode.String(),
			Type:      transactions[i].Type.Uint8(),
			Amount:    transactions[i].Amount,
			CreatedAt: now,
		}
	}
	return rows
}
