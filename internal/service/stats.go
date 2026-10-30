package service

import (
	"context"
	"database/sql"
	"fmt"
)

// Stats — счётчики дашборда (04-api.md §5).
type Stats struct {
	Active     int `json:"active"`
	InProgress int `json:"in_progress"`
	Done7d     int `json:"done_7d"`
	Overdue    int `json:"overdue"`
	Due24h     int `json:"due_24h"`
	Total      int `json:"total"`
}

// StatsService считает статистику одним запросом (docs/03-data-model.md §4).
type StatsService struct {
	db *sql.DB
}

// NewStatsService собирает сервис статистики.
func NewStatsService(db *sql.DB) *StatsService {
	return &StatsService{db: db}
}

// Get возвращает счётчики пользователя.
func (s *StatsService) Get(ctx context.Context, userID string) (*Stats, error) {
	const query = `
		SELECT
			count(*) FILTER (WHERE status <> 'done')                                       AS active,
			count(*) FILTER (WHERE status = 'in_progress')                                 AS in_progress,
			count(*) FILTER (WHERE status = 'done'
			                 AND updated_at >= now() - interval '7 days')                  AS done_7d,
			count(*) FILTER (WHERE status <> 'done' AND due_at IS NOT NULL
			                 AND due_at < now())                                           AS overdue,
			count(*) FILTER (WHERE status <> 'done' AND due_at IS NOT NULL
			                 AND due_at >= now()
			                 AND due_at < now() + interval '24 hours')                      AS due_24h,
			count(*)                                                                       AS total
		FROM tasks
		WHERE user_id = $1`

	var stats Stats
	err := s.db.QueryRowContext(ctx, query, userID).Scan(
		&stats.Active, &stats.InProgress, &stats.Done7d,
		&stats.Overdue, &stats.Due24h, &stats.Total,
	)
	if err != nil {
		return nil, fmt.Errorf("статистика пользователя: %w", err)
	}
	return &stats, nil
}
