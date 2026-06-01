import { useMemo, useState } from "react";
import {
  Icon,
  type ClickyCommandRuntime,
  type ClickyResolvedCommand,
  type RenderLink,
} from "@flanksource/clicky-ui";
import { EntityExplorerApp } from "@flanksource/clicky-ui/api-explorer";
import { MissionControlLogo } from "@flanksource/icons/mi";
import { Link, Navigate, NavLink, Route, Routes, useLocation, useNavigate, useParams } from "react-router-dom";
import { apiClient } from "./api";
import { CatalogSidebar } from "./CatalogSidebar";
import { TypeView } from "./TypeView";
import { ItemView } from "./ItemView";
import { AccessBrowser, type AccessBrowserMode } from "./access/AccessBrowser";
import { CommandPalette, CommandPaletteButton } from "./CommandPalette";
import { PlaybookBrowser } from "./playbooks/PlaybookBrowser";
import { SettingsBrowser } from "./settings/SettingsBrowser";
import { SettingsMenu } from "./settings/SettingsMenu";
import { UI_BASE, routerPathFromHref } from "./navigation";

const renderLink: RenderLink = ({ to, className, children, title, key }) => (
  <Link key={key} to={routerPathFromHref(to)} className={className} title={title}>
    {children}
  </Link>
);

export function App() {
  const navigate = useNavigate();
  const [commandPaletteOpen, setCommandPaletteOpen] = useState(false);

  const navigateTo = (href: string) => navigate(routerPathFromHref(href));

  const commandRuntime = useMemo<ClickyCommandRuntime>(
    () => ({
      client: apiClient,
      hrefForCommand: (resolved) => buildCommandHref(resolved),
      onNavigate: (resolved) => {
        const href = buildCommandHref(resolved);
        if (!href) return;
        navigate(routerPathFromHref(href));
      },
    }),
    [navigate],
  );

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
      <CommandPalette
        open={commandPaletteOpen}
        onOpenChange={setCommandPaletteOpen}
        onNavigate={navigateTo}
      />
      <aside className="flex w-72 shrink-0 flex-col border-r border-border bg-muted/30">
        <Link to="/" className="flex h-14 items-center border-b border-border px-4 hover:bg-accent/40">
          <MissionControlLogo square={false} size={36} title="Mission Control" className="w-auto" />
        </Link>
        <div className="min-h-0 flex-1 overflow-auto">
          <div className="border-b border-border p-2">
            <div className="mb-2">
              <CommandPaletteButton onClick={() => setCommandPaletteOpen(true)} />
            </div>
            <SidebarButton
              to="/access/users"
              icon="lucide:user"
              label="Access Users"
            />
            <SidebarButton
              to="/access/groups"
              icon="lucide:users"
              label="Access Groups"
            />
            <SidebarButton
              to="/playbooks"
              icon="lucide:book-open-check"
              label="Playbooks"
            />
            <SidebarButton
              to="/settings/scrapers"
              icon="lucide:radar"
              label="Scrapers"
            />
            <SidebarButton
              to="/settings/scrape-plugins"
              icon="lucide:plug-zap"
              label="Scrape Plugins"
            />
          </div>
          <CatalogSidebar />
        </div>
        <div className="flex flex-col gap-density-2 border-t border-border p-2">
          <NavLink
            to="/explorer"
            className={({ isActive }) => [
              "flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              isActive
                ? "bg-accent text-accent-foreground"
                : "text-foreground/80 hover:bg-accent hover:text-accent-foreground",
            ].join(" ")}
          >
            <Icon name="lucide:database-zap" className="h-4 w-4 shrink-0 text-muted-foreground" />
            <span>API Explorer</span>
          </NavLink>
          <SettingsMenu />
        </div>
      </aside>

      <main className="min-h-0 min-w-0 flex-1 overflow-auto">
        <Routes>
          <Route index element={<Home />} />
          <Route path="type/:configType" element={<TypeRoute />} />
          <Route path="item/:id" element={<ItemRoute commandRuntime={commandRuntime} />} />
          <Route path="access/:mode" element={<AccessRoute />} />
          <Route path="access/:mode/:id" element={<AccessRoute />} />
          <Route path="playbooks" element={<PlaybookBrowser mode="list" />} />
          <Route path="playbooks/runs" element={<PlaybookBrowser mode="runs" />} />
          <Route path="playbooks/runs/:runId" element={<PlaybookRunRoute />} />
          <Route path="settings/scrapers" element={<SettingsScrapersRoute />} />
          <Route path="settings/scrapers/:scraperId" element={<SettingsScrapersRoute />} />
          <Route path="settings/scrape-plugins" element={<SettingsBrowser mode="scrape-plugins" />} />
          <Route path="explorer/*" element={<ExplorerRoute />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </main>
    </div>
  );
}

