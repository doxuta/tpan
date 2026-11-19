package bridge

// Thân tạm của đợt 0; thợ đợt 2 thay (hợp đồng ở types.go).

import (
	"context"
	"errors"
	"io"

	"github.com/doxuta/tpan/internal/ai"
)

var errTodo = errors.New("bridge: chưa làm (M8 đợt 2)")

type hubState struct{}

func Listen(ctx context.Context, path string) (*Hub, error) { return nil, errTodo }

func (h *Hub) Run(ctx context.Context, job ai.Job) (<-chan ai.Event, error) { return nil, errTodo }

func (h *Hub) Status() Status { _ = h.st; return Status{} }

func Link(ctx context.Context, path string, in io.Reader, out io.Writer) error { return errTodo }

func (c *Client) Serve(ctx context.Context) error { return errTodo }
