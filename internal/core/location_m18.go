package core

import "github.com/doxuta/tpan/internal/site"

// Nơi một bản sao lưu nằm (M18): trường location của API (GET
// /api/v1/backups) và của công cụ MCP list_backups. Tính lúc đọc từ ba cột
// của dòng và mã đích đang đặt; không lưu trong DB.
const (
	LocationMachine     = "machine"      // chỉ trên máy
	LocationRemote      = "remote"       // chỉ trên kho, ở đích đang đặt
	LocationBoth        = "both"         // trên máy và trên kho, ở đích đang đặt
	LocationOtherTarget = "other_target" // đã lên một đích khác đích đang đặt
	LocationPushFailed  = "push_failed"  // lần đẩy gần nhất lỗi
)

// BackupLocation cho biết bản b nằm ở đâu. hasTarget=false (chưa đặt đích):
// dòng stored nào cũng là của một đích khác. Chỉ trả một nhãn, không kèm khoá
// object, lỗi hay tên kho: người đọc (user thường, agent) không cần những thứ
// đó để biết bản nằm ở đâu, và chúng có thể nêu bucket hay đường dẫn.
func BackupLocation(b Backup, cur string, hasTarget bool) string {
	switch b.Remote {
	case site.RemoteFailed:
		return LocationPushFailed
	case site.RemoteStored:
		if !hasTarget || b.RemoteTarget != cur {
			return LocationOtherTarget
		}
		if b.Local {
			return LocationBoth
		}
		return LocationRemote
	}
	return LocationMachine
}
