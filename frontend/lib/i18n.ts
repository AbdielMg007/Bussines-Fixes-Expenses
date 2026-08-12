export type Language = "es" | "en";

export const defaultLanguage: Language = "es";
export const languageStorageKey = "runway-ui-language";

const spanish = {
  loadingRunway: "Cargando Runway…",
  loadingAccounts: "Cargando cuentas…",
  loadingCashPosition: "Cargando tu posición de efectivo…",
  loadingSettings: "Cargando configuración…",
  returningToSignIn: "Volviendo al inicio de sesión…",
  signIn: "Iniciar sesión",
  signingIn: "Iniciando sesión…",
  signInTitle: "Ve el margen de tu dinero.",
  signInDescription: "Inicia sesión para ver tu liquidez actual y tus próximos compromisos.",
  email: "Correo electrónico",
  password: "Contraseña",
  invalidCredentials: "Correo electrónico o contraseña inválidos.",
  signInUnavailable: "No es posible iniciar sesión en este momento.",
  dashboard: "Panel",
  accounts: "Cuentas",
  logOut: "Cerrar sesión",
  language: "Idioma",
  safeToSpendToday: "Disponible para gastar hoy",
  fundingAccount: "Cuenta de origen",
  openingLiquidCash: "Efectivo líquido inicial",
  reserve: "Reserva",
  projectedMinimum: "Mínimo proyectado",
  deficit: "Déficit",
  earliestBreach: "primer incumplimiento",
  chooseFunding: "Elige una cuenta activa de efectivo o banco para ver el monto disponible.",
  noEligibleFunding: "No hay una cuenta de origen elegible",
  cashOrBankRequired: "Se requiere una cuenta de efectivo o banco",
  cashOrBankRequiredDetail: "Crea una cuenta activa de efectivo o banco e inclúyela en la Política de proyección.",
  safe: "Tu cuenta seleccionada y proyección actual respaldan este monto.",
  constrainedByFuture: "Un mínimo futuro de flujo de efectivo limita lo que puedes gastar hoy.",
  constrainedByFunding: "El saldo de la cuenta seleccionada limita lo que puedes gastar hoy.",
  belowReserve: "Tu proyección base ya está por debajo de la reserva configurada.",
  unsupportedFunding: "Esta cuenta aún no puede usarse para calcular Disponible para gastar.",
  invalidProjectionConfiguration: "La Política de proyección debe corregirse antes de calcular tu efectivo.",
  baselineProjection: "Proyección base",
  futureCashTimeline: "Cronología futura de efectivo",
  opening: "Inicial",
  noFutureEvents: "No hay eventos futuros proyectados dentro de este horizonte.",
  excludedInflows: "Ingresos futuros excluidos",
  retry: "Reintentar",
  unableToLoadProjection: "No fue posible cargar la proyección.",
  unableToLoadPolicy: "No fue posible cargar la Política de proyección. Inténtalo de nuevo cuando el servicio esté disponible.",
  updatePolicy: "Actualiza la Política de proyección antes de que Runway pueda calcular tu cronología de efectivo.",
  projectionSettings: "Configuración de proyección",
  updateProjection: "Actualiza tu proyección de efectivo",
  setupProjection: "Configura tu proyección de efectivo",
  policyDescription: "Esta configuración define qué cuentas de efectivo proyecta Runway. No se guarda nada hasta elegir Guardar.",
  horizonDays: "Días de horizonte",
  cashReserve: "Reserva de efectivo (MXN)",
  financialTimezone: "Zona horaria financiera",
  inflowPolicy: "Política de ingresos",
  confirmedInflowsOnly: "Solo ingresos confirmados",
  includeExpectedInflows: "Incluir ingresos esperados",
  liquidAccountSelection: "Selección de cuentas líquidas",
  allActiveLiquid: "Todas las cuentas activas de efectivo y banco",
  chooseSpecificAccounts: "Elegir cuentas específicas",
  staleSelection: "Esta selección guardada ya no puede participar en efectivo líquido.",
  unavailableAccount: "Cuenta no disponible",
  remove: "Quitar",
  createLiquidAccountFirst: "Primero crea una cuenta activa de efectivo o banco.",
  saveProjectionSettings: "Guardar configuración de proyección",
  saving: "Guardando…",
  invalidHorizon: "El horizonte debe ser un número entero de días.",
  unableToSavePolicy: "No fue posible guardar la configuración de proyección.",
  yourLedger: "Tu libro mayor",
  createFirstAccount: "Crea tu primera cuenta.",
  createAccount: "Crear cuenta",
  name: "Nombre",
  accountType: "Tipo de cuenta",
  addAccount: "Agregar cuenta",
  addingAccount: "Agregando…",
  unableToCreateAccount: "No fue posible crear la cuenta.",
  noAccountSelected: "No hay una cuenta seleccionada",
  createAccountToStart: "Crea una cuenta para comenzar un libro mayor.",
  active: "Activa",
  archived: "Archivada",
  archivedDetail: "Archivada — los registros históricos permanecen de solo lectura.",
  activeAccount: "Cuenta activa",
  archive: "Archivar",
  archiveConfirm: "¿Archivar {name}? La actividad histórica seguirá visible.",
  unableToArchive: "No fue posible archivar la cuenta.",
  balance: "Saldo",
  loadingBalance: "Cargando saldo…",
  movements: "Movimientos",
  noTransactions: "Aún no hay movimientos registrados.",
  manualMovement: "Movimiento manual",
  postTransaction: "Registrar transacción",
  effect: "Efecto",
  amount: "Monto (MXN)",
  financialDate: "Fecha financiera",
  memo: "Nota",
  post: "Registrar",
  posting: "Registrando…",
  retryPending: "Reintentar solicitud pendiente",
  retryPendingFirst: "Primero reintenta la solicitud pendiente",
  unableToPost: "No fue posible registrar el movimiento.",
  retryFailed: "Falló el reintento.",
  internalTransfer: "Transferencia interna",
  moveMoneyOrPayCard: "Mover dinero o pagar una tarjeta",
  from: "Desde",
  to: "Hacia",
  createTransfer: "Crear transferencia",
  moving: "Moviendo…",
  unableToTransfer: "No fue posible crear la transferencia.",
  cash: "Efectivo",
  bank: "Banco",
  creditCard: "Tarjeta de crédito",
  loan: "Préstamo",
  assetInflow: "Entrada de activo",
  assetOutflow: "Salida de activo",
  liabilityCharge: "Cargo de pasivo",
  liabilityPayment: "Pago de pasivo",
  linkedTransfer: "transferencia vinculada",
  mandatoryOutflow: "Salida obligatoria",
  mandatoryManualOutflow: "Salida manual obligatoria",
  eligibleManualInflow: "Ingreso manual elegible",
  expectedManualInflow: "Ingreso manual esperado incluido por política",
  confirmedReceivable: "Cuenta por cobrar confirmada",
  expectedReceivable: "Cuenta por cobrar esperada incluida por política",
  obligationOccurrence: "Obligación programada",
  manualScheduledFlow: "Flujo manual programado",
  receivable: "Cuenta por cobrar",
  uncertainReceivable: "Cuenta por cobrar incierta",
  undatedReceivable: "Cuenta por cobrar sin fecha",
  policyExcludedInflow: "Ingreso excluido por política",
  cancelled: "Cancelado",
  settled: "Liquidado",
  collected: "Cobrado",
  indeterminate: "No disponible: falta confirmar un pago de tarjeta.",
  safeToSpendUnavailable: "No disponible",
  projectionIncomplete: "La proyección no está completa por un pago de tarjeta pendiente de revisar.",
  cardPaymentIntentMissing: "Falta la intención de pago de la tarjeta.",
  cardPaymentIntentNeedsReview: "La intención de pago de la tarjeta requiere revisión.",
  cardPaymentDueTodayUnsettled: "El pago de la tarjeta vence hoy y aún no está liquidado explícitamente.",
  cardPaymentPastDueUnsettled: "Hay un pago de tarjeta vencido sin liquidación explícita.",
  invalidCardPaymentFlow: "El flujo de pago de tarjeta requiere corrección.",
  unknownCardProjectionIssue: "Hay una condición de tarjeta que impide calcular el efectivo de forma determinista.",
  cardManagement: "Administración de tarjeta",
  statements: "Estados de cuenta",
  noStatements: "Aún no hay estados de cuenta registrados.",
  registerStatement: "Registrar estado de cuenta",
  cycleStart: "Inicio del ciclo",
  cycleEnd: "Fin del ciclo",
  statementAuthority: "Autoridad del estado",
  estimated: "Estimado",
  issued: "Emitido",
  statementBalance: "Saldo del estado (MXN)",
  minimumPayment: "Pago mínimo",
  ppngi: "Pago para no generar intereses",
  dueDate: "Fecha de vencimiento",
  cycle: "Ciclo",
  revision: "Revisión",
  superseded: "Sustituido",
  paymentIntent: "Intención de pago",
  noPaymentIntent: "No hay una intención de pago vigente para este ciclo.",
  plannedPayment: "Pago planeado (MXN)",
  plannedDate: "Fecha planeada",
  needsReview: "Requiere revisión",
  intentNeedsReviewWarning: "Este pago planeado debe revisarse antes de que Runway pueda calcular efectivo disponible con certeza.",
  cancelIntentConfirm: "¿Cancelar esta intención de pago? No se realizará ningún pago automáticamente.",
  unableToLoadCard: "No fue posible cargar la información de la tarjeta.",
  unableToSaveStatement: "No fue posible guardar el estado de cuenta.",
  unableToSaveIntent: "No fue posible guardar la intención de pago.",
  explicitSettlement: "Transferencia para liquidar explícitamente",
  linkSettlement: "Vincular liquidación",
  noEligibleCardTransfer: "No hay transferencias publicadas hacia esta tarjeta para vincular. Una transferencia genérica no se vincula automáticamente.",
  unableToSettleIntent: "No fue posible vincular la liquidación.",
  createMsi: "Crear plan MSI",
  noCardCharges: "Primero registra un cargo de pasivo de esta tarjeta.",
  sourceCharge: "Cargo de tarjeta de origen",
  description: "Descripción",
  principal: "Principal (MXN)",
  installmentCount: "Número de mensualidades",
  unableToCreateMsi: "No fue posible crear el plan MSI.",
  msiPlans: "Planes MSI",
  noMsiPlans: "Aún no hay planes MSI.",
  paidPrincipal: "Principal pagado",
  outstandingPrincipal: "Principal pendiente",
  allocations: "Asignaciones",
  upcomingCardPayments: "Próximos pagos de tarjeta",
  intendedAmount: "Monto previsto",
  explicitlySettledAmount: "Liquidado explícitamente",
  remainingFutureAmount: "Monto futuro pendiente",
  fullySettled: "Liquidado por completo",
  noRemainingPlannedPayment: "No queda ningún pago futuro planeado.",
  save: "Guardar",
  cancel: "Cancelar",
  invalidAmount: "Ingresa un monto no negativo con hasta dos decimales.",
  amountOutOfRange: "El monto está fuera del rango admitido por el navegador.",
} as const;

