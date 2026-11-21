// Hàng tab (mục 6 của docs/GIAO-DIEN.md, bản 2), bọc Radix Tabs 1.2. Radix lo
// bàn phím (mũi tên trái phải, Home, End) và nối aria-controls với khối nội
// dung. Nội dung của tab không mở bị gỡ khỏi trang (Radix chỉ dựng tab đang mở),
// nên state cần sống qua lần đổi tab phải giữ ở component cha.
//
//   <Tabs label="Các phần của site" value={tab} onValueChange={setTab}
//     tabs={[{ value: "tong-quan", label: "Tổng quan" }, { value: "log", label: "Log" }]}>
//     <TabPanel value="tong-quan">…</TabPanel>
//     <TabPanel value="log">…</TabPanel>
//   </Tabs>
//
// Không đè id của nút tab: Radix tự sinh id và dùng nó cho aria-labelledby của
// khối nội dung. Móc cho kiểm thử là data-tab="<giá trị>" trên nút tab.

import * as RT from "@radix-ui/react-tabs";
import type { ReactNode } from "react";

export type TabSpec<T extends string> = { value: T; label: ReactNode; count?: number };

type Props<T extends string> = {
  /** Tên đọc được của hàng tab. */
  label: string;
  value: T;
  onValueChange: (v: T) => void;
  tabs: TabSpec<T>[];
  children: ReactNode;
};

export function Tabs<T extends string>({ label, value, onValueChange, tabs, children }: Props<T>) {
  return (
    <RT.Root value={value} onValueChange={(v) => onValueChange(v as T)} className="flex flex-col gap-[20px]">
      {/* Khung cuộn ngang riêng khi hẹp; -mx/px 4 px chừa chỗ cho viền focus. */}
      <div className="-mx-[4px] overflow-x-auto px-[4px]">
        <RT.List aria-label={label} className="flex min-w-max gap-[4px] border-b border-hair">
          {tabs.map((t) => (
            <RT.Trigger
              key={t.value}
              value={t.value}
              data-tab={t.value}
              className="-mb-px inline-flex h-[44px] items-center gap-[6px] border-b-2 border-transparent bg-transparent px-[12px] text-[14px] whitespace-nowrap text-ink-soft hover:text-ink data-[state=active]:border-accent data-[state=active]:font-semibold data-[state=active]:text-accent"
            >
              {t.label}
              {t.count !== undefined && <span className="text-[12px] font-normal text-muted tabular-nums">{t.count}</span>}
            </RT.Trigger>
          ))}
        </RT.List>
      </div>
      {children}
    </RT.Root>
  );
}

/** Nội dung một tab: một dãy khối cách nhau 20 px như nội dung màn. */
export function TabPanel({ value, children }: { value: string; children: ReactNode }) {
  return (
    <RT.Content value={value} className="flex flex-col gap-[20px] focus-visible:outline-offset-4">
      {children}
    </RT.Content>
  );
}
