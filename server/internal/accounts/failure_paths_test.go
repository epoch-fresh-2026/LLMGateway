package accounts

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"LLMGateway/server/internal/crypto"
	apperrors "LLMGateway/server/internal/errors"
	"LLMGateway/server/internal/httpcommon"
)

var errPersistence = errors.New("persistence unavailable")

type accountFailurePort struct {
	Port
	err         error
	credentials Credentials
	session     Session
}

func (p accountFailurePort) GetUserCredentialsByUsername(context.Context, string) (Credentials, error) {
	return p.credentials, p.err
}
func (p accountFailurePort) GetUserCredentialsByID(context.Context, int) (Credentials, error) {
	return p.credentials, p.err
}
func (p accountFailurePort) GetSessionByTokenHash(context.Context, string) (Session, error) {
	return p.session, p.err
}
func (p accountFailurePort) DeleteSessionByTokenHash(context.Context, string) (bool, error) {
	return false, p.err
}

type accountFailureTx struct {
	Tx
	operation string
	err       error
	found     bool
}

func (x accountFailureTx) InsertUserWithCredentials(CredentialsInput) (Account, error) {
	if x.operation == "user" {
		return Account{}, x.err
	}
	return Account{ID: 1}, nil
}
func (x accountFailureTx) InsertSession(SessionInput) (int, error) { return 0, x.err }
func (x accountFailureTx) GetUser(int) (User, error)               { return User{ID: 1}, nil }
func (x accountFailureTx) InsertKey(KeyInsert) (int, error)        { return 0, x.err }
func (x accountFailureTx) UpdateKeyActive(int, int, bool) (ClientKey, bool, error) {
	return ClientKey{}, x.found, x.err
}
func (x accountFailureTx) DeleteQuotaReservationsForKey(int, int) error {
	if x.operation == "reservations" {
		return x.err
	}
	return nil
}
func (x accountFailureTx) DeleteKey(int, int) (bool, error)         { return x.found, x.err }
func (x accountFailureTx) UpdateNickname(int, string) (bool, error) { return x.found, x.err }
func (x accountFailureTx) UpdatePassword(int, string) (bool, error) { return x.found, x.err }

type accountTxManager struct{ tx Tx }

func (m accountTxManager) InTx(_ context.Context, fn func(Tx) error) error { return fn(m.tx) }
func accountServer(p Port, x Tx) *Server {
	return New(p, accountTxManager{x}, WithAuthConfig(AuthConfig{BcryptCost: crypto.MinPasswordCost, SessionTTL: time.Hour, RegistrationOpen: true}))
}

func TestAccountPersistenceFailures(t *testing.T) {
	hash, err := crypto.HashPassword("password123", crypto.MinPasswordCost)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		fn   func(*Server) error
		port accountFailurePort
		tx   accountFailureTx
		want error
	}{
		{"login lookup", func(a *Server) error { _, e := a.Login(ctx, "alice", "password123"); return e }, accountFailurePort{err: errPersistence}, accountFailureTx{}, errPersistence},
		{"login insert session", func(a *Server) error { _, e := a.Login(ctx, "alice", "password123"); return e }, accountFailurePort{credentials: Credentials{PasswordHash: hash}}, accountFailureTx{err: errPersistence}, errPersistence},
		{"register insert session", func(a *Server) error { _, e := a.Register(ctx, "alice", "password123"); return e }, accountFailurePort{}, accountFailureTx{err: errPersistence}, errPersistence},
		{"register insert user", func(a *Server) error { _, e := a.Register(ctx, "alice", "password123"); return e }, accountFailurePort{}, accountFailureTx{operation: "user", err: errPersistence}, errPersistence},
		{"register conflict lookup fails", func(a *Server) error { _, e := a.Register(ctx, "alice", "password123"); return e }, accountFailurePort{err: errPersistence}, accountFailureTx{operation: "user", err: apperrors.ErrInvalid}, apperrors.ErrInvalid},
		{"key insert", func(a *Server) error { _, e := a.CreateKey(ctx, 1, KeyInput{}); return e }, accountFailurePort{}, accountFailureTx{err: errPersistence}, errPersistence},
		{"key update", func(a *Server) error {
			v := true
			_, e := a.UpdateKey(ctx, 1, 1, KeyUpdateInput{IsActive: &v})
			return e
		}, accountFailurePort{}, accountFailureTx{err: errPersistence}, errPersistence},
		{"key update missing", func(a *Server) error {
			v := true
			_, e := a.UpdateKey(ctx, 1, 1, KeyUpdateInput{IsActive: &v})
			return e
		}, accountFailurePort{}, accountFailureTx{}, apperrors.ErrNotFound},
		{"key release reservations", func(a *Server) error { return a.DeleteKey(ctx, 1, 1) }, accountFailurePort{}, accountFailureTx{operation: "reservations", err: errPersistence}, errPersistence},
		{"key delete", func(a *Server) error { return a.DeleteKey(ctx, 1, 1) }, accountFailurePort{}, accountFailureTx{err: errPersistence}, errPersistence},
		{"profile password validation", func(a *Server) error {
			_, e := a.UpdateProfile(ctx, 1, ProfileUpdateInput{NewPassword: "short"})
			return e
		}, accountFailurePort{}, accountFailureTx{}, ErrPasswordLength},
		{"profile lookup", func(a *Server) error {
			_, e := a.UpdateProfile(ctx, 1, ProfileUpdateInput{NewPassword: "password123"})
			return e
		}, accountFailurePort{err: errPersistence}, accountFailureTx{}, errPersistence},
		{"profile nickname", func(a *Server) error {
			n := "alice"
			_, e := a.UpdateProfile(ctx, 1, ProfileUpdateInput{Nickname: &n})
			return e
		}, accountFailurePort{}, accountFailureTx{err: errPersistence}, errPersistence},
		{"profile nickname missing", func(a *Server) error {
			n := "alice"
			_, e := a.UpdateProfile(ctx, 1, ProfileUpdateInput{Nickname: &n})
			return e
		}, accountFailurePort{}, accountFailureTx{}, apperrors.ErrNotFound},
		{"profile password update", func(a *Server) error {
			_, e := a.UpdateProfile(ctx, 1, ProfileUpdateInput{CurrentPassword: "password123", NewPassword: "password456"})
			return e
		}, accountFailurePort{credentials: Credentials{PasswordHash: hash}}, accountFailureTx{err: errPersistence}, errPersistence},
		{"profile password missing", func(a *Server) error {
			_, e := a.UpdateProfile(ctx, 1, ProfileUpdateInput{CurrentPassword: "password123", NewPassword: "password456"})
			return e
		}, accountFailurePort{credentials: Credentials{PasswordHash: hash}}, accountFailureTx{}, apperrors.ErrNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := accountServer(tc.port, tc.tx)
			if e := tc.fn(a); !errors.Is(e, tc.want) {
				t.Fatalf("err=%v want=%v", e, tc.want)
			}
		})
	}
	a := accountServer(accountFailurePort{session: Session{ExpiresAt: time.Unix(0, 0)}}, nil)
	if _, e := a.AuthenticateSession(ctx, "token"); !errors.Is(e, apperrors.ErrNotFound) {
		t.Fatal(e)
	}
	if e := a.Logout(ctx, " "); e != nil {
		t.Fatal(e)
	}
}

