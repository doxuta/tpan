# t2: với ĐỦ hàng rào (lib.sh), WordPress còn làm được các việc thường ngày không.
# A trong lồng, B ở PHP-FPM chung; cùng một script chạy ở cả hai để so từng dòng.
# Việc thử qua HTTP (như khi bấm trong wp-admin): gọi HTTPS ra ngoài, phân giải
# tên, tải và giải nén một plugin, upload ảnh và sinh thumbnail, session, gửi mail.
# Việc thử qua WP-CLI trong lồng: các lệnh tpan vẫn gọi (config, core, cron, db).
long_go $A >/dev/null 2>&1
long_dung $A 256 100 60
long_vao $A >/dev/null

read -r -d '' T <<'PHP'
<?php
require __DIR__ . '/wp-load.php';
require_once ABSPATH . 'wp-admin/includes/file.php';
require_once ABSPATH . 'wp-admin/includes/image.php';
require_once ABSPATH . 'wp-admin/includes/plugin.php';
header('Content-Type: text/plain');
function ok($b){ return $b ? 'dat' : 'HONG'; }
echo "user: " . posix_getpwuid(posix_geteuid())['name'] . "\n";
echo "db (trang chu da nap): " . ok((bool) get_option('siteurl')) . "\n";
echo "phan giai ten api.wordpress.org: " . ok(gethostbyname('api.wordpress.org') !== 'api.wordpress.org') . "\n";
$r = wp_remote_get('https://api.wordpress.org/core/version-check/1.7/', ['timeout' => 20]);
echo "HTTPS ra ngoai (WP HTTP API): " . (is_wp_error($r) ? 'HONG: ' . $r->get_error_message() : 'dat, ma ' . wp_remote_retrieve_response_code($r)) . "\n";
$tmp = download_url('https://downloads.wordpress.org/plugin/hello-dolly.1.7.2.zip', 30);
if (is_wp_error($tmp)) { echo "tai plugin: HONG: " . $tmp->get_error_message() . "\n"; }
else {
  echo "tai plugin vao thu muc tam: dat (" . dirname($tmp) . ")\n";
  WP_Filesystem();
  $dest = WP_PLUGIN_DIR . '/thu-m17';
  $u = unzip_file($tmp, $dest);
  echo "giai nen plugin: " . (is_wp_error($u) ? 'HONG: ' . $u->get_error_message() : ok(file_exists($dest . '/hello-dolly/hello.php'))) . "\n";
  @unlink($tmp);
  global $wp_filesystem; $wp_filesystem->delete($dest, true);
  echo "xoa plugin thu: " . ok(!file_exists($dest)) . "\n";
}
echo "get_filesystem_method: " . get_filesystem_method() . "\n";
$im = imagecreatetruecolor(1200, 800); imagefill($im, 0, 0, imagecolorallocate($im, 20, 120, 200));
ob_start(); imagejpeg($im); $bits = ob_get_clean();
$up = wp_upload_bits('thu-m17.jpg', null, $bits);
if (!empty($up['error'])) { echo "upload anh: HONG: " . $up['error'] . "\n"; }
else {
  $id = wp_insert_attachment(['post_mime_type' => 'image/jpeg', 'post_title' => 'thu', 'post_status' => 'inherit'], $up['file']);
  $meta = wp_generate_attachment_metadata($id, $up['file']);
  echo "upload anh + thumbnail: " . ok(!empty($meta['sizes'])) . " (" . count($meta['sizes'] ?? []) . " co, quyen file " . substr(sprintf('%o', fileperms($up['file'])), -4) . ", nhom " . filegroup($up['file']) . ")\n";
  wp_delete_attachment($id, true);
}
session_start(); $_SESSION['x'] = 1; session_write_close();
echo "session: " . ok(count(glob(ini_get('session.save_path') . '/sess_*')) > 0) . " (" . ini_get('session.save_path') . ")\n";
array_map('unlink', glob(ini_get('session.save_path') . '/sess_*'));
echo "wp_mail: " . (wp_mail('nobody@example.com', 'thu', 'thu') ? 'gui duoc' : 'khong gui duoc') . " (sendmail_path=" . ini_get('sendmail_path') . ")\n";
echo "preg (PCRE JIT=" . ini_get('pcre.jit') . "): " . ok(preg_match('/^(\w+)@(\w+)\.com$/', 'a@example.com') === 1) . "\n";
echo "opcache: " . (function_exists('opcache_get_status') ? 'co, API ' . (@opcache_get_status(false) ? 'MO' : 'bi khoa') : 'khong co') . "\n";
echo "disk_free_space: " . ok(disk_free_space(ABSPATH) > 0) . " | sys_get_temp_dir: " . sys_get_temp_dir() . "\n";
echo "cron spawn (loopback HTTP toi chinh site): "; $l = wp_remote_get(home_url('/?thu=1'), ['timeout' => 10]); echo (is_wp_error($l) ? 'khong: ' . $l->get_error_message() : 'ma ' . wp_remote_retrieve_response_code($l)) . "\n";
PHP
echo "$T" | put_php $A t2.php; echo "$T" | put_php $B t2.php

