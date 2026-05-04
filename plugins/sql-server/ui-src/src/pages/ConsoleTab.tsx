import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import Editor, { type OnMount } from "@monaco-editor/react";

// Avoid pulling monaco-editor types directly — it's a peer dep that
// the plugin's UI doesn't list. Derive the editor handle type from
// OnMount's first parameter instead.
type MonacoEditor = Parameters<OnMount>[0];
type Monaco = Parameters<OnMount>[1];
type Disposable = { dispose: () => void };

import { History, Loader2, Play, Trash2, Wand2 } from "lucide-react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Button } from "@flanksource/clicky-ui";
import { callOp, configIDFromURL } from "../lib/api";
import {
  clearHistory,
  loadHistory,
  saveToHistory,
  type HistoryEntry,
} from "../lib/history";
import { registerSqlCompletion, type SchemaInfo } from "../lib/sql-completion";
import { ErrorBox, Card } from "./StatsTab";
import { ConsoleResults, type QueryResult } from "./ConsoleResults";
import { DatabasePicker } from "./DatabasePicker";

interface ExplainResult {
  plan: string;
  format: string;
}

const DEFAULT_QUERY = "SELECT @@VERSION;\n";

// readDeepLink returns the seed statement and run flag from `?q=&run=1`,
// then strips both from the URL so a refresh does not re-execute.
function readDeepLink(): { seed: string | null; autoRun: boolean } {
  const params = new URLSearchParams(window.location.search);
  const seed = params.get("q");
  const autoRun = params.get("run") === "1";
  if (seed != null || params.has("run")) {
    params.delete("q");
    params.delete("run");
    const next = params.toString();
    const search = next ? `?${next}` : "";
    window.history.replaceState({}, "", window.location.pathname + search + window.location.hash);
  }
  return { seed, autoRun };
}