const english: { [K in keyof typeof spanish]: string } = {
  loadingRunway: "Loading Runway…", loadingAccounts: "Loading accounts…", loadingCashPosition: "Loading your cash position…", loadingSettings: "Loading settings…", returningToSignIn: "Returning to sign in…",
  signIn: "Sign in", signingIn: "Signing in…", signInTitle: "See the room in your money.", signInDescription: "Sign in to view your current runway and upcoming commitments.", email: "Email", password: "Password", invalidCredentials: "Invalid email or password.", signInUnavailable: "Unable to sign in right now.",
  dashboard: "Dashboard", accounts: "Accounts", logOut: "Log out", language: "Language", safeToSpendToday: "Safe to spend today", fundingAccount: "Funding account", openingLiquidCash: "Opening liquid cash", reserve: "Reserve", projectedMinimum: "Projected minimum", deficit: "Deficit", earliestBreach: "earliest breach", chooseFunding: "Choose an active cash or bank account to see Safe-to-Spend.", noEligibleFunding: "No eligible funding account", cashOrBankRequired: "A cash or bank account is required", cashOrBankRequiredDetail: "Create an active cash or bank account, then include it in ProjectionPolicy.",
  safe: "Your selected account and current projection both support this amount.", constrainedByFuture: "An upcoming cash-flow low is limiting today’s spend.", constrainedByFunding: "The selected funding account balance is the limiting factor.", belowReserve: "Your baseline projection is already below the configured reserve.", unsupportedFunding: "This account cannot fund Safe-to-Spend yet.", invalidProjectionConfiguration: "ProjectionPolicy must be corrected before Runway can calculate your cash.",
  baselineProjection: "Baseline projection", futureCashTimeline: "Future cash timeline", opening: "Opening", noFutureEvents: "No future projected events inside this horizon.", excludedInflows: "Excluded future inflows", retry: "Try again", unableToLoadProjection: "Unable to load the projection.", unableToLoadPolicy: "Unable to load ProjectionPolicy. Try again after the service is available.", updatePolicy: "Update ProjectionPolicy before Runway can calculate your cash timeline.",
  projectionSettings: "Projection settings", updateProjection: "Update your cash projection", setupProjection: "Set up your cash projection", policyDescription: "These settings define which cash accounts Runway projects. Nothing is saved until you choose Save.", horizonDays: "Horizon days", cashReserve: "Cash reserve (MXN)", financialTimezone: "Financial timezone", inflowPolicy: "Inflow policy", confirmedInflowsOnly: "Confirmed inflows only", includeExpectedInflows: "Include expected inflows", liquidAccountSelection: "Liquid account selection", allActiveLiquid: "All active cash and bank accounts", chooseSpecificAccounts: "Choose specific accounts", staleSelection: "This saved selection can no longer participate in liquid cash.", unavailableAccount: "Unavailable account", remove: "Remove", createLiquidAccountFirst: "Create an active cash or bank account first.", saveProjectionSettings: "Save projection settings", saving: "Saving…", invalidHorizon: "Horizon must be a whole number of days.", unableToSavePolicy: "Unable to save projection settings.",
  yourLedger: "Your ledger", createFirstAccount: "Create your first account.", createAccount: "Create account", name: "Name", accountType: "Account type", addAccount: "Add account", addingAccount: "Adding…", unableToCreateAccount: "Unable to create account.", noAccountSelected: "No account selected", createAccountToStart: "Create an account to start a ledger.", active: "Active", archived: "Archived", archivedDetail: "Archived — historical records remain read-only.", activeAccount: "Active account", archive: "Archive", archiveConfirm: "Archive {name}? Historical activity stays visible.", unableToArchive: "Unable to archive account.", balance: "Balance", loadingBalance: "Loading balance…", movements: "Movements", noTransactions: "No posted transactions yet.", manualMovement: "Manual movement", postTransaction: "Post a transaction", effect: "Effect", amount: "Amount (MXN)", financialDate: "Financial date", memo: "Memo", post: "Post transaction", posting: "Posting…", retryPending: "Retry pending request", retryPendingFirst: "Retry the pending request first", unableToPost: "Unable to post movement.", retryFailed: "Retry failed.", internalTransfer: "Internal transfer", moveMoneyOrPayCard: "Move money or pay a card", from: "From", to: "To", createTransfer: "Create transfer", moving: "Moving…", unableToTransfer: "Unable to create transfer.",
  cash: "Cash", bank: "Bank", creditCard: "Credit card", loan: "Loan", assetInflow: "Asset inflow", assetOutflow: "Asset outflow", liabilityCharge: "Liability charge", liabilityPayment: "Liability payment", linkedTransfer: "linked transfer", mandatoryOutflow: "Mandatory obligation outflow", mandatoryManualOutflow: "Mandatory manual outflow", eligibleManualInflow: "Eligible manual inflow", expectedManualInflow: "Expected manual inflow allowed by policy", confirmedReceivable: "Confirmed receivable", expectedReceivable: "Expected receivable allowed by policy", obligationOccurrence: "Scheduled obligation", manualScheduledFlow: "Manual scheduled flow", receivable: "Receivable", uncertainReceivable: "Uncertain receivable", undatedReceivable: "Undated receivable", policyExcludedInflow: "Inflow excluded by policy", cancelled: "Cancelled", settled: "Settled", collected: "Collected", indeterminate: "Unavailable: a card payment still needs confirmation.", safeToSpendUnavailable: "Unavailable", projectionIncomplete: "The projection is incomplete because a card payment still needs review.", cardPaymentIntentMissing: "A card payment intent is missing.", cardPaymentIntentNeedsReview: "The card payment intent needs review.", cardPaymentDueTodayUnsettled: "A card payment is due today and has not been explicitly settled.", cardPaymentPastDueUnsettled: "A past-due card payment has not been explicitly settled.", invalidCardPaymentFlow: "The card payment flow needs correction.", unknownCardProjectionIssue: "A card condition prevents a deterministic cash calculation.", cardManagement: "Card management", statements: "Statements", noStatements: "No statements have been registered yet.", registerStatement: "Register statement", cycleStart: "Cycle start", cycleEnd: "Cycle end", statementAuthority: "Statement authority", estimated: "Estimated", issued: "Issued", statementBalance: "Statement balance (MXN)", minimumPayment: "Minimum payment", ppngi: "Payment to avoid interest", dueDate: "Due date", cycle: "Cycle", revision: "Revision", superseded: "Superseded", paymentIntent: "Payment intent", noPaymentIntent: "There is no current payment intent for this cycle.", plannedPayment: "Planned payment (MXN)", plannedDate: "Planned date", needsReview: "Needs review", intentNeedsReviewWarning: "This planned payment must be reviewed before Runway can calculate available cash with certainty.", cancelIntentConfirm: "Cancel this payment intent? No payment will be made automatically.", unableToLoadCard: "Unable to load card information.", unableToSaveStatement: "Unable to save the statement.", unableToSaveIntent: "Unable to save the payment intent.", explicitSettlement: "Transfer to explicitly settle", linkSettlement: "Link settlement", noEligibleCardTransfer: "There are no posted transfers to this card to link. A generic transfer is never linked automatically.", unableToSettleIntent: "Unable to link the settlement.", createMsi: "Create MSI plan", noCardCharges: "Post a liability charge to this card first.", sourceCharge: "Source card charge", description: "Description", principal: "Principal (MXN)", installmentCount: "Installment count", unableToCreateMsi: "Unable to create the MSI plan.", msiPlans: "MSI plans", noMsiPlans: "There are no MSI plans yet.", paidPrincipal: "Paid principal", outstandingPrincipal: "Outstanding principal", allocations: "Allocations", upcomingCardPayments: "Upcoming card payments", intendedAmount: "Intended amount", explicitlySettledAmount: "Explicitly settled", remainingFutureAmount: "Remaining future amount", fullySettled: "Fully settled", noRemainingPlannedPayment: "No planned future payment remains.", save: "Save", cancel: "Cancel", invalidAmount: "Enter a non-negative amount with up to two decimal places.", amountOutOfRange: "Amount is outside the browser-supported range.",
};

