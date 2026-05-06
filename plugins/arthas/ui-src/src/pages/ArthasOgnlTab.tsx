import { useMemo, useState } from "react";
import { Check, Copy, Play } from "lucide-react";
import { Button } from "@/components/ui/button";
import { execArthas } from "./ArthasDashboardTab";

type OgnlRun = {
  expression: string;
  results: unknown[];
  ranAt: string;
};

const EXAMPLES = [
  '@java.lang.System@getProperty("java.version")',
  '@java.lang.System@getenv()',
  '@java.lang.management.ManagementFactory@getRuntimeMXBean().getInputArguments()',
];

export function ArthasOgnlTab({ sessionId }: { sessionId: string }) {
  const [expression, setExpression] = useState(EXAMPLES[0]);
  const [runs, setRuns] = useState<OgnlRun[]>([]);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const latest = runs[0] ?? null;
  const rendered = useMemo(() => (latest ? renderOgnlResults(latest.results) : ""), [latest]);

  async function run() {
    const expr = expression.trim();
    if (!expr) return;
    setPending(true);
    setError(null);
    try {
      const { results } = await execArthas(sessionId, `ognl '${escapeSingleQuotes(expr)}'`);
      throwIfOgnlError(results);
      setRuns((current) => [{ expression: expr, results, ranAt: new Date().toISOString() }, ...current].slice(0, 20));
    } catch (err) {
      setError(err instanceof Error ? err.message : "OGNL command failed");
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="grid h-full min-h-0 grid-cols-[minmax(22rem,34rem)_1fr] gap-3 p-4">
      <section className="flex min-h-0 flex-col gap-3">
        <div>
          <h3 className="text-sm font-semibold">OGNL Console</h3>
          <p className="text-xs text-muted-foreground">Run expressions through the selected Arthas session.</p>
        </div>

        <textarea
          className="min-h-40 rounded-md border border-input bg-background p-2 font-mono text-xs outline-none focus:border-primary"
          value={expression}
          onChange={(event) => setExpression((event.target as HTMLTextAreaElement).value)}
          spellcheck={false}
        />

        <div className="flex items-center justify-between gap-2">
          <div className="flex flex-wrap gap-1">
            {EXAMPLES.map((example) => (
              <Button key={example} size="xs" variant="ghost" onClick={() => setExpression(example)}>
                {shortExample(example)}
              </Button>
            ))}
          </div>
          <Button size="sm" loading={pending} disabled={!expression.trim()} onClick={run}>
            <Play className="h-3 w-3" /> Run
          </Button>
        </div>

        {error && <div className="rounded-md border border-red-200 bg-red-50 p-2 text-xs text-red-700">{error}</div>}

        <div className="min-h-0 flex-1 overflow-auto rounded-md border border-border">
          {runs.length === 0 ? (
            <p className="p-3 text-xs text-muted-foreground">No OGNL runs yet.</p>
          ) : (
            runs.map((run) => (
              <button
                key={`${run.ranAt}:${run.expression}`}
                type="button"
                className="block w-full border-b border-border p-2 text-left text-xs last:border-b-0 hover:bg-muted"
                onClick={() => setExpression(run.expression)}
              >
                <div className="truncate font-mono">{run.expression}</div>
                <div className="mt-1 text-muted-foreground">{new Date(run.ranAt).toLocaleTimeString()}</div>
              </button>
            ))
          )}
        </div>
      </section>

      <section className="flex min-h-0 flex-col gap-2">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold">Result</h3>
          {latest && <CopyValueButton value={rendered} />}
        </div>
        <pre className="min-h-0 flex-1 overflow-auto rounded-md border border-border bg-muted/30 p-3 text-xs">
          {latest ? rendered : "Run an expression to see the result."}
        </pre>
      </section>
    </div>
  );
}

function renderOgnlResults(results: unknown[]): string {
  const ognl = (results as Array<{ type?: string; value?: unknown }>).find((r) => r?.type === "ognl" && "value" in r);
  return stringifyPretty(ognl ? ognl.value : results);
}

function stringifyPretty(value: unknown): string {
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

function throwIfOgnlError(results: unknown[]): void {
  for (const result of results as Array<{ type?: string; message?: string; statusCode?: number }>) {
    if (result?.type === "ognl" && result.statusCode && result.statusCode >= 400) {
      throw new Error(result.message ?? `arthas ognl failed (${result.statusCode})`);
    }
  }
}

function escapeSingleQuotes(value: string): string {
  return value.replace(/\\/g, "\\\\").replace(/'/g, "\\'");
}

function shortExample(value: string): string {
  const match = value.match(/@([^@]+)@([^()]+)/);
  return match ? match[2] : value.slice(0, 24);
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
    >
      {copied ? <Check className="h-3 w-3" /> : <Copy className="h-3 w-3" />}
    </Button>
  );
}
