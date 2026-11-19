package ai

// Thân tạm của đợt 0; thợ đợt 2 thay bằng bản thật (hợp đồng ở panel_types.go).

import (
	"context"
	"errors"
)

var errTodo = errors.New("ai: chưa làm (M8 đợt 2)")

// panelState: thợ đợt 2 thay bằng trạng thái thật.
type panelState struct{}

func (p *Panel) Preview(ctx context.Context, in AskInput) (AskPreview, error) {
	return AskPreview{}, errTodo
}

func (p *Panel) Ask(ctx context.Context, in AskInput) (AskStarted, error) {
	return AskStarted{}, errTodo
}

func (p *Panel) Subscribe(ctx context.Context, sessionID string, seq int) (past []Event, live <-chan Event, cancel func(), ok bool, err error) {
	return nil, nil, func() {}, false, errTodo
}

func (p *Panel) Session(ctx context.Context, id string) (SessionDetail, error) {
	return SessionDetail{}, errTodo
}

func (p *Panel) Recover(ctx context.Context) error { _ = p.st; return errTodo }
