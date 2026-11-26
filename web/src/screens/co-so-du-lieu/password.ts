// M36 (v0.2.17): luật mật khẩu DB tự đặt, kiểm ngay trên trình duyệt để báo
// lỗi trước khi gửi. Phải khớp core.checkChosenDBPassword (máy chủ kiểm lại).
// Không import file chuỗi: test chạy bằng node --test, không đọc được .tsx.

export const dbPasswordMin = 12;
export const dbPasswordMax = 64;
/** Các dấu được nhận, ngoài chữ cái không dấu và chữ số. */
export const dbPasswordSymbols = "!#%*+,-./:=?@^_~";

const allowed = /^[A-Za-z0-9!#%*+,\-./:=?@^_~]*$/;

export type DbPasswordProblem = "short" | "long" | "chars" | "classes" | "name";

/** Lỗi đầu tiên của mật khẩu pw cho user DB name; null là dùng được. */
export function dbPasswordProblem(pw: string, name: string): DbPasswordProblem | null {
  if (pw.length < dbPasswordMin) return "short";
  if (pw.length > dbPasswordMax) return "long";
  if (!allowed.test(pw)) return "chars";
  if (!/[A-Za-z]/.test(pw) || !/[0-9]/.test(pw)) return "classes";
  if (name !== "" && pw.toLowerCase().includes(name.toLowerCase())) return "name";
  return null;
}
