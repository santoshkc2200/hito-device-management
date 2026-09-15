package staffauth

import "net/http"

const (
	SessionCookieName = "hdms_staff_session"
	CSRFCookieName    = "hdms_staff_csrf"
	CSRFHeaderName    = "X-CSRF-Token"
)

// SessionCookie mirrors auth.SessionCookie but under a distinct name, so a
// staff session and an administrator session can coexist in one browser and
// can never be confused for one another.
func SessionCookie(token string, ttlSeconds int) *http.Cookie {
	return &http.Cookie{
		Name: SessionCookieName, Value: token, Path: "/",
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: ttlSeconds,
	}
}

// CSRFCookie is readable by the app, which echoes it back in X-CSRF-Token.
func CSRFCookie(token string, ttlSeconds int) *http.Cookie {
	return &http.Cookie{
		Name: CSRFCookieName, Value: token, Path: "/",
		HttpOnly: false, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: ttlSeconds,
	}
}

func ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{Name: SessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1}
}

func ExpiredCSRFCookie() *http.Cookie {
	return &http.Cookie{Name: CSRFCookieName, Value: "", Path: "/", HttpOnly: false, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1}
}
