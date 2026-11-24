# M28 đợt 2 nhóm B: soát cuối (P4, P7, F1)

Ngày 25/11/2026. Thợ soát chưa đọc mã của nhóm B, chỉ được giao đặc tả (mục 6.28 của `docs/KE-HOACH.md`) và các commit, nhiệm vụ là tìm đường vòng qua từng bản vá. Chỉ đọc mã và chạy chương trình nháp; không dùng máy ảo.

**Kết luận:** không tìm được đường nào vượt qua phần cốt lõi của ba bản vá (P4 so theo tên miền gốc eTLD+1, P7 đặt chủ của job theo loại job, F1 không bao giờ cấp lại UID). Có 12 phát hiện: 1 nặng, 1 vừa, 10 nhẹ. Repo công khai: phát hiện đã vá ghi đủ; phát hiện chưa vá chỉ ghi loại lỗi và ảnh hưởng.

## Đã vá trong đợt này

### B1 (nặng): site mang đúng tên dùng để vào panel lấy được cookie phiên của admin

Panel chạy ở `https://<tên máy>:8443`. Cookie phiên `__Host-tpan` là host-only nhưng trình duyệt không tách cookie theo cổng, nên trang ở `https://<tên máy>/` (cổng 443) nhận được cookie phiên của panel khi admin đang đăng nhập mở trang đó; trang tự gọi lại chính nó là request cùng site nên `SameSite=Strict` không chặn. Một khách vai `user` biết tên máy (chính họ cũng vào panel ở đó), tạo site mang đúng tên đó và xin SSL cho nó: luật P4 chỉ so với các site đang có. Admin mở trang đó một lần là khách có phiên admin, tức là có root. Có từ M13 (lúc khách tự tạo site được), không do P4 gây ra.

Vá: tên dùng để vào panel là tên dành riêng. Gồm các tên DNS trong chứng chỉ của panel (`/etc/tpan/tls/cert.pem`, `tpan setup` đặt hostname vào đó) và mọi tên (`Host`, bỏ cổng, không nhận IP) mà admin đã dùng ở các lần đăng nhập thành công. `PrepareAdd` từ chối mọi site mới mang đúng một tên đó (`domain_reserved`), với mọi vai và cả dòng lệnh. Chi tiết và test: mục 6.28 của `KE-HOACH.md`, nhật ký `docs/thay-doi/M28.md`.

### B2 (vừa): một user làm hỏng mọi lần xoá hẳn trên máy

Bước dọn của F1 chạy `find /tmp -xdev -uid N ... -delete` bằng root. Một điểm mount FUSE của user khác trong `/tmp` (không có `allow_other`) từ chối cả root, nên `find` thoát 1; bản đầu của F1 coi đó là hỏng, không `userdel`, và job xoá hẳn hoàn tác. Một site bị chiếm giữ được trạng thái đó lâu dài, và mọi lần xoá hẳn của mọi khách (kể cả bộ hẹn giờ thùng rác) đều hỏng. Podman không root (`fuse-overlayfs`) cũng gây ra đúng lỗi này mà không cần ai cố ý.

Vá: bước dọn file tạm là cố gắng tối đa. `find` thoát khác 0 thì hỏi lại `find ... -print -quit`; chỉ hỏng khi lần hỏi lại in ra một file của UID, mã thoát của nó không tính. UID mới không bao giờ cấp lại nên file sót dưới một điểm mount như vậy không theo sang ai. Test: `TestSiteUserAbsentDonDauVet` có ca "điểm mount FUSE của user khác". Chưa thử trên máy ảo.

### B6 (nhẹ): migration 2800 bỏ sót hai loại job

Bản cũ suy chủ từ `target` cho mọi loại job. Ngoài `user.*` và `cage.sync`, còn hai loại chỉ admin chạy mà `target` vẫn qua được `validate.ParseDomain`: `fix.service.start` (`target` là tên dịch vụ, ví dụ `php8.3-fpm`) và `backup.key.new` (`target` là `backup.key`). Khách từng đặt site đúng tên đó là chủ của các dòng cũ. Vá: migration 2801 đưa hai loại đó về chủ `admin`, dạng hẹp (vài loại ngoài danh sách tra chủ đã được đặt chủ rõ có chủ ý, như `backup.push` của bộ hẹn giờ).

