# TPAN

Panel tự động hoá máy chủ web: cài LEMP hoặc LAMP lên một VPS Ubuntu 24.04 và tạo nhiều site WordPress trên cùng một máy. Một file chạy duy nhất viết bằng Go, có CLI, API và giao diện web.

*A small server panel that installs LEMP or LAMP and runs many WordPress sites on one Ubuntu VPS. Work in progress.*

**Trạng thái: đang dựng.** Hiện mới có khung dự án (mốc M0): build được, chạy được `tpan version` và `tpan serve` với một trang tạm. Các chức năng mô tả ở trên chưa có. Thiết kế và các mốc nằm ở [docs/KE-HOACH.md](docs/KE-HOACH.md); hình thức giao diện nằm ở [docs/GIAO-DIEN.md](docs/GIAO-DIEN.md), kèm các trang mẫu trong `docs/mau-giao-dien/`.

## Cần có để phát triển

- Go 1.27 trở lên, Node 22 trở lên, pnpm
- golangci-lint
- Multipass và jq (máy ảo Ubuntu 24.04 để test)

## Bắt đầu

```bash
make            # xem danh sách lệnh
make check      # go vet, golangci-lint, unit test, kiểm kiểu TypeScript
make build      # build bin/tpan cho máy đang dùng
make vm-up      # tạo máy ảo test và snapshot "clean"
make vm-deploy  # build cho Linux rồi chép vào máy ảo
make vm-reset   # đưa máy ảo về trạng thái sạch
```

Làm giao diện: mở hai terminal, chạy `make dev-api` và `make dev-web`, rồi vào http://localhost:5173.

## Cảnh báo

TPAN thay đổi cấu hình hệ thống bằng quyền root. Chỉ chạy nó trong máy ảo test hoặc trên một VPS sạch dành riêng cho nó.
