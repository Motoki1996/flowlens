package http

import "net/http"

const (
	sessionCookieName = "flowlens_session"
	stateCookieName   = "flowlens_oauth_state"
)

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

// setState sets a short-lived cookie holding the OAuth anti-CSRF state.
func (c cookieManager) setState(w http.ResponseWriter, state string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600, // 10 minutes
	})
}

// clearState expires the OAuth state cookie.
func (c cookieManager) clearState(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   c.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
