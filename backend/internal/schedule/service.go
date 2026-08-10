package schedule

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

type ServiceOptions struct {
	Clock       func() time.Time
	IDGenerator func() (string, error)
}

type Service struct {
	repository  Repository
	clock       func() time.Time
	idGenerator func() (string, error)
}

func NewService(repository Repository, options ServiceOptions) (*Service, error) {
	if repository == nil {
		return nil, errors.New("invalid schedule service configuration")
	}
	if options.Clock == nil {
		options.Clock = time.Now
	}
	if options.IDGenerator == nil {
		options.IDGenerator = generateID
	}
	return &Service{repository: repository, clock: options.Clock, idGenerator: options.IDGenerator}, nil
}

func (s *Service) CreateObligation(
	ctx context.Context, ownerID, name string, amount money.Money, recurrence domainschedule.Recurrence,
	startDate financialdate.Date, endDate *financialdate.Date, key applicationledger.IdempotencyKey,
) (domainschedule.Obligation, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.Obligation{}, err
	}
	id, err := s.idGenerator()
	if err != nil {
		return domainschedule.Obligation{}, err
	}
	created, err := domainschedule.NewObligation(id, ownerID, name, amount, recurrence, startDate, endDate, s.clock().UTC())
	if err != nil {
		return domainschedule.Obligation{}, err
	}
	return s.repository.CreateObligation(ctx, created, mutation(key,
		"v1", "create_obligation", ownerID, created.DisplayName(), strconv.FormatInt(amount.MinorUnits(), 10),
		amount.Currency().Code(), recurrence.String(), startDate.String(), optionalDate(endDate),
	))
}

func (s *Service) GetObligation(ctx context.Context, ownerID, obligationID string) (domainschedule.Obligation, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.Obligation{}, err
	}
	return s.repository.GetObligation(ctx, ownerID, obligationID)
}

func (s *Service) ListObligations(ctx context.Context, ownerID string) ([]domainschedule.Obligation, error) {
	if err := validateOwner(ownerID); err != nil {
		return nil, err
	}
	return s.repository.ListObligations(ctx, ownerID)
}

func (s *Service) ArchiveObligation(
	ctx context.Context, ownerID, obligationID string, inactiveFrom financialdate.Date, key applicationledger.IdempotencyKey,
) (domainschedule.Obligation, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.Obligation{}, err
	}
	return s.repository.ArchiveObligation(
		ctx, ownerID, obligationID, inactiveFrom, s.clock().UTC(),
		mutation(key, "v2", "archive_obligation", ownerID, obligationID, inactiveFrom.String()),
	)
}

func (s *Service) ExpandObligation(ctx context.Context, ownerID, obligationID string, from, to financialdate.Date) ([]domainschedule.ScheduledCashFlow, error) {
	obligation, err := s.GetObligation(ctx, ownerID, obligationID)
	if err != nil {
		return nil, err
	}
	return domainschedule.ExpandObligation(obligation, from, to)
}

func (s *Service) CreateManualScheduledFlow(
	ctx context.Context, ownerID string, amount money.Money, direction domainschedule.Direction,
	date financialdate.Date, sourceKind domainschedule.SourceKind, amountProvenance domainschedule.AmountProvenance,
	dateProvenance domainschedule.DateProvenance, inclusion domainschedule.InclusionEligibility,
	key applicationledger.IdempotencyKey,
) (domainschedule.ScheduledCashFlow, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	id, err := s.idGenerator()
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	flow, err := domainschedule.NewManualScheduledCashFlow(
		id, ownerID, amount, direction, date, sourceKind, amountProvenance, dateProvenance, inclusion, s.clock().UTC(),
	)
	if err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	return s.repository.CreateScheduledFlow(ctx, flow, mutation(key,
		"v1", "create_scheduled_flow", ownerID, strconv.FormatInt(amount.MinorUnits(), 10), amount.Currency().Code(),
		direction.String(), date.String(), sourceKind.String(), amountProvenance.String(), dateProvenance.String(), inclusion.String(),
	))
}

