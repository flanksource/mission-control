// T-SQL Monaco completion provider. Suggests tables, columns (after a
// `tableName.` trigger or for tables already named in FROM/JOIN), keywords,
// and built-in functions. Schema is loaded once via the `schema` operation
// and re-registered when it changes.

import type { OnMount } from "@monaco-editor/react";

export interface SchemaInfo {
  tables: {
    name: string;
    schema: string;
    columns: { name: string; dataType: string }[];
  }[];
}

type Monaco = Parameters<OnMount>[1];
type Disposable = { dispose: () => void };

const TSQL_KEYWORDS = [
  "SELECT", "FROM", "WHERE", "AND", "OR", "NOT", "IN", "EXISTS",
  "INSERT", "INTO", "VALUES", "UPDATE", "SET", "DELETE",
  "JOIN", "INNER", "LEFT", "RIGHT", "OUTER", "CROSS", "ON",
  "GROUP", "BY", "ORDER", "ASC", "DESC", "HAVING",
  "TOP", "DISTINCT", "AS", "WITH", "NOLOCK",
  "CREATE", "ALTER", "DROP", "TABLE", "INDEX", "VIEW",
  "BEGIN", "END", "IF", "ELSE", "WHILE", "RETURN",
  "DECLARE", "EXEC", "EXECUTE", "GO",
  "NULL", "IS", "LIKE", "BETWEEN", "CASE", "WHEN", "THEN",
  "UNION", "ALL", "EXCEPT", "INTERSECT",
];

const TSQL_FUNCTIONS = [
  "COUNT", "SUM", "AVG", "MIN", "MAX",
  "CAST", "CONVERT", "ISNULL", "COALESCE", "NULLIF",
  "LEN", "SUBSTRING", "CHARINDEX", "REPLACE", "LTRIM", "RTRIM", "TRIM",
  "UPPER", "LOWER", "LEFT", "RIGHT",
  "GETDATE", "DATEADD", "DATEDIFF", "DATEPART", "FORMAT",
  "ROW_NUMBER", "RANK", "DENSE_RANK", "OVER", "PARTITION",
  "STRING_AGG", "STUFF", "CONCAT",
];

// Returns the identifier immediately before a dot, e.g. "Customers." -> "Customers".
function tableBeforeDot(line: string, column: number): string | null {
  const before = line.substring(0, column - 1);
  const match = before.match(/(\w+)\.\s*$/);
  return match ? match[1] : null;
}

// Captures table names referenced in FROM/JOIN clauses; lets us suggest
// columns of in-scope tables without requiring a `tableName.` trigger.
function referencedTables(text: string): string[] {
  const pattern = /(?:FROM|JOIN)\s+(?:\[?(\w+)\]?\.)?\[?(\w+)\]?(?:\s+(?:AS\s+)?\w+)?/gi;
  const out: string[] = [];
  let m: RegExpExecArray | null;
  while ((m = pattern.exec(text)) !== null) {
    out.push(m[2]);
  }
  return out;
}

export function registerSqlCompletion(monaco: Monaco, schema: SchemaInfo): Disposable {
  const { CompletionItemKind, CompletionItemInsertTextRule } = monaco.languages;

  const tablesByName = new Map<string, SchemaInfo["tables"][number]>();
  for (const t of schema.tables) {
    tablesByName.set(t.name.toUpperCase(), t);
  }

  // The provider callback's `model` / `position` types come from
  // monaco-editor (a peer dep). Plugin UI doesn't list it, so we type
  // them as the structural minimum we actually use.
  type EditorModel = {
    getLineContent(lineNumber: number): string;
    getValue(): string;
    getWordUntilPosition(p: { lineNumber: number; column: number }): {
      startColumn: number;
      endColumn: number;
    };
  };
  type Position = { lineNumber: number; column: number };

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
      const suggestions: Parameters<typeof Array.prototype.push>[0][] = [];

      // 1) After "tableName." → only that table's columns
      const line = model.getLineContent(position.lineNumber);
      const beforeDot = tableBeforeDot(line, position.column);
      if (beforeDot) {
        const t = tablesByName.get(beforeDot.toUpperCase());
        if (t) {
          for (const col of t.columns) {
            suggestions.push({
              label: col.name,
              kind: CompletionItemKind.Field,
              detail: col.dataType,
              insertText: col.name,
              range,
            });
          }
          return { suggestions };
        }
      }

      // 2) Tables
      for (const t of schema.tables) {
        suggestions.push({
          label: t.name,
          kind: CompletionItemKind.Struct,
          detail: `${t.schema} (${t.columns.length} cols)`,
          insertText: t.name,
          range,
        });
      }

      // 3) Columns from tables already referenced in the buffer
      for (const ref of referencedTables(model.getValue())) {
        const t = tablesByName.get(ref.toUpperCase());
        if (!t) continue;
        for (const col of t.columns) {
          suggestions.push({
            label: col.name,
            kind: CompletionItemKind.Field,
            detail: `${t.name}.${col.dataType}`,
            insertText: col.name,
            range,
          });
        }
      }

      // 4) Keywords + functions
      for (const kw of TSQL_KEYWORDS) {
        suggestions.push({
          label: kw,
          kind: CompletionItemKind.Keyword,
          insertText: kw,
          range,
        });
      }
      for (const fn of TSQL_FUNCTIONS) {
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
