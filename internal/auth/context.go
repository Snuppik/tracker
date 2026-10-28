package auth

import "context"

type userIDKey struct{}

// WithUserID кладёт идентификатор пользователя из токена в контекст запроса.
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserID достаёт идентификатор пользователя. Второе значение — признак наличия.
func UserID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey{}).(string)
	return id, ok && id != ""
}
