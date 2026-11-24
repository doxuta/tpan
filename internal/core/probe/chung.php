{{define "chung"}}
// File thử của tpan isolation probe (M17, mục 3.9 của docs/thay-doi/M17-thiet-ke.md).
// tpan ghi file này vào site thử A (hoặc B) ngay trước khi gọi nó và xoá ngay
// sau đó. Không nhận tham số nào từ request: mọi đường dẫn được tpan điền sẵn
// lúc ghi. Chỉ in một dòng TPAN_PROBE=CO|KHONG, tên user đang chạy và một số
// đếm; không in nội dung file hay tên của site nào.
function tp_user() {
    if (!function_exists('posix_getpwuid')) {
        return '?';
    }
    $p = @posix_getpwuid(posix_geteuid());
    return ($p && isset($p['name'])) ? $p['name'] : (string) posix_geteuid();
}
// tp_run chạy một chương trình bằng mảng argv (proc_open không qua /bin/sh),
// trả [mã thoát, stdout]. Đi bằng tiến trình con vì open_basedir chặn hàm đọc
// file của PHP, còn kẻ tấn công thật thì gọi được lệnh: đường thử phải đo cái
// lệnh thấy, không phải cái open_basedir chặn.
//
// stderr đi vào ống rồi bỏ, không mở file /dev/null: open_basedir của pool
// chặn chính PHP mở /dev/null, proc_open trả false, và mọi đường gọi lệnh ra
// "bị chặn" giả (gặp ở e2e M17: C1 ngoài lồng "0 mục"). Không chạy được lệnh
// thì dừng luôn, không in dòng TPAN_PROBE: ô đó là "không kết luận", không bao
// giờ là "bị chặn". Các lệnh thử in rất ít ra stderr nên đọc hết stdout rồi
// mới đọc stderr không kẹt ống.
function tp_run(array $argv) {
    $p = @proc_open($argv, [1 => ['pipe', 'w'], 2 => ['pipe', 'w']], $pipes);
    if (!is_resource($p)) {
        echo "\nTPAN_PROBE_LOI=proc_open\n";
        exit;
    }
    $out = stream_get_contents($pipes[1]);
    fclose($pipes[1]);
    stream_get_contents($pipes[2]);
    fclose($pipes[2]);
    return [proc_close($p), (string) $out];
}
function tp_kq($co, $n) {
    echo "\nTPAN_PROBE=" . ($co ? 'CO' : 'KHONG') . ' user=' . tp_user() . ' n=' . intval($n) . "\n";
}
{{end}}
