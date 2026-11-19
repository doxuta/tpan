# Kịch bản gây lỗi

Mục 6.7 của `docs/KE-HOACH.md`: để kiểm chính hệ thống health, mỗi kịch bản gây đúng một lỗi có chủ ý trên **máy ảo test** (không bao giờ trên VPS). Chạy trên Mac, trong thư mục repo; lệnh trong máy ảo đi qua `scripts/vm.sh root <argv>`.

```bash
scripts/faults/NN-ten.sh break <domain>     # gây lỗi
sudo tpan health                             # (trong máy ảo) phải báo Fail ở đúng chặng
sudo tpan health --fix --yes                 # sửa tự động phải đưa về đạt
scripts/faults/NN-ten.sh restore <domain>   # chỉ khi sửa tự động hỏng: đưa máy về tay
```

| Kịch bản | Gây lỗi | Chặng | Phép kiểm Fail | Sửa tự động |
|---|---|---|---|---|
| `01-php-fpm-stop.sh` | `systemctl stop php<v>-fpm` | PHP | `service.active:php<v>-fpm`, `php.socket:<domain>` | `service.start` |
| `02-pool-socket-rm.sh` | xoá socket pool của site | PHP | `php.socket:<domain>` | `php.restart` |
| `03-vhost-break.sh` | thêm dòng sai cú pháp vào vhost, không reload | Web | `config.nginx:nginx` / `config.apache:apache2`, `drift:<vhost>` | `web.rewrite` |
| `04-wpconfig-perm.sh` | `chmod 644 wp-config.php` | WordPress | `wp.config:<domain>` | `wp.config.perm` |
| `05-db-user-lock.sh` | `ALTER USER ... ACCOUNT LOCK` | DB | `db.access:<domain>` | `db.unlock` |
| `06-disk-full.sh` | file giả `/var/tmp/tpan-fault-fill` cho `/` dùng ~91% | Máy | `disk.usage:/` | không có: `restore` là cách duy nhất |

Chặng "đầu cuối" (`http.site:<domain>`) gọi HTTP thật nên lỗi theo mọi chặng trước nó; đó không tính là chặng thứ hai (docs/thay-doi/M4.md).

`06-disk-full.sh` không nhận domain và luôn phải `restore` sau khi xong; e2e chạy nó cuối cùng. Phần chung (đọc layout của site qua `tpan site info --json`, bản PHP và web server qua `tpan stack status --json`) nằm ở `lib.sh`.
