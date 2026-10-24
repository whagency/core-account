package account

import (
	"context"
	"fmt"
	"strings"

	"account-service/internal/domain"
	"account-service/internal/transport/grpc/pb/accountv1"

	"github.com/shopspring/decimal"
	commonDecode "github.com/whagency/core-common/decode"
	appErrors "github.com/whagency/core-common/errorsx"
	commonGRPC "github.com/whagency/core-common/grpc"
	"github.com/whagency/core-common/logger"
	"github.com/whagency/core-common/services/currency"
	commonTools "github.com/whagency/core-common/tools"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service exposes the domain account controller over the gRPC transport, providing
// request validation, transport level observability and error mapping.
type Service struct {
	accountv1.UnimplementedAccountServiceServer

	transactionCtrl domain.TransactionController
	depositCtrl     domain.DepositController
}

// NewService constructs a gRPC handler facade for the account domain controller.
// It is responsible for translating protobuf requests into domain level DTOs and
// translating the domain response and errors back into gRPC friendly structures.
func NewService(
	transactionCtrl domain.TransactionController,
	depositCtrl domain.DepositController,
) *Service {
	return &Service{
		transactionCtrl: transactionCtrl,
		depositCtrl:     depositCtrl,
	}
}

// MakeTransaction orchestrates balance mutations for a business transaction.
//
// The handler accepts checkout payments and converts them into domain.MakeTxParams
// so that the domain layer can apply the
// appropriate journal specification (legs, fees and validations). In addition to the
// business behaviour it also covers transport level responsibilities:
//   - validating the gRPC payload, including presence checks, enum defaults and
//     canonical decimal formatting for monetary fields;
//   - translating optional transport level fields (to_user_id, fee,
//     idempotency_key, description) into their domain equivalents;
//   - enforcing consistent error semantics by mapping domain specific
//     ErrorResponse instances into gRPC status codes while preserving the
//     textual message for clients;
//   - propagating idempotent responses when the controller returns a previously
//     processed journal entry.
//
// Possible responses:
//   - codes.InvalidArgument when the payload is malformed or required business
//     identifiers are missing;
//   - codes.NotFound when referenced domain entities (users, assets) do not
//     exist;
//   - codes.FailedPrecondition for optimistic concurrency / duplicate
//     conflicts reported by the domain layer;
//   - codes.Internal for unexpected failures.
//
// Successful calls return the created journal identifier together with its
// resulting status so that clients can correlate with asynchronous processing
// (for example, notification or settlement queues).
func (s *Service) MakeTransaction(ctx context.Context, req *accountv1.MakeTransactionRequest) (*accountv1.MakeTransactionResponse, error) {
	params, err := decodeMakeTransactionRequest(req)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("[GRPC][AccountService:MakeTransaction]%s", err))
	}

	res, err := s.transactionCtrl.MakeTransaction(ctx, params)
	if err != nil {
		logger.LogCtx(ctx).Errorf("[GRPC][AccountService:MakeTransaction]%s", err)

		if resp, ok := appErrors.AsErrorResponse(err); ok {
			if mappedErr := mapMakeTransactionErrorResponse(resp); mappedErr != nil {
				return nil, mappedErr
			}
			return nil, status.Error(commonGRPC.MapHTTPStatusToCode(resp.HTTPStatus), fmt.Sprintf("[GRPC][AccountService:MakeTransaction][err: %s]", resp.Message))
		}

		return nil, status.Error(codes.Internal, "[GRPC][AccountService:MakeTransaction][err: internal error]")
	}

	return &accountv1.MakeTransactionResponse{
		JournalId: res.JournalID.Uint64(),
		Status:    accountv1.JournalStatus(res.Status),
	}, nil
}

