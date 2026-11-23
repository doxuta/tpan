// Khung trang chung của mọi màn trừ Đăng nhập (bản 2: mục 3.5 của
// docs/thay-doi/M16-thiet-ke.md, trang mẫu docs/mau-giao-dien/v2/*.html):
// thanh đầu trang dính trên cùng, menu trái 232 px dính dưới nó, rồi nội dung.
// Dưới 1024 px menu trái ẩn sau nút "Menu" và hiện thành một khối dưới thanh
// đầu trang.
//
// Chưa đăng nhập (GET /api/v1/session trả logged_in=false) thì chuyển tới
// /dang-nhap, kèm ?next= để đăng nhập xong quay lại đúng chỗ.

import { useEffect, useState, type ReactNode } from "react";
import { Link, Navigate, NavLink, Outlet, useLocation } from "react-router";
import { errorMessage, loginHref, loginPath, post } from "./api/client";
import { useSelfAutoCheck } from "./api/m15";
import { useExitViewAs, useWho, type Who } from "./api/m13-users";
import { machineStatus, useOverview, useSession } from "./api/queries";
import type { Overview } from "./api/types";
import { AiDrawer } from "./ai/AiDrawer";
import { AiProvider, useAi } from "./ai/AiProvider";
import { CommandPalette } from "./ai/CommandPalette";
import { drawerShortcut, paletteKbd, paletteShortcut } from "./ai/platform";
import { Avatar } from "./components/Avatar";
import { Button, buttonClass, IconButton } from "./components/Button";
import { Icon } from "./components/Icon";
import { JobTrackerProvider } from "./components/JobTracker";
import { Logo, Wordmark } from "./components/Logo";
import { Menu, MenuCheckboxItem, MenuItem, MenuLabel, MenuRadioGroup, MenuRadioItem, MenuSeparator } from "./components/Menu";
import { ScreenHeader } from "./components/ScreenHeader";
import { StatusMark } from "./components/StatusMark";
import { useLang, useStrings, type Lang } from "./i18n";
import { strings as aiStrings } from "./i18n/ai";
import { strings } from "./i18n/khung";
import { strings as phienBan } from "./i18n/phien-ban";
import { menuPath, screens, type MenuGroup, type ScreenEntry } from "./routes";
import { useTheme, type ThemeChoice } from "./theme";
import { useTips } from "./tips";

type T = (typeof strings)["vi"];

