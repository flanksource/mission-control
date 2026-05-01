import { useCallback, useMemo, useState } from "react";
import {
  DensitySwitcher,
  EntityExplorerApp,
  Icon,
  ThemeSwitcher,
  useHistoryRoute,
  type ClickyCommandRuntime,
  type ClickyResolvedCommand,
  type RenderLink,
  type UseHistoryRouteOptions,
} from "@flanksource/clicky-ui";
import { MissionControlLogo } from "@flanksource/icons/mi";
import { apiClient } from "./api";
import { CatalogSidebar } from "./CatalogSidebar";
import { TypeView } from "./TypeView";
import { ItemView } from "./ItemView";
import { AccessBrowser, type AccessBrowserMode } from "./access/AccessBrowser";
import { CommandPalette, CommandPaletteButton } from "./CommandPalette";
import { PlaybookBrowser } from "./playbooks/PlaybookBrowser";
import { SettingsBrowser, type SettingsMode } from "./settings/SettingsBrowser";

export type Route =
  | { kind: "type"; configType: string }
  | { kind: "item"; id: string }
  | { kind: "access"; mode: AccessBrowserMode; id?: string }
  | { kind: "playbooks"; mode: "list" | "runs"; runId?: undefined }
  | { kind: "playbooks"; mode: "run"; runId: string }
  | { kind: "settings"; mode: "scrapers"; scraperId?: string }
  | { kind: "settings"; mode: Exclude<SettingsMode, "scrapers"> }
  | { kind: "explorer" }
  | { kind: "home" };

const BASE = "/ui";

function parseRoute(pathname: string): Route {
  const rel = pathname.startsWith(BASE) ? pathname.slice(BASE.length) : pathname;
  if (rel === "" || rel === "/") return { kind: "home" };
  const typeMatch = rel.match(/^\/type\/(.+)$/);
  if (typeMatch) return { kind: "type", configType: decodeURIComponent(typeMatch[1]) };
  const itemMatch = rel.match(/^\/item\/(.+)$/);
  if (itemMatch) return { kind: "item", id: decodeURIComponent(itemMatch[1]) };
  const accessMatch = rel.match(/^\/access\/(users|groups)(?:\/(.+))?$/);
  if (accessMatch) {
    return {
      kind: "access",
      mode: accessMatch[1] as AccessBrowserMode,
      id: accessMatch[2] ? decodeURIComponent(accessMatch[2]) : undefined,
    };
  }
  const playbookRunMatch = rel.match(/^\/playbooks\/runs\/(.+)$/);
  if (playbookRunMatch) return { kind: "playbooks", mode: "run", runId: decodeURIComponent(playbookRunMatch[1]) };
  if (rel === "/playbooks/runs") return { kind: "playbooks", mode: "runs" };
  if (rel === "/playbooks") return { kind: "playbooks", mode: "list" };
  const scraperSettingsMatch = rel.match(/^\/settings\/scrapers(?:\/(.+))?$/);
  if (scraperSettingsMatch) {
    return {
      kind: "settings",
      mode: "scrapers",
      scraperId: scraperSettingsMatch[1] ? decodeURIComponent(scraperSettingsMatch[1]) : undefined,
    };
  }
  if (rel === "/settings/scrape-plugins") return { kind: "settings", mode: "scrape-plugins" };
  if (rel.startsWith("/explorer")) return { kind: "explorer" };
  return { kind: "home" };
}

function buildRoute(route: Route): string {
  switch (route.kind) {
    case "home":
      return `${BASE}/`;
    case "type":
      return `${BASE}/type/${encodeURIComponent(route.configType)}`;
    case "item":
      return `${BASE}/item/${encodeURIComponent(route.id)}`;
    case "access":
      return `${BASE}/access/${route.mode}${route.id ? `/${encodeURIComponent(route.id)}` : ""}`;
    case "playbooks":
      if (route.mode === "run") return `${BASE}/playbooks/runs/${encodeURIComponent(route.runId)}`;
      if (route.mode === "runs") return `${BASE}/playbooks/runs`;
      return `${BASE}/playbooks`;
    case "settings":
      if (route.mode === "scrapers") {
        return `${BASE}/settings/scrapers${route.scraperId ? `/${encodeURIComponent(route.scraperId)}` : ""}`;
      }
      return `${BASE}/settings/${route.mode}`;
    case "explorer":
      return `${BASE}/explorer`;
  }
}

const routeOptions: UseHistoryRouteOptions<Route> = {
  parse: (pathname) => parseRoute(pathname),
  build: buildRoute,
};

const renderLink: RenderLink = ({ to, className, children, title, key }) => (
  <a
    key={key}
    href={to}
    className={className}
    title={title}
    onClick={(e) => {
      // Plain left-clicks should use the SPA history; let modifier-clicks
      // and non-primary buttons fall through to native navigation.
      if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.button !== 0) {
        return;
      }
      e.preventDefault();
      window.history.pushState(null, "", to);
      window.dispatchEvent(new PopStateEvent("popstate"));
    }}
  >
    {children}
  </a>
);

