// Package mcp chạy MCP server của tpan qua stdio (mục 8.3 của
// docs/KE-HOACH.md), dùng SDK Go chính thức github.com/modelcontextprotocol/go-sdk
// (bản ghi trong go.mod). Gói này chỉ là lớp nối: mỗi công cụ trong
// tools.Catalog() thành một công cụ MCP cùng tên, mô tả và InputSchema; mọi lần
// gọi đi qua tools.Session.Call (chỗ che dữ liệu và ghi phiên duy nhất).
//
// Hợp đồng M7. Thợ của đợt 2 viết ở server.go (cùng test, dùng client của SDK
// nối bằng transport trong bộ nhớ):
//
//	// NewServer dựng MCP server với mọi công cụ của tools.Catalog() gọi qua
//	// sess. Tên server "tpan", phiên bản buildinfo.Version. Lỗi của công cụ
//	// (*tools.Error) trả về thành kết quả IsError với text đã che, không phải
//	// lỗi giao thức. Lúc client gửi initialize, ghi clientInfo (tên và phiên
//	// bản) vào phiên bằng sess.SetClient. Mỗi công cụ có Annotations:
//	// ReadOnlyHint cho KindRead và KindPlan (lập Plan không đổi gì trên máy).
//	func NewServer(sess *tools.Session) *sdk.Server
//	// Serve chạy server trên stdin/stdout cho tới khi client đóng kết nối hoặc
//	// ctx bị huỷ. stdout chỉ chở JSON-RPC: mọi log ra stderr.
//	func Serve(ctx context.Context, sess *tools.Session) error
package mcp
