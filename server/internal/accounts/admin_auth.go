package accounts

import (
	"errors"
	"net/http"
	"time"

	apperrors "LLMGateway/server/internal/errors"
	"LLMGateway/server/internal/httpcommon"
)

// sessionCookieName is the management-console session cookie.
const sessionCookieName = "llmgateway_session"

type authCredentialsInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (a *Server) registerAuthRoutes(mux *http.ServeMux) {
	httpcommon.HandleAdmin(mux, "/admin/auth/register", a.authRegister)
	httpcommon.HandleAdmin(mux, "/admin/auth/login", a.authLogin)
	httpcommon.HandleAdmin(mux, "/admin/auth/logout", a.authLogout)
	httpcommon.HandleAdmin(mux, "/admin/auth/me", a.authMe)
}

func (a *Server) authRegister(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPost {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	var in authCredentialsInput
	if err := httpcommon.ReadJSON(r, &in); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	result, err := a.Register(r.Context(), in.Username, in.Password)
	if err != nil {
		return authError(err)
	}
	res := httpcommon.Handled(map[string]any{"account": result.Account})
	res.Cookies = []*http.Cookie{a.sessionCookie(result.Token)}
	return res
}

func (a *Server) authLogin(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPost {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	var in authCredentialsInput
	if err := httpcommon.ReadJSON(r, &in); err != nil {
		return httpcommon.HTTPError(http.StatusBadRequest, "invalid json")
	}
	result, err := a.Login(r.Context(), in.Username, in.Password)
	if err != nil {
		return authError(err)
	}
	res := httpcommon.Handled(map[string]any{"account": result.Account})
	res.Cookies = []*http.Cookie{a.sessionCookie(result.Token)}
	return res
}

func (a *Server) authLogout(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodPost {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	if err := a.Logout(r.Context(), sessionTokenFromRequest(r)); err != nil {
		return authError(err)
	}
	res := httpcommon.Handled(map[string]any{"logged_out": true})
	res.Cookies = []*http.Cookie{a.clearSessionCookie()}
	return res
}

func (a *Server) authMe(r *http.Request) httpcommon.AdminResult {
	if r.Method != http.MethodGet {
		return httpcommon.HTTPError(http.StatusMethodNotAllowed, "method not allowed")
	}
	account, err := a.AuthenticateSession(r.Context(), sessionTokenFromRequest(r))
	if err != nil {
		return authError(err)
	}
	return httpcommon.Handled(account)
}

func (a *Server) sessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.auth.CookieSecure,
		MaxAge:   int(a.auth.SessionTTL.Seconds()),
		Expires:  a.now().Add(a.auth.SessionTTL),
	}
}

func (a *Server) clearSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   a.auth.CookieSecure,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	}
}

func sessionTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func authError(err error) httpcommon.AdminResult {
	switch {
	case errors.Is(err, ErrRegistrationDisabled):
		return httpcommon.HTTPError(http.StatusForbidden, "registration disabled")
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, apperrors.ErrNotFound):
		return httpcommon.HTTPError(http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, apperrors.ErrInvalid):
		return httpcommon.HTTPError(http.StatusBadRequest, httpcommon.MessageFor(err))
	default:
		return httpcommon.HTTPError(http.StatusInternalServerError, "internal error")
	}
}
