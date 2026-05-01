import type { CSSProperties, ReactNode } from "react";
import { Icon } from "@flanksource/clicky-ui";
import { ConfigIcon } from "../ConfigIcon";

export type PageBreadcrumbItem = {
  label: ReactNode;
  href?: string;
  icon?: string;
  mono?: boolean;
  title?: string;
  className?: string;
};

export type DetailPageLayoutProps = {
  breadcrumbs?: ReactNode;
  actions?: ReactNode;
  header?: ReactNode;
  main?: ReactNode;
  sidebars?: ReactNode;
  children?: ReactNode;
  maxWidthClassName?: string;
  sidebarWidthClassName?: string;
  className?: string;
};

export function DetailPageLayout({
  breadcrumbs,
  actions,
  header,
  main,
  sidebars,
  children,
  maxWidthClassName = "max-w-[118rem]",
  sidebarWidthClassName = "24rem",
  className,
}: DetailPageLayoutProps) {
  const body = main ?? children;
  return (
    <div className={["flex h-full min-h-0 flex-col bg-muted/10", className].filter(Boolean).join(" ")}>
      <div className="min-h-0 flex-1 overflow-auto p-5">
        <div className={["mx-auto flex w-full min-w-0 flex-col gap-5", maxWidthClassName].filter(Boolean).join(" ")}>
          {(breadcrumbs || actions) && (
            <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
              {breadcrumbs ? <div className="min-w-0">{breadcrumbs}</div> : <div />}
              {actions && <div className="flex shrink-0 flex-wrap items-center gap-2">{actions}</div>}
            </div>
          )}
          {header}
          {body && sidebars ? (
            <div
              className="grid min-w-0 gap-5 xl:grid-cols-[minmax(0,1fr)_var(--detail-sidebar-width)]"
              style={{ "--detail-sidebar-width": sidebarWidthClassName } as CSSProperties}
            >
              <div className="min-w-0">{body}</div>
              <aside className="min-w-0">{sidebars}</aside>
            </div>
          ) : (
            body
          )}
        </div>
      </div>
    </div>
  );
}

export function PageBreadcrumbs({ items }: { items: PageBreadcrumbItem[] }) {
  return (
    <nav aria-label="Breadcrumb" className="flex min-w-0 flex-wrap items-center gap-3 text-sm">
      {items.map((item, index) => {
        const content = (
          <>
            {item.icon && <Icon name={item.icon} className="shrink-0" />}
            <span className={["truncate", item.mono && "font-mono font-semibold"].filter(Boolean).join(" ")}>{item.label}</span>
          </>
        );
        return (
          <span key={index} className="inline-flex min-w-0 items-center gap-3">
            {index > 0 && <span className="text-lg text-muted-foreground">/</span>}
            {item.href ? (
              <a
                href={item.href}
                title={item.title}
                className={[
                  "inline-flex h-9 min-w-0 items-center gap-2 rounded-md border border-border bg-background px-3 font-medium text-foreground shadow-sm hover:bg-accent/50",
                  item.className,
                ].filter(Boolean).join(" ")}
              >
                {content}
              </a>
            ) : (
              <span
                title={item.title}
                className={[
                  "inline-flex min-w-0 items-center gap-2",
                  index === items.length - 1 ? "text-foreground" : "text-muted-foreground",
                  item.className,
                ].filter(Boolean).join(" ")}
              >
                {content}
              </span>
            )}
          </span>
        );
      })}
    </nav>
  );
}

export type EntityHeaderProps = {
  icon?: ReactNode | string | null;
  title: ReactNode;
  eyebrow?: ReactNode;
  tags?: ReactNode;
  description?: ReactNode;
  meta?: ReactNode;
  aside?: ReactNode;
  children?: ReactNode;
  variant?: "plain" | "card";
  titleSize?: "md" | "lg";
  className?: string;
};

export function EntityHeader({
  icon,
  title,
  eyebrow,
  tags,
  description,
  meta,
  aside,
  children,
  variant = "plain",
  titleSize = "md",
  className,
}: EntityHeaderProps) {
  const iconNode = icon === null || icon === undefined ? null : typeof icon === "string" ? <HeaderIcon name={icon} /> : icon;
  return (
    <header
      className={[
        variant === "card" ? "rounded-lg border border-border bg-background p-5 shadow-sm" : "border-b border-border px-6 py-4",
        className,
      ].filter(Boolean).join(" ")}
    >
      <div className="flex min-w-0 flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 items-start gap-3">
          {iconNode && (
            <div className="mt-0.5 flex h-10 w-10 shrink-0 items-center justify-center rounded-md border border-border bg-muted/50">
              {iconNode}
            </div>
          )}
          <div className="min-w-0">
            {eyebrow && <div className="mb-1">{eyebrow}</div>}
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <h1 className={["truncate font-semibold", titleSize === "lg" ? "text-2xl tracking-tight" : "text-xl"].join(" ")}>
                {title}
              </h1>
              {tags}
            </div>
            {description && <div className="mt-1 min-w-0 text-sm text-muted-foreground">{description}</div>}
            {meta && <div className="mt-2 flex min-w-0 flex-wrap items-center gap-2">{meta}</div>}
          </div>
        </div>
        {aside && <div className="min-w-0">{aside}</div>}
      </div>
      {children && <div className="mt-5">{children}</div>}
    </header>
  );
}

export function HeaderPill({ icon, label, mono = false }: { icon: string; label: ReactNode; mono?: boolean }) {
  return (
    <span className="inline-flex min-w-0 max-w-full items-center gap-1 rounded border border-border bg-muted/40 px-2 py-1 text-xs text-muted-foreground">
      <HeaderIcon name={icon} className="h-3.5 max-w-3.5 shrink-0" />
      <span className={["truncate", mono && "font-mono"].filter(Boolean).join(" ")}>{label}</span>
    </span>
  );
}

export function HeaderIcon({ name, className }: { name?: string | null; className?: string }) {
  if (!name) return <ConfigIcon primary="playbook" className={className ?? "h-5 max-w-5"} />;
  if (name.includes(":") && !name.includes("::")) {
    return <Icon name={name} className={className ?? "text-xl"} />;
  }
  return <ConfigIcon primary={name} className={className ?? "h-5 max-w-5 text-xl"} />;
}
