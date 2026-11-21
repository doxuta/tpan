package core

// Hợp đồng của M15: nút cập nhật TPAN trong panel (mục 6.14 của
// docs/KE-HOACH.md). File này chỉ có kiểu, hằng và chú thích hợp đồng; thân
// hàm nằm ở selfupdate_m15.go (và phần sửa PrepareRollback ở selfupdate.go).
//
// Vì sao phải có cơ chế riêng: lượt cập nhật kết thúc bằng `systemctl restart
// tpan`. Lệnh nào chạy như tiến trình con của tpan.service thì bị systemd dừng
// cùng dịch vụ ở đúng bước đó (đã thử trên máy ảo, docs/bang-chung/
// M15-thu-co-che.log, phần 1: bước restart báo "context canceled", engine trả
// file cũ về, nhật ký không ghi được). Vì vậy panel không tự chạy Plan
// self.update. Nó nhờ systemd khởi một đơn vị tạm (transient unit) nằm ở cgroup
// riêng, và đơn vị đó chạy lại chính Plan mà `tpan self-update` vẫn chạy:
//
//	trình duyệt ── POST /api/v1/self/update ──▶ tpan serve (tpan.service)
//	                                              │ SelfUpdate.Start
//	                                              ▼
//	   systemd-run --unit tpan-self-update --collect \
//	       /usr/local/bin/tpan internal self-update
//	                                              │ (cgroup riêng, PID 1 là cha)
//	                                              ▼
//	   SelfUpdate.Run: Plan self.update như CLI ── systemctl restart tpan
//	                   rồi ghi kết quả vào settings và audit
//
// Dòng lệnh của đơn vị tạm là hằng: không có chữ nào lấy từ request. Ai bấm và
// từ IP nào đi qua DB (SettingSelfLast), không qua argv.

import (
	"time"
)

// SelfUnit là tên đơn vị systemd tạm chạy một lượt cập nhật hoặc quay về từ
// panel (systemd tự thêm đuôi .service). Tên cố định là cách bảo đảm "chỉ một
// lượt cùng lúc": systemd từ chối khởi đơn vị thứ hai cùng tên. Log của lượt
// chạy đọc bằng `journalctl -u tpan-self-update`, còn đó sau khi đơn vị đã dọn.
const SelfUnit = "tpan-self-update"

// SettingSelfLast giữ SelfRun (JSON) của lượt gần nhất chạy từ panel. Start ghi
// nó với Status SelfRunning trước khi khởi đơn vị tạm; Run ghi đè khi xong.
// Nằm ở settings để màn đọc lại được sau khi panel restart.
const SettingSelfLast = "update.self_last"

// Loại lượt chạy (SelfRun.Action) và tham số action của Start, Run.
const (
	SelfActionUpdate   = "update"
	SelfActionRollback = "rollback"
)

// Trạng thái của một lượt chạy (SelfRun.Status).
const (
	// SelfRunning: Start đã ghi, đơn vị tạm chưa ghi kết quả.
	SelfRunning = "running"
	// SelfOK: job thành công và file chạy ở TpanBin đã là bản khác (To khác From).
	SelfOK = "ok"
	// SelfSame: chỉ với update. Job thành công nhưng bản tải về trùng bản đang
	// chạy: không thay file, không restart.
	SelfSame = "same"
	// SelfFailed: job hỏng (đã hoàn tác: file chạy cũ còn nguyên) hoặc Plan bị
	// từ chối lúc dựng. Lý do ở Error.
	SelfFailed = "failed"
	// SelfInterrupted không bao giờ được ghi vào settings: server trả nó thay
	// cho SelfRunning khi đơn vị tạm không còn chạy mà kết quả chưa được ghi
	// (đơn vị bị giết giữa chừng, máy tắt). Xem internal/server/types_m15.go.
	SelfInterrupted = "interrupted"
)

// Mã từ chối mới của M15 (RefusedError.Code). self_no_old và self_url_invalid
// đã có từ M12.
const (
	// CodeSelfBusy: đơn vị tạm SelfUnit đang chạy.
	CodeSelfBusy = "self_update_running"
	// CodeSelfRollbackDB: file chạy cũ (TpanBin+".old") không mở được tpan.db
	// hiện tại, thường vì bản mới đã đổi cấu trúc DB. Quay về thì dịch vụ khởi
	// động lại liên tục (đã gặp ở M13), nên từ chối trước khi đổi file.
	CodeSelfRollbackDB = "self_rollback_db"
	// CodeSelfStart: systemd-run không khởi được đơn vị tạm vì lý do khác
	// "đang chạy" (máy không có systemd-run, hết bộ nhớ...).
	CodeSelfStart = "self_start_failed"
)

