// Package remember implements persistent logins using cookies
package remember

import (
	"bytes"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"net/http"

	"github.com/pkg/errors"

	"github.com/volatiletech/authboss"
)

const (
	nNonceSize = 32
)

var (
	errUserMissing = errors.New("user not loaded in callback")
)

func init() {
	authboss.RegisterModule("remember", &Remember{})
}

// Remember module
type Remember struct {
	*authboss.Authboss
}

// Init module
func (r *Remember) Init(ab *authboss.Authboss) error {
	r.Authboss = ab

	r.Events.After(authboss.EventAuth, r.RememberAfterAuth)
	//TODO(aarondl): Rectify this once oauth2 is done
	// r.Events.After(authboss.EventOAuth, r.RememberAfterAuth)
	r.Events.After(authboss.EventPasswordReset, r.AfterPasswordReset)

	return nil
}

// RememberAfterAuth creates a remember token and saves it in the user's cookies.
func (r *Remember) RememberAfterAuth(w http.ResponseWriter, req *http.Request, handled bool) (bool, error) {
	rmIntf := req.Context().Value(authboss.CTXKeyRM)
	if rmIntf == nil {
		return false, nil
	} else if rm, ok := rmIntf.(bool); ok && !rm {
		return false, nil
	}

	user := r.Authboss.CurrentUserP(w, req)
	hash, token, err := GenerateToken(user.GetPID())
	if err != nil {
		return false, err
	}

	storer := authboss.EnsureCanRemember(r.Authboss.Config.Storage.Server)
	if err = storer.AddRememberToken(user.GetPID(), hash); err != nil {
		return false, err
	}

	authboss.PutCookie(w, authboss.CookieRemember, token)

	return false, nil
}

/*
// TODO(aarondl): Either discard or make this useful later after oauth2
// afterOAuth is called after oauth authentication is successful.
// Has to pander to horrible state variable packing to figure out if we want
// to be remembered.
func (r *Remember) afterOAuth(ctx *authboss.Context) error {
	sessValues, ok := ctx.SessionStorer.Get(authboss.SessionOAuth2Params)
	if !ok {
		return nil
	}

	var values map[string]string
	if err := json.Unmarshal([]byte(sessValues), &values); err != nil {
		return err
	}

	val, ok := values[authboss.CookieRemember]
	should := ok && val == "true"

	if !should {
		return nil
	}

	if ctx.User == nil {
		return errUserMissing
	}

	uid, err := ctx.User.StringErr(authboss.StoreOAuth2Provider)
	if err != nil {
		return err
	}
	provider, err := ctx.User.StringErr(authboss.StoreOAuth2Provider)
	if err != nil {
		return err
	}

	if _, err := r.new(ctx.CookieStorer, uid+";"+provider); err != nil {
		return errors.Wrap(err, "failed to create remember token")
	}

	return nil
}
*/

// Middleware automatically authenticates users if they have remember me tokens
// If the user has been loaded already, it returns early
func Middleware(ab *authboss.Authboss) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(authboss.CTXKeyPID) == nil && r.Context().Value(authboss.CTXKeyUser) == nil {
				if err := Authenticate(ab, w, r); err != nil {
					logger := ab.RequestLogger(r)
					logger.Errorf("failed to authenticate user via remember me: %+v", err)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Authenticate the user using their remember cookie.
// If the cookie proves unusable it will be deleted. A cookie
// may be unusable for the following reasons:
// - Can't decode the base64
// - Invalid token format
// - Can't find token in DB
func Authenticate(ab *authboss.Authboss, w http.ResponseWriter, req *http.Request) error {
	logger := ab.RequestLogger(req)
	cookie, ok := authboss.GetCookie(req, authboss.CookieRemember)
	if !ok {
		return nil
	}

	rawToken, err := base64.URLEncoding.DecodeString(cookie)
	if err != nil {
		authboss.DelCookie(w, authboss.CookieRemember)
		logger.Infof("failed to decode remember me cookie, deleting cookie")
		return nil
	}

	index := bytes.IndexByte(rawToken, ';')
	if index < 0 {
		authboss.DelCookie(w, authboss.CookieRemember)
		logger.Infof("failed to decode remember me token, deleting cookie")
		return nil
	}

	pid := string(rawToken[:index])
	sum := sha512.Sum512(rawToken)
	hash := base64.StdEncoding.EncodeToString(sum[:])

	storer := authboss.EnsureCanRemember(ab.Config.Storage.Server)
	err = storer.UseRememberToken(pid, hash)
	switch {
	case err == authboss.ErrTokenNotFound:
		logger.Infof("remember me cookie had a token that was not in storage, deleting cookie")
		authboss.DelCookie(w, authboss.CookieRemember)
		return nil
	case err != nil:
		return err
	}

	hash, token, err := GenerateToken(pid)
	if err != nil {
		return err
	}

	if err = storer.AddRememberToken(pid, hash); err != nil {
		return errors.Wrap(err, "failed to save me token")
	}

	authboss.PutSession(w, authboss.SessionKey, pid)
	authboss.PutSession(w, authboss.SessionHalfAuthKey, "true")
	authboss.DelCookie(w, authboss.CookieRemember)
	authboss.PutCookie(w, authboss.CookieRemember, token)

	return nil
}

// AfterPasswordReset is called after the password has been reset, since
// it should invalidate all tokens associated to that user.
func (r *Remember) AfterPasswordReset(w http.ResponseWriter, req *http.Request, handled bool) (bool, error) {
	user, err := r.Authboss.CurrentUser(w, req)
	if err != nil {
		return false, err
	}

	logger := r.Authboss.RequestLogger(req)
	storer := authboss.EnsureCanRemember(r.Authboss.Config.Storage.Server)

	pid := user.GetPID()
	authboss.DelCookie(w, authboss.CookieRemember)

	logger.Infof("deleting tokens and rm cookies for user %s due to password reset", pid)

	return false, storer.DelRememberTokens(pid)
}

// GenerateToken creates a remember me token
func GenerateToken(pid string) (hash string, token string, err error) {
	rawToken := make([]byte, nNonceSize+len(pid)+1)
	copy(rawToken, []byte(pid))
	rawToken[len(pid)] = ';'

	if _, err := rand.Read(rawToken[len(pid)+1:]); err != nil {
		return "", "", errors.Wrap(err, "failed to create remember me nonce")
	}

	sum := sha512.Sum512(rawToken)
	return base64.StdEncoding.EncodeToString(sum[:]), base64.URLEncoding.EncodeToString(rawToken), nil
}
