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
function tp_run(array $argv) {
    $p = @proc_open($argv, [1 => ['pipe', 'w'], 2 => ['file', '/dev/null', 'w']], $pipes);
    if (!is_resource($p)) {
        return [-1, ''];
    }
    $out = stream_get_contents($pipes[1]);
    fclose($pipes[1]);
    return [proc_close($p), (string) $out];
}
function tp_kq($co, $n) {
    echo "\nTPAN_PROBE=" . ($co ? 'CO' : 'KHONG') . ' user=' . tp_user() . ' n=' . intval($n) . "\n";
}
{{end}}
