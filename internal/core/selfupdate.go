package core

// `tpan self-update` (mục 6.11 của docs/KE-HOACH.md). Hợp đồng M12 đợt 0; thân
// do thợ đợt 2 viết. Chỉ CLI gọi: job thay file chạy và khởi động lại
// tpan.service, chạy từ panel thì cắt chính tiến trình đang chạy job.
//
// Plan "self.update":
//  1. steps.SelfDownload: tải <base>/tpan-linux-<Arch> và <base>/SHA256SUMS vào
//     TpanBin+".new", kiểm sha256, chạy `<file> version` để đọc bản mới. base
//     là https://...; http chỉ nhận với host loopback (e2e trên máy ảo).
//  2. steps.SelfInstall: TpanBin → TpanBin+".old", TpanBin+".new" → TpanBin
//     (rename, cùng thư mục). Undo đổi ngược.
//  3. steps.ServiceRestart{tpan, OnlyIfActive: true}.
//  Kiểm chứng: `TpanBin version` chạy được. Bản tải về trùng bản đang chạy thì
//  SelfDownload báo và các bước sau được bỏ qua (Check true).
// Plan "self.rollback": steps.SelfInstall{New: TpanBin+".old"...} đổi ngược
// rồi restart. Không có .old thì từ chối self_no_old.

import "context"

// Status trả phần self-update của màn Cập nhật. Chỉ đọc.
func (s *SelfUpdate) Status(ctx context.Context) (SelfStatus, error) {
	return SelfStatus{}, errM12Todo
}

// Prepare dựng Plan "self.update". baseURL rỗng thì đọc settings, rồi DefaultSelfURL.
func (s *SelfUpdate) Prepare(ctx context.Context, baseURL string) (*Planned, error) {
	return nil, errM12Todo
}

// PrepareRollback dựng Plan "self.rollback".
func (s *SelfUpdate) PrepareRollback(ctx context.Context) (*Planned, error) {
	return nil, errM12Todo
}
