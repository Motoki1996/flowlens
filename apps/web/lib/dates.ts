/** formatDateValue renders a Date the way every task screen shows dates. */
export function formatDateValue(date: Date) {
  return date.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

/** formatDate renders an ISO timestamp from the API. */
export function formatDate(iso: string) {
  return formatDateValue(new Date(iso));
}

/**
 * toApiDate turns a picked day into the RFC3339 timestamp the API decodes into
 * a *time.Time — a bare "2026-08-01" is rejected as an invalid body. The day is
 * read in local time and anchored at midnight UTC, so the date the user clicked
 * is the date the API stores regardless of the browser's timezone (which
 * toISOString would shift).
 */
export function toApiDate(date: Date | undefined): string | null {
  if (!date) return null;
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}T00:00:00Z`;
}

/** fromApiDate parses a nullable API date back into a Calendar value. */
export function fromApiDate(iso: string | null): Date | undefined {
  return iso ? new Date(iso) : undefined;
}

/**
 * toDateParam renders a Date as the bare YYYY-MM-DD that the cross-project
 * Task collection's dueBefore/dueAfter/startedBefore query params expect
 * (parseDateQueryParam, apps/api/internal/http/task_handler.go) — unlike
 * toApiDate, which produces the full RFC3339 timestamp a task's own body
 * fields need. The day is read in local time, same as toApiDate.
 */
export function toDateParam(date: Date): string {
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return `${date.getFullYear()}-${month}-${day}`;
}