// SelfRun là một lượt cập nhật hoặc quay về chạy từ panel.
type SelfRun struct {
	Action string `json:"action"` // SelfActionUpdate | SelfActionRollback
	Status string `json:"status"` // SelfRunning | SelfOK | SelfSame | SelfFailed (server có thể trả SelfInterrupted)
	// From là buildinfo.Version của bản đang chạy lúc bấm.
	From string `json:"from"`
	// To là bản nằm ở TpanBin sau lượt chạy, đọc bằng `TpanBin version`. Rỗng
	// khi còn đang chạy hoặc không đọc được.
	To string `json:"to,omitempty"`
	// Error là lỗi của job hoặc lời từ chối (tiếng Việt, như engine in ra); chỉ
	// có khi Status là SelfFailed. Ví dụ: sai sha256, không tải được.
	Error string `json:"error,omitempty"`
	// JobID là job trong bảng jobs (xem từng bước ở màn Lịch sử); rỗng khi Plan
	// bị từ chối trước khi thành job.
	JobID string `json:"job_id,omitempty"`
	// Actor là user TPAN đã bấm; IP là địa chỉ của trình duyệt đó. Lượt chạy
	// tay `tpan internal self-update` không qua panel thì Actor rỗng, IP "cli".
	Actor    string    `json:"actor"`
	IP       string    `json:"ip"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
}

// SelfCheck là kết quả "Kiểm bản mới": bản xem trước của nút Cập nhật. Chỉ
// đọc, không tải file chạy.
type SelfCheck struct {
	// Current là buildinfo.Version của bản đang chạy.
	Current string `json:"current"`
	// Latest là tên bản ở gốc tải, đọc từ file VERSION ở đó (make release ghi
	// file này từ M15). Rỗng khi gốc tải không có file VERSION (bản phát hành
	// cũ): khi đó chỉ biết "có bản khác" qua sha256.
	Latest string `json:"latest"`
	// URL là gốc tải (settings update.self_url hoặc DefaultSelfURL), Asset là
	// tên file sẽ tải ở đó (tpan-linux-<arch>).
	URL   string `json:"url"`
	Asset string `json:"asset"`
	// SHA256 là số ghi cho Asset trong SHA256SUMS của gốc tải: file tải về
	// phải khớp số này thì mới được đặt vào chỗ file chạy.
	SHA256 string `json:"sha256"`
	// UpToDate: sha256 của TpanBin trùng SHA256, hoặc Latest trùng Current.
	// true thì giao diện không đưa nút chạy.
	UpToDate bool `json:"up_to_date"`
}

// SelfRollbackPreview là bản xem trước của nút "Quay về bản trước".
type SelfRollbackPreview struct {
	// Current là bản đang chạy, Old là bản của TpanBin+".old" (đọc bằng
	// `TpanBin.old version`).
	Current string `json:"current"`
	Old     string `json:"old"`
}

// ---- Hàm của SelfUpdate thêm ở M15 (thân ở selfupdate_m15.go) ----
//
// Check hỏi gốc tải. Chỉ đọc máy, hai lệnh mạng:
//   - curl <base>/SHA256SUMS (bắt buộc; không được thì trả lỗi, không phải
//     RefusedError, để server ghi thành "không hỏi được gốc tải");
//   - curl <base>/VERSION (tuỳ có; lỗi nào cũng coi như không có, Latest rỗng).
//     Nội dung chỉ nhận một dòng khớp ^[0-9A-Za-z][0-9A-Za-z.+_-]{0,63}$, vì
//     nó đến từ mạng và được in lên màn hình; không khớp thì coi như không có.
// rồi đọc TpanBin tính sha256 để điền UpToDate (máy không có file ở TpanBin,
// như máy giả của --demo, thì chỉ so tên bản). Khi lỗi, SelfCheck trả về vẫn
// có Current, URL, Asset để màn nói được "đang chạy bản nào, hỏi ở đâu". Phần gọi curl và đọc
// SHA256SUMS dùng lại mã của steps/self.go (checkSelfBase, selfCurl,
// parseSums) qua một hàm công khai mới ở đó:
//
//	func SelfLatest(ctx context.Context, sys system.System, base, arch string) (sum, version string, err error)
//
//	func (s *SelfUpdate) Check(ctx context.Context) (SelfCheck, error)
//
// Busy: đơn vị tạm SelfUnit đang chạy? `systemctl is-active tpan-self-update.service`
// in "active", "activating" hoặc "deactivating" là đang chạy; "inactive",
// "failed", hay mã thoát khác 0 với output khác là không.
//
//	func (s *SelfUpdate) Busy(ctx context.Context) (bool, error)
//
// Last đọc SettingSelfLast. ok false khi chưa có lượt nào (hoặc JSON hỏng:
// coi như chưa có, không làm hỏng màn).
//
//	func (s *SelfUpdate) Last(ctx context.Context) (run SelfRun, ok bool, err error)
//
// RollbackPreview trả hai tên bản cho hộp xác nhận. Từ chối như
// PrepareRollback: self_no_old, CodeSelfRollbackDB.
//
//	func (s *SelfUpdate) RollbackPreview(ctx context.Context) (SelfRollbackPreview, error)
//
// PrepareRollback (đã có từ M12, selfupdate.go) thêm một điều kiện đầu, áp cho
// cả CLI: chạy `TpanBin.old site list` (lệnh chỉ đọc, có từ v0.1.0, mở tpan.db
// như dịch vụ sẽ mở). Mã thoát khác 0 thì từ chối CodeSelfRollbackDB, kèm
// output của chính file cũ (đã thử: v0.1.2 trên DB bản 1400 in "DB ở bản 1400,
// tpan này chỉ biết tới bản 900: hãy dùng tpan mới hơn", mã 1; v0.2.0 mã 0).
// Lời từ chối nói đường lùi thật: chép lại bản sao tpan.db từ trước khi nâng.
//
// Start là phần chạy trong tpan serve khi người dùng bấm nút. Thứ tự:
//  1. action phải là SelfActionUpdate hoặc SelfActionRollback.
//  2. Busy thì từ chối CodeSelfBusy.
//  3. rollback: PrepareRollback để lời từ chối (self_no_old,
//     CodeSelfRollbackDB) về thẳng người bấm. update: baseURL và checkSelfBase
//     như Prepare (self_url_invalid). Không tải gì ở đây.
//  4. Ghi SettingSelfLast: {Action, Status: SelfRunning, From: s.Version,
//     Actor, IP, Started: now}.
//  5. Chạy đúng argv này qua s.Sys.Run (không shell, không có chữ nào từ
//     request):
//     systemd-run --unit tpan-self-update --collect --description <chữ cố định>
//     /usr/local/bin/tpan internal self-update            (update)
//     systemd-run ... /usr/local/bin/tpan internal self-update --rollback
//     --collect để lượt lỗi không nằm lại ở trạng thái failed và chặn lượt sau
//     (M15-thu-co-che.log, phần 3). systemd-run trả về ngay khi đơn vị đã khởi.
//     Chữ mô tả là một trong hai hằng (cập nhật, quay về) để journal gọi đúng
//     tên lượt chạy.
//  6. systemd-run lỗi: stderr có "already loaded" thì từ chối CodeSelfBusy
//     (hai người bấm cùng lúc), còn lại CodeSelfStart kèm stderr. Cả hai
//     trường hợp ghi lại SettingSelfLast như trước bước 4 (xoá dấu "running"
//     vừa ghi: trả giá trị cũ, hoặc Status SelfFailed kèm Error nếu trước đó
//     chưa có gì). Chỉ ghi lại khi settings còn đúng là dấu vừa ghi: lượt đang
//     chạy của người kia có thể vừa ghi kết quả của nó vào đó.
//
//	func (s *SelfUpdate) Start(ctx context.Context, action, actor, ip string, now time.Time) (SelfRun, error)
//
// Run là thân của `tpan internal self-update [--rollback]`, chạy TRONG đơn vị
// tạm bằng file chạy cũ. Thứ tự:
//  1. Đọc SettingSelfLast. Có và đang SelfRunning với đúng action thì dùng
//     (Actor, IP, From, Started); không thì dựng mới {Action, From: s.Version,
//     IP: "cli", Started: now()} (ai đó gõ lệnh bằng tay).
//  2. ctx = engine.WithActor(ctx, run.Actor) để job ghi đúng người bấm.
//  3. Prepare(ctx, "") hoặc PrepareRollback(ctx); bị từ chối thì Status
//     SelfFailed, Error là câu tiếng Việt của lời từ chối, sang bước 6.
//  4. Execute(ctx, onEvent). JobID lấy từ Report.
//  5. Đọc bản ở TpanBin bằng `TpanBin version` (To). Job thành công: update mà
//     To trùng From là SelfSame, còn lại SelfOK. Job không thành công:
//     SelfFailed, Error là Report.Job.Error (rỗng thì err.Error()).
//  6. Finished = now(); ghi SettingSelfLast bằng context.WithoutCancel(ctx):
//     kết quả phải được ghi kể cả khi lượt chạy bị ngắt.
// Trả SelfRun đã ghi và Report (cho CLI in). Job hỏng không phải lỗi của Run
// (xem SelfRun.Status). Lỗi trả về có ba trường hợp: chưa làm được gì (action
// lạ, không đọc được settings; SelfRun rỗng); Plan bị từ chối lúc dựng (kết
// quả failed ĐÃ ghi, và chính *RefusedError được trả kèm để người gọi lấy mã
// từ chối cho audit); job đã chạy nhưng không ghi được kết quả.
// Audit do người gọi (cmd/tpan) ghi: Action "self.update" | "self.rollback",
// Target "<From> -> <To>", Result là Status ("ok", "same") hoặc trạng thái
// job khi hỏng, IP và Actor lấy từ SelfRun.
//
//	func (s *SelfUpdate) Run(ctx context.Context, action string, now func() time.Time, onEvent func(engine.Event)) (SelfRun, engine.Report, error)
