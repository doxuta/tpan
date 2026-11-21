// Hình đại diện (mục 6 của docs/GIAO-DIEN.md, bản 2): hình tròn nền
// --accent-tint, chữ cái đầu của tên viết hoa màu --accent. Chỉ để nhìn: tên đã
// có chữ bên cạnh, nên ẩn khỏi trình đọc màn hình.

export function Avatar({ name, size = 32 }: { name: string; size?: 28 | 32 }) {
  // Lấy theo điểm mã (không cắt đôi một ký tự ngoài BMP); tên rỗng thì "?".
  const first = Array.from(name.trim())[0] ?? "?";
  return (
    <span
      aria-hidden="true"
      className={`flex flex-none items-center justify-center rounded-full bg-accent-tint font-semibold text-accent ${size === 28 ? "size-[28px] text-[13px]" : "size-[32px] text-[14px]"}`}
    >
      {first.toUpperCase()}
    </span>
  );
}