export function App() {
  const [route] = useHistoryRoute<Route>(routeOptions);
  const [commandPaletteOpen, setCommandPaletteOpen] = useState(false);

  const navigateTo = useCallback((href: string) => {
    window.history.pushState(null, "", href);
    window.dispatchEvent(new PopStateEvent("popstate"));
  }, []);

  const commandRuntime = useMemo<ClickyCommandRuntime>(
    () => ({
      client: apiClient,
      hrefForCommand: (resolved) => buildCommandHref(resolved),
      onNavigate: (resolved) => {
        const href = buildCommandHref(resolved);
        if (!href) return;
        window.history.pushState(null, "", href);
        window.dispatchEvent(new PopStateEvent("popstate"));
      },
    }),
    [],
  );

  return (
    <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
      <CommandPalette
        open={commandPaletteOpen}
        onOpenChange={setCommandPaletteOpen}
        onNavigate={navigateTo}
      />
      <aside className="flex w-72 shrink-0 flex-col border-r border-border bg-muted/30">
        <a href={buildRoute({ kind: "home" })} className="flex h-14 items-center border-b border-border px-4 hover:bg-accent/40">
          <MissionControlLogo square={false} size={36} title="Mission Control" className="w-auto" />
        </a>
        <div className="min-h-0 flex-1 overflow-auto">
          <div className="border-b border-border p-2">
            <div className="mb-2">
              <CommandPaletteButton onClick={() => setCommandPaletteOpen(true)} />
            </div>
            <SidebarButton
              active={route.kind === "access" && route.mode === "users"}
              icon="lucide:user"
              label="Access Users"
              href={buildRoute({ kind: "access", mode: "users" })}
            />
            <SidebarButton
              active={route.kind === "access" && route.mode === "groups"}
              icon="lucide:users"
              label="Access Groups"
              href={buildRoute({ kind: "access", mode: "groups" })}
            />
            <SidebarButton
              active={route.kind === "playbooks"}
              icon="lucide:book-open-check"
              label="Playbooks"
              href={buildRoute({ kind: "playbooks", mode: "list" })}
            />
            <SidebarButton
              active={route.kind === "settings" && route.mode === "scrapers"}
              icon="lucide:radar"
              label="Scrapers"
              href={buildRoute({ kind: "settings", mode: "scrapers" })}
            />
            <SidebarButton
              active={route.kind === "settings" && route.mode === "scrape-plugins"}
              icon="lucide:plug-zap"
              label="Scrape Plugins"
              href={buildRoute({ kind: "settings", mode: "scrape-plugins" })}
            />
          </div>
          <CatalogSidebar
            selected={route.kind === "type" ? route.configType : null}
          />
        </div>
        <div className="flex flex-col gap-density-2 border-t border-border p-2">
          <a
            href={buildRoute({ kind: "explorer" })}
            className={[
              "flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors",
              route.kind === "explorer"
                ? "bg-accent text-accent-foreground"
                : "text-foreground/80 hover:bg-accent hover:text-accent-foreground",
            ].join(" ")}
          >
            <Icon name="lucide:database-zap" className="h-4 w-4 shrink-0 text-muted-foreground" />
            <span>API Explorer</span>
          </a>
          <ThemeSwitcher className="w-full justify-between" />
          <DensitySwitcher className="w-full justify-between" />
        </div>
      </aside>

      <main className="min-h-0 min-w-0 flex-1 overflow-auto">
        {route.kind === "home" && (
          <div className="p-8 text-sm text-muted-foreground">
            Select a resource type from the sidebar, or open the API Explorer.
          </div>
        )}
        {route.kind === "type" && (
          <TypeView configType={route.configType} />
        )}
        {route.kind === "item" && (
          <ItemView id={route.id} commandRuntime={commandRuntime} />
        )}
        {route.kind === "access" && (
          <AccessBrowser mode={route.mode} id={route.id} />
        )}
        {route.kind === "playbooks" && (
          <PlaybookBrowser mode={route.mode} runId={route.runId} />
        )}
        {route.kind === "settings" && (
          <SettingsBrowser mode={route.mode} scraperId={route.mode === "scrapers" ? route.scraperId : undefined} />
        )}
        {route.kind === "explorer" && (
          <EntityExplorerApp
            client={apiClient}
            pathname={window.location.pathname}
            renderLink={renderLink}
            basePath={`${BASE}/explorer`}
          />
        )}
      </main>
    </div>
  );
}

function SidebarButton({
  active,
  icon,
  label,
  href,
}: {
  active: boolean;
  icon: string;
  label: string;
  href: string;
}) {
  return (
    <a
      href={href}
      className={[
        "mb-1 flex w-full items-center justify-between gap-2 rounded px-2 py-1.5 text-sm transition-colors last:mb-0",
        active
          ? "bg-accent font-semibold text-accent-foreground"
          : "text-foreground/80 hover:bg-accent/50 hover:text-accent-foreground",
      ].join(" ")}
    >
      <span className="inline-flex min-w-0 items-center gap-2">
        <Icon name={icon} className="h-4 w-4 shrink-0 text-muted-foreground" />
        <span className="truncate">{label}</span>
      </span>
    </a>
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
    return `${BASE}/item/${encodeURIComponent(id)}`;
  }

  if (command === "searchResources") {
    const configType = resolved.request.flags?.config_type;
    if (configType) return `${BASE}/type/${encodeURIComponent(configType)}`;
  }

  const params = new URLSearchParams();
  for (const a of resolved.request.args ?? []) params.append("arg", a);
  for (const [k, v] of Object.entries(resolved.request.flags ?? {})) params.set(k, v);
  const qs = params.toString();
  return `${BASE}/explorer/commands/${encodeURIComponent(command)}${qs ? `?${qs}` : ""}`;
}
