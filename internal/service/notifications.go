package service

import (
	"context"
	"database/sql"
	"strings"
	"uuid"

	"tracker/internal/model"
	"tracker/internal/store"
)

// NotificationService — лента уведомлений и отметки о прочтении.
type NotificationService struct {
	db *sql.DB
}

// NewNotificationService собирает сервис уведомлений.
func NewNotificationService(db *sql.DB) *NotificationService {
	return &NotificationService{db: db}
}

// NotificationList — страница ленты и полное число непрочитанных.
type NotificationList struct {
	Items  []model.Notification
	Unread int
}

// List возвращает ленту и счётчик непрочитанных (всегда полный, 04-api.md §6).
func (s *NotificationService) List(ctx context.Context, userID string, unreadOnly bool, limit int) (*NotificationList, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	notifications := store.NewNotificationStore(s.db)
	items, err := notifications.List(ctx, userID, unreadOnly, limit)
	if err != nil {
		return nil, err
	}
	unread, err := notifications.CountUnread(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &NotificationList{Items: items, Unread: unread}, nil
}

// MarkRead отмечает уведомление прочитанным.
func (s *NotificationService) MarkRead(ctx context.Context, userID, id string) error {
	return store.NewNotificationStore(s.db).MarkRead(ctx, userID, id)
}

// MarkAllRead отмечает прочитанными все уведомления пользователя.
func (s *NotificationService) MarkAllRead(ctx context.Context, userID string) error {
	return store.NewNotificationStore(s.db).MarkAllRead(ctx, userID)
}

// PushService — подписки браузеров на Web Push.
type PushService struct {
	db        *sql.DB
	publicKey string
	enabled   bool
}

// NewPushService собирает сервис подписок.
func NewPushService(db *sql.DB, publicKey string, enabled bool) *PushService {
	return &PushService{db: db, publicKey: publicKey, enabled: enabled}
}

// PublicKey возвращает VAPID-публичный ключ. Пустая строка = push выключен.
func (s *PushService) PublicKey() string {
	return s.publicKey
}

// Save сохраняет подписку браузера.
func (s *PushService) Save(ctx context.Context, userID, endpoint, p256dh, auth, userAgent string) error {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return model.NewValidation("endpoint", "обязательное поле")
	}
	if p256dh == "" {
		return model.NewValidation("keys.p256dh", "обязательное поле")
	}
	if auth == "" {
		return model.NewValidation("keys.auth", "обязательное поле")
	}

	sub := &model.PushSubscription{
		ID:        uuid.NewV7().String(),
		UserID:    userID,
		Endpoint:  endpoint,
		P256dh:    p256dh,
		Auth:      auth,
		UserAgent: userAgent,
	}
	return store.NewPushStore(s.db).Save(ctx, sub)
}

// Delete удаляет подписку по endpoint.
func (s *PushService) Delete(ctx context.Context, userID, endpoint string) error {
	return store.NewPushStore(s.db).DeleteByEndpoint(ctx, userID, endpoint)
}
