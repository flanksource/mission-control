// Result-set rendering for the Console tab: table + row inspector + export
// menu. Kept separate from ConsoleTab.tsx so the editor/header logic stays
// readable and so the export/inspector code can grow without crowding it.

import { useMemo, useState } from "preact/hooks";
import { ChevronDown, Download, Copy } from "lucide-react";
import {
  Badge,
  Button,
  DataTable,
  JsonView,
  Modal,
} from "@flanksource/clicky-ui";
import {
  downloadBlob,
  slugify,
  toCsv,
  toJson,
  type ExportColumn,
} from "../lib/exporters";
import { Card } from "./StatsTab";

export interface ColumnType {
  name: string;
  type?: string;
  nullable?: boolean;
}

export interface ResultSet {
  columns?: string[];
  columnTypes?: ColumnType[];
  rows?: Record<string, unknown>[];
  rowCount: number;
  rowsAffected?: number;
  hasRows: boolean;
}

export interface QueryResult {
  // Multi-set fields populated by the backend for batched statements
  // (e.g. `INSERT ...; SELECT ...;`). Single-statement queries also
  // arrive as a one-element resultSets array; the legacy fields below
  // mirror the last row-bearing set so older renderers still work.
  resultSets?: ResultSet[];
  columns: string[];
  columnTypes?: ColumnType[];
  rows: Record<string, unknown>[];
  rowCount: number;
  rowsAffected?: number;
  durationMs: number;
  isSelect: boolean;
  statement: string;
  database?: string;
}

export function ConsoleResults({ result }: { result: QueryResult }) {
  // Older payloads (and direct callers) won't include resultSets — wrap
  // the legacy fields into a one-element array so the renderer is uniform.
  const sets: ResultSet[] = useMemo(() => {
    if (result.resultSets && result.resultSets.length > 0) return result.resultSets;
    if (result.isSelect) {
      return [
        {
          columns: result.columns,
          columnTypes: result.columnTypes,
          rows: result.rows,
          rowCount: result.rowCount,
          hasRows: true,
        },
      ];
    }
    return [
      {
        rowCount: 0,
        rowsAffected: result.rowsAffected ?? 0,
        hasRows: false,
      },
    ];
  }, [result]);

  return (
    <div className="flex flex-col gap-density-2">
      <div className="flex items-center gap-density-2 px-density-1">
        <Badge variant="outline">{result.durationMs} ms</Badge>
        {result.database && <Badge variant="outline">db: {result.database}</Badge>}
        {sets.length > 1 && <Badge variant="outline">{sets.length} result sets</Badge>}
      </div>
      {sets.map((set, idx) => (
        <ResultSetView
          key={idx}
          index={idx}
          total={sets.length}
          set={set}
          baseName={result.database || "results"}
        />
      ))}
    </div>
  );
}

function ResultSetView({
  index,
  total,
  set,
  baseName,
}: {
  index: number;
  total: number;
  set: ResultSet;
  baseName: string;
}) {
  const [inspectRow, setInspectRow] = useState<Record<string, unknown> | null>(null);

  // Statement that didn't produce a row set (INSERT/UPDATE/DELETE batch
  // entry). Show the affected count and move on.
  if (!set.hasRows) {
    return (
      <Card title={total > 1 ? `Statement #${index + 1}` : "Statement executed"}>
        Rows affected: {set.rowsAffected ?? 0}
      </Card>
    );
  }

  const columns = set.columns ?? [];
  const rows = set.rows ?? [];
  if (rows.length === 0) {
    return (
      <Card title={total > 1 ? `Result set #${index + 1}` : "No rows"}>
        Statement returned no rows.
      </Card>
    );
  }

  const exportColumns: ExportColumn[] = columns.map((c) => ({ key: c, label: c }));
  const typeByColumn = new Map(set.columnTypes?.map((c) => [c.name, c.type]) ?? []);

  return (
    <div className="flex flex-col gap-density-1">
      <div className="flex items-center gap-density-2 px-density-1">
        {total > 1 && (
          <span className="text-xs font-semibold text-muted-foreground">
            Result set #{index + 1}
          </span>
        )}
        <Badge variant="outline">{set.rowCount} rows</Badge>
        <div className="flex-1" />
        <ExportMenu
          rows={rows}
          columns={exportColumns}
          baseName={total > 1 ? `${baseName}-${index + 1}` : baseName}
        />
      </div>
      <DataTable
        data={rows}
        columns={columns}
        autoFilter
        columnResizeStorageKey={`sql-server-console-${index}`}
        onRowClick={(row) => setInspectRow(row as Record<string, unknown>)}
      />
      <RowInspector
        row={inspectRow}
        columns={columns}
        typeByColumn={typeByColumn}
        onClose={() => setInspectRow(null)}
      />
    </div>
  );
}

