// Khung trang chung của mọi màn trừ Đăng nhập (mục 6 và 7 của
// docs/GIAO-DIEN.md; trang mẫu docs/mau-giao-dien/bang-dieu-khien-*.html):
// thanh đầu trang, rồi một hàng flex có xuống dòng gồm menu chính (192 px) và
// nội dung. Màn hẹp thì menu nằm trên, nội dung nằm dưới.
//
// Chưa đăng nhập (GET /api/v1/session trả logged_in=false) thì chuyển tới
// /dang-nhap, kèm ?next= để đăng nhập xong quay lại đúng chỗ.

import { useEffect, useState, type ReactNode } from "react";
import { Link, Navigate, NavLink, Outlet, useLocation } from "react-router";
import { errorMessage, loginHref, loginPath, post } from "./api/client";
import { useExitViewAs, useWho, type Who } from "./api/m13-users";
import { machineStatus, useOverview, useSession } from "./api/queries";
import type { Overview, StackSummary } from "./api/types";
import { AiDrawer } from "./ai/AiDrawer";
import { AiProvider, useAi } from "./ai/AiProvider";
import { JobTrackerProvider } from "./components/JobTracker";
import { CommandPalette } from "./ai/CommandPalette";
import { drawerShortcut, paletteKbd, paletteShortcut } from "./ai/platform";
import { buttonClass } from "./components/Button";
import { Logo } from "./components/Logo";
import { ScreenHeader } from "./components/ScreenHeader";
import { SegmentButton } from "./components/SegmentButton";
import { Tag } from "./components/Tag";
import { useLang, useStrings, type Lang } from "./i18n";
import { strings as aiStrings } from "./i18n/ai";
import { strings } from "./i18n/khung";
import { screens, type MenuGroup, type ScreenEntry } from "./routes";
import { useTheme, type Theme } from "./theme";
import { useTips } from "./tips";

type T = (typeof strings)["vi"];

export function Layout() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const location = useLocation();
  const session = useSession();
  const loggedIn = session.data?.logged_in === true;
  const overview = useOverview(loggedIn);
  const who = useWho();

  if (session.isPending) return <p className="px-[32px] py-[24px] text-muted">{t.loading}</p>;
  if (session.isError) {
    return (
      <div className="px-[32px] py-[24px]">
        <p className="m-0">{errorMessage(session.error, lang)}</p>
        <button type="button" onClick={() => window.location.reload()} className={buttonClass("secondary", "mt-[12px]")}>
          {t.retry}
        </button>
      </div>
    );
  }
  if (!loggedIn) return <Navigate to={loginHref(location.pathname + location.search)} replace />;

  return (
    // AiProvider bọc cả user thường: vài màn dùng chung (sự cố, site, job) gọi useAi
    // nên thiếu provider là lỗi. Nhưng ngăn AI và nút hỏi AI chỉ vẽ cho admin; thanh lệnh
    // có cho cả hai vai (user thường chỉ thấy màn của mình). Provider không gọi API AI với user thường.
    <AiProvider>
      <JobTrackerProvider>
        <Shell t={t} hostname={session.data.hostname ?? ""} demo={session.data.demo} overview={overview.data} who={who} version={session.data.version ?? ""} />
      </JobTrackerProvider>
    </AiProvider>
  );
}

/**
 * Khung trang sau khi đã đăng nhập. Tách khỏi Layout vì ngăn AI và thanh lệnh
 * cần AiProvider, mà provider chỉ nên có khi đã đăng nhập (nó báo máy chủ mỗi
 * lần chuyển mục). Ngăn AI nằm ở đây, không trong màn nào, nên đi theo người
 * dùng qua mọi mục.
 */
