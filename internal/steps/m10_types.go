package steps

// Hợp đồng M10 (đợt 0): các Step của "chuyển LEMP ⇄ LAMP" (mục 6.6 của
// docs/KE-HOACH.md).
//
// File này chỉ có chú thích. Thợ đợt 1 viết file mới switch_m10.go (và
// switch_m10_test.go), cộng đúng một thay đổi ở file cũ: trường NoRestart của
// ApacheModules (apache.go). Mỗi Step có `var _ engine.Step = (*X)(nil)`. Core
// (đợt 2) ghép Plan "stack.switch" dựa trên đúng tên và trường xuất dưới đây.
// Luật chung của gói (common.go) vẫn áp dụng: tham số sai thì Check và Apply
// lỗi trước khi chạy lệnh; Apply đã đổi máy mà bước sau trong chính Apply lỗi
// thì tự trả máy về (engine không Undo bước có Apply lỗi).
//
// Vì sao cần các Step riêng: Plan chuyển web server ghi cấu hình của web server
// MỚI trong lúc web server CŨ vẫn giữ cổng 80 và 443. Từ đầu Plan tới bước
// WebSwap, không lệnh nào được khởi động, khởi động lại hay reload web server
// mới (nó sẽ tranh cổng với web server cũ hoặc lỗi vì đang tắt). AptInstall,
// Service, ApacheModules của M1 đều làm việc đó, nên không dùng thẳng được.
//
// ------------------------------------------------------------ switch_m10.go
//
//	// AptInstallNoStart cài gói mà không cho dịch vụ của gói tự chạy, rồi tắt
//	// "bật khi khởi động" của các unit trong Disable (mục 6.6 bước 1, mục 16).
//	//   1. Ghi /usr/sbin/policy-rc.d (0755 root, nội dung "#!/bin/sh", dòng
//	//      đầu tpan, "exit 101"). Có sẵn một policy-rc.d không phải của tpan
//	//      thì nhớ nội dung, quyền, chủ để trả lại y nguyên.
//	//   2. `apt-get -o DPkg::Lock::Timeout=300 install -y <Packages>` (cùng
//	//      kiểm tên gói và cùng chặn mod_php như AptInstall).
//	//   3. LUÔN trả policy-rc.d về như trước bước 1 (defer trong Apply, cả khi
//	//      apt-get lỗi): để sót file này thì mọi lần apt-get sau đó trên máy
//	//      (kể cả unattended-upgrades) không khởi động lại được dịch vụ nào.
//	//   4. `systemctl disable --now <Disable...>`: policy-rc.d chặn khởi động
//	//      nhưng KHÔNG chặn enable (đã kiểm ở M1, mục 16); để unit mới enabled
//	//      thì máy khởi động lại giữa chừng sẽ có hai web server tranh cổng 80.
//	//      --now để dừng luôn nếu vì lý do gì nó đang chạy.
//	// Check: mọi gói đã cài (missingPackages) VÀ mọi unit trong Disable không
//	// enabled và không active (`systemctl is-enabled` / `is-active` thoát khác 0).
//	// Undo: không làm gì (NoUndo khác rỗng): gói đã cài giữ lại như AptInstall,
//	// và web server mới phải ở trạng thái tắt, không bật khi khởi động sau khi
//	// hoàn tác (Undo "bật lại" là đem lại đúng chuyện hai web server tranh cổng).
//	// Info: ID "apt.install-nostart:<gói,...>"; Commands liệt kê đủ bốn việc;
//	// KHÔNG khai policy-rc.d trong Files: engine ghi mọi file trong Files vào
//	// managed_files, mà file này bị gỡ ngay trong Apply, nên health sẽ báo "file
//	// không còn" mãi. Disable khớp unitName; Packages không rỗng.
//	type AptInstallNoStart struct {
//		Packages []string // ["apache2"] hoặc ["nginx"]
//		Disable  []string // ["apache2"] hoặc ["nginx"]
//	}
//
//	// WebSwap là mục 6.6 bước 3: dừng web server cũ, khởi động web server mới.
//	// Danger (mọi site gián đoạn trong khoảng giữa hai lệnh).
//	// Check: From không active VÀ To active (chạy lại sau một lần hỏng giữa
//	// chừng thì hội tụ, không dừng rồi chạy lại vô ích).
//	// Apply: `systemctl stop <From>`, rồi `systemctl start <To>`. Start lỗi thì
//	// `systemctl start <From>` để site sống lại, trả lỗi kèm stdout/stderr của
//	// lệnh start lỗi (errors.Join nếu bật lại cũng lỗi). Stop lỗi thì trả lỗi,
//	// thử start lại From nếu nó vốn đang chạy.
//	// Undo: `systemctl stop <To>`, rồi `systemctl start <From>`.
//	// Không enable, không disable: việc đó là của Service (To) và ServiceDisable
//	// (From) ở sau bước kiểm HTTP giữa chừng.
//	// Info: ID "web.swap:<From>:<To>"; Commands là hai lệnh của Apply.
//	// From, To khớp unitName và khác nhau.
//	type WebSwap struct {
//		From, To string // "nginx", "apache2"
//	}
//
//	// ServiceDisable tắt "bật khi khởi động" của một unit, không dừng nó (mục
//	// 6.6 bước 5: `disable` web server cũ sau khi mọi site đã chạy trên cái mới).
//	// Check: unit không enabled. Apply: `systemctl disable <Unit>`. Undo: unit
//	// enabled trước Apply thì `systemctl enable <Unit>`, không thì không làm gì.
//	// Info: ID "service.disable:<Unit>". Unit khớp unitName.
//	type ServiceDisable struct {
//		Unit string
//	}
//
// ----------------------------------------------------------------- apache.go
//
//	// Trường mới của ApacheModules:
//	//   NoRestart bool
//	// true: không bao giờ `systemctl restart apache2` (Apply, Undo, và lúc tự
//	// trả module về khi lỗi); vẫn chạy `apachectl configtest` như cũ. Dùng khi
//	// apache2 đang tắt (Plan chuyển web server: nó chỉ chạy ở WebSwap).
//	// Commands của Info không còn lệnh restart khi NoRestart.
