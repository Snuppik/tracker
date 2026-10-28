package model

import "time"

// PushSubscription — подписка браузера на Web Push.
type PushSubscription struct {
	ID         string    `json:"-"`
	UserID     string    `json:"-"`
	Endpoint   string    `json:"endpoint"`
	P256dh     string    `json:"-"`
	Auth       string    `json:"-"`
	UserAgent  string    `json:"-"`
	CreatedAt  time.Time `json:"-"`
	LastSeenAt time.Time `json:"-"`
}
