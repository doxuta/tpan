// Sáng và tối (mục 5 của docs/GIAO-DIEN.md).
//
// web/public/theme.js đã đặt data-theme trước khi trang vẽ. File này lo phần
// sau đó: nút Sáng/Tối trên thanh đầu trang, lựa chọn "Theo hệ điều hành" ở
// trang Cài đặt, và đổi theo hệ điều hành khi người dùng chưa chọn gì.

import { useSyncExternalStore } from "react";

export type Theme = "light" | "dark";
/** "system": không lưu khoá, theo prefers-color-scheme. */
export type ThemeChoice = Theme | "system";

const storageKey = "tpan-theme"; // phải trùng với web/public/theme.js
const listeners = new Set<() => void>();
const media = window.matchMedia("(prefers-color-scheme: dark)");

function stored(): Theme | null {
  try {
    const v = localStorage.getItem(storageKey);
    return v === "light" || v === "dark" ? v : null;
  } catch {
    return null;
  }
}

function apply(t: Theme) {
  document.documentElement.setAttribute("data-theme", t);
  listeners.forEach((l) => l());
}

// Chưa chọn gì thì đổi theo hệ điều hành ngay cả khi panel đang mở.
media.addEventListener("change", () => {
  if (stored() === null) apply(media.matches ? "dark" : "light");
});

/** Đổi chế độ và nhớ lựa chọn; "system" xoá khoá để lần sau theo hệ điều hành. */
export function setTheme(choice: ThemeChoice) {
  try {
    if (choice === "system") localStorage.removeItem(storageKey);
    else localStorage.setItem(storageKey, choice);
  } catch {
    // Không nhớ được thì vẫn đổi cho lần mở này.
  }
  apply(choice === "system" ? (media.matches ? "dark" : "light") : choice);
}

function snapshot(): string {
  // Chuỗi gộp hai giá trị để useSyncExternalStore so sánh được bằng ===.
  return `${document.documentElement.getAttribute("data-theme") === "dark" ? "dark" : "light"}|${stored() ?? "system"}`;
}

function subscribe(l: () => void) {
  listeners.add(l);
  return () => {
    listeners.delete(l);
  };
}

/** theme: chế độ đang hiện; choice: điều người dùng đã chọn (để trang Cài đặt hiện đúng ô). */
export function useTheme(): { theme: Theme; choice: ThemeChoice; setTheme: (c: ThemeChoice) => void } {
  const [theme, choice] = useSyncExternalStore(subscribe, snapshot).split("|") as [Theme, ThemeChoice];
  return { theme, choice, setTheme };
}