export function Layout() {
  const t = useStrings(strings);
  const { lang } = useLang();
  const location = useLocation();
  const session = useSession();
  const loggedIn = session.data?.logged_in === true;
  // M20: chính sách bắt bật hai bước mà user chưa bật: máy chủ trả 403 mfa_enroll_required cho mọi việc
  // trừ trang bật, nên đừng gọi tổng quan và đưa người dùng tới /bat-hai-buoc.
  const mustEnroll = loggedIn && session.data?.mfa_enroll_required === true;
  const overview = useOverview(loggedIn && !mustEnroll);
  const who = useWho();

  if (session.isPending) return <p className="px-[32px] py-[24px] text-muted">{t.loading}</p>;
  // Chỉ báo lỗi cả trang khi chưa từng đọc được phiên. Đã có dữ liệu mà lần hỏi
  // lại hỏng (ví dụ quay lại tab đúng lúc panel restart vì cập nhật TPAN, mục
  // 6.14) thì giữ khung trang: khối "Phiên bản và cập nhật" phải sống qua lần
  // restart, và từng màn tự báo lỗi của lời gọi của nó.
  if (session.isError && !session.data) {
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
  if (mustEnroll) return <Navigate to="/bat-hai-buoc" replace />;

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
  // Màn hẹp: menu trái mở thành một khối dưới thanh đầu trang.
  const [navOpen, setNavOpen] = useState(false);
  const [logoutError, setLogoutError] = useState(false);
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

  // Router không tự cuộn tới neo (#phien-ban, #bm-co-lap). Màn tải lười và đầy
  // dần khi dữ liệu về, nên cuộn ngay rồi cuộn lại mỗi lần nội dung đổi cỡ
  // trong 2,5 giây đầu. location.key đổi ở mỗi lần bấm liên kết, kể cả khi đang
  // ở sẵn đúng đường dẫn đó, nên bấm lại vẫn cuộn. scroll-padding-top của html
  // (styles.css) giữ khối không nằm khuất dưới thanh đầu trang dính.
  const { hash, key } = useLocation();
  useEffect(() => {
    const id = decodeURIComponent(hash.slice(1));
    const main = document.getElementById("noi-dung");
    if (!id || !main) return;
    let focused = false;
    const go = () => {
      const el = document.getElementById(id);
      if (!el) return;
      el.scrollIntoView({ block: "start" });
      // Như neo thường của trình duyệt: Tab kế tiếp đi tiếp từ khối đó (chỉ có tác dụng với phần tử có tabIndex).
      // Khối neo có thể bị thay bằng một nút mới khi dữ liệu về (thẻ "đang tải" thành thẻ có dữ liệu,
      // ví dụ #bm-co-lap): focus khi đó rơi về <body>, nên đặt lại vào nút mới. Người dùng đã đi
      // chỗ khác (focus không ở <body>) thì không giành lại.
      if (!focused || document.activeElement === document.body) el.focus({ preventScroll: true });
      focused = true;
    };
    const watch = new ResizeObserver(go); // gọi go ngay lần đầu quan sát
    watch.observe(main);
    const stop = setTimeout(() => watch.disconnect(), 2500);
    return () => {
      watch.disconnect();
      clearTimeout(stop);
    };
  }, [hash, key]);

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
    <>
      <a href="#noi-dung" className="sr-only focus:not-sr-only focus:fixed focus:top-[8px] focus:left-[8px] focus:z-50 focus:rounded-ctl focus:bg-surface focus:p-[8px]">
        {t.skipToContent}
      </a>
      {who.viewedBy && <ViewAsBar t={t} name={who.name} admin={who.viewedBy} />}
      <Header
        t={t}
        hostname={hostname || overview?.hostname || ""}
        demo={demo}
        overview={overview}
        who={who}
        navOpen={navOpen}
        onToggleNav={() => setNavOpen((o) => !o)}
        onOpenPalette={() => setPalette(true)}
        onLogout={logout}
      />
      {logoutError && (
        <p role="alert" className="m-0 border-b border-hair bg-surface px-[16px] py-[8px] text-[13px] lg:px-[24px]">
          {t.logoutFailed}
        </p>
      )}
      <div className="flex flex-col lg:flex-row lg:items-start">
        <SideMenu t={t} overview={overview} admin={admin} open={navOpen} onNavigate={() => setNavOpen(false)}>
          {/* Phiên bản đang chạy, lấy từ /api/v1/session: nhìn là biết máy này đã cập nhật chưa. */}
          {version && (
            <div id="dong-phien-ban" className="mt-auto px-[10px] pt-[20px] pb-[4px] font-mono text-[12px] text-muted">
              {/* Phiên "xem như" chỉ đọc và không phải của admin thật: giữ chữ thường như user. */}
              <VersionLine version={version} admin={admin && !who.viewedBy} />
            </div>
          )}
        </SideMenu>
        <main id="noi-dung" tabIndex={-1} className="box-border flex w-full max-w-[1264px] min-w-0 flex-1 flex-col gap-[20px] px-[16px] pt-[24px] pb-[40px] focus:outline-none lg:px-[32px]">
          <Outlet />
        </main>
      </div>
      {admin && <AiDrawer />}
      <CommandPalette open={palette} onOpenChange={setPalette} />
    </>
  );
}

/**
 * Dòng phiên bản ở cuối menu trái (mục 6.14, quyết định 10; bản 2 bỏ chân
 * trang). Admin: liên kết tới khối "Phiên bản và cập nhật" ở màn Cài đặt, kèm
 * một ô vuông nhỏ khi lần kiểm gần nhất thấy bản mới. "Có bản mới" là thông
 * tin, không phải tình trạng máy, nên dấu này màu --ink và không dùng hình nào
 * của bốn dấu trạng thái (mục 2 của GIAO-DIEN.md). User thường: chữ thường, và
 * không gọi /api/v1/self (route chỉ admin).
 */
