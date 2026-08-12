package schedule

type Recurrence struct{ value string }

var (
	oneTimeRecurrence  = Recurrence{value: "one_time"}
	weeklyRecurrence   = Recurrence{value: "weekly"}
	biweeklyRecurrence = Recurrence{value: "biweekly"}
	monthlyRecurrence  = Recurrence{value: "monthly"}
)

func OneTime() Recurrence           { return oneTimeRecurrence }
func Weekly() Recurrence            { return weeklyRecurrence }
func Biweekly() Recurrence          { return biweeklyRecurrence }
func Monthly() Recurrence           { return monthlyRecurrence }
func (r Recurrence) String() string { return r.value }

func ParseRecurrence(value string) (Recurrence, error) {
	switch value {
	case oneTimeRecurrence.value:
		return oneTimeRecurrence, nil
	case weeklyRecurrence.value:
		return weeklyRecurrence, nil
	case biweeklyRecurrence.value:
		return biweeklyRecurrence, nil
	case monthlyRecurrence.value:
		return monthlyRecurrence, nil
	default:
		return Recurrence{}, ErrInvalidRecurrence
	}
}

type ObligationStatus struct{ value string }

var (
	activeObligationStatus   = ObligationStatus{value: "active"}
	archivedObligationStatus = ObligationStatus{value: "archived"}
)

func ActiveObligation() ObligationStatus    { return activeObligationStatus }
func ArchivedObligation() ObligationStatus  { return archivedObligationStatus }
func (s ObligationStatus) String() string   { return s.value }
func (s ObligationStatus) IsActive() bool   { return s == activeObligationStatus }
func (s ObligationStatus) IsArchived() bool { return s == archivedObligationStatus }

func ParseObligationStatus(value string) (ObligationStatus, error) {
	switch value {
	case activeObligationStatus.value:
		return activeObligationStatus, nil
	case archivedObligationStatus.value:
		return archivedObligationStatus, nil
	default:
		return ObligationStatus{}, ErrInvalidObligation
	}
}

type Direction struct{ value string }

var (
	inflowDirection  = Direction{value: "inflow"}
	outflowDirection = Direction{value: "outflow"}
)

func Inflow() Direction            { return inflowDirection }
func Outflow() Direction           { return outflowDirection }
func (d Direction) String() string { return d.value }

func ParseDirection(value string) (Direction, error) {
	switch value {
	case inflowDirection.value:
		return inflowDirection, nil
	case outflowDirection.value:
		return outflowDirection, nil
	default:
		return Direction{}, ErrInvalidDirection
	}
}

type SourceKind struct{ value string }

var (
	obligationSource              = SourceKind{value: "obligation"}
	receivableSource              = SourceKind{value: "receivable"}
	manualExpectedIncomeSource    = SourceKind{value: "manual_expected_income"}
	manualOtherSource             = SourceKind{value: "manual_other"}
	creditCardPaymentIntentSource = SourceKind{value: "credit_card_payment_intent"}
)

func ObligationSource() SourceKind                   { return obligationSource }
func ReceivableSource() SourceKind                   { return receivableSource }
func ManualExpectedIncomeSource() SourceKind         { return manualExpectedIncomeSource }
func ManualOtherSource() SourceKind                  { return manualOtherSource }
func CreditCardPaymentIntentSource() SourceKind      { return creditCardPaymentIntentSource }
func (s SourceKind) String() string                  { return s.value }
func (s SourceKind) IsManual() bool                  { return s == manualExpectedIncomeSource || s == manualOtherSource }
func (s SourceKind) IsCreditCardPaymentIntent() bool { return s == creditCardPaymentIntentSource }

func ParseSourceKind(value string) (SourceKind, error) {
	switch value {
	case obligationSource.value:
		return obligationSource, nil
	case receivableSource.value:
		return receivableSource, nil
	case manualExpectedIncomeSource.value:
		return manualExpectedIncomeSource, nil
	case manualOtherSource.value:
		return manualOtherSource, nil
	case creditCardPaymentIntentSource.value:
		return creditCardPaymentIntentSource, nil
	default:
		return SourceKind{}, ErrInvalidSourceKind
	}
}

type FlowStatus struct{ value string }

var (
	scheduledFlowStatus = FlowStatus{value: "scheduled"}
	settledFlowStatus   = FlowStatus{value: "settled"}
	cancelledFlowStatus = FlowStatus{value: "cancelled"}
)

func ScheduledFlow() FlowStatus        { return scheduledFlowStatus }
func SettledFlow() FlowStatus          { return settledFlowStatus }
func CancelledFlow() FlowStatus        { return cancelledFlowStatus }
func (s FlowStatus) String() string    { return s.value }
func (s FlowStatus) IsScheduled() bool { return s == scheduledFlowStatus }
func (s FlowStatus) IsSettled() bool   { return s == settledFlowStatus }
func (s FlowStatus) IsCancelled() bool { return s == cancelledFlowStatus }