func TestAccountCryptoFailures(t *testing.T) {
	hash, e := crypto.HashPassword("password123", crypto.MinPasswordCost)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	a := accountServer(accountFailurePort{credentials: Credentials{PasswordHash: hash}}, nil)
	a.auth.BcryptCost = crypto.MinPasswordCost - 1
	if _, e := a.Register(ctx, "alice", "password123"); e == nil {
		t.Fatal("invalid bcrypt cost accepted")
	}
	if _, e := a.UpdateProfile(ctx, 1, ProfileUpdateInput{NewPassword: "password456", CurrentPassword: "password123"}); e == nil {
		t.Fatal("invalid bcrypt cost accepted")
	}
}

func TestAccountHTTPValidation(t *testing.T) {
	a := accountServer(accountFailurePort{err: errPersistence}, accountFailureTx{})
	mux := http.NewServeMux()
	a.RegisterAdminRoutes(mux)
	for _, tc := range []struct {
		name, method, path, body string
		identity                 bool
		status                   int
	}{
		{"profile anonymous", "GET", "/admin/profile", "", false, 401},
		{"keys anonymous", "GET", "/admin/keys", "", false, 401},
		{"key anonymous", "PUT", "/admin/keys/1", "{}", false, 401},
		{"profile method", "DELETE", "/admin/profile", "", true, 405},
		{"keys method", "DELETE", "/admin/keys", "", true, 405},
		{"key method", "GET", "/admin/keys/1", "", true, 405},
		{"key id", "PUT", "/admin/keys/nope", "{}", true, 400},
		{"key json", "PUT", "/admin/keys/1", "{", true, 400},
		{"keys json", "POST", "/admin/keys", "{", true, 400},
		{"profile json", "PUT", "/admin/profile", "{", true, 400},
		{"register method", "GET", "/admin/auth/register", "", false, 405},
		{"register json", "POST", "/admin/auth/register", "{", false, 400},
		{"login method", "GET", "/admin/auth/login", "", false, 405},
		{"login json", "POST", "/admin/auth/login", "{", false, 400},
		{"logout method", "GET", "/admin/auth/logout", "", false, 405},
		{"logout storage", "POST", "/admin/auth/logout", "", false, 500},
		{"me method", "POST", "/admin/auth/me", "", false, 405},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.identity {
				r = r.WithContext(httpcommon.WithIdentity(r.Context(), httpcommon.Identity{UserID: 1}))
			}
			if tc.name == "logout storage" {
				r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "token"})
			}
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{{"invalid", apperrors.ErrInvalid, 400}, {"unavailable", errPersistence, 500}} {
		t.Run(tc.name, func(t *testing.T) {
			r := authError(tc.err)
			if r.Status != tc.status {
				t.Fatalf("result=%+v", r)
			}
		})
	}
}

func TestAccountDefaultsAndKeyDisplay(t *testing.T) {
	active := false
	if _, e := accountServer(accountFailurePort{}, accountFailureTx{}).CreateKey(context.Background(), 1, KeyInput{IsActive: &active}); e != nil {
		t.Fatal(e)
	}
	a := New(nil, nil, WithClock(nil), WithAuthConfig(AuthConfig{}))
	if a.auth.SessionTTL <= 0 || a.auth.BcryptCost != crypto.DefaultPasswordCost {
		t.Fatalf("defaults=%+v", a.auth)
	}
	if extractKeySuffix("sk-x") != "sk-x" {
		t.Fatal("short keys must remain intact")
	}
	if string(normalizePermissions([]byte(`{"models":["allowed"]}`))) != `{"models":["allowed"]}` {
		t.Fatal("explicit permissions lost")
	}
}
