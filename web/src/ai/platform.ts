// Hai thứ phụ thuộc máy người dùng: phím tắt hiện theo hệ điều hành, và ngăn AI
// nằm cạnh nội dung hay phủ lên nó.

import { useSyncExternalStore } from "react";

/** macOS và iOS dùng ⌘; mọi máy khác dùng Ctrl. Bắt phím thì nhận cả hai (xem Layout). */
const isMac = /Mac|iPhone|iPad|iPod/.test(navigator.platform);

/** Chữ hiện trên ô lệnh. */
export const paletteKbd = isMac ? "⌘K" : "Ctrl K";
/** Giá trị cho aria-keyshortcuts. */
export const paletteShortcut = isMac ? "Meta+K" : "Control+K";
export const drawerShortcut = isMac ? "Meta+/" : "Control+/";

// Đủ chỗ cho menu 192 + nội dung 560 + ngăn 400 + hai khoảng cách 32 và đệm
// 32 hai bên (mục 7 của GIAO-DIEN.md), cộng dư cho thanh cuộn dọc. Hẹp hơn thì
// ngăn phủ lên nội dung thay vì ép nội dung rớt xuống dưới ngăn.
const media = window.matchMedia("(min-width: 1320px)");

function subscribe(onChange: () => void): () => void {
  media.addEventListener("change", onChange);
  return () => media.removeEventListener("change", onChange);
}

/** true: màn đủ rộng để ngăn AI nằm cạnh nội dung; false: ngăn là lớp phủ. */
export function useWide(): boolean {
  return useSyncExternalStore(subscribe, () => media.matches);
}
