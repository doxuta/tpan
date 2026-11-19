package claudecode

// Thân tạm của đợt 0; thợ đợt 1 thay (hợp đồng ở types.go).

import (
	"context"
	"errors"

	"github.com/doxuta/tpan/internal/ai"
)

var errTodo = errors.New("claudecode: chưa làm (M8 đợt 1)")

func New(cfg Config) (*Brain, error) { return nil, errTodo }

func (b *Brain) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error) {
	_, _ = b.cfg, b.promptFile
	return nil, errTodo
}

func Args(cfg Config, job ai.Job, promptFile string) []string { return nil }

func Gate(in Init) error { return errTodo }

func (p *Parser) Line(line []byte) []ai.Event { _ = p.tools; return nil }

func Version(ctx context.Context, cfg Config) (string, error) { return "", errTodo }

func Auth(ctx context.Context, cfg Config) (AuthStatus, error) { return AuthStatus{}, errTodo }
