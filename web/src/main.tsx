// Điểm vào của giao diện: nạp CSS (màu, font), dựng router từ danh sách màn
// ở routes.tsx, và bọc các provider dùng chung: ngôn ngữ, Gợi ý, TanStack Query.
//
// Router chạy ở "data mode" của react-router 8 (createBrowserRouter +
// RouterProvider) để mỗi màn tải lười thành một file JS riêng qua lazy.

import { StrictMode, useEffect, type ComponentType } from "react";
import { createRoot } from "react-dom/client";
import { createBrowserRouter, type RouteObject } from "react-router";
import { RouterProvider } from "react-router/dom";
import { QueryClientProvider } from "@tanstack/react-query";
import "./styles.css";
import { queryClient } from "./api/queries";
import { LangProvider, useLang } from "./i18n";
import { AdminOnly, Layout, NotFound } from "./Layout";
import { screens } from "./routes";
import { TipsProvider } from "./tips";

// Lúc file JS của màn đầu tiên đang tải, react-router cần biết vẽ gì thay
// (HydrateFallback). Việc tải chỉ mất một nhịp nên để trống, khỏi chớp chữ.
const Blank = () => null;

type Loaded = (typeof screens)[number] & { load: () => Promise<ComponentType> };

function route(s: Loaded): RouteObject {
  // Màn của admin: user thường gõ thẳng đường dẫn thì thấy trang "không có quyền"
  // thay vì một màn chỉ toàn lỗi 403 (server vẫn là nơi chặn thật).
  const load = s.admin
    ? async () => {
        const Screen = await s.load();
        return function AdminScreen() {
          return (
            <AdminOnly>
              <Screen />
            </AdminOnly>
          );
        };
      }
    : s.load;
  return { path: s.path, lazy: { Component: load }, HydrateFallback: Blank };
}

const withScreen = screens.filter((s) => s.load !== undefined) as Loaded[];

const router = createBrowserRouter([
  ...withScreen.filter((s) => s.bare).map(route),
  {
    Component: Layout,
    HydrateFallback: Blank,
    children: [...withScreen.filter((s) => !s.bare).map(route), { path: "*", Component: NotFound }],
  },
]);

/** Giữ thuộc tính lang của <html> khớp ngôn ngữ đang chọn (trình đọc màn hình đọc đúng giọng). */
function HtmlLang() {
  const { lang } = useLang();
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);
  return null;
}

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <LangProvider>
      <HtmlLang />
      <TipsProvider>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </TipsProvider>
    </LangProvider>
  </StrictMode>,
);