func ParseFlowStatus(value string) (FlowStatus, error) {
	switch value {
	case scheduledFlowStatus.value:
		return scheduledFlowStatus, nil
	case settledFlowStatus.value:
		return settledFlowStatus, nil
	case cancelledFlowStatus.value:
		return cancelledFlowStatus, nil
	default:
		return FlowStatus{}, ErrInvalidFlowStatus
	}
}

type AmountProvenance struct{ value string }

var (
	exactAmountProvenance     = AmountProvenance{value: "exact"}
	estimatedAmountProvenance = AmountProvenance{value: "estimated"}
)

func ExactAmount() AmountProvenance       { return exactAmountProvenance }
func EstimatedAmount() AmountProvenance   { return estimatedAmountProvenance }
func (p AmountProvenance) String() string { return p.value }

func ParseAmountProvenance(value string) (AmountProvenance, error) {
	switch value {
	case exactAmountProvenance.value:
		return exactAmountProvenance, nil
	case estimatedAmountProvenance.value:
		return estimatedAmountProvenance, nil
	default:
		return AmountProvenance{}, ErrInvalidProvenance
	}
}

type DateProvenance struct{ value string }

var (
	exactDateProvenance     = DateProvenance{value: "exact"}
	estimatedDateProvenance = DateProvenance{value: "estimated"}
	assumedDateProvenance   = DateProvenance{value: "assumed_by_scenario"}
)

func ExactDate() DateProvenance                  { return exactDateProvenance }
func EstimatedDate() DateProvenance              { return estimatedDateProvenance }
func AssumedByScenarioDate() DateProvenance      { return assumedDateProvenance }
func (p DateProvenance) String() string          { return p.value }
func (p DateProvenance) IsScenarioAssumed() bool { return p == assumedDateProvenance }

func ParseDateProvenance(value string) (DateProvenance, error) {
	switch value {
	case exactDateProvenance.value:
		return exactDateProvenance, nil
	case estimatedDateProvenance.value:
		return estimatedDateProvenance, nil
	case assumedDateProvenance.value:
		return assumedDateProvenance, nil
	default:
		return DateProvenance{}, ErrInvalidProvenance
	}
}

// InclusionEligibility is source metadata only. Eligible does not mean that a
// future ProjectionPolicy has included the flow.
type InclusionEligibility struct{ value string }

var (
	eligibleInclusion = InclusionEligibility{value: "eligible"}
	excludedInclusion = InclusionEligibility{value: "excluded"}
)

func EligibleForPolicy() InclusionEligibility  { return eligibleInclusion }
func ExcludedFromPolicy() InclusionEligibility { return excludedInclusion }
func (i InclusionEligibility) String() string  { return i.value }

func ParseInclusionEligibility(value string) (InclusionEligibility, error) {
	switch value {
	case eligibleInclusion.value:
		return eligibleInclusion, nil
	case excludedInclusion.value:
		return excludedInclusion, nil
	default:
		return InclusionEligibility{}, ErrInvalidInclusion
	}
}

type Certainty struct{ value string }

var (
	confirmedCertainty = Certainty{value: "confirmed"}
	expectedCertainty  = Certainty{value: "expected"}
	uncertainCertainty = Certainty{value: "uncertain"}
)

func Confirmed() Certainty         { return confirmedCertainty }
func Expected() Certainty          { return expectedCertainty }
func Uncertain() Certainty         { return uncertainCertainty }
func (c Certainty) String() string { return c.value }

func ParseCertainty(value string) (Certainty, error) {
	switch value {
	case confirmedCertainty.value:
		return confirmedCertainty, nil
	case expectedCertainty.value:
		return expectedCertainty, nil
	case uncertainCertainty.value:
		return uncertainCertainty, nil
	default:
		return Certainty{}, ErrInvalidCertainty
	}
}

type ReceivableStatus struct{ value string }

var (
	openReceivableStatus      = ReceivableStatus{value: "open"}
	partialReceivableStatus   = ReceivableStatus{value: "partially_collected"}
	collectedReceivableStatus = ReceivableStatus{value: "collected"}
	cancelledReceivableStatus = ReceivableStatus{value: "cancelled"}
)

func OpenReceivable() ReceivableStatus      { return openReceivableStatus }
func PartialReceivable() ReceivableStatus   { return partialReceivableStatus }
func CollectedReceivable() ReceivableStatus { return collectedReceivableStatus }
func CancelledReceivable() ReceivableStatus { return cancelledReceivableStatus }
func (s ReceivableStatus) String() string   { return s.value }
func (s ReceivableStatus) IsFinal() bool {
	return s == collectedReceivableStatus || s == cancelledReceivableStatus
}

func ParseReceivableStatus(value string) (ReceivableStatus, error) {
	switch value {
	case openReceivableStatus.value:
		return openReceivableStatus, nil
	case partialReceivableStatus.value:
		return partialReceivableStatus, nil
	case collectedReceivableStatus.value:
		return collectedReceivableStatus, nil
	case cancelledReceivableStatus.value:
		return cancelledReceivableStatus, nil
	default:
		return ReceivableStatus{}, ErrInvalidReceivableStatus
	}
}
