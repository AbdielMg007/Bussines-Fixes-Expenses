import { displayLocale, t, type Language } from "./i18n";

export function formatMoney(minorUnits: number, currency = "MXN", language: Language = "es"): string {
  return new Intl.NumberFormat(displayLocale(language), {
    style: "currency",
    currency,
    currencyDisplay: "symbol",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(minorUnits / 100);
}

// Financial dates are date-only values. Rendering in UTC prevents a browser
// timezone from shifting 2026-08-15 into the prior calendar day.
export function formatFinancialDate(value: string, language: Language = "es"): string {
  const [year, month, day] = value.split("-").map(Number);
  if (!year || !month || !day) return value;
  return new Intl.DateTimeFormat(displayLocale(language), {
    timeZone: "UTC",
    month: "short",
    day: "numeric",
    year: "numeric",
  }).format(new Date(Date.UTC(year, month - 1, day)));
}

export function parseMinorUnits(value: string, language: Language = "es"): number {
  const normalized = value.trim();
  const match = /^(\d+)(?:\.(\d{1,2}))?$/.exec(normalized);
  if (!match) throw new Error(t(language, "invalidAmount"));

  const whole = Number(match[1]);
  const fraction = Number((match[2] ?? "").padEnd(2, "0") || "0");
  if (!Number.isSafeInteger(whole) || whole > Math.floor(Number.MAX_SAFE_INTEGER / 100)) {
    throw new Error(t(language, "amountOutOfRange"));
  }
  return whole * 100 + fraction;
}

export function todayFinancialDate(): string {
  const now = new Date();
  const year = now.getFullYear();
  const month = String(now.getMonth() + 1).padStart(2, "0");
  const day = String(now.getDate()).padStart(2, "0");
  return `${year}-${month}-${day}`;
}