function Shell({ t, hostname, demo, overview, who, version }: { t: T; hostname: string; demo: boolean; overview?: Overview; who: Who; version: string }) {
  const ai = useAi();
  const [palette, setPalette] = useState(false);
  const admin = who.isAdmin;

  // ⌘K (Ctrl+K) mở hoặc đóng thanh lệnh; ⌘/ (Ctrl+/) mở hoặc đóng ngăn AI. Bắt ở
  // cả hai hệ điều hành vì người dùng Mac cắm bàn phím ngoài vẫn gõ Ctrl.
  // User thường (mục 6.12) có thanh lệnh để mở màn nhưng không có ngăn AI, nên chỉ bắt ⌘K.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return;
      if (e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPalette((p) => !p);
      } else if (e.key === "/" && admin) {
        e.preventDefault();
        ai.toggle();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [admin, ai.toggle]);

  return (
    <>
      {who.viewedBy && <ViewAsBar t={t} name={who.name} admin={who.viewedBy} />}
      <div className="mx-auto box-border max-w-[1440px] px-[32px] pb-[48px]">
        <a href="#noi-dung" className="sr-only focus:not-sr-only focus:absolute focus:top-[8px] focus:left-[8px] focus:bg-surface focus:p-[8px]">
          {t.skipToContent}
        </a>
        <Header t={t} hostname={hostname || overview?.hostname || ""} stack={overview?.stack} demo={demo} overview={overview} who={who} onOpenPalette={() => setPalette(true)} />
        <div className="flex flex-wrap gap-x-[32px]">
          <Menu t={t} overview={overview} admin={admin} />
          <main id="noi-dung" tabIndex={-1} className="min-w-0 flex-[999_1_560px]">
            <Outlet />
          </main>
          {admin && <AiDrawer />}
        </div>
        {/* Phiên bản đang chạy, lấy từ /api/v1/session: nhìn là biết máy này đã cập nhật chưa. */}
        {version && (
          <footer className="mt-[40px] border-t border-hair pt-[12px] font-mono text-[12px] text-muted">
            TPAN {version}
          </footer>
        )}
        <CommandPalette open={palette} onOpenChange={setPalette} />
      </div>
    </>
  );
}

/**
 * Dải "đang xem như <tên>" (mục 6.12). Màu nổi bật nhưng vẫn trong hệ: đảo
 * --ink và --on-ink (nền đen chữ trắng ở sáng, ngược lại ở tối). Vàng và đỏ
 * chỉ dành cho dấu trạng thái (mục 2 của GIAO-DIEN.md) nên không dùng ở đây.
 */
function ViewAsBar({ t, name, admin }: { t: T; name: string; admin: string }) {
  const exit = useExitViewAs();
  return (
    <div role="status" className="bg-ink text-on-ink">
      <div className="mx-auto box-border flex max-w-[1440px] flex-wrap items-center gap-x-[20px] gap-y-[8px] px-[32px] py-[10px]">
        <div className="min-w-0 flex-[1_1_320px]">
          <span className="font-semibold">{t.viewAsTitle(name)}</span>
          <span className="ml-[10px]">{t.viewAsBody(admin)}</span>
        </div>
        <button
          type="button"
          disabled={exit.isPending}
          onClick={() => exit.mutate()}
          className="inline-flex h-[44px] items-center border border-on-ink bg-ink px-[14px] text-[14px] font-medium text-on-ink hover:bg-on-ink hover:text-ink disabled:cursor-default sm:h-[40px]"
        >
          {exit.isPending ? t.viewAsExiting : t.viewAsExit}
        </button>
        {exit.isError && <p role="alert" className="m-0 basis-full text-[13px]">{t.viewAsExitFailed}</p>}
      </div>
    </div>
  );
}

/** Bọc màn chỉ dành cho admin: user thường thấy trang "không có quyền" gọn, không vỡ. */
export function AdminOnly({ children }: { children: ReactNode }) {
  const { isAdmin } = useWho();
  return isAdmin ? <>{children}</> : <Forbidden />;
}

function Forbidden() {
  const t = useStrings(strings);
  return (
    <>
      <ScreenHeader title={t.forbiddenTitle} summary={t.forbiddenBody} />
      <p className="mt-[16px]">
        <Link to="/">{t.notFoundHome}</Link>
      </p>
    </>
  );
}

