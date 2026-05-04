import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ErrorDetails, Icon, Tree } from "@flanksource/clicky-ui";
import { fetchCatalogSummary } from "./api";
import { errorDiagnosticsFromUnknown } from "./api/http";
import { buildTypeTree, type TypeNode } from "./buildTree";
import { ConfigIcon } from "./ConfigIcon";

export type CatalogSidebarProps = {
  selected: string | null;
};

export function CatalogSidebar({ selected }: CatalogSidebarProps) {
  const [expandAll, setExpandAll] = useState<boolean | null>(null);
  const { data, isLoading, error } = useQuery({
    queryKey: ["catalog-summary"],
    queryFn: fetchCatalogSummary,
  });

  const roots = useMemo(
    () => buildTypeTree(data ?? []),
    [data],
  );

  if (isLoading) {
    return (
      <div className="p-4 text-xs text-muted-foreground">Loading catalog…</div>
    );
  }

  if (error) {
    const diagnostics = errorDiagnosticsFromUnknown(error) ?? { message: "Failed to load catalog", context: [] };
    return (
      <div className="p-4">
        <ErrorDetails diagnostics={diagnostics} />
      </div>
    );
  }

  if (roots.length === 0) {
    return (
      <div className="p-4 text-xs text-muted-foreground">No resources yet.</div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center justify-end gap-1 border-b border-border/70 px-2 py-1">
        <button
          type="button"
          onClick={() => setExpandAll(true)}
          className="inline-flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          aria-label="Expand all"
          title="Expand all"
        >
          <Icon name="codicon:expand-all" className="text-sm" />
        </button>
        <button
          type="button"
          onClick={() => setExpandAll(false)}
          className="inline-flex h-6 w-6 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-accent-foreground"
          aria-label="Collapse all"
          title="Collapse all"
        >
          <Icon name="codicon:collapse-all" className="text-sm" />
        </button>
      </div>
      <Tree<TypeNode>
        roots={roots}
        getChildren={(node) => node.children}
        getKey={(node) => node.fullType ?? node.typePath}
        expandAll={expandAll}
        onExpandAllChange={setExpandAll}
        showControls={false}
        onSelect={(node) => {
          if (node.fullType) {
            window.location.href = `/ui/type/${encodeURIComponent(node.fullType)}`;
          }
        }}
        renderRow={({ node, hasChildren }) => {
          const isLeaf = !hasChildren && !!node.fullType;
          const isSelected = isLeaf && selected === node.fullType;
          const rowClassName = [
            "flex w-full min-w-0 items-center justify-start gap-1 rounded px-1.5 py-0.5 text-left text-[13px]",
            isSelected
              ? "bg-accent font-semibold text-accent-foreground"
              : "text-foreground/80 hover:bg-accent/50 hover:text-accent-foreground",
          ].join(" ");

          const content = (
            <>
              <ConfigIcon
                primary={node.fullType ?? node.typePath}
                secondary={node.parentTypePath}
                className="h-4 max-w-4 shrink-0 text-muted-foreground"
              />
              <span className="min-w-0 flex-1 truncate">{node.label}</span>
              <span className="shrink-0 rounded-full bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground">
                {node.count}
              </span>
            </>
          );

          if (isLeaf && node.fullType) {
            return (
              <a
                className={rowClassName}
                href={`/ui/type/${encodeURIComponent(node.fullType)}`}
                onClick={(event) => event.stopPropagation()}
              >
                {content}
              </a>
            );
          }

          return (
            <div className={rowClassName}>
              {content}
            </div>
          );
        }}
        indentPx={10}
        basePaddingPx={4}
        className="catalog-compact-tree p-1"
        toolbarClassName="py-0.5 pr-1"
      />
    </div>
  );
}
