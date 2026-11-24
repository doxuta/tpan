# t2b: (1) hàng rào mặc định sau khi bỏ PrivateUsers: WordPress và điểm bảo mật;
# (2) từng thuộc tính siết thêm, thử riêng: WordPress còn chạy không.
# Mỗi lượt: dựng lồng với thuộc tính thêm, chạy t2.php qua HTTP (đếm dòng HONG) và
# ba lệnh wp trong lồng.
read -r -d '' T <<'PHP'
<?php
require __DIR__ . '/wp-load.php';
require_once ABSPATH . 'wp-admin/includes/file.php';
require_once ABSPATH . 'wp-admin/includes/image.php';
header('Content-Type: text/plain');
function ok($b){ return $b ? 'dat' : 'HONG'; }
$r = wp_remote_get('https://api.wordpress.org/core/version-check/1.7/', ['timeout' => 20]);
echo "https: " . ok(!is_wp_error($r) && wp_remote_retrieve_response_code($r) == 200) . "\n";
$tmp = download_url('https://downloads.wordpress.org/plugin/hello-dolly.1.7.2.zip', 30);
if (is_wp_error($tmp)) echo "plugin: HONG " . $tmp->get_error_message() . "\n";
else { WP_Filesystem(); $dest = WP_PLUGIN_DIR . '/thu-m17'; $u = unzip_file($tmp, $dest);
  echo "plugin: " . ok(!is_wp_error($u) && file_exists($dest . '/hello-dolly/hello.php')) . "\n";
  @unlink($tmp); global $wp_filesystem; $wp_filesystem->delete($dest, true); }
$im = imagecreatetruecolor(1200, 800); ob_start(); imagejpeg($im); $bits = ob_get_clean();
$up = wp_upload_bits('thu-m17.jpg', null, $bits);
if (!empty($up['error'])) echo "upload: HONG " . $up['error'] . "\n";
else { $id = wp_insert_attachment(['post_mime_type' => 'image/jpeg', 'post_title' => 'thu', 'post_status' => 'inherit'], $up['file']);
  $meta = wp_generate_attachment_metadata($id, $up['file']);
  echo "upload: " . ok(!empty($meta['sizes'])) . " nhom=" . filegroup($up['file']) . "\n"; wp_delete_attachment($id, true); }
echo "preg: " . ok(@preg_match('/^(\w+)@(\w+)\.com$/', 'a@example.com') === 1) . " loi=" . preg_last_error() . "\n";
echo "exec: " . ok(trim((string) @shell_exec('echo xin-chao')) === 'xin-chao') . "\n";
echo "chmod: " . ok(@chmod(__FILE__, 0640)) . "\n";
PHP

luot(){ # nhãn, các dòng thêm vào drop-in (mỗi tham số một dòng)
  local nhan=$1; shift
  long_go $A >/dev/null 2>&1
  long_dung $A 256 100 60
  local f=/etc/systemd/system/tpan-cage-$SA-.service.d/20-them.conf
  if [ $# -gt 0 ]; then { echo "[Service]"; printf '%s\n' "$@"; } > $f; systemctl daemon-reload; fi
  long_vao $A >/dev/null
  echo "$T" | put_php $A t2.php
  local http out hong w1 w2 w3
  http=$(code_a ''); out=$(ca t2.php)
  hong=$(echo "$out" | grep -c HONG)
  run_in $A wp plugin install hello-dolly --path=/var/www/$A/public >/dev/null 2>&1; w1=$?
  run_in $A wp plugin delete hello-dolly --path=/var/www/$A/public >/dev/null 2>&1; w2=$?
  run_in $A wp core verify-checksums --path=/var/www/$A/public >/dev/null 2>&1; w3=$?
  printf '%-44s trang chủ %s | t2.php: %s dòng hỏng [%s] | wp: %s %s %s | điểm: %s\n' "$nhan" "$http" "$hong" \
    "$(echo "$out" | tr '\n' ';' | cut -c1-150)" "$w1" "$w2" "$w3" \
    "$(systemd-analyze security tpan-cage-$SA-php.service --no-pager 2>/dev/null | tail -1 | sed 's/.*service: //')"
  journalctl -u tpan-cage-$SA-php.service --since "-40s" --no-pager -o cat 2>/dev/null | grep -iE 'error|warn|fail|denied' | head -3 | sed 's/^/      journal: /'
  rm -f /var/www/$A/public/t2.php
}

echo "## 1. Hàng rào mặc định (đã bỏ PrivateUsers)"
luot "mặc định"
echo "   nhóm thật của file upload nhìn từ ngoài lồng: thư mục uploads $(stat -c '%a %U:%G' /var/www/$A/public/wp-content/uploads 2>&1)"

echo; echo "## 2. Thử thêm từng thuộc tính"
luot "PrivateUsers=yes" "PrivateUsers=yes"
luot "MemoryDenyWriteExecute=yes" "MemoryDenyWriteExecute=yes"
luot "SystemCallFilter=~@privileged" "SystemCallFilter=~@privileged"
luot "SystemCallFilter=~@resources" "SystemCallFilter=~@resources"
luot "~@privileged + ~@resources" "SystemCallFilter=~@privileged @resources"
luot "NoExecPaths=<thư mục site>" "NoExecPaths=/var/www/$A"
luot "IPAddressDeny=localhost (chặn 127.0.0.0/8)" "IPAddressDeny=localhost"
luot "RestrictAddressFamilies=AF_UNIX AF_INET" "RestrictAddressFamilies=" "RestrictAddressFamilies=AF_UNIX AF_INET"
luot "PrivateNetwork=yes" "PrivateNetwork=yes"

echo; echo "## 3. Máy tắt user namespace thì PrivateUsers ra sao"
old=$(sysctl -n user.max_user_namespaces); sysctl -qw user.max_user_namespaces=0
luot "PrivateUsers=yes, max_user_namespaces=0" "PrivateUsers=yes"
luot "mặc định, max_user_namespaces=0"
sysctl -qw user.max_user_namespaces=$old

echo; echo "## Dọn"
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
