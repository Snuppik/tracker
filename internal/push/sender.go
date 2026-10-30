// Package push отправляет Web Push уведомления через VAPID (T-14).
package push

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"tracker/internal/model"
)

// ErrSubscriptionGone означает, что endpoint больше не существует (404/410)
// и подписку надо удалить из БД.
var ErrSubscriptionGone = errors.New("push subscription is gone")

// sendTimeout ограничивает одну отправку: мёртвые endpoints не должны
// блокировать тик планировщика (02-architecture.md §5).
const sendTimeout = 5 * time.Second

// Payload — тело пуша, которое читает sw.js (docs/06-reminders-push.md §4.5).
type Payload struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	TaskID string `json:"task_id,omitempty"`
}

// Sender — обёртка над webpush-go с ключами из env.
type Sender struct {
	publicKey  string
	privateKey string
	subject    string
	enabled    bool
}

// New создаёт отправитель. Без ключей Enabled() == false, in-app лента работает.
func New(publicKey, privateKey, subject string) *Sender {
	return &Sender{
		publicKey:  publicKey,
		privateKey: privateKey,
		subject:    subject,
		enabled:    publicKey != "" && privateKey != "",
	}
}

// Enabled сообщает, настроен ли VAPID.
func (s *Sender) Enabled() bool {
	return s.enabled
}

// PublicKey возвращает публичный VAPID-ключ.
func (s *Sender) PublicKey() string {
	return s.publicKey
}

// Send отправляет уведомление в одну подписку.
func (s *Sender) Send(ctx context.Context, sub model.PushSubscription, payload Payload) error {
	if !s.enabled {
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("сериализация пуша: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	subscription := &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys: webpush.Keys{
			P256dh: sub.P256dh,
			Auth:   sub.Auth,
		},
	}
	opts := &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             30,
	}

	resp, err := webpush.SendNotificationWithContext(ctx, body, subscription, opts)
	if err != nil {
		return fmt.Errorf("отправка пуша: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return ErrSubscriptionGone
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("push endpoint вернул %d", resp.StatusCode)
	}
	return nil
}