function VersionLine({ version, admin }: { version: string; admin: boolean }) {
  const t = useStrings(phienBan);
  const hasNew = useSelfAutoCheck(admin);
  if (!admin) return <>TPAN {version}</>;
  return (
    <Link to="/settings#phien-ban" title={t.heading} className="inline-flex min-h-[24px] items-center text-muted no-underline hover:text-accent hover:underline">
      TPAN {version}
      {hasNew && (
        <>
          <span aria-hidden="true" className="ml-[6px] inline-block size-[6px] bg-ink" />
          <span className="sr-only">, {t.footerNew}</span>
        </>
      )}
    </Link>
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
      <div className="box-border flex flex-wrap items-center gap-x-[20px] gap-y-[8px] px-[16px] py-[10px] lg:px-[24px]">
        <div className="min-w-0 flex-[1_1_320px]">
          <span className="font-semibold">{t.viewAsTitle(name)}</span>
          <span className="ml-[10px]">{t.viewAsBody(admin)}</span>
        </div>
        <button
          type="button"
          disabled={exit.isPending}
          onClick={() => exit.mutate()}
          className="inline-flex h-[44px] items-center rounded-ctl border border-on-ink bg-ink px-[14px] text-[14px] font-medium text-on-ink hover:bg-on-ink hover:text-ink disabled:cursor-default sm:h-[40px]"
        >
          {exit.isPending ? t.viewAsExiting : t.viewAsExit}
        </button>
        {exit.isError && (
          <p role="alert" className="m-0 basis-full text-[13px]">
            {t.viewAsExitFailed}
          </p>
        )}
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
      <p className="m-0">
        <Link to="/">{t.notFoundHome}</Link>
      </p>
    </>
  );
}

type HeaderProps = {
  t: T;
  hostname: string;
  demo: boolean;
  overview?: Overview;
  who: Who;
  navOpen: boolean;
  onToggleNav: () => void;
  onOpenPalette: () => void;
  onLogout: () => void;
};

