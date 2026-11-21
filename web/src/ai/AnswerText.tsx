// Câu trả lời của AI hiện như chữ thường. Không dùng dangerouslySetInnerHTML:
// câu trả lời có thể chứa chữ do người lạ gửi tới (mục 8.1, nguyên tắc 5), nên
// mọi thứ đi qua React và chỉ bốn kiểu được vẽ khác chữ thường:
//
//   **chữ**             in đậm
//   `mã`                chữ mono trong dòng (Code)
//   ```                 khối nhiều dòng (Code block), mở và đóng bằng ba dấu `
//   mã
//   ```
//   tpan:site/<domain>  ...  đổi thành tên mục (nút "Đi tới" nằm ở cuối lượt)
//
// Xuống dòng giữ nguyên (whitespace-pre-wrap). Mọi thứ khác, kể cả thẻ HTML
// hay Markdown khác, hiện đúng từng ký tự.

import type { ReactNode } from "react";
import { Code } from "../components/MonoText";
import { useLang } from "../i18n";
import { linkPattern, resolveLink } from "./views";

// Khối mã: ```[tên ngôn ngữ]\n...\n```. Nhóm 1 là thân khối. Đứng đầu danh sách
// các kiểu để ba dấu ` không bị nhận nhầm là mã trong dòng.
const fence = /```[^\n`]*\n([\s\S]*?)\n?```/;
const tokenPattern = new RegExp(`${fence.source}|\\*\\*([^*\\n]+)\\*\\*|\`([^\`\\n]+)\`|${linkPattern.source}`, "g");

// AI (và bộ não demo) hay bọc liên kết kiểu Markdown: [blog.example.com](tpan:site/blog.example.com).
// Bỏ phần bọc để chỉ còn tpan:..., nếu không chữ hiện ra là "[blog.example.com](Trang site blog.example.com)".
const mdLink = /\[[^\]\n]*\]\((tpan:[^)\s]+)\)/g;

export function AnswerText({ text: raw }: { text: string }) {
  const { lang } = useLang();
  const text = raw.replace(mdLink, "$1");
  const nodes: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(tokenPattern)) {
    const at = m.index;
    let before = text.slice(last, at);
    let end = at + m[0].length;
    if (m[1] !== undefined) {
      // Khối mã tự chiếm dòng: bỏ một dấu xuống dòng ngay trước và ngay sau nó, nếu không có một dòng trống thừa.
      if (before.endsWith("\n")) before = before.slice(0, -1);
      if (text[end] === "\n") end++;
    }
    if (before) nodes.push(before);
    if (m[1] !== undefined) {
      nodes.push(
        <Code key={at} block className="my-[6px]">
          {m[1]}
        </Code>,
      );
    } else if (m[2] !== undefined) {
      nodes.push(<strong key={at}>{m[2]}</strong>);
    } else if (m[3] !== undefined) {
      nodes.push(<Code key={at}>{m[3]}</Code>);
    } else {
      // Liên kết tpan:...: route có thật thì hiện tên mục, không thì để nguyên chữ thường.
      // Tên mục có thể chứa tên miền nên viết bằng chữ thường (sans), chỉ đậm hơn một chút.
      const link = resolveLink(m[4]!, m[5]!, m[6] !== undefined, lang);
      nodes.push(
        link ? (
          <span key={at} className="font-medium">
            {link.label}
          </span>
        ) : (
          m[0]
        ),
      );
    }
    last = end;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return <p className="m-0 text-[14px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]">{nodes}</p>;
}
