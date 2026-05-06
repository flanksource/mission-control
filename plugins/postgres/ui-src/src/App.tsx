import { useState } from "preact/hooks";
import { Activity, Database, Gauge, ListTree, Lock, Search } from "lucide-react";
import { configIDFromURL } from "./lib/api";
import { StatsTab } from "./pages/StatsTab";
import { ConsoleTab } from "./pages/ConsoleTab";
import { SessionsTab } from "./pages/SessionsTab";
import { LocksTab } from "./pages/LocksTab";
import { VacuumTab } from "./pages/VacuumTab";
import { SlowQueriesTab } from "./pages/SlowQueriesTab";

const tabs = [
  { key: "stats", label: "Stats", icon: Activity, render: () => <StatsTab /> },
  { key: "console", label: "Console", icon: Database, render: () => <ConsoleTab /> },
  { key: "sessions", label: "Sessions", icon: ListTree, render: () => <SessionsTab /> },
  { key: "locks", label: "Locks", icon: Lock, render: () => <LocksTab /> },
  { key: "vacuum", label: "Vacuum", icon: Gauge, render: () => <VacuumTab /> },
  { key: "slow", label: "Slow Queries", icon: Search, render: () => <SlowQueriesTab /> },
] as const;

type TabKey = (typeof tabs)[number]["key"];

function initialTab(): TabKey {
  const params = new URLSearchParams(window.location.search);
  if (params.has("q")) return "console";
  return "stats";
}

export function App() {
  const [active, setActive] = useState<TabKey>(initialTab);
  const configID = configIDFromURL();

  if (!configID) {
    return (
      <div className="p-density-4">
        <h2>Postgres</h2>
        <p>
          No <code>config_id</code> in the iframe URL.
        </p>
      </div>
    );
  }

  const Active = tabs.find((t) => t.key === active)!;

  return (
    <div className="flex h-screen flex-col">
      <nav className="flex flex-wrap gap-density-1 border-b border-border bg-muted/30 px-density-3 py-density-2">
        {tabs.map((t) => {
          const Icon = t.icon;
          const isActive = t.key === active;
          return (
            <button
              key={t.key}
              onClick={() => setActive(t.key)}
              className={
                "inline-flex items-center gap-density-1 rounded-md border px-density-2 py-density-1 text-sm " +
                (isActive
                  ? "border-input bg-background font-semibold text-foreground"
                  : "border-transparent bg-transparent font-medium text-muted-foreground hover:text-foreground")
              }
            >
              <Icon size={14} />
              {t.label}
            </button>
          );
        })}
      </nav>
      <main className="flex-1 overflow-auto p-density-3">{Active.render()}</main>
    </div>
  );
}
