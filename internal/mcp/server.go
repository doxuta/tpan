package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/doxuta/tpan/internal/buildinfo"
	"github.com/doxuta/tpan/internal/tools"
)

// maxClientLen giới hạn tên client lưu vào ai_sessions.client: clientInfo do
// client tự khai, không được để nó nhồi chuỗi dài tuỳ ý vào DB và giao diện.
const maxClientLen = 200

// internalErrorText là thứ agent nhận khi công cụ lỗi mà không phải *tools.Error.
// Hợp đồng nói Call luôn trả *tools.Error; lỗi khác là lỗi lập trình, và text
// của nó CHƯA qua bộ che, nên không bao giờ đưa nguyên cho agent. Viết tiếng
// Anh như mọi lỗi khác trả cho agent (tools.Session.agentError).
const internalErrorText = "internal: tpan hit an internal error while running the tool"

// NewServer dựng MCP server với mọi công cụ của tools.Catalog() gọi qua sess.
// Xem hợp đồng ở doc.go.
func NewServer(sess *tools.Session) *sdk.Server {
	return newServer(sess, os.Stderr)
}

// newServer tách nơi ghi log để test im lặng được.
func newServer(sess *tools.Session, logw io.Writer) *sdk.Server {
	log := slog.New(slog.NewTextHandler(logw, &slog.HandlerOptions{Level: slog.LevelWarn}))
	srv := sdk.NewServer(&sdk.Implementation{Name: "tpan", Version: buildinfo.Version}, &sdk.ServerOptions{Logger: log})
	for _, t := range tools.Catalog() {
		srv.AddTool(sdkTool(t), toolHandler(sess, t.Name, log))
	}
	srv.AddReceivingMiddleware(recordClient(sess, log))
	return srv
}

// sdkTool đổi một công cụ của danh mục sang dạng của SDK. InputSchema giữ
// nguyên json.RawMessage: SDK chấp nhận mọi giá trị Marshal được thành object
// có type "object", và gửi đúng từng byte cho client.
func sdkTool(t tools.Tool) *sdk.Tool {
	closed := false // chỉ chạm máy này, không ra "thế giới mở" (web, API ngoài)
	ann := &sdk.ToolAnnotations{
		// Lập Plan cũng không đổi gì trên máy: nó chỉ lưu một Plan chờ duyệt.
		ReadOnlyHint:  t.Kind == tools.KindRead || t.Kind == tools.KindPlan,
		OpenWorldHint: &closed,
	}
	return &sdk.Tool{Name: t.Name, Description: t.Description, InputSchema: t.InputSchema, Annotations: ann}
}

// toolHandler chuyển một lần gọi công cụ sang sess.Call, chỗ duy nhất che dữ
// liệu và ghi phiên. Lỗi của công cụ thành kết quả IsError (agent đọc được và
// tự sửa), không thành lỗi giao thức.
func toolHandler(sess *tools.Session, name string, log *slog.Logger) sdk.ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		args := req.Params.Arguments
		// Công cụ không đối số: client có thể gửi thiếu hẳn hoặc null, mà Call
		// giải JSON chặt.
		if len(args) == 0 || string(args) == "null" {
			args = json.RawMessage(`{}`)
		}
		out, err := sess.Call(ctx, name, args)
		if err != nil {
			var te *tools.Error
			text := internalErrorText
			if errors.As(err, &te) {
				text = te.Error()
			} else {
				// Chỉ ghi kiểu lỗi, không ghi text: chạy qua `ssh <máy> tpan
				// mcp` thì stderr cũng về máy của agent, mà text này chưa che.
				log.Error("công cụ trả lỗi không phải *tools.Error", "tool", name, "type", fmt.Sprintf("%T", err))
			}
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: text}}}, nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(out)}}}, nil
	}
}

// recordClient ghi clientInfo vào phiên ngay sau lần bắt tay. SDK v1.8.0 có
// hai kiểu bắt tay: "initialize" cũ và "server/discover" (SEP-2575, giao thức
// 2026-07-28, client của SDK thử nó trước); cả hai đều để clientInfo ở
// ServerSession.InitializeParams. Làm ở middleware, đồng bộ với chính request
// bắt tay, thay vì InitializedHandler: handler đó chỉ chạy khi có
// notifications/initialized, mà đường discover không gửi.
func recordClient(sess *tools.Session, log *slog.Logger) sdk.Middleware {
	var (
		mu   sync.Mutex
		last string
	)
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil || (method != "initialize" && method != "server/discover") {
				return res, err
			}
			ss, ok := req.GetSession().(*sdk.ServerSession)
			if !ok {
				return res, err
			}
			p := ss.InitializeParams()
			if p == nil || p.ClientInfo == nil {
				return res, err
			}
			client := clientName(p.ClientInfo)
			mu.Lock()
			defer mu.Unlock()
			if client == "" || client == last {
				return res, err
			}
			// Ghi hỏng không làm hỏng bắt tay: agent vẫn làm việc được, mọi lần
			// gọi vẫn vào ai_calls; chỉ thiếu tên client.
			if serr := sess.SetClient(ctx, client); serr != nil {
				log.Error("ghi tên client MCP vào phiên", "session", sess.ID(), "error", serr)
				return res, err
			}
			last = client
			return res, err
		}
	}
}

// clientName là "tên phiên-bản", bỏ ký tự điều khiển, cắt ở maxClientLen.
func clientName(impl *sdk.Implementation) string {
	s := strings.TrimSpace(impl.Name + " " + impl.Version)
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	for len(s) > maxClientLen {
		_, n := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-n]
	}
	return s
}

// Serve chạy server trên stdin/stdout cho tới khi client đóng kết nối hoặc ctx
// bị huỷ. stdout chỉ chở JSON-RPC: SDK ghi log ra stderr (newServer).
func Serve(ctx context.Context, sess *tools.Session) error {
	return serveOn(ctx, sess, &sdk.StdioTransport{}, os.Stderr)
}

// serveOn tách transport để test được đường "agent ngắt kết nối" bằng pipe.
func serveOn(ctx context.Context, sess *tools.Session, t sdk.Transport, logw io.Writer) error {
	err := newServer(sess, logw).Run(ctx, t)
	// Ctrl+C, systemd hay SSH đóng kết nối là cách dừng bình thường, không phải lỗi.
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) || errors.Is(err, sdk.ErrConnectionClosed) {
		return nil
	}
	return fmt.Errorf("MCP server qua stdio của phiên %s: %w", sess.ID(), err)
}
