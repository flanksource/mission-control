import type { HTMLAttributes } from "react";

export function Badge({ className = "", children }: HTMLAttributes<HTMLSpanElement>) {
  return (
    <span className={`inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground ${className}`}>
      {children}
    </span>
  );
}