function Header({ t, hostname, stack, demo, overview, who, onOpenPalette }: { t: T; hostname: string; stack?: StackSummary; demo: boolean; overview?: Overview; who: Who; onOpenPalette: () => void }) {
  const a = useStrings(aiStrings);
  const ai = useAi();
  const { lang, setLang } = useLang();
  const { theme, setTheme } = useTheme();
  const { tips, setTips } = useTips();
  // User thường: overview đã lọc theo chủ, nên logo phản ánh site và sự cố của họ.
  const status = overview ? machineStatus(overview) : null;

  return (
    <header className="flex flex-wrap items-center gap-x-[24px] gap-y-[12px] border-b-2 border-ink pt-[20px] pb-[18px]">
      <div className="flex items-center gap-[14px]">
        <Link
          to="/"
          aria-label={status ? `${t.homeLabel} · ${t.machineState(t.status[status])}` : t.homeLabel}
          className="flex items-center gap-[10px] text-ink no-underline hover:text-ink"
        >
          <Logo status={status} />
          <span className="font-mono text-[22px] leading-none font-semibold tracking-[0.04em]">TPAN</span>
        </Link>
        {hostname && <span className="font-mono text-[13px] text-muted">{hostname}</span>}
      </div>

      <div className="flex flex-wrap gap-[6px]">
        {demo && (
          <Tag title={t.demoHint}>
            {t.demo}
            <span className="sr-only">: {t.demoHint}</span>
          </Tag>
        )}
        {stack && <StackTags t={t} stack={stack} />}
        {who.identity && who.name !== "" && (
          // Tên đang đăng nhập và vai (mục 6.12). Nhãn là chữ mono như các nhãn khác trên thanh.
          <Tag title={t.signedInAs(who.name, t.roles[who.identity.role])}>
            {who.name} · {t.roles[who.identity.role]}
            <span className="sr-only">: {t.signedInAs(who.name, t.roles[who.identity.role])}</span>
          </Tag>
        )}
      </div>

      <div className="ml-auto flex flex-[1_1_420px] flex-wrap items-center justify-end gap-[10px]">
        <button
          type="button"
          onClick={onOpenPalette}
          aria-haspopup="dialog"
          aria-keyshortcuts={paletteShortcut}
          className="box-border flex h-[44px] max-w-[400px] flex-[1_1_260px] items-center gap-[8px] border border-ink bg-surface px-[10px] text-left text-[14px] text-muted hover:bg-pre sm:h-[40px]"
        >
          <svg aria-hidden="true" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="flex-none">
            <circle cx="11" cy="11" r="6.5" />
            <path d="M16 16l4.5 4.5" />
          </svg>
          <span className="min-w-0 flex-1 overflow-hidden text-ellipsis whitespace-nowrap">{who.isAdmin ? t.command : t.commandUser}</span>
          <kbd className="border border-hair px-[5px] py-[1px] font-mono text-[11px] text-muted">{paletteKbd}</kbd>
        </button>

        {who.isAdmin && (
          <button
            type="button"
            id="nut-hoi-ai"
            aria-expanded={ai.open}
            aria-controls="ngan-ai"
            aria-keyshortcuts={drawerShortcut}
            title={a.askAiHint}
            onClick={ai.toggle}
            className={buttonClass("secondary")}
          >
            {a.askAi}
          </button>
        )}

        <SegmentButton<Lang>
          label={t.langGroup}
          value={lang}
          onChange={setLang}
          options={[
            { value: "vi", label: "VI" },
            { value: "en", label: "EN" },
          ]}
        />
        <SegmentButton<Theme>
          label={t.themeGroup}
          value={theme}
          onChange={setTheme}
          options={[
            { value: "light", label: t.light },
            { value: "dark", label: t.dark },
          ]}
        />
        <button type="button" role="switch" aria-checked={tips} onClick={() => setTips(!tips)} className={buttonClass("secondary")}>
          {t.tips(tips)}
        </button>
      </div>
    </header>
  );
}