func (s *Service) GetScheduledFlow(ctx context.Context, ownerID, flowID string) (domainschedule.ScheduledCashFlow, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	return s.repository.GetScheduledFlow(ctx, ownerID, flowID)
}

func (s *Service) ListScheduledFlows(ctx context.Context, ownerID string) ([]domainschedule.ScheduledCashFlow, error) {
	if err := validateOwner(ownerID); err != nil {
		return nil, err
	}
	return s.repository.ListScheduledFlows(ctx, ownerID)
}

func (s *Service) CancelScheduledFlow(ctx context.Context, ownerID, flowID string, key applicationledger.IdempotencyKey) (domainschedule.ScheduledCashFlow, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.ScheduledCashFlow{}, err
	}
	return s.repository.CancelScheduledFlow(ctx, ownerID, flowID, s.clock().UTC(), mutation(key, "v1", "cancel_scheduled_flow", ownerID, flowID))
}

func (s *Service) CreateReceivable(
	ctx context.Context, ownerID, name string, originalAmount money.Money, expectedDate *financialdate.Date,
	certainty domainschedule.Certainty, key applicationledger.IdempotencyKey,
) (domainschedule.Receivable, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.Receivable{}, err
	}
	id, err := s.idGenerator()
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	receivable, err := domainschedule.NewReceivable(id, ownerID, name, originalAmount, expectedDate, certainty, s.clock().UTC())
	if err != nil {
		return domainschedule.Receivable{}, err
	}
	return s.repository.CreateReceivable(ctx, receivable, mutation(key,
		"v1", "create_receivable", ownerID, receivable.DisplayName(), strconv.FormatInt(originalAmount.MinorUnits(), 10),
		originalAmount.Currency().Code(), optionalDate(expectedDate), certainty.String(),
	))
}

func (s *Service) GetReceivable(ctx context.Context, ownerID, receivableID string) (domainschedule.Receivable, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.Receivable{}, err
	}
	return s.repository.GetReceivable(ctx, ownerID, receivableID)
}

func (s *Service) ListReceivables(ctx context.Context, ownerID string) ([]domainschedule.Receivable, error) {
	if err := validateOwner(ownerID); err != nil {
		return nil, err
	}
	return s.repository.ListReceivables(ctx, ownerID)
}

func (s *Service) RecordReceivableCollection(
	ctx context.Context, ownerID, receivableID string, amount money.Money, ledgerTransactionID string,
	key applicationledger.IdempotencyKey,
) (domainschedule.ReceivableCollection, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	collectionID, err := s.idGenerator()
	if err != nil {
		return domainschedule.ReceivableCollection{}, err
	}
	ledgerTransactionID = strings.TrimSpace(ledgerTransactionID)
	return s.repository.RecordReceivableCollection(
		ctx, ownerID, receivableID, collectionID, amount, ledgerTransactionID, s.clock().UTC(),
		mutation(key, "v1", "collect_receivable", ownerID, receivableID, strconv.FormatInt(amount.MinorUnits(), 10), amount.Currency().Code(), ledgerTransactionID),
	)
}

func (s *Service) CancelReceivable(ctx context.Context, ownerID, receivableID string, key applicationledger.IdempotencyKey) (domainschedule.Receivable, error) {
	if err := validateOwner(ownerID); err != nil {
		return domainschedule.Receivable{}, err
	}
	return s.repository.CancelReceivable(ctx, ownerID, receivableID, s.clock().UTC(), mutation(key, "v1", "cancel_receivable", ownerID, receivableID))
}

func mutation(key applicationledger.IdempotencyKey, parts ...string) applicationledger.MutationIdentity {
	return applicationledger.MutationIdentity{Key: key, Fingerprint: applicationledger.CanonicalFingerprint(parts...)}
}

func optionalDate(date *financialdate.Date) string {
	if date == nil {
		return ""
	}
	return date.String()
}

func validateOwner(ownerID string) error {
	if strings.TrimSpace(ownerID) == "" {
		return ErrInvalidOwner
	}
	return nil
}

func generateID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