function Header({ t, hostname, demo, overview, who, navOpen, onToggleNav, onOpenPalette, onLogout }: HeaderProps) {
  const a = useStrings(aiStrings);
  const ai = useAi();
  // User thường: overview đã lọc theo chủ, nên logo phản ánh site và sự cố của họ.
  const status = overview ? machineStatus(overview) : null;
  const incidents = overview?.incidents.length ?? 0;
  const host = hostname || (demo ? t.demo : "");
  const search = who.isAdmin ? t.command : t.commandUser;

  return (
    // Từ 1024 px: một hàng, cao đúng 61 px (--header-h trong styles.css) mà menu trái dính theo.
    <header className="sticky top-0 z-30 box-border flex flex-wrap items-center gap-x-[20px] gap-y-[10px] border-b border-hair bg-surface px-[16px] py-[10px] lg:flex-nowrap lg:px-[24px]">
      <IconButton icon={navOpen ? "close" : "menu"} label={t.menuToggle} aria-expanded={navOpen} aria-controls="menu-chinh" onClick={onToggleNav} className="-ml-[8px] text-ink lg:hidden" />
      <Link
        to="/"
        aria-label={status ? `${t.homeLabel} · ${t.machineState(t.status[status])}` : t.homeLabel}
        className="flex flex-none items-center gap-[10px] text-ink no-underline hover:text-ink"
      >
        <Logo status={status} size={28} />
        <Wordmark />
      </Link>
      {host && (
        <span title={demo ? `${host} · ${t.demoHint}` : host} className="hidden max-w-[220px] min-w-0 flex-none truncate font-mono text-[13px] text-muted sm:block">
          {host}
          {demo && <span className="sr-only">: {t.demoHint}</span>}
        </span>
      )}

      <button
        type="button"
        onClick={onOpenPalette}
        aria-haspopup="dialog"
        aria-keyshortcuts={paletteShortcut}
        className="box-border hidden h-[40px] max-w-[440px] min-w-[160px] flex-[1_1_280px] items-center gap-[8px] rounded-ctl border border-line bg-soft px-[12px] text-left text-[14px] text-muted hover:border-field sm:flex"
      >
        <Icon name="search" size={16} />
        <span title={search} className="min-w-0 flex-1 truncate">
          {search}
        </span>
        <kbd className="rounded-chip border border-line bg-surface px-[6px] py-[1px] font-mono text-[12px] text-muted">{paletteKbd}</kbd>
      </button>

      <div className="ml-auto flex flex-none items-center gap-[8px]">
        <IconButton icon="search" label={t.searchLabel} aria-haspopup="dialog" aria-keyshortcuts={paletteShortcut} onClick={onOpenPalette} className="sm:hidden" />
        {incidents > 0 && (
          <Link to="/incidents" className="flex h-[44px] items-center gap-[8px] rounded-ctl px-[10px] font-medium whitespace-nowrap text-fail-text no-underline hover:bg-soft hover:text-fail-text sm:h-[40px]">
            <StatusMark status="fail" size="sm" />
            <span>{t.incidentsLink(incidents)}</span>
          </Link>
        )}
        {who.isAdmin && (
          // Nút duy nhất mở ngăn AI (bản 2 bỏ nút nổi ở góc). Dưới 640 px chỉ còn biểu tượng,
          // nhãn đọc giữ nguyên. "…" khi AI đang trả lời, để đóng ngăn rồi vẫn biết nó còn chạy.
          <Button id="nut-hoi-ai" aria-label={a.askAi} aria-expanded={ai.open} aria-controls="ngan-ai" aria-keyshortcuts={drawerShortcut} title={a.askAiHint} onClick={ai.toggle} className="max-sm:px-[12px]">
            <Icon name="chat" size={16} className="sm:hidden" />
            <span className="hidden sm:inline">{a.askAi}</span>
            {ai.running && (
              <span aria-hidden="true" className="text-muted">
                …
              </span>
            )}
          </Button>
        )}
        <AccountMenu t={t} who={who} onLogout={onLogout} />
      </div>
    </header>
  );
}

/** Menu tài khoản (mục 3.5): ngôn ngữ, giao diện, Gợi ý, đăng xuất. */
function AccountMenu({ t, who, onLogout }: { t: T; who: Who; onLogout: () => void }) {
  const { lang, setLang } = useLang();
  const { choice, setTheme } = useTheme();
  const { tips, setTips } = useTips();
  const role = who.identity ? t.roles[who.identity.role] : t.roles.admin;
  const name = who.name || "TPAN";
  const label = t.accountMenu(name, role);
  return (
    <Menu
      label={label}
      trigger={
        <button type="button" aria-label={label} className="flex h-[44px] items-center gap-[10px] rounded-ctl border border-transparent bg-transparent pr-[8px] pl-[6px] text-ink hover:bg-soft sm:h-[40px]">
          <Avatar name={name} size={28} />
          <span className="hidden flex-col items-start leading-[1.2] sm:flex">
            <span title={name} className="max-w-[160px] truncate font-medium">
              {name}
            </span>
            <span className="text-[12px] text-muted">{role}</span>
          </span>
          <Icon name="chevron-down" size={16} className="hidden text-muted sm:block" />
        </button>
      }
    >
      <MenuLabel>
        {name} · {role}
      </MenuLabel>
      <MenuSeparator />
      <MenuRadioGroup<Lang> label={t.langGroup} value={lang} onValueChange={setLang}>
        <MenuRadioItem value="vi">{t.langVi}</MenuRadioItem>
        <MenuRadioItem value="en">{t.langEn}</MenuRadioItem>
      </MenuRadioGroup>
      <MenuSeparator />
      <MenuRadioGroup<ThemeChoice> label={t.themeMenu} value={choice} onValueChange={setTheme}>
        <MenuRadioItem value="light">{t.light}</MenuRadioItem>
        <MenuRadioItem value="dark">{t.dark}</MenuRadioItem>
        <MenuRadioItem value="system">{t.themeSystem}</MenuRadioItem>
      </MenuRadioGroup>
      <MenuSeparator />
      <MenuCheckboxItem checked={tips} onCheckedChange={setTips}>
        {t.tipsItem}
      </MenuCheckboxItem>
      <MenuSeparator />
      <MenuItem onSelect={onLogout}>{t.menu.logout}</MenuItem>
    </Menu>
  );
}

