import { useMemo, useState } from "react";
import type { ReactNode } from "react";
import { Check, Copy, Download, FileText, Flame, Play, Square, TerminalSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { pluginURL } from "@/lib/api";
import { execArthas } from "./ArthasDashboardTab";

type ProfilerRun = {
  command: string;
  displayCommand: string;
  results: unknown[];
  ranAt: string;
  flamegraphURL?: string;
};

type ProfilerValueKey =
  | "event"
  | "duration"
  | "alloc"
  | "begin"
  | "chunksize"
  | "chunktime"
  | "clock"
  | "cstack"
  | "end"
  | "exclude"
  | "features"
  | "format"
  | "include"
  | "interval"
  | "jfrsync"
  | "jstackdepth"
  | "lock"
  | "loop"
  | "minwidth"
  | "signal"
  | "timeout"
  | "title"
  | "wall";

type ProfilerFlagKey =
  | "annotate"
  | "allUser"
  | "methodSignatures"
  | "libraryNames"
  | "live"
  | "norm"
  | "reverse"
  | "simpleNames"
  | "sched"
  | "threads"
  | "total"
  | "ttsp";

type ValueOption = {
  key: ProfilerValueKey;
  flag: string;
  label: string;
  placeholder?: string;
  options?: string[];
};

type FlagOption = {
  key: ProfilerFlagKey;
  flag: string;
  label: string;
};

const DEFAULT_VALUES: Record<ProfilerValueKey, string> = {
  event: "cpu",
  duration: "30",
  alloc: "",
  begin: "",
  chunksize: "",
  chunktime: "",
  clock: "",
  cstack: "",
  end: "",
  exclude: "",
  features: "",
  format: "flamegraph",
  include: "",
  interval: "",
  jfrsync: "",
  jstackdepth: "",
  lock: "",
  loop: "",
  minwidth: "",
  signal: "",
  timeout: "",
  title: "",
  wall: "",
};

const VALUE_OPTIONS: ValueOption[] = [
  { key: "alloc", flag: "--alloc", label: "Alloc interval", placeholder: "bytes" },
  { key: "begin", flag: "--begin", label: "Begin function", placeholder: "SafepointSynchronize::begin" },
  { key: "chunksize", flag: "--chunksize", label: "JFR chunk size", placeholder: "100m" },
  { key: "chunktime", flag: "--chunktime", label: "JFR chunk time", placeholder: "1h" },
  { key: "clock", flag: "--clock", label: "Clock", options: ["", "monotonic", "tsc"] },
  { key: "cstack", flag: "--cstack", label: "C stack", options: ["", "fp", "dwarf", "lbr", "no"] },
  { key: "end", flag: "--end", label: "End function", placeholder: "RuntimeService::record_safepoint_synchronized" },
  { key: "exclude", flag: "--exclude", label: "Exclude", placeholder: "*Unsafe.park*" },
  { key: "features", flag: "--features", label: "Features" },
  { key: "include", flag: "--include", label: "Include", placeholder: "java/*" },
  { key: "interval", flag: "--interval", label: "Interval ns", placeholder: "10000000" },
  { key: "jfrsync", flag: "--jfrsync", label: "JFR sync", placeholder: "profile or +events" },
  { key: "jstackdepth", flag: "--jstackdepth", label: "Stack depth", placeholder: "2048" },
  { key: "lock", flag: "--lock", label: "Lock threshold", placeholder: "nanoseconds" },
  { key: "loop", flag: "--loop", label: "Loop", placeholder: "continuous profile interval" },
  { key: "minwidth", flag: "--minwidth", label: "Min width", placeholder: "percent" },
  { key: "signal", flag: "--signal", label: "Signal" },
  { key: "timeout", flag: "--timeout", label: "Timeout", placeholder: "10m or 2026-05-06T14:00:00" },
  { key: "title", flag: "--title", label: "Title" },
  { key: "wall", flag: "--wall", label: "Wall interval ms", placeholder: "200" },
];

const FLAG_OPTIONS: FlagOption[] = [
  { key: "annotate", flag: "-a", label: "Annotate Java methods" },
  { key: "allUser", flag: "--all-user", label: "User-mode events only" },
  { key: "methodSignatures", flag: "-g", label: "Method signatures" },
  { key: "libraryNames", flag: "-l", label: "Prepend library names" },
  { key: "live", flag: "--live", label: "Live allocations only" },
  { key: "norm", flag: "--norm", label: "Normalize lambda names" },
  { key: "reverse", flag: "--reverse", label: "Reverse stack output" },
  { key: "simpleNames", flag: "-s", label: "Simple class names" },
  { key: "sched", flag: "--sched", label: "Group by scheduling policy" },
  { key: "threads", flag: "--threads", label: "Separate threads" },
  { key: "total", flag: "--total", label: "Count total value" },
  { key: "ttsp", flag: "--ttsp", label: "Time-to-safepoint" },
];

const OUTPUT_FORMATS = ["flat", "traces", "collapsed", "flamegraph", "tree", "jfr", "md"];
const EVENT_OPTIONS = ["cpu", "alloc", "lock", "wall", "itimer", "cache-misses"];

export function ArthasProfilerTab({ sessionId }: { sessionId: string }) {
  const [values, setValues] = useState<Record<ProfilerValueKey, string>>(DEFAULT_VALUES);
  const [flags, setFlags] = useState<Partial<Record<ProfilerFlagKey, boolean>>>({});
  const [extraArgs, setExtraArgs] = useState("");
  const [customAction, setCustomAction] = useState("");
  const [actionArg, setActionArg] = useState("");
  const [runs, setRuns] = useState<ProfilerRun[]>([]);
  const [pending, setPending] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [view, setView] = useState<ProfilerOutputView>("flamegraph");

  const latest = runs[0] ?? null;
  const latestFlamegraph = latest?.flamegraphURL;
  const rawOutput = latest ? stringifyResults(latest.results) : "";
  const markdown = useMemo(() => buildArthasMarkdown(runs, latestFlamegraph), [runs, latestFlamegraph]);

  async function runCommand(command: string, pendingName: string, displayCommand = command) {
    setPending(pendingName);
    setError(null);
    try {
      const { results } = await execArthas(sessionId, command);
      const flamegraphURL = extractFlamegraphURL(results, sessionId);
      setRuns((current) => [{ command, displayCommand, results, ranAt: new Date().toISOString(), flamegraphURL }, ...current].slice(0, 20));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Profiler command failed");
    } finally {
      setPending(null);
    }
  }

  const startCommand = buildProfilerCommand("start", values, flags, extraArgs, "", { omitValues: ["duration", "format"] });
  const sampleCommand = buildProfilerCommand("start", values, flags, extraArgs, "", { omitValues: ["format"] });
  const statusCommand = buildProfilerCommand("status", values, flags, "", "", { onlyAction: true });
  const stopCommand = buildProfilerCommand("stop", values, flags, extraArgs, "", { onlyValues: ["format", "minwidth", "title"] });
  const customCommand = customAction.trim()
    ? buildProfilerCommand(customAction.trim(), values, flags, extraArgs, actionArg)
    : "";

  return (
    <div className="grid h-full min-h-0 grid-cols-[minmax(24rem,32rem)_1fr] gap-3 p-4">
      <section className="flex min-h-0 flex-col gap-3">
        <div>
          <h3 className="text-sm font-semibold">Profiler</h3>
          <p className="text-xs text-muted-foreground">Capture async-profiler flamegraphs from the selected JVM.</p>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <ValueField
            option={{ key: "event", flag: "--event", label: "Event", options: EVENT_OPTIONS }}
            value={values.event}
            onChange={(value) => setValue(setValues, "event", value)}
          />
          <ValueField
            option={{ key: "duration", flag: "--duration", label: "Duration seconds" }}
            value={values.duration}
            onChange={(value) => setValue(setValues, "duration", value)}
          />
          <ValueField
            option={{ key: "format", flag: "--format", label: "Output format", options: OUTPUT_FORMATS }}
            value={values.format}
            onChange={(value) => setValue(setValues, "format", value)}
          />
        </div>

        <details className="rounded-md border border-border p-2">
          <summary className="cursor-pointer text-xs font-semibold text-muted-foreground">Advanced profiler args</summary>
          <div className="mt-2 grid grid-cols-2 gap-2">
            {VALUE_OPTIONS.map((option) => (
              <ValueField
                key={option.key}
                option={option}
                value={values[option.key]}
                onChange={(value) => setValue(setValues, option.key, value)}
              />
            ))}
          </div>
          <div className="mt-3 grid grid-cols-2 gap-2">
            {FLAG_OPTIONS.map((option) => (
              <label key={option.key} className="flex items-center gap-2 text-xs">
                <input
                  type="checkbox"
                  checked={!!flags[option.key]}
                  onChange={(event) => setFlags((current) => ({ ...current, [option.key]: (event.target as HTMLInputElement).checked }))}
                />
                <span>{option.label}</span>
                <span className="ml-auto font-mono text-muted-foreground">{option.flag}</span>
              </label>
            ))}
          </div>
          <div className="mt-3 grid grid-cols-2 gap-2">
            <label className="flex flex-col gap-1 text-xs">
              <span className="text-muted-foreground">Extra args</span>
              <input
                className="h-8 rounded-md border border-input bg-background px-2 font-mono text-xs"
                value={extraArgs}
                onChange={(e) => setExtraArgs((e.target as HTMLInputElement).value)}
                placeholder="--format md=20 --timeout 5m"
              />
            </label>
            <label className="flex flex-col gap-1 text-xs">
              <span className="text-muted-foreground">Action arg</span>
              <input
                className="h-8 rounded-md border border-input bg-background px-2 font-mono text-xs"
                value={actionArg}
                onChange={(e) => setActionArg((e.target as HTMLInputElement).value)}
                placeholder="attribute name pattern"
              />
            </label>
            <label className="flex flex-col gap-1 text-xs">
              <span className="text-muted-foreground">Custom action</span>
              <input
                className="h-8 rounded-md border border-input bg-background px-2 font-mono text-xs"
                value={customAction}
                onChange={(e) => setCustomAction((e.target as HTMLInputElement).value)}
                placeholder="list"
              />
            </label>
            <Button size="sm" variant="outline" disabled={!customCommand} loading={pending === "custom"} onClick={() => runCommand(customCommand, "custom")}>
              Run action
            </Button>
          </div>
        </details>

        <div className="rounded-md border border-border bg-muted/20 p-2 text-xs">
          <div className="mb-1 text-muted-foreground">Command preview</div>
          <pre className="overflow-auto font-mono">{sampleCommand}</pre>
        </div>

        <div className="grid grid-cols-2 gap-2">
          <Button size="sm" loading={pending === "start"} onClick={() => runCommand(startCommand, "start")}>
            <Play className="h-3 w-3" /> Start
          </Button>
          <Button size="sm" variant="secondary" loading={pending === "sample"} onClick={() => runCommand(sampleCommand, "sample")}>
            <Play className="h-3 w-3" /> Timed sample
          </Button>
          <Button size="sm" variant="outline" loading={pending === "status"} onClick={() => runCommand(statusCommand, "status")}>
            Status
          </Button>
          <Button size="sm" variant="destructive" loading={pending === "stop"} onClick={() => runCommand(stopCommand, "stop")}>
            <Square className="h-3 w-3" /> Stop
          </Button>
        </div>

        {error && <div className="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>}

        <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border">
          {runs.length === 0 ? (
            <p className="p-3 text-xs text-muted-foreground">Run profiler commands to collect output.</p>
          ) : (
            runs.map((run) => (
              <div key={`${run.ranAt}:${run.command}`} className="border-b border-border p-2 text-xs last:border-b-0">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate font-mono">{run.displayCommand}</span>
                  <span className="shrink-0 text-muted-foreground">{new Date(run.ranAt).toLocaleTimeString()}</span>
                </div>
                {run.flamegraphURL && (
                  <a className="mt-1 inline-flex items-center gap-1 text-primary hover:underline" href={run.flamegraphURL} target="_blank" rel="noreferrer">
                    <FileText className="h-3 w-3" /> Flamegraph
                  </a>
                )}
              </div>
            ))
          )}
        </div>
      </section>

      <section className="flex min-h-0 flex-col gap-2">
        <div className="flex items-center justify-between gap-2">
          <OutputSwitch value={view} onChange={setView} />
          <div className="flex items-center gap-1">
            {latestFlamegraph && (
              <>
                <a className="inline-flex h-6 items-center gap-1 rounded-md border border-transparent px-2 text-xs hover:bg-muted" href={latestFlamegraph} target="_blank" rel="noreferrer">
                  <Flame className="h-3 w-3" /> Open
                </a>
                <a className="inline-flex h-6 items-center gap-1 rounded-md border border-transparent px-2 text-xs hover:bg-muted" href={latestFlamegraph} download>
                  <Download className="h-3 w-3" /> HTML
                </a>
              </>
            )}
            <Button size="xs" variant="ghost" onClick={() => downloadText("arthas-profiler.md", markdown, "text/markdown")}>
              <Download className="h-3 w-3" /> MD
            </Button>
            {rawOutput && (
              <Button size="xs" variant="ghost" onClick={() => downloadText("arthas-profiler-output.txt", rawOutput, "text/plain")}>
                <Download className="h-3 w-3" /> TXT
              </Button>
            )}
            <CopyValueButton value={copyValueForView(view, markdown, rawOutput, latestFlamegraph)} />
          </div>
        </div>

        <div className="min-h-0 flex-1 overflow-hidden rounded-md border border-border bg-muted/20">
          {view === "flamegraph" ? (
            latestFlamegraph ? (
              <iframe title="Arthas profiler flamegraph" src={latestFlamegraph} className="h-full w-full border-0" />
            ) : (
              <div className="flex h-full items-center justify-center p-4 text-sm text-muted-foreground">
                Stop a profiler run with HTML output to render a flamegraph here.
              </div>
            )
          ) : view === "markdown" ? (
            <pre className="h-full overflow-auto bg-muted/30 p-3 text-xs">{markdown}</pre>
          ) : (
            <pre className="h-full overflow-auto bg-muted/30 p-3 text-xs">{rawOutput || "Run a profiler command to see Arthas output."}</pre>
          )}
        </div>
      </section>
    </div>
  );
}

function ValueField({
  option,
  value,
  onChange,
}: {
  option: ValueOption;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs">
      <span className="flex items-center justify-between gap-2 text-muted-foreground">
        <span>{option.label}</span>
        <span className="font-mono">{option.flag}</span>
      </span>
      {option.options ? (
        <select
          className="h-8 rounded-md border border-input bg-background px-2 text-xs"
          value={value}
          onChange={(e) => onChange((e.target as HTMLSelectElement).value)}
        >
          {option.options.map((item) => (
            <option key={item || "empty"} value={item}>{item || "default"}</option>
          ))}
        </select>
      ) : (
        <input
          className="h-8 rounded-md border border-input bg-background px-2 font-mono text-xs"
          value={value}
          placeholder={option.placeholder}
          onChange={(e) => onChange((e.target as HTMLInputElement).value)}
        />
      )}
    </label>
  );
}

function setValue(
  setter: (fn: (current: Record<ProfilerValueKey, string>) => Record<ProfilerValueKey, string>) => void,
  key: ProfilerValueKey,
  value: string,
): void {
  setter((current) => ({ ...current, [key]: value }));
}

function buildProfilerCommand(
  action: string,
  values: Record<ProfilerValueKey, string>,
  flags: Partial<Record<ProfilerFlagKey, boolean>>,
  extraArgs: string,
  actionArg = "",
  options: {
    omitValues?: ProfilerValueKey[];
    onlyValues?: ProfilerValueKey[];
    onlyAction?: boolean;
  } = {},
): string {
  const parts = ["profiler", action];
  if (!options.onlyAction) {
    const omit = new Set(options.omitValues ?? []);
    const only = options.onlyValues ? new Set(options.onlyValues) : null;
    const valueOptions = [
      { key: "event", flag: "--event" },
      { key: "duration", flag: "--duration" },
      { key: "format", flag: "--format" },
      ...VALUE_OPTIONS.map((option) => ({ key: option.key, flag: option.flag })),
    ] as Array<{ key: ProfilerValueKey; flag: string }>;

    for (const option of valueOptions) {
      if (omit.has(option.key)) continue;
      if (only && !only.has(option.key)) continue;
      const value = values[option.key]?.trim();
      if (value) parts.push(option.flag, quoteArg(value));
    }

    for (const option of FLAG_OPTIONS) {
      if (flags[option.key]) parts.push(option.flag);
    }
  }

  const trimmedActionArg = actionArg.trim();
  if (trimmedActionArg) parts.push(quoteArg(trimmedActionArg));
  const trimmedExtra = extraArgs.trim();
  if (trimmedExtra && !options.onlyAction) parts.push(trimmedExtra);
  return parts.join(" ");
}

function quoteArg(value: string): string {
  if (/^[A-Za-z0-9_./:=+@,%*-]+$/.test(value)) return value;
  return `'${value.replace(/'/g, "\\'")}'`;
}

function extractFlamegraphURL(results: unknown[], sessionId: string): string | undefined {
  const text = stringifyResults(results);
  const htmlPath = text.match(/(?:\/tmp\/arthas-output\/|arthas-output\/)([^\s"'<>]+\.html)/)?.[1];
  if (htmlPath) return pluginURL(`proxy/${sessionId}/arthas-output/${htmlPath}`);
  const direct = text.match(/https?:\/\/[^\s"'<>]+\.html/)?.[0];
  return direct;
}

function buildArthasMarkdown(runs: ProfilerRun[], flamegraphURL?: string): string {
  const latest = runs[0];
  const history = runs.map((run) => `- ${new Date(run.ranAt).toLocaleString()} \`${run.displayCommand}\``);
  const lines = [
    "# Arthas Profiler Output",
    "",
    `- Captured at: ${latest ? new Date(latest.ranAt).toLocaleString() : "not captured"}`,
    `- Latest command: \`${latest?.displayCommand ?? "none"}\``,
    `- Flamegraph: ${flamegraphURL ?? "not available"}`,
    "",
    "## Command History",
    "",
    ...(history.length > 0 ? history : ["- none"]),
    "",
    "## Latest Arthas Output",
    "",
    "```json",
    latest ? stringifyResults(latest.results) : "[]",
    "```",
  ];
  return lines.join("\n");
}

type ProfilerOutputView = "flamegraph" | "markdown" | "raw";

function stringifyResults(value: unknown): string {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function CopyValueButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Button
      size="xs"
      variant="ghost"
      onClick={async () => {
        await navigator.clipboard.writeText(value);
        setCopied(true);
        setTimeout(() => setCopied(false), 1200);
      }}
      disabled={!value}
    >
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
    </Button>
  );
}

function OutputSwitch({
  value,
  onChange,
}: {
  value: ProfilerOutputView;
  onChange: (value: ProfilerOutputView) => void;
}) {
  return (
    <fieldset className="inline-flex items-center gap-1 rounded-md bg-muted p-1" aria-label="Profiler output">
      <OutputSwitchOption
        value="flamegraph"
        checked={value === "flamegraph"}
        onChange={onChange}
        icon={<Flame className="h-3 w-3" />}
        label="Flamegraph"
      />
      <OutputSwitchOption
        value="markdown"
        checked={value === "markdown"}
        onChange={onChange}
        icon={<FileText className="h-3 w-3" />}
        label="Markdown"
      />
      <OutputSwitchOption
        value="raw"
        checked={value === "raw"}
        onChange={onChange}
        icon={<TerminalSquare className="h-3 w-3" />}
        label="Raw"
      />
    </fieldset>
  );
}

function OutputSwitchOption({
  value,
  checked,
  onChange,
  icon,
  label,
}: {
  value: ProfilerOutputView;
  checked: boolean;
  onChange: (value: ProfilerOutputView) => void;
  icon: ReactNode;
  label: string;
}) {
  return (
    <label className={`inline-flex h-6 cursor-pointer items-center gap-1 rounded px-2 text-xs font-medium ${checked ? "bg-background text-foreground shadow" : "text-muted-foreground hover:text-foreground"}`}>
      <input
        type="radio"
        className="sr-only"
        name="profiler-output"
        checked={checked}
        onChange={() => onChange(value)}
      />
      {icon}
      {label}
    </label>
  );
}

function copyValueForView(
  view: ProfilerOutputView,
  markdown: string,
  rawOutput: string,
  flamegraphURL?: string,
): string {
  if (view === "markdown") return markdown;
  if (view === "raw") return rawOutput;
  return flamegraphURL ?? "";
}

function downloadText(filename: string, text: string, type: string): void {
  const blob = new Blob([text], { type });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}