// AutomaticManualApproveDeposits triggers the automatic approval of pending deposits for a user.
func (s *Service) AutomaticManualApproveDeposits(ctx context.Context, req *accountv1.AutomaticManualApproveDepositsRequest) (*accountv1.AutomaticManualApproveDepositsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "[GRPC][AccountService:AutomaticManualApproveDeposits][err: request is required]")
	}

	if req.GetUserId() == 0 {
		return nil, status.Error(codes.InvalidArgument, "[GRPC][AccountService:AutomaticManualApproveDeposits][err: user_id is required]")
	}

	if err := s.depositCtrl.AutomaticManualApprove(ctx, domain.NewIDFromUint64(req.GetUserId())); err != nil {
		logger.LogCtx(ctx).Errorf("[GRPC][AccountService:AutomaticManualApproveDeposits]%s", err)

		if resp, ok := appErrors.AsErrorResponse(err); ok {
			return nil, status.Error(commonGRPC.MapHTTPStatusToCode(resp.HTTPStatus), fmt.Sprintf("[GRPC][AccountService:AutomaticManualApproveDeposits][err: %s]", resp.Message))
		}

		return nil, status.Error(codes.Internal, "[GRPC][AccountService:AutomaticManualApproveDeposits][err: internal error]")
	}

	return &accountv1.AutomaticManualApproveDepositsResponse{}, nil
}

// GetJournalByIdempotencyKey looks up a journal entry without creating one.
//
// Unlike MakeTransaction this call has no side effects, which is the whole point:
// it exists so compensation and reconciliation flows can ask whether a posting
// happened without risking creating it.
//
// A reversed journal is reported as not found, because RollbackTransaction frees
// the original key for a re-attempt. For a caller asking "is there a live posting
// to compensate?" that is the correct answer.
func (s *Service) GetJournalByIdempotencyKey(
	ctx context.Context,
	req *accountv1.GetJournalByIdempotencyKeyRequest,
) (*accountv1.GetJournalByIdempotencyKeyResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "[GRPC][AccountService:GetJournalByIdempotencyKey][err: request is required]")
	}

	bizType := req.GetType()
	if bizType == accountv1.JournalBizType_JOURNAL_BIZ_TYPE_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "[GRPC][AccountService:GetJournalByIdempotencyKey][err: type is required]")
	}
	entryType, ok := mapJournalBizTypeToDomain(bizType)
	if !ok {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("[GRPC][AccountService:GetJournalByIdempotencyKey][err: unsupported type: %d]", bizType))
	}

	key := strings.TrimSpace(req.GetIdempotencyKey())
	if key == "" {
		return nil, status.Error(codes.InvalidArgument, "[GRPC][AccountService:GetJournalByIdempotencyKey][err: idempotency_key is required]")
	}

	res, err := s.transactionCtrl.FindJournalByIdempotencyKey(ctx, domain.FindJournalParams{
		Type:           entryType,
		IdempotencyKey: key,
	})
	if err != nil {
		logger.LogCtx(ctx).Errorf("[GRPC][AccountService:GetJournalByIdempotencyKey]%s", err)

		if resp, ok := appErrors.AsErrorResponse(err); ok {
			return nil, status.Error(commonGRPC.MapHTTPStatusToCode(resp.HTTPStatus), fmt.Sprintf("[GRPC][AccountService:GetJournalByIdempotencyKey][err: %s]", resp.Message))
		}

		return nil, status.Error(codes.Internal, "[GRPC][AccountService:GetJournalByIdempotencyKey][err: internal error]")
	}

	return &accountv1.GetJournalByIdempotencyKeyResponse{
		Found:     res.Found,
		JournalId: res.JournalID.Uint64(),
		Status:    accountv1.JournalStatus(res.Status),
	}, nil
}

// decodeMakeTransactionRequest normalises the protobuf payload by performing
// presence validation, trimming transport strings and parsing decimals so the
// domain layer receives a consistent command.
func decodeMakeTransactionRequest(req *accountv1.MakeTransactionRequest) (domain.MakeTxParams, error) {
	if req == nil {
		return domain.MakeTxParams{}, fmt.Errorf("[decodeMakeTransactionRequest][err: request is required]")
	}

	normalized := makeTransactionRequest{payload: req}
	return normalized.decode()
}

