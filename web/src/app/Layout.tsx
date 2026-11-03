import { useEffect, useState } from "react";
import { NavLink, Outlet, useLocation } from "react-router";
import {
  Boxes,
  Database,
  Layers,
  LayoutDashboard,
  LogOut,
  Map as MapIcon,
  Menu,
  Moon,
  Palette,
  Image,
  Search,
  Settings2,
  Sun,
} from "lucide-react";
import { logout } from "@/api/client";
import { useHealth } from "@/api/hooks";
import { Button } from "@/components/ui/button";
import { StatusDot } from "@/components/ui/badge";
import { APP_NAME, AppLogo, MapServerCredit } from "@/components/ui/credit";
import { Tooltip } from "@/components/ui/misc";
import { cn } from "@/lib/utils";
import { useTheme } from "./theme";
import { CommandPalette } from "./CommandPalette";

type NavItem = { to: string; label: string; icon: typeof Database; end?: boolean };

const nav: { title?: string; items: NavItem[] }[] = [
  { items: [{ to: "/", label: "Dashboard", icon: LayoutDashboard, end: true }] },
  {
    title: "Data",
    items: [
      { to: "/workspaces", label: "Workspaces", icon: Boxes },
      { to: "/stores", label: "Stores", icon: Database },
      { to: "/layers", label: "Layers", icon: Layers },
    ],
  },
  {
    title: "Styling",
    items: [
      { to: "/styles", label: "Styles", icon: Palette },
      { to: "/icons", label: "Icons", icon: Image },
    ],
  },
  {
    title: "Tools",
    items: [
      { to: "/map", label: "Map viewer", icon: MapIcon },
      { to: "/system", label: "System", icon: Settings2 },
    ],
  },
];

export function Layout({ onLogout }: { onLogout: () => void }) {
  const { theme, toggle } = useTheme();
  const health = useHealth();
  const [mobileOpen, setMobileOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const location = useLocation();

  useEffect(() => setMobileOpen(false), [location.pathname]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  const ok = health.data ? health.data.status === "ok" : health.isError ? false : undefined;
  const healthText = health.data
    ? Object.entries(health.data.checks)
        .map(([k, c]) => `${k}: ${c.ok ? "OK" : c.error}`)
        .join(" · ")
    : health.isError
      ? "API unreachable"
      : "Checking…";

  const sidebar = (
    <nav className="flex h-full flex-col px-2.5 py-3">
      <div className="mb-3 flex h-8 items-center gap-2 px-2">
        <AppLogo className="size-6" />
        <span className="text-[13px] font-semibold">{APP_NAME}</span>
        <span className="rounded border px-1 text-[10px] font-medium text-muted-foreground">ADMIN</span>
      </div>
      <div className="flex flex-col gap-3">
        {nav.map((group, i) => (
          <div key={i} className="flex flex-col gap-px">
            {group.title && (
              <div className="px-2 pb-1 text-[11px] font-medium uppercase tracking-wider text-muted-foreground/70">
                {group.title}
              </div>
            )}
            {group.items.map(({ to, label, icon: Icon, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  cn(
                    "flex h-8 items-center gap-2.5 rounded-md px-2 text-[13px] transition-colors",
                    isActive
                      ? "bg-accent font-medium text-accent-foreground"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground",
                  )
                }
              >
                <Icon className="size-4" />
                {label}
              </NavLink>
            ))}
          </div>
        ))}
      </div>
      <div className="mt-auto">
        <Button
          variant="ghost"
          className="w-full justify-start px-2 text-muted-foreground"
          onClick={() => {
            void logout();
            onLogout();
          }}
        >
          <LogOut /> Sign out
        </Button>
        <MapServerCredit className="mt-2 border-t px-2 pt-2" />
      </div>
    </nav>
  );

  return (
    <div className="flex h-full">
      {/* Desktop sidebar */}
      <aside className="hidden w-52 shrink-0 border-r bg-sidebar md:block">{sidebar}</aside>

      {/* Mobile sidebar */}
      {mobileOpen && (
        <div className="fixed inset-0 z-40 md:hidden">
          <div className="absolute inset-0 bg-black/40" onClick={() => setMobileOpen(false)} />
          <aside className="absolute inset-y-0 left-0 w-60 border-r bg-sidebar shadow-xl">{sidebar}</aside>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-12 shrink-0 items-center gap-2 border-b bg-background/80 px-4 backdrop-blur sm:px-6">
          <Button variant="ghost" size="icon" className="md:hidden" onClick={() => setMobileOpen(true)}>
            <Menu />
          </Button>
          <button
            onClick={() => setPaletteOpen(true)}
            className="flex h-8 w-full max-w-xs cursor-pointer items-center gap-2 rounded-md border border-input bg-card px-2.5 text-[13px] text-muted-foreground transition-colors hover:bg-muted"
          >
            <Search className="size-3.5" />
            <span className="flex-1 text-left">Search layers, stores…</span>
            <kbd className="hidden rounded border bg-muted px-1.5 font-mono text-[10px] sm:inline">Ctrl K</kbd>
          </button>
          <div className="ml-auto flex items-center gap-1">
            <Tooltip content={healthText}>
              <span className="flex items-center gap-2 rounded-md px-2 py-1 text-xs text-muted-foreground">
                <StatusDot ok={ok} />
                <span className="hidden sm:inline">{ok === undefined ? "…" : ok ? "Healthy" : "Degraded"}</span>
              </span>
            </Tooltip>
            <Button variant="ghost" size="icon" onClick={toggle} aria-label="Toggle theme">
              {theme === "dark" ? <Sun /> : <Moon />}
            </Button>
          </div>
        </header>
        <main className="flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-[1600px] px-4 py-5 sm:px-6">
            <Outlet />
          </div>
        </main>
      </div>

      <CommandPalette open={paletteOpen} onOpenChange={setPaletteOpen} />
    </div>
  );
}

