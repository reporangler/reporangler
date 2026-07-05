package auth

import (
	"context"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/reporangler/reporangler/internal/types"
)

// Service implements login (token issue) and token introspection.
type Service struct {
	store     *Store
	tokenLife time.Duration
}

// NewService constructs the auth service with a token lifetime.
func NewService(store *Store, tokenLife time.Duration) *Service {
	return &Service{store: store, tokenLife: tokenLife}
}

// Login authenticates database credentials and issues a login token.
// Login type "http-basic" is coerced to "database"; "ldap" is not implemented.
func (s *Service) Login(ctx context.Context, loginType, username, password string) (types.User, error) {
	switch strings.ToLower(loginType) {
	case "", "database", "http-basic":
		// database path
	default:
		// "ldap" and anything else are unsupported for now
		return types.User{}, ErrUnauthorized
	}

	user, hash, err := s.store.userByUsername(ctx, username)
	if err != nil {
		return types.User{}, ErrUnauthorized
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return types.User{}, ErrUnauthorized
	}

	token, err := GenerateToken()
	if err != nil {
		return types.User{}, err
	}
	if err := s.store.insertLoginToken(ctx, user.ID, token, time.Now().Add(s.tokenLife)); err != nil {
		return types.User{}, err
	}
	user.Token = token
	return user, nil
}

// CheckToken validates an Authorization header value and returns the user.
// The literal public token is rejected (as lib-reporangler's AuthClient did).
func (s *Service) CheckToken(ctx context.Context, authHeader string) (types.User, error) {
	token := StripBearer(authHeader)
	if token == "" || token == types.PublicToken {
		return types.User{}, ErrUnauthorized
	}
	user, err := s.store.userByToken(ctx, token)
	if err != nil {
		return types.User{}, err
	}
	user.Token = token
	return user, nil
}
