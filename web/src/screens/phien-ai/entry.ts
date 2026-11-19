// Chữ nói lối vào của một phiên: dùng chung cho danh sách và trang một phiên.

import type { AISession } from "../../api/types";
import type { strings } from "../../i18n/phien-ai";

type T = (typeof strings)["vi"];

export function entryText(t: Pick<T, "entryPanel" | "entryMcp" | "entryMcpClient">, s: Pick<AISession, "entry" | "client">): string {
  if (s.entry === "panel") return t.entryPanel;
  return s.client ? t.entryMcpClient(s.client) : t.entryMcp;
}
