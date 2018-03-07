package authboss

import (
	"context"
	"net/http"
)

type contextKey string

// CTX Keys for authboss
const (
	CTXKeyPID  contextKey = "pid"
	CTXKeyUser contextKey = "user"

	CTXKeySessionState contextKey = "session"
	CTXKeyCookieState  contextKey = "cookie"

	// CTXKeyData is a context key for the accumulating
	// map[string]interface{} (authboss.HTMLData) to pass to the
	// renderer
	CTXKeyData contextKey = "data"

	// CTXKeyRM is used to flag the remember me module to actually do the
	// remembering, since this is a per-user operation, authentication modules
	// need to supply this key if they wish to allow users to be remembered.
	CTXKeyRM contextKey = "rm"
)

func (c contextKey) String() string {
	return "authboss ctx key " + string(c)
}

// CurrentUserID retrieves the current user from the session.
func (a *Authboss) CurrentUserID(w http.ResponseWriter, r *http.Request) (string, error) {
	if pid := r.Context().Value(CTXKeyPID); pid != nil {
		return pid.(string), nil
	}

	pid, _ := GetSession(r, SessionKey)
	return pid, nil
}

// CurrentUserIDP retrieves the current user but panics if it's not available for
// any reason.
func (a *Authboss) CurrentUserIDP(w http.ResponseWriter, r *http.Request) string {
	i, err := a.CurrentUserID(w, r)
	if err != nil {
		panic(err)
	} else if len(i) == 0 {
		panic(ErrUserNotFound)
	}

	return i
}

// CurrentUser retrieves the current user from the session and the database.
// Before the user is loaded from the database the context key is checked.
// If the session doesn't have the user ID ErrUserNotFound will be returned.
func (a *Authboss) CurrentUser(w http.ResponseWriter, r *http.Request) (User, error) {
	if user := r.Context().Value(CTXKeyUser); user != nil {
		return user.(User), nil
	}

	pid, err := a.CurrentUserID(w, r)
	if err != nil {
		return nil, err
	} else if len(pid) == 0 {
		return nil, ErrUserNotFound
	}

	return a.currentUser(r.Context(), pid)
}

// CurrentUserP retrieves the current user but panics if it's not available for
// any reason.
func (a *Authboss) CurrentUserP(w http.ResponseWriter, r *http.Request) User {
	i, err := a.CurrentUser(w, r)
	if err != nil {
		panic(err)
	} else if i == nil {
		panic(ErrUserNotFound)
	}
	return i
}

func (a *Authboss) currentUser(ctx context.Context, pid string) (User, error) {
	return a.Storage.Server.Load(ctx, pid)
}

// LoadCurrentUserID takes a pointer to a pointer to the request in order to
// change the current method's request pointer itself to the new request that
// contains the new context that has the pid in it.
func (a *Authboss) LoadCurrentUserID(w http.ResponseWriter, r **http.Request) (string, error) {
	if pid := (*r).Context().Value(CTXKeyPID); pid != nil {
		return pid.(string), nil
	}

	pid, err := a.CurrentUserID(w, *r)
	if err != nil {
		return "", err
	}

	if len(pid) == 0 {
		return "", nil
	}

	ctx := context.WithValue((**r).Context(), CTXKeyPID, pid)
	*r = (**r).WithContext(ctx)

	return pid, nil
}

// LoadCurrentUserIDP loads the current user id and panics if it's not found
func (a *Authboss) LoadCurrentUserIDP(w http.ResponseWriter, r **http.Request) string {
	pid, err := a.LoadCurrentUserID(w, r)
	if err != nil {
		panic(err)
	} else if len(pid) == 0 {
		panic(ErrUserNotFound)
	}

	return pid
}

// LoadCurrentUser takes a pointer to a pointer to the request in order to
// change the current method's request pointer itself to the new request that
// contains the new context that has the user in it. Calls LoadCurrentUserID
// so the primary id is also put in the context.
func (a *Authboss) LoadCurrentUser(w http.ResponseWriter, r **http.Request) (User, error) {
	if user := (*r).Context().Value(CTXKeyUser); user != nil {
		return user.(User), nil
	}

	pid, err := a.LoadCurrentUserID(w, r)
	if err != nil {
		return nil, err
	}

	if len(pid) == 0 {
		return nil, nil
	}

	ctx := (**r).Context()
	user, err := a.currentUser(ctx, pid)
	if err != nil {
		return nil, err
	}

	ctx = context.WithValue(ctx, CTXKeyUser, user)
	*r = (**r).WithContext(ctx)
	return user, nil
}

// LoadCurrentUserP does the same as LoadCurrentUser but panics if
// the current user is not found.
func (a *Authboss) LoadCurrentUserP(w http.ResponseWriter, r **http.Request) User {
	user, err := a.LoadCurrentUser(w, r)
	if err != nil {
		panic(err)
	} else if user == nil {
		panic(ErrUserNotFound)
	}

	return user
}
