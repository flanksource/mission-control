import { QuickStatsRow } from "./QuickStatsRow";
import { RecentlyUsedPanel } from "./RecentlyUsedPanel";
import { PlaybookRunsPanel } from "./PlaybookRunsPanel";
import { RecentlyUpdatedConfigsPanel } from "./RecentlyUpdatedConfigsPanel";

export function LandingPage() {
  return (
    <div className="mx-auto flex w-full max-w-7xl flex-col gap-6 p-6">
      <header className="flex flex-col gap-1">
        <h1 className="text-xl font-semibold text-foreground">Mission Control</h1>
        <p className="text-sm text-muted-foreground">
          Recent activity and quick access to what you've been working on.
        </p>
      </header>

      <QuickStatsRow />

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <RecentlyUsedPanel />
        <PlaybookRunsPanel />
        <div className="lg:col-span-2">
          <RecentlyUpdatedConfigsPanel />
        </div>
      </div>
    </div>
  );
}
