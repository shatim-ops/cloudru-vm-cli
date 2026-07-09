package auth

import "context"

// TokenProvider abstracts credential sources.
type TokenProvider interface {
	// Token returns a valid IAM token, refreshing if needed.
	Token(ctx context.Context) (Token, error)
}