export function ConsoleTab() {
  const configID = configIDFromURL();
  const deepLink = useRef(readDeepLink()).current;
  const [statement, setStatement] = useState(deepLink.seed ?? DEFAULT_QUERY);
  const [database, setDatabase] = useState("");
  const [history, setHistory] = useState<HistoryEntry[]>(loadHistory);
  const [showHistory, setShowHistory] = useState(false);
  const editorRef = useRef<MonacoEditor | null>(null);
  const monacoRef = useRef<Monaco | null>(null);
  const completionRef = useRef<Disposable | null>(null);
  const pendingAutoRunRef = useRef(deepLink.autoRun && !!deepLink.seed);

  // Schema fetch is best-effort — autocomplete degrades gracefully to
  // keywords + functions when it fails. Re-fetches when the database the
  // user types changes (debounced via react-query's queryKey).
  const schemaQuery = useQuery({
    queryKey: ["schema", configID, database],
    queryFn: () => callOp<SchemaInfo>("schema", configID, { database }),
    staleTime: 5 * 60_000,
    retry: false,
  });

  const queryMut = useMutation({
    mutationFn: (toRun: string) =>
      callOp<QueryResult>("query", configID, { statement: toRun, database }),
  });
  const explainMut = useMutation({
    mutationFn: (toRun: string) =>
      callOp<ExplainResult>("explain", configID, {
        statement: toRun,
        database,
        format: "xml",
      }),
  });

  // executeQuery runs the editor selection if non-empty (so the user can
  // highlight one statement out of many), else the full buffer.
  const executeQuery = useCallback(
    (mode: "run" | "explain") => {
      const ed = editorRef.current;
      let toRun = statement;
      if (ed) {
        const sel = ed.getSelection();
        const model = ed.getModel();
        if (sel && model && !sel.isEmpty()) {
          toRun = model.getValueInRange(sel);
        } else {
          toRun = ed.getValue();
        }
      }
      const trimmed = toRun.trim();
      if (!trimmed) return;
      setHistory(saveToHistory(trimmed));
      if (mode === "run") queryMut.mutate(trimmed);
      else explainMut.mutate(trimmed);
    },
    [statement, queryMut, explainMut],
  );

  const handleEditorMount: OnMount = useCallback(
    (editor, monaco) => {
      editorRef.current = editor;
      monacoRef.current = monaco;
      editor.addAction({
        id: "sql-server-execute",
        label: "Execute SQL",
        keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter],
        run: () => executeQuery("run"),
      });
      editor.addAction({
        id: "sql-server-explain",
        label: "Explain SQL",
        keybindings: [monaco.KeyMod.CtrlCmd | monaco.KeyMod.Shift | monaco.KeyCode.Enter],
        run: () => executeQuery("explain"),
      });
      if (schemaQuery.data) {
        completionRef.current = registerSqlCompletion(monaco, schemaQuery.data);
      }
      if (pendingAutoRunRef.current) {
        pendingAutoRunRef.current = false;
        // queueMicrotask so the editor's value is committed first.
        queueMicrotask(() => executeQuery("run"));
      }
    },
    [executeQuery, schemaQuery.data],
  );

  // Re-register the completion provider whenever the schema changes.
  // The mount-time branch above handles the first fetch when Monaco is
  // already up; this effect handles late arrivals + schema refresh.
  useEffect(() => {
    const monaco = monacoRef.current;
    const schema = schemaQuery.data;
    if (!monaco || !schema) return;
    completionRef.current?.dispose();
    completionRef.current = registerSqlCompletion(monaco, schema);
    return () => {
      completionRef.current?.dispose();
      completionRef.current = null;
    };
  }, [schemaQuery.data]);

  return (
    <section className="flex h-[calc(100vh-80px)] flex-col gap-density-2">
      <header className="flex flex-wrap items-center gap-density-2">
        <label className="flex items-center gap-density-1 text-xs text-muted-foreground">
          Database
          <DatabasePicker
            configID={configID}
            value={database}
            onChange={setDatabase}
            emptyLabel="Default database"
          />
        </label>
        <Button size="sm" onClick={() => executeQuery("run")} disabled={queryMut.isPending}>
          {queryMut.isPending ? <Loader2 size={12} className="spin" /> : <Play size={12} />} Run
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => executeQuery("explain")}
          disabled={explainMut.isPending}
        >
          {explainMut.isPending ? <Loader2 size={12} className="spin" /> : <Wand2 size={12} />} Explain
        </Button>
        <HistoryButton
          history={history}
          open={showHistory}
          onToggle={() => setShowHistory((v) => !v)}
          onClose={() => setShowHistory(false)}
          onSelect={(q) => {
            setStatement(q);
            editorRef.current?.setValue(q);
            setShowHistory(false);
          }}
          onClear={() => {
            clearHistory();
            setHistory([]);
          }}
        />
        <span className="ml-auto text-xs text-muted-foreground">
          ⌘/Ctrl+Enter runs · ⌘/Ctrl+Shift+Enter explains
        </span>
      </header>

      <div className="min-h-[160px] flex-[0_0_30%] overflow-hidden rounded-md border border-border">
        <Editor
          defaultLanguage="sql"
          value={statement}
          onChange={(v) => setStatement(v ?? "")}
          onMount={handleEditorMount}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            scrollBeyondLastLine: false,
            automaticLayout: true,
          }}
        />
      </div>

      <div className="flex-1 overflow-auto">
        {queryMut.error && <ErrorBox error={queryMut.error as Error} />}
        {queryMut.data && <ConsoleResults result={queryMut.data} />}
        {explainMut.data && (
          <Card title="Execution plan (XML)" className="mt-density-2">
            <pre className="m-0 max-h-[360px] overflow-auto whitespace-pre-wrap font-mono text-[11px]">
              {explainMut.data.plan}
            </pre>
          </Card>
        )}
        {explainMut.error && <ErrorBox error={explainMut.error as Error} />}
      </div>
    </section>
  );
}

interface HistoryButtonProps {
  history: HistoryEntry[];
  open: boolean;
  onToggle: () => void;
  onClose: () => void;
  onSelect: (query: string) => void;
  onClear: () => void;
}

function HistoryButton({ history, open, onToggle, onClose, onSelect, onClear }: HistoryButtonProps) {
  return (
    <div className="relative">
      <Button variant="outline" size="sm" onClick={onToggle}>
        <History size={12} /> History
      </Button>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={onClose} />
          <div className="absolute left-0 top-full z-50 mt-1 max-h-80 w-[28rem] overflow-auto rounded-md border border-border bg-popover shadow-md">
            {history.length === 0 ? (
              <div className="p-density-2 text-xs text-muted-foreground">No query history</div>
            ) : (
              <>
                <div className="flex items-center justify-between border-b border-border bg-muted/40 px-density-2 py-density-1">
                  <span className="text-[11px] uppercase tracking-wide text-muted-foreground">
                    {history.length} previous queries
                  </span>
                  <button
                    type="button"
                    onClick={onClear}
                    className="inline-flex items-center gap-density-1 text-[11px] text-muted-foreground hover:text-destructive"
                  >
                    <Trash2 size={11} /> Clear
                  </button>
                </div>
                {history.map((entry) => (
                  <button
                    key={entry.timestamp}
                    type="button"
                    className="block w-full truncate border-b border-border px-density-2 py-density-1 text-left font-mono text-xs last:border-0 hover:bg-accent"
                    onClick={() => onSelect(entry.query)}
                    title={entry.query}
                  >
                    {entry.query}
                  </button>
                ))}
              </>
            )}
          </div>
        </>
      )}
    </div>
  );
}