function Home() {
  return (
    <div className="p-8 text-sm text-muted-foreground">
      Select a resource type from the sidebar, or open the API Explorer.
    </div>
  );
}

function TypeRoute() {
  const { configType } = useParams();
  return <TypeView configType={configType ?? ""} />;
}

function ItemRoute({ commandRuntime }: { commandRuntime: ClickyCommandRuntime }) {
  const { id } = useParams();
  return <ItemView id={id ?? ""} commandRuntime={commandRuntime} />;
}

function AccessRoute() {
  const { mode, id } = useParams();
  if (mode !== "users" && mode !== "groups") return <Navigate to="/access/users" replace />;
  return <AccessBrowser mode={mode as AccessBrowserMode} id={id} />;
}

function PlaybookRunRoute() {
  const { runId } = useParams();
  return <PlaybookBrowser mode="run" runId={runId ?? ""} />;
}

function SettingsScrapersRoute() {
  const { scraperId } = useParams();
  return <SettingsBrowser mode="scrapers" scraperId={scraperId} />;
}

function ExplorerRoute() {
  const location = useLocation();
  const pathname = `${UI_BASE}${location.pathname}`;
  return (
    <EntityExplorerApp
      client={apiClient}
      pathname={pathname}
      renderLink={renderLink}
      basePath={`${UI_BASE}/explorer`}
    />
  );
}

function SidebarButton({
  to,
  icon,
  label,
}: {
  to: string;
  icon: string;
  label: string;
}) {
  return (
    <NavLink
      to={to}
      className={({ isActive }) => [
        "mb-1 flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-sm transition-colors last:mb-0",
        isActive
          ? "bg-accent font-semibold text-accent-foreground"
          : "text-foreground/80 hover:bg-accent/50 hover:text-accent-foreground",
      ].join(" ")}
    >
      <span className="inline-flex min-w-0 items-center gap-2">
        <Icon name={icon} className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="truncate">{label}</span>
      </span>
    </NavLink>
  );
}

// buildCommandHref maps a resolved clicky command (command name + args/flags)
// into a /ui route. getResource → /ui/item/:id; everything else falls through
// to the API Explorer command URL.
function buildCommandHref(resolved: ClickyResolvedCommand): string | undefined {
  const command = resolved.request.command;
  if (!command) return undefined;

  if (command === "getResource") {
    const id = resolved.request.args?.[0];
    if (!id) return undefined;
    return `${UI_BASE}/item/${encodeURIComponent(id)}`;
  }

  if (command === "searchResources") {
    const configType = resolved.request.flags?.config_type;
    if (configType) return `${UI_BASE}/type/${encodeURIComponent(configType)}`;
  }

  const params = new URLSearchParams();
  for (const a of resolved.request.args ?? []) params.append("arg", a);
  for (const [k, v] of Object.entries(resolved.request.flags ?? {})) params.set(k, v);
  const qs = params.toString();
  return `${UI_BASE}/explorer/commands/${encodeURIComponent(command)}${qs ? `?${qs}` : ""}`;
}
