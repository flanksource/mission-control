import { useState } from "react";
import { Badge } from "@flanksource/clicky-ui";

export type TagListProps = {
  values?: Array<string | null | undefined> | null;
  maxVisible?: number;
  emptyLabel?: string;
  className?: string;
};

export function TagList({
  values,
  maxVisible = 3,
  emptyLabel = "-",
  className,
}: TagListProps) {
  const items = Array.from(new Set((values ?? []).map((value) => String(value ?? "").trim()).filter(Boolean)));
  const [open, setOpen] = useState(false);

  if (items.length === 0) return <span className="text-muted-foreground">{emptyLabel}</span>;

  const visible = items.slice(0, maxVisible);
  const hidden = items.slice(maxVisible);

  return (
    <div
      className={[
        "relative inline-flex min-w-0 max-w-full items-center",
        className,
      ].filter(Boolean).join(" ")}
      title={items.join(", ")}
    >
      <div className="flex min-w-0 max-w-full items-center gap-1 overflow-hidden whitespace-nowrap">
        {visible.map((item) => (
          <Badge key={item} size="xxs" className="shrink" maxWidth="12rem" wrap={false}>
            {item}
          </Badge>
        ))}
      </div>
      {hidden.length > 0 && (
        <button
          type="button"
          className="ml-1 inline-flex shrink-0 items-center rounded border border-border bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground hover:text-foreground"
          onClick={(event) => {
            event.preventDefault();
            event.stopPropagation();
            setOpen((current) => !current);
          }}
          aria-expanded={open}
        >
          +{hidden.length} more
        </button>
      )}
      {open && hidden.length > 0 && (
        <div
          className="absolute right-0 top-full z-50 mt-1 max-h-60 w-max max-w-md overflow-auto rounded-md border border-border bg-popover p-2 shadow-lg"
          onClick={(event) => event.stopPropagation()}
        >
          <div className="flex flex-col gap-1">
            {hidden.map((item) => (
              <Badge key={item} size="xxs" maxWidth="24rem" wrap={false}>
                {item}
              </Badge>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
