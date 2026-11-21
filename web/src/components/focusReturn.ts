// Trả focus về đúng chỗ khi một hộp thoại đóng (phát hiện V6 của lần soát cuối
// M16). Radix Dialog chỉ tự trả focus về Dialog.Trigger, mà panel mở hộp thoại
// bằng state (từ menu ba chấm, từ nút có điều kiện), nên không có trigger và
// focus rơi về <body>: người dùng bàn phím phải Tab lại từ đầu trang.
//
// Cách làm: nhớ phần tử cuối cùng nhận focus nằm NGOÀI menu thả và hộp thoại.
// Mở hộp thoại từ một mục của menu ba chấm thì phần tử đó là chính nút ba chấm
// (focus vào menu và vào hộp thoại không ghi đè), nên đóng hộp thoại là về lại
// nút ba chấm. Phần tử đã bị gỡ khỏi trang (ví dụ hàng vừa bị xoá) thì để Radix
// làm như mặc định.

let last: HTMLElement | null = null;

document.addEventListener(
  "focusin",
  (e) => {
    const t = e.target;
    if (t instanceof HTMLElement && !t.closest('[role="menu"],[role="dialog"],[role="alertdialog"]')) last = t;
  },
  true,
);

/** Gắn vào onCloseAutoFocus của RadixDialog.Content. */
export function returnFocus(e: Event) {
  if (last && last.isConnected && last !== document.body) {
    e.preventDefault();
    last.focus();
  }
}
