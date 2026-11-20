// Câu trả lời của AI hiện như chữ thường. Không dùng dangerouslySetInnerHTML:
// câu trả lời có thể chứa chữ do người lạ gửi tới (mục 8.1, nguyên tắc 5), nên
// mọi thứ đi qua React và chỉ ba kiểu được vẽ khác chữ thường:
//
//   **chữ**   in đậm
//   `mã`      chữ mono
//   tpan:site/<domain>  ...  đổi thành tên mục (nút "Đi tới" nằm ở cuối lượt)
//
// Xuống dòng giữ nguyên (whitespace-pre-wrap). Mọi thứ khác, kể cả thẻ HTML
// hay Markdown khác, hiện đúng từng ký tự.

import type { ReactNode } from "react";
import { useLang } from "../i18n";
import { linkPattern, resolveLink } from "./views";

const tokenPattern = new RegExp(`\\*\\*([^*\\n]+)\\*\\*|\`([^\`\\n]+)\`|${linkPattern.source}`, "g");

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
    if (at > last) nodes.push(text.slice(last, at));
    if (m[1] !== undefined) {
      nodes.push(<strong key={at}>{m[1]}</strong>);
    } else if (m[2] !== undefined) {
      nodes.push(
        <code key={at} className="font-mono text-[13px] [overflow-wrap:anywhere]">
          {m[2]}
        </code>,
      );
    } else {
      // Liên kết tpan:...: route có thật thì hiện tên mục, không thì để nguyên chữ thường.
      const link = resolveLink(m[3]!, m[4]!, m[5] !== undefined, lang);
      nodes.push(link ? <span key={at} className="font-mono text-[13px]">{link.label}</span> : m[0]);
    }
    last = at + m[0].length;
  }
  if (last < text.length) nodes.push(text.slice(last));
  return <p className="m-0 text-[14px] leading-[1.5] whitespace-pre-wrap [overflow-wrap:anywhere]">{nodes}</p>;
}