echo "## 1. Qua HTTP: A (trong lồng)"; ca t2.php | sed 's/^/  /'
echo; echo "## 1b. Qua HTTP: B (PHP-FPM chung, để so)"; cb t2.php | sed 's/^/  /'

echo; echo "## 2. WP-CLI trong lồng (các lệnh tpan gọi và vài lệnh người dùng hay cần)"
W(){ printf '  wp %-52s -> ' "$*"; out=$(run_in $A wp "$@" --path=/var/www/$A/public 2>&1); rc=$?; echo "mã $rc | $(echo "$out" | tail -1 | cut -c1-110)"; }
W core is-installed
W core version
W core verify-checksums
W option get siteurl
W config get DB_NAME --type=constant
W config set TPAN_THU 1 --raw --type=constant --quiet
W config delete TPAN_THU --type=constant --quiet
W db query 'SHOW DATABASES' --skip-column-names
W cron event run --due-now --quiet
W plugin install hello-dolly --activate
W plugin list --field=name
W plugin delete hello-dolly
W rewrite structure '/%postname%/' --quiet
W user list --field=user_login
W db export /var/www/$A/tmp/thu.sql
W eval 'echo gethostbyname("downloads.wordpress.org");'
echo "  quyền wp-config.php sau các lệnh: $(stat -c '%a %U:%G' /var/www/$A/public/wp-config.php) | file xuất: $(stat -c '%a %U:%G %s byte' /var/www/$A/tmp/thu.sql 2>&1)"
rm -f /var/www/$A/tmp/thu.sql
echo "  mật khẩu qua stdin (wp config set --prompt): $(printf 'giatri-bi-mat\n' | run_in $A wp config set TPAN_THU2 --prompt --type=constant --quiet --path=/var/www/$A/public >/dev/null 2>&1; echo mã $?) | đọc lại: $(run_in $A wp config get TPAN_THU2 --type=constant --path=/var/www/$A/public 2>&1)"
run_in $A wp config delete TPAN_THU2 --type=constant --quiet --path=/var/www/$A/public
echo "  tar giải nén WordPress từ /var/cache/tpan (như bước WPFiles): $(run_in $A sh -c 'ls /var/cache/tpan/ | head -3 | tr "\n" " "')"

echo; echo "## 3. Thời gian một lệnh wp: 5 lần trong lồng, 5 lần ngoài lồng (giây)"
for i in 1 2 3 4 5; do t0=$(date +%s.%N); run_in $A wp option get siteurl --path=/var/www/$A/public >/dev/null 2>&1; t1=$(date +%s.%N); printf '  trong %s' "$(echo "$t1 - $t0" | bc)"; done; echo
for i in 1 2 3 4 5; do t0=$(date +%s.%N); runuser -u $UB -- wp option get siteurl --path=/var/www/$B/public >/dev/null 2>&1; t1=$(date +%s.%N); printf '  ngoai %s' "$(echo "$t1 - $t0" | bc)"; done; echo

echo; echo "## 4. Log của FPM riêng nằm trong journal của đúng unit"
journalctl -u tpan-cage-$SA-php.service --since "-3min" --no-pager -o cat | tail -4
echo "php-error.log của A: $(stat -c '%a %U:%G %s byte' /var/log/tpan/sites/$A/php-error.log 2>&1)"

echo; echo "## Dọn"
rm -f /var/www/$A/public/t2.php /var/www/$B/public/t2.php
long_go $A
echo "sau khi dọn: A HTTP $(code_a ''), B HTTP $(code_b '')"
