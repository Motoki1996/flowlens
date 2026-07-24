package http

import "net/http"

const sessionCookieName = "flowlens_session"

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
