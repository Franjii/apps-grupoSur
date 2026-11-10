package models

import "context"

type contextKey string

const (
	csrfTokenContextKey contextKey = "csrf_token"
	userContextKey      contextKey = "user"
)

// ContextWithCSRFToken returns a new context with the CSRF token set.
func ContextWithCSRFToken(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfTokenContextKey, token)
}

// CSRFTokenFromContext extracts the CSRF token from the context.
func CSRFTokenFromContext(ctx context.Context) string {
	token, _ := ctx.Value(csrfTokenContextKey).(string)
	return token
}

// ContextWithUser returns a new context with the authenticated user set.
func ContextWithUser(ctx context.Context, user *User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// UserFromContext extracts the authenticated user from the request context.
func UserFromContext(ctx context.Context) *User {
	user, _ := ctx.Value(userContextKey).(*User)
	return user
}
