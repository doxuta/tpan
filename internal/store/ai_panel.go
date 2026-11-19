package store

// Thân tạm của đợt 0; thợ đợt 1 thay bằng bản thật (hợp đồng ở ai_panel_types.go).

import (
	"context"
	"errors"
	"time"

	"github.com/doxuta/tpan/internal/engine"
)

var errTodo = errors.New("store: chưa làm (M8 đợt 1)")

func (s *Store) CreateAITurn(ctx context.Context, t AITurn) error { return errTodo }

func (s *Store) FinishAITurn(ctx context.Context, sessionID string, seq int, f TurnEnd) error {
	return errTodo
}

func (s *Store) AITurns(ctx context.Context, sessionID string) ([]AITurn, error) {
	return nil, errTodo
}

func (s *Store) AITurn(ctx context.Context, sessionID string, seq int) (AITurn, error) {
	return AITurn{}, errTodo
}

func (s *Store) CountAITurnsSince(ctx context.Context, since time.Time) (int, error) {
	return 0, errTodo
}

func (s *Store) FailRunningAITurns(ctx context.Context, msg string, at time.Time) (int, error) {
	return 0, errTodo
}

func (s *Store) AISessions(ctx context.Context, limit int) ([]AISessionSummary, error) {
	return nil, errTodo
}

func (s *Store) PlansBySession(ctx context.Context, id string) ([]engine.PendingPlan, error) {
	return nil, errTodo
}

func (s *Store) AddAIView(ctx context.Context, v AIView) error { return errTodo }

func (s *Store) AIViews(ctx context.Context, limit int) ([]AIView, error) { return nil, errTodo }
