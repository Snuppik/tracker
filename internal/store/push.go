package store

import (
	"context"
	"fmt"

	"tracker/internal/model"
)

// PushStore — доступ к подпискам браузеров на Web Push.
type PushStore struct {
	db DBTX
}

// NewPushStore создаёт хранилище поверх *sql.DB или *sql.Tx.
func NewPushStore(db DBTX) *PushStore {
	return &PushStore{db: db}
}

// Save сохраняет или обновляет подписку по endpoint (идемпотентно, 04-api.md §7).
func (s *PushStore) Save(ctx context.Context, sub *model.PushSubscription) error {
	const query = `
		INSERT INTO push_subscriptions (id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, now(), now())
		ON CONFLICT (endpoint) DO UPDATE SET
			user_id      = EXCLUDED.user_id,
			p256dh       = EXCLUDED.p256dh,
			auth         = EXCLUDED.auth,
			user_agent   = EXCLUDED.user_agent,
			last_seen_at = now()`

	_, err := s.db.ExecContext(ctx, query, sub.ID, sub.UserID, sub.Endpoint, sub.P256dh, sub.Auth, sub.UserAgent)
	if err != nil {
		return fmt.Errorf("сохранение push-подписки: %w", err)
	}
	return nil
}

// DeleteByEndpoint удаляет подписку пользователя.
func (s *PushStore) DeleteByEndpoint(ctx context.Context, userID, endpoint string) error {
	const query = `DELETE FROM push_subscriptions WHERE endpoint = $1 AND user_id = $2`

	if _, err := s.db.ExecContext(ctx, query, endpoint, userID); err != nil {
		return fmt.Errorf("удаление push-подписки: %w", err)
	}
	return nil
}

// ListByUser возвращает все подписки пользователя.
func (s *PushStore) ListByUser(ctx context.Context, userID string) ([]model.PushSubscription, error) {
	const query = `
		SELECT id, user_id, endpoint, p256dh, auth
		FROM push_subscriptions
		WHERE user_id = $1
		ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("список push-подписок: %w", err)
	}
	defer rows.Close()

	items := make([]model.PushSubscription, 0, 2)
	for rows.Next() {
		var sub model.PushSubscription
		if err := rows.Scan(&sub.ID, &sub.UserID, &sub.Endpoint, &sub.P256dh, &sub.Auth); err != nil {
			return nil, fmt.Errorf("чтение push-подписки: %w", err)
		}
		items = append(items, sub)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("обход push-подписок: %w", err)
	}
	return items, nil
}
