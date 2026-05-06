// Postgres Monaco completion provider. Suggests schemas, tables, columns,
// keywords, and common built-in functions from the plugin's schema operation.

import type { OnMount } from "@monaco-editor/react";

export interface SchemaInfo {
  tables?: {
    name: string;
    schema: string;
    columns?: { name: string; dataType: string }[] | null;
  }[] | null;
}

type Monaco = Parameters<OnMount>[1];
type Disposable = { dispose: () => void };

const POSTGRES_KEYWORDS = [
  "SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "EXISTS",
  "INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE", "RETURNING",
  "JOIN", "INNER", "LEFT", "RIGHT", "FULL", "OUTER", "CROSS", "ON",
  "GROUP", "BY", "ORDER", "ASC", "DESC", "HAVING", "LIMIT", "OFFSET",
  "DISTINCT", "AS", "WITH", "RECURSIVE", "UNION", "ALL", "EXCEPT", "INTERSECT",
  "CREATE", "ALTER", "DROP", "TABLE", "INDEX", "VIEW", "MATERIALIZED", "SCHEMA",
  "BEGIN", "COMMIT", "ROLLBACK", "NULL", "IS", "LIKE", "ILIKE", "BETWEEN",
  "CASE", "WHEN", "THEN", "ELSE", "END", "TRUE", "FALSE",
];

const POSTGRES_FUNCTIONS = [
  "COUNT", "SUM", "AVG", "MIN", "MAX",
  "COALESCE", "NULLIF", "CAST", "EXTRACT",
  "LOWER", "UPPER", "LEFT", "RIGHT", "SUBSTRING", "TRIM", "LENGTH",
  "NOW", "CURRENT_DATE", "CURRENT_TIMESTAMP", "DATE_TRUNC", "AGE",
  "JSONB_BUILD_OBJECT", "JSONB_AGG", "ARRAY_AGG", "STRING_AGG",
  "ROW_NUMBER", "RANK", "DENSE_RANK", "LAG", "LEAD",
];

type TableInfo = NonNullable<NonNullable<SchemaInfo["tables"]>[number]>;

type EditorModel = {
  getLineContent(lineNumber: number): string;
  getValue(): string;
  getWordUntilPosition(p: { lineNumber: number; column: number }): {
    startColumn: number;
    endColumn: number;
  };
};
type Position = { lineNumber: number; column: number };

function cleanIdentifier(s: string): string {
  return s.replace(/^"|"$/g, "").trim();
}

function key(s: string): string {
  return cleanIdentifier(s).toLowerCase();
}

function tableBeforeDot(line: string, column: number): string | null {
  const before = line.substring(0, column - 1);
  const match = before.match(/(?:"([^"]+)"|([A-Za-z_][\w$]*))\.\s*$/);
  return match ? (match[1] ?? match[2]) : null;
}

function referencedTables(text: string): Map<string, string> {
  const pattern =
    /(?:FROM|JOIN)\s+(?:(?:"([^"]+)"|([A-Za-z_][\w$]*))\.)?(?:"([^"]+)"|([A-Za-z_][\w$]*))(?:\s+(?:AS\s+)?(?:"([^"]+)"|([A-Za-z_][\w$]*)))?/gi;
  const out = new Map<string, string>();
  let m: RegExpExecArray | null;
  while ((m = pattern.exec(text)) !== null) {
    const schema = m[1] ?? m[2] ?? "";
    const table = m[3] ?? m[4] ?? "";
    const alias = m[5] ?? m[6] ?? "";
    if (!table) continue;
    const qualified = schema ? `${schema}.${table}` : table;
    out.set(key(table), qualified);
    if (alias) out.set(key(alias), qualified);
  }
  return out;
}

function tables(schema: SchemaInfo): TableInfo[] {
  return (schema.tables ?? []).map((t) => ({ ...t, columns: t.columns ?? [] }));
}

export function registerSqlCompletion(monaco: Monaco, schema: SchemaInfo): Disposable {
  const { CompletionItemKind, CompletionItemInsertTextRule } = monaco.languages;
  const allTables = tables(schema);
  const tablesByKey = new Map<string, TableInfo>();
  const schemas = new Set<string>();

  for (const t of allTables) {
    schemas.add(t.schema);
    tablesByKey.set(key(t.name), t);
    tablesByKey.set(key(`${t.schema}.${t.name}`), t);
  }

  return monaco.languages.registerCompletionItemProvider("sql", {
    triggerCharacters: ["."],

    provideCompletionItems(model: EditorModel, position: Position) {
      const word = model.getWordUntilPosition(position);
      const range = {
        startLineNumber: position.lineNumber,
        endLineNumber: position.lineNumber,
        startColumn: word.startColumn,
        endColumn: word.endColumn,
      };
      const suggestions: any[] = [];
      const refs = referencedTables(model.getValue());

      const beforeDot = tableBeforeDot(model.getLineContent(position.lineNumber), position.column);
      if (beforeDot) {
        const target = refs.get(key(beforeDot)) ?? beforeDot;
        const t = tablesByKey.get(key(target));
        if (t) {
          for (const col of t.columns ?? []) {
            suggestions.push({
              label: col.name,
              kind: CompletionItemKind.Field,
              detail: `${t.schema}.${t.name} ${col.dataType}`,
              insertText: col.name,
              range,
            });
          }
          return { suggestions };
        }
      }

      for (const schemaName of schemas) {
        suggestions.push({
          label: schemaName,
          kind: CompletionItemKind.Module,
          detail: "schema",
          insertText: schemaName,
          range,
        });
      }

      for (const t of allTables) {
        suggestions.push({
          label: t.name,
          kind: CompletionItemKind.Struct,
          detail: `${t.schema} (${(t.columns ?? []).length} cols)`,
          insertText: t.name,
          range,
        });
        suggestions.push({
          label: `${t.schema}.${t.name}`,
          kind: CompletionItemKind.Struct,
          detail: `${(t.columns ?? []).length} cols`,
          insertText: `${t.schema}.${t.name}`,
          range,
        });
      }

      for (const ref of refs.values()) {
        const t = tablesByKey.get(key(ref));
        if (!t) continue;
        for (const col of t.columns ?? []) {
          suggestions.push({
            label: col.name,
            kind: CompletionItemKind.Field,
            detail: `${t.name}.${col.dataType}`,
            insertText: col.name,
            range,
          });
        }
      }

      for (const kw of POSTGRES_KEYWORDS) {
        suggestions.push({ label: kw, kind: CompletionItemKind.Keyword, insertText: kw, range });
      }
      for (const fn of POSTGRES_FUNCTIONS) {
        suggestions.push({
          label: fn,
          kind: CompletionItemKind.Function,
          insertText: fn + "($0)",
          insertTextRules: CompletionItemInsertTextRule.InsertAsSnippet,
          range,
        });
      }

      return { suggestions };
    },
  });
}