type makeTransactionRequest struct {
	payload *accountv1.MakeTransactionRequest
}

// decode performs presence validation, trimming and parsing of the transport
// payload, returning a domain friendly DTO or a gRPC status error when the
// request is malformed.
func (r makeTransactionRequest) decode() (domain.MakeTxParams, error) {
	bizType := r.payload.GetType()
	if bizType == accountv1.JournalBizType_JOURNAL_BIZ_TYPE_UNSPECIFIED {
		return domain.MakeTxParams{}, fmt.Errorf("[makeTransactionRequest:decode][err: type is required]")
	}
	entryType, ok := mapJournalBizTypeToDomain(bizType)
	if !ok {
		return domain.MakeTxParams{}, fmt.Errorf("[makeTransactionRequest:decode][err: unsupported type: %d]", bizType)
	}

	if err := commonDecode.RequirePositiveUint64("user_id", r.payload.GetUserId()); err != nil {
		return domain.MakeTxParams{}, fmt.Errorf("[makeTransactionRequest:decode]%w", err)
	}

	var toUserID *domain.ID
	if r.payload.ToUserId != nil {
		if err := commonDecode.RequirePositiveUint64("to_user_id", r.payload.GetToUserId()); err != nil {
			return domain.MakeTxParams{}, fmt.Errorf("[makeTransactionRequest:decode]%w", err)
		}
		id := domain.NewIDFromUint64(r.payload.GetToUserId())
		toUserID = &id
	}

	assetCodeStr, err := commonDecode.ParseRequiredUpperString("asset_code", r.payload.GetAssetCode())
	if err != nil {
		return domain.MakeTxParams{}, fmt.Errorf("[makeTransactionRequest:decode]%w", err)
	}
	assetCode := currency.NewAssetCode(assetCodeStr)

	amount, err := commonDecode.ParseRequiredDecimal("amount", r.payload.GetAmount())
	if err != nil {
		return domain.MakeTxParams{}, err
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return domain.MakeTxParams{}, fmt.Errorf("[makeTransactionRequest:decode][err: amount must be greater than zero]")
	}
	amount = amount.Shift(currency.DefaultAssetScale)

	fee := decimal.Zero
	if r.payload.Fee != nil {
		fee, err = commonDecode.ParseRequiredDecimal("fee", r.payload.GetFee())
		if err != nil {
			return domain.MakeTxParams{}, err
		}

		if fee.LessThan(decimal.Zero) {
			return domain.MakeTxParams{}, fmt.Errorf("[makeTransactionRequest:decode][err: fee cannot be negative]")
		}
		fee = fee.Shift(currency.DefaultAssetScale)
	}

	params := domain.MakeTxParams{
		Type:      entryType,
		AssetCode: assetCode,
		UserID:    domain.NewIDFromUint64(r.payload.GetUserId()),
		ToUserID:  toUserID,
		Amount:    amount,
		Fee:       fee,
	}

	if key := commonTools.TrimmedOrNil(r.payload.IdempotencyKey); key != nil {
		params.IdempotencyKey = key
	}

	if description := commonTools.TrimmedOrNil(r.payload.Description); description != nil {
		params.Description = description
	}

	return params, nil
}

// mapJournalBizTypeToDomain maps transport-level business operation types to
// the corresponding domain journal type.
func mapJournalBizTypeToDomain(bizType accountv1.JournalBizType) (domain.JournalBizType, bool) {
	switch bizType {
	case accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT:
		return domain.JournalBizTypeCheckoutPayment, true
	case accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT_HOLD:
		return domain.JournalBizTypeCheckoutPaymentHold, true
	case accountv1.JournalBizType_JOURNAL_BIZ_TYPE_CHECKOUT_PAYMENT_HOLD_RELEASE:
		return domain.JournalBizTypeCheckoutPaymentHoldRelease, true
	default:
		return 0, false
	}
}
