package http

import "net/http"

const sessionCookieName = "flowlens_session"

// csrfCookieName holds the double-submit CSRF token (issue #92). Unlike the
// session cookie it is deliberately not HttpOnly: the web app's helper
// (apps/web/lib/csrf.ts) reads it via document.cookie to echo it back as the
// X-CSRF-Token header, which is the whole point of the double-submit
// pattern — a cross-site request can make the browser send the cookie, but
// has no way to read its value to also set the header.
const csrfCookieName = "flowlens_csrf"

// cookieManager centralises cookie attributes so dev (http) and prod
// (https) differ only in the Secure flag.
type cookieManager struct {
	secure bool
}

// setSession sets the HttpOnly session cookie. maxAgeSeconds > 0 sets the
// lifetime; the caller keeps it aligned with the server-side session TTL.
func (c cookieManager) setSession(w http.ResponseWriter, token string, maxAgeSeconds int) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAgeSeconds,
	})
}

// clearSession expires the session cookie.
func (c cookieManager) clearSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// setCSRF sets the non-HttpOnly CSRF cookie alongside the session cookie,
// with the same lifetime and Secure/SameSite policy.
func (c cookieManager) setCSRF(w http.ResponseWriter, token string, maxAgeSeconds int) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAgeSeconds,
	})
}

// clearCSRF expires the CSRF cookie.
func (c cookieManager) clearCSRF(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     csrfCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: false,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