function StackTags({ t, stack }: { t: T; stack: StackSummary }) {
  if (!stack.installed) return <Tag>{t.stackMissing}</Tag>;
  return (
    <>
      {stack.web && <Tag>{stack.web === "nginx" ? "LEMP" : "LAMP"}</Tag>}
      {stack.web && <Tag>{stack.web}</Tag>}
      {stack.php_version && <Tag>{t.php(stack.php_version)}</Tag>}
    </>
  );
}

const groupOrder: MenuGroup[] = ["chinh", "may", "ai", "taiKhoan"];

const itemClass =
  "flex min-h-[44px] items-center justify-between gap-[8px] border-l-[3px] py-[4px] pr-[10px] pl-[12px] text-[14px] text-ink no-underline hover:text-ink sm:min-h-[40px]";

function Menu({ t, overview, admin }: { t: T; overview?: Overview; admin: boolean }) {
  const [logoutError, setLogoutError] = useState(false);
  // User thường không thấy mục của màn chỉ dành cho admin (routes.tsx, cờ admin).
  const items = screens.filter((s): s is ScreenEntry & { menu: NonNullable<ScreenEntry["menu"]> } => s.menu !== undefined && (admin || !s.admin));

  const logout = async () => {
    setLogoutError(false);
    try {
      await post<void>("/api/v1/logout");
      // Tải lại hẳn để bỏ mọi dữ liệu của phiên cũ đang giữ trong bộ nhớ.
      window.location.assign(loginPath);
    } catch {
      setLogoutError(true);
    }
  };

  return (
    <nav aria-label={t.navLabel} className="min-w-0 flex-[1_1_192px] border-r border-hair pt-[20px] pb-[8px]">
      {groupOrder.map((g) => {
        const inGroup = items.filter((s) => s.menu.group === g);
        if (inGroup.length === 0 && g !== "taiKhoan") return null;
        return (
          <div key={g}>
            {g !== "chinh" && <div className="pt-[18px] pb-[6px] pl-[15px] font-mono text-[11px] tracking-[0.08em] text-muted">{t.groups[g]}</div>}
            {inGroup.map((s) => {
              const n = overview && s.menu.count ? s.menu.count(overview) : undefined;
              const label = (
                <>
                  <span>{t.menu[s.menu.label]}</span>
                  {n !== undefined && <Count n={n} alert={s.menu.alert === true} t={t} />}
                </>
              );
              // Mục chỉ là liên kết (chưa có màn) thì không bao giờ là "đang mở".
              if (!s.load) {
                return (
                  <Link key={s.menu.label} to={s.path} className={`${itemClass} border-transparent`}>
                    {label}
                  </Link>
                );
              }
              return (
                <NavLink
                  key={s.menu.label}
                  to={s.path}
                  end={s.path === "/"}
                  // NavLink tự đặt aria-current="page" khi đang mở.
                  className={({ isActive }) => `${itemClass} ${isActive ? "border-ink bg-surface font-semibold" : "border-transparent"}`}
                >
                  {label}
                </NavLink>
              );
            })}
            {g === "taiKhoan" && (
              <>
                <button type="button" onClick={logout} className={`${itemClass} w-full border-transparent text-left`}>
                  {t.menu.logout}
                </button>
                {logoutError && (
                  <p role="alert" className="m-0 py-[6px] pr-[10px] pl-[15px] text-[13px] text-fail-text">
                    {t.logoutFailed}
                  </p>
                )}
              </>
            )}
          </div>
        );
      })}
    </nav>
  );
}

function Count({ n, alert, t }: { n: number; alert: boolean; t: T }) {
  if (alert && n > 0) {
    return (
      <span className="bg-fail px-[7px] py-[1px] font-mono text-[12px] font-semibold text-on-fail">
        {n}
        <span className="sr-only">{t.openIncidents}</span>
      </span>
    );
  }
  return <span className="font-mono text-[12px] text-muted">{n}</span>;
}

/** Đường dẫn không khớp màn nào. */
export function NotFound() {
  const t = useStrings(strings);
  return (
    <>
      <ScreenHeader title={t.notFoundTitle} summary={t.notFoundBody} />
      <p className="mt-[16px]">
        <Link to="/">{t.notFoundHome}</Link>
      </p>
    </>
  );
}
