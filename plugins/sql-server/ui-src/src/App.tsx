import { useState } from "preact/hooks";
import { Activity, Database, ListTree, ScrollText, Wrench } from "lucide-react";
import { configIDFromURL } from "./lib/api";
import { StatsTab } from "./pages/StatsTab";
import { ConsoleTab } from "./pages/ConsoleTab";
import { TraceTab } from "./pages/TraceTab";
import { ProcessesTab } from "./pages/ProcessesTab";
import { DefragTab } from "./pages/DefragTab";

const tabs = [
  { key: "stats", label: "Stats", icon: Activity, render: () => <StatsTab /> },
  { key: "console", label: "Console", icon: Database, render: () => <ConsoleTab /> },
  { key: "trace", label: "Trace", icon: ScrollText, render: () => <TraceTab /> },
  { key: "processes", label: "Processes", icon: ListTree, render: () => <ProcessesTab /> },
  { key: "defrag", label: "Defrag", icon: Wrench, render: () => <DefragTab /> },
] as const;

type TabKey = (typeof tabs)[number]["key"];

// initialTab honours `?q=` deep links by jumping straight to the console.
// The console reads `q` and `run` itself and clears them after firing once.
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
        <h2>SQL Server</h2>
        <p>
          No <code>config_id</code> in the iframe URL. The host should pass the
          Connection's UUID as a query parameter.
        </p>
      </div>
    );
  }

  const Active = tabs.find((t) => t.key === active)!;

  return (
    <div className="flex h-screen flex-col">
      <nav className="flex gap-density-1 border-b border-border bg-muted/30 px-density-3 py-density-2">
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
