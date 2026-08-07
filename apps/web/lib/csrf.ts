// Client-side helper for the double-submit CSRF check (issue #92): the API
// sets a non-HttpOnly `flowlens_csrf` cookie alongside the session cookie at
// login, and expects every session-authenticated mutation to echo its value
// back in an X-CSRF-Token header. Only same-origin script can read the
// cookie to do that, which is what makes the pattern work.

const CSRF_COOKIE_NAME = "flowlens_csrf";

function readCookie(name: string): string | null {
  const match = document.cookie
    .split("; ")
    .find((row) => row.startsWith(`${name}=`));
  return match ? decodeURIComponent(match.slice(name.length + 1)) : null;
}

/**
 * csrfHeaders returns the header(s) to spread into a mutating fetch's
 * `headers`. Empty before login (no cookie yet), which is fine: the API
 * only enforces the check on already-session-authenticated requests.
 */
export function csrfHeaders(): Record<string, string> {
  const token = readCookie(CSRF_COOKIE_NAME);
  return token ? { "X-CSRF-Token": token } : {};
}