const groupOrder: MenuGroup[] = ["chinh", "may", "ai", "taiKhoan"];

type MenuEntry = ScreenEntry & { menu: NonNullable<ScreenEntry["menu"]> };

function SideMenu({ t, overview, admin, open, onNavigate, children }: { t: T; overview?: Overview; admin: boolean; open: boolean; onNavigate: () => void; children: ReactNode }) {
  // User thường không thấy mục của màn chỉ dành cho admin (routes.tsx, cờ admin).
  const items = screens.filter((s): s is MenuEntry => s.menu !== undefined && (admin || !s.admin));
  return (
    <nav
      id="menu-chinh"
      aria-label={t.navLabel}
      className={
        `${open ? "flex" : "hidden"} box-border flex-col gap-[2px] border-b border-hair bg-surface px-[12px] py-[16px] ` +
        "lg:sticky lg:top-[var(--header-h)] lg:flex lg:h-[calc(100vh-var(--header-h))] lg:w-[232px] lg:flex-none lg:overflow-y-auto lg:border-r lg:border-b-0"
      }
    >
      {groupOrder.map((g) => {
        const inGroup = items.filter((s) => s.menu.group === g);
        if (inGroup.length === 0) return null;
        return (
          <div key={g} className="flex flex-col gap-[2px]">
            {g !== "chinh" && <div className="px-[10px] pt-[16px] pb-[6px] text-[12px] font-medium text-muted">{t.groups[g]}</div>}
            {inGroup.map((s) => {
              const to = menuPath(s, admin);
              const n = overview && s.menu.count ? s.menu.count(overview) : undefined;
              return (
                <NavLink
                  key={s.menu.label}
                  to={to}
                  end={to === "/" || to === "/dashboard"}
                  onClick={onNavigate}
                  // NavLink tự đặt aria-current="page" khi đang mở.
                  className={({ isActive }) =>
                    "flex min-h-[44px] items-center gap-[10px] rounded-ctl px-[10px] no-underline sm:min-h-[40px] " +
                    (isActive ? "bg-accent-tint font-semibold text-accent hover:text-accent" : "text-ink-soft hover:bg-soft hover:text-ink-soft")
                  }
                >
                  {({ isActive }) => (
                    <>
                      <Icon name={s.menu.icon} size={18} className={isActive ? "" : "text-muted"} />
                      <span className="min-w-0 flex-1">{t.menu[s.menu.label]}</span>
                      {n !== undefined && <Count n={n} alert={s.menu.alert === true} t={t} />}
                    </>
                  )}
                </NavLink>
              );
            })}
          </div>
        );
      })}
      {children}
    </nav>
  );
}

function Count({ n, alert, t }: { n: number; alert: boolean; t: T }) {
  if (alert && n > 0) {
    return (
      <span className="min-w-[22px] rounded-chip bg-fail px-[6px] py-[1px] text-center text-[12px] font-semibold text-on-fail tabular-nums">
        {n}
        <span className="sr-only">{t.openIncidents}</span>
      </span>
    );
  }
  return <span className="text-[12px] font-normal text-muted tabular-nums">{n}</span>;
}

/** Đường dẫn không khớp màn nào. */
export function NotFound() {
  const t = useStrings(strings);
  return (
    <>
      <ScreenHeader title={t.notFoundTitle} summary={t.notFoundBody} />
      <p className="m-0">
        <Link to="/">{t.notFoundHome}</Link>
      </p>
    </>
  );
}