### B9 (nhẹ): câu chữ

`KE-HOACH.md` nói site cũ (UID dưới 1000) khi xoá hẳn "không còn dấu vết nào đi theo". Bước dọn chỉ xét crontab, tiến trình, `/tmp`, `/var/tmp`, `/dev/shm`; đã sửa câu cho đúng giới hạn đó.

## Vá ở lượt sau (cùng ngày, mỗi cái kèm test)

- B3 (nhẹ): sự cố của site gắn theo tên domain, nên ai tạo lại cùng tên sau khi chủ cũ xoá hẳn thì đọc được sự cố cũ. Vá: `store.IncidentsOf`, `IncidentOwner` chỉ tính sự cố mở từ lúc site hiện tại được tạo (`opened_at >= sites.created_at`); admin thấy hết. Test `TestSuCoCuKhongTheoDomainSangChuMoi`.
- B4 (nhẹ): bản xem trước tạo site cho biết một tên miền gốc đã có khách khác dùng mà không để lại dấu. Vá: lời từ chối `site_not_found` ở bản xem trước của `POST /sites` ghi nhật ký (`site_not_found_dry_run`), `POST /apply?dry_run=1` ghi một dòng tóm tắt (số dòng bị từ chối, không ghi tên). Chưa có giới hạn tần suất. Test `TestXemTruocBiTuChoiCoNhatKyB4`.
- B5 (nhẹ): khôi phục từ thùng rác không xét luật P4. Vá: người không phải admin khôi phục site mà trùng tên miền gốc với site của người khác thì 404 như P4. Test `TestKhoiPhucChungTenMienGocB5`.
- B7 (nhẹ): site có tên là chính một đuôi công cộng (chỉ admin tạo được) bị bỏ qua khi so. Vá: mọi tên con của site đó coi như đã có người dùng. Test `TestRegistrableTakenTenLaDuoiCongCongB7`.
- B8 (nhẹ): một tiến trình chỉ còn giữ UID của user ở saved-UID không bị tìm thấy, nên `userdel` thoát 8 với lời sai nguyên nhân. Vá: luôn chạy `runuser -u <user> -- kill -KILL -1` một lần trước khi tìm tiến trình; lời của mã 8 nói cả khả năng đó. Test trong `TestSiteUserAbsentDonDauVet`.
- B10 (nhẹ): khôi phục bản `tpan.db` cũ làm lùi bộ đếm UID. Vá: mức sàn lấy thêm từ `/var/lib/tpan/site-uid-max` (root 0600), ghi trước khi tạo user. Khôi phục cả thư mục `/var/lib/tpan` từ một bản chụp thì vẫn lùi (chỉ còn mức sàn từ `/etc/passwd`). Test trong `TestSiteUserCapUID`.
- B11 (nhẹ, có từ M2): tên user Linux và tên DB của site suy từ một đoạn băm ngắn của tên miền; khách có thể chiếm trước tên đó của một tên miền khác. Vá: site mới có slug trùng slug của một site đang có (mọi trạng thái) mà khác tên miền thì bị từ chối 409 `slug_taken`; độ dài băm không đổi (đổi là đổi tên user, DB của site đang chạy). Test `TestPrepareAddSlugTrungB11`.
- B12 (nhẹ): bản sao lưu cũ của chủ trước bị dọn khi người khác tạo lại cùng tên miền. Vá: dọn và đếm hạn mức chỉ tính bản của chủ hiện tại. Test `TestDonBackupChiTheoChuHienTai`.

## Đã thử, không phá được (tóm tắt)

P4: chuẩn hoá tên (hoa thường, dấu chấm cuối, IDN), `www` và tên phụ, MCP, khôi phục backup, đua hai request song song. P7: từng loại job trong `store.jobOwnerLookup`, `GET /jobs/{id}/events`, SSE, danh sách job. F1: mọi đường gọi `useradd`, cấp số khi hai job chạy gần nhau, tiến trình fork nhanh, tiến trình trong lồng, symlink và hardlink trong thư mục tạm, đua với crontab.
