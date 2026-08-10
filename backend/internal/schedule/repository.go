package schedule

import (
	"context"
	"time"

	"runway/backend/internal/domain/financialdate"
	"runway/backend/internal/domain/money"
	domainschedule "runway/backend/internal/domain/schedule"
	applicationledger "runway/backend/internal/ledger"
)

type Repository interface {
	CreateObligation(context.Context, domainschedule.Obligation, applicationledger.MutationIdentity) (domainschedule.Obligation, error)
	GetObligation(context.Context, string, string) (domainschedule.Obligation, error)
	ListObligations(context.Context, string) ([]domainschedule.Obligation, error)
	ArchiveObligation(context.Context, string, string, financialdate.Date, time.Time, applicationledger.MutationIdentity) (domainschedule.Obligation, error)

	CreateScheduledFlow(context.Context, domainschedule.ScheduledCashFlow, applicationledger.MutationIdentity) (domainschedule.ScheduledCashFlow, error)
	GetScheduledFlow(context.Context, string, string) (domainschedule.ScheduledCashFlow, error)
	ListScheduledFlows(context.Context, string) ([]domainschedule.ScheduledCashFlow, error)
	CancelScheduledFlow(context.Context, string, string, time.Time, applicationledger.MutationIdentity) (domainschedule.ScheduledCashFlow, error)

	CreateReceivable(context.Context, domainschedule.Receivable, applicationledger.MutationIdentity) (domainschedule.Receivable, error)
	GetReceivable(context.Context, string, string) (domainschedule.Receivable, error)
	ListReceivables(context.Context, string) ([]domainschedule.Receivable, error)
	RecordReceivableCollection(context.Context, string, string, string, money.Money, string, time.Time, applicationledger.MutationIdentity) (domainschedule.ReceivableCollection, error)
	CancelReceivable(context.Context, string, string, time.Time, applicationledger.MutationIdentity) (domainschedule.Receivable, error)
}
