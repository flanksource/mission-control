// Database <Select> shared by Console + Trace + Processes. Loads the
// instance's online databases via the `databases-list` op (cached for 60s
// across tabs), and falls back to a free-text input if the list cannot
// be fetched — autocomplete on the Console tab is a nice-to-have, but
// running a query without it should still be possible.

import { useMemo } from "preact/hooks";
import { Select } from "@flanksource/clicky-ui";
import { useDatabases } from "../lib/use-databases";

export interface DatabasePickerProps {
  configID: string;
  value: string;
  onChange: (next: string) => void;
  // emptyLabel is what the "no filter" entry shows. Console wants
  // "Default database" (since blank means "use the connection's default");
  // Trace wants "All databases" (blank scopes the trace to every DB).
  emptyLabel: string;
  className?: string;
  disabled?: boolean;
}

export function DatabasePicker({
  configID,
  value,
  onChange,
  emptyLabel,
  className,
  disabled,
}: DatabasePickerProps) {
  const dbs = useDatabases(configID);

  const options = useMemo(() => {
    const list = dbs.data ?? [];
    return [{ value: "", label: emptyLabel }, ...list.map((name) => ({ value: name, label: name }))];
  }, [dbs.data, emptyLabel]);

  // When the fetch fails, fall back to a plain text input so the user
  // is never stuck. The placeholder hints that the list is unavailable
  // so they know they're typing a name by hand.
  if (dbs.error) {
    return (
      <input
        placeholder="database (autocomplete unavailable)"
        value={value}
        onChange={(e) => onChange(e.currentTarget.value)}
        className={
          "h-control-h w-72 rounded-md border border-input bg-background px-2 text-sm " +
          (className ?? "")
        }
        disabled={disabled}
      />
    );
  }

  return (
    <div className={"min-w-[220px] " + (className ?? "")}>
      <Select
        value={value}
        options={options}
        onChange={(e) => onChange(e.currentTarget.value)}
        disabled={disabled || dbs.isLoading}
      />
    </div>
  );
}