function ExportMenu({
  rows,
  columns,
  baseName,
}: {
  rows: Record<string, unknown>[];
  columns: ExportColumn[];
  baseName: string;
}) {
  const [open, setOpen] = useState(false);
  const slug = slugify(baseName);

  const exportJson = () => {
    downloadBlob(`${slug}.json`, "application/json", toJson(rows));
    setOpen(false);
  };
  const exportCsv = () => {
    downloadBlob(`${slug}.csv`, "text/csv;charset=utf-8", toCsv(rows, columns));
    setOpen(false);
  };

  return (
    <div className="relative">
      <Button variant="outline" size="sm" onClick={() => setOpen((v) => !v)}>
        <Download size={12} /> Export <ChevronDown size={11} />
      </Button>
      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute right-0 top-full z-50 mt-1 w-40 overflow-hidden rounded-md border border-border bg-popover shadow-md">
            <button
              type="button"
              className="block w-full px-density-2 py-density-1 text-left text-xs hover:bg-accent"
              onClick={exportJson}
            >
              JSON ({rows.length} rows)
            </button>
            <button
              type="button"
              className="block w-full px-density-2 py-density-1 text-left text-xs hover:bg-accent"
              onClick={exportCsv}
            >
              CSV ({rows.length} rows)
            </button>
          </div>
        </>
      )}
    </div>
  );
}

function RowInspector({
  row,
  columns,
  typeByColumn,
  onClose,
}: {
  row: Record<string, unknown> | null;
  columns: string[];
  typeByColumn: Map<string, string | undefined>;
  onClose: () => void;
}) {
  // Strip null/undefined entries — they add noise to wide tables (an
  // OLTP row often has a dozen optional columns) and the user can already
  // see the column list in the data table itself.
  const { entries, hiddenCount } = useMemo(() => {
    if (!row) return { entries: [] as Array<{ key: string; value: unknown; type?: string }>, hiddenCount: 0 };
    const all = columns.map((key) => ({ key, value: row[key], type: typeByColumn.get(key) }));
    const visible = all.filter((e) => e.value !== null && e.value !== undefined);
    return { entries: visible, hiddenCount: all.length - visible.length };
  }, [row, columns, typeByColumn]);

  return (
    <Modal open={row != null} onClose={onClose} title="Row details" size="xl">
      {row && (
        <>
          <dl className="m-0 grid grid-cols-[max-content_1fr] gap-x-density-3 gap-y-density-2 text-xs">
            {entries.map((entry) => (
              <RowInspectorField key={entry.key} entry={entry} />
            ))}
          </dl>
          {hiddenCount > 0 && (
            <p className="m-0 mt-density-2 text-[11px] italic text-muted-foreground">
              {hiddenCount} null column{hiddenCount === 1 ? "" : "s"} hidden
            </p>
          )}
        </>
      )}
    </Modal>
  );
}

function RowInspectorField({
  entry,
}: {
  entry: { key: string; value: unknown; type?: string };
}) {
  const { key, value, type } = entry;
  return (
    <>
      <dt className="flex flex-col items-end gap-density-1">
        <span className="font-mono text-foreground">{key}</span>
        {type && (
          <span className="text-[10px] uppercase tracking-wide text-muted-foreground">
            {type}
          </span>
        )}
      </dt>
      <dd className="m-0 min-w-0">
        <RowInspectorValue value={value} />
      </dd>
    </>
  );
}

function RowInspectorValue({ value }: { value: unknown }) {
  if (value === null || value === undefined) {
    return <em className="text-muted-foreground">null</em>;
  }
  if (typeof value === "string") {
    const json = tryParseJson(value);
    if (json !== undefined) return <JsonView data={json} />;
    return <ScalarText text={value} />;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return <ScalarText text={String(value)} />;
  }
  return <JsonView data={value} />;
}

function ScalarText({ text }: { text: string }) {
  const isMultiline = text.includes("\n");
  return (
    <div className="group flex min-w-0 items-start gap-density-1">
      <pre
        className={
          "m-0 max-h-72 min-w-0 flex-1 overflow-auto whitespace-pre-wrap break-words font-mono text-[11px] " +
          (isMultiline ? "rounded-md border border-border bg-muted/30 p-density-2" : "")
        }
      >
        {text}
      </pre>
      <button
        type="button"
        title="Copy"
        className="shrink-0 rounded p-density-1 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover:opacity-100"
        onClick={() => copyToClipboard(text)}
      >
        <Copy size={11} />
      </button>
    </div>
  );
}

function copyToClipboard(text: string) {
  if (typeof navigator === "undefined" || !navigator.clipboard) return;
  navigator.clipboard.writeText(text).catch(() => undefined);
}

function tryParseJson(text: string): unknown | undefined {
  const trimmed = text.trim();
  if (!trimmed) return undefined;
  if (trimmed[0] !== "{" && trimmed[0] !== "[") return undefined;
  try {
    return JSON.parse(trimmed);
  } catch {
    return undefined;
  }
}