const translations = { es: spanish, en: english } as const;
export type TranslationKey = keyof typeof spanish;

export function t(language: Language, key: TranslationKey, variables: Record<string, string | number> = {}): string {
  return translations[language][key].replace(/\{(\w+)\}/g, (_, name: string) => String(variables[name] ?? `{${name}}`));
}

export function displayLocale(language: Language): string {
  return language === "es" ? "es-MX" : "en-US";
}

export function readLanguage(value: string | null): Language {
  return value === "en" || value === "es" ? value : defaultLanguage;
}

export function accountTypeText(language: Language, value: string): string {
  return t(language, ({ cash: "cash", bank: "bank", credit_card: "creditCard", loan: "loan" } as Record<string, TranslationKey>)[value] ?? "accounts");
}

export function accountStatusText(language: Language, value: string): string {
  return t(language, value === "archived" ? "archived" : "active");
}

export function effectText(language: Language, value: string): string {
  return t(language, ({ asset_inflow: "assetInflow", asset_outflow: "assetOutflow", liability_charge: "liabilityCharge", liability_payment: "liabilityPayment" } as Record<string, TranslationKey>)[value] ?? "movements");
}

export function sourceText(language: Language, value: string): string {
  return t(language, ({ obligation_occurrence: "obligationOccurrence", manual_scheduled_flow: "manualScheduledFlow", receivable: "receivable", credit_card_payment_intent: "paymentIntent" } as Record<string, TranslationKey>)[value] ?? "movements");
}

export function inclusionText(language: Language, value: string): string {
  return t(language, ({ mandatory_obligation_outflow: "mandatoryOutflow", mandatory_manual_outflow: "mandatoryManualOutflow", eligible_manual_inflow: "eligibleManualInflow", expected_manual_inflow_allowed_by_policy: "expectedManualInflow", confirmed_receivable: "confirmedReceivable", expected_receivable_allowed_by_policy: "expectedReceivable", authoritative_card_payment_intent: "paymentIntent" } as Record<string, TranslationKey>)[value] ?? "movements");
}

export function exclusionText(language: Language, value: string): string {
  return t(language, ({ uncertain_receivable: "uncertainReceivable", undated_receivable: "undatedReceivable", policy_excluded_inflow: "policyExcludedInflow", cancelled: "cancelled", settled: "settled", collected: "collected" } as Record<string, TranslationKey>)[value] ?? "movements");
}
