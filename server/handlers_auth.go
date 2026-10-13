// handlers_auth.go: login, logout, the session probe and password change.
// Bad credentials always answer a uniform 401 — no account enumeration.
package server

import (
	"net/http"
	"strconv"
)

type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type passwordInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleLogin authenticates the admin and issues the session cookie.
func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	in := loginInput{}
	if err := decodeBody(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if in.Username == "" {
		in.Username = a.adminUsername()
	}

	if d := a.LoginThrottle.check(); d > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(d.Seconds())+1))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":       "too many failed attempts; try again later",
			"status":      http.StatusTooManyRequests,
			"retry_after": int(d.Seconds()) + 1,
		})
		return
	}

	mustChange, ok := a.verifyLogin(in.Username, in.Password)
	if !ok {
		a.LoginThrottle.fail()
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	a.LoginThrottle.reset()

	token := a.Sessions.create(in.Username, mustChange)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionTTL.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"username":             in.Username,
		"must_change_password": mustChange,
	})
}

// handleSession reports the current session state. It answers 200 with
// authenticated=false (rather than 401) so the UI can probe it on boot.
func (a *App) handleSession(w http.ResponseWriter, r *http.Request) {
	sess := a.sessionFrom(r)
	if sess == nil {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":        true,
		"username":             sess.Username,
		"must_change_password": sess.MustChange,
	})
}

// handleLogout drops the session and clears the cookie.
func (a *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.Sessions.drop(c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handlePassword changes the admin password and rotates the session token
// (the old cookie value becomes void) to defeat session fixation.
func (a *App) handlePassword(w http.ResponseWriter, r *http.Request) {
	sess := a.sessionFrom(r)
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "not logged in")
		return
	}
	in := passwordInput{}
	if err := decodeBody(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := a.changePassword(sess.Username, in.CurrentPassword, in.NewPassword); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	if c, err := r.Cookie(sessionCookie); err == nil {
		// clear the gate on the live session first, then rotate the token so
		// the fresh session inherits must_change=false
		a.Sessions.clearMustChange(c.Value)
		if token := a.Sessions.rotate(c.Value); token != "" {
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookie,
				Value:    token,
				Path:     "/",
				MaxAge:   int(sessionTTL.Seconds()),
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
