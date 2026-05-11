import type { ReactNode } from "react";
import { Badge, Section as ClickySection, type BadgeProps } from "@flanksource/clicky-ui";

type StatColor = "red" | "orange" | "yellow" | "blue" | "green" | "gray";

const statColorClasses: Record<StatColor, string> = {
  red: "border-red-200 bg-red-50 text-red-700",
  orange: "border-orange-200 bg-orange-50 text-orange-700",
  yellow: "border-yellow-200 bg-yellow-50 text-yellow-700",
  blue: "border-blue-200 bg-blue-50 text-blue-700",
  green: "border-green-200 bg-green-50 text-green-700",
  gray: "border-slate-200 bg-slate-50 text-slate-700",
};

export { Badge };
export type { BadgeProps };

export function Section({
  title,
  children,
}: {
  title: ReactNode;
  children: ReactNode;
  variant?: string;
  size?: string;
}) {
  return (
    <ClickySection title={title} defaultOpen>
      {children}
    </ClickySection>
  );
}

export function StatCard({
  label,
  value,
  sublabel,
  color = "gray",
  shrink,
  valueClassName,
}: {
  label: ReactNode;
  value: ReactNode;
  sublabel?: ReactNode;
  variant?: string;
  size?: string;
  color?: StatColor;
  shrink?: boolean;
  valueClassName?: string;
}) {
  return (
    <div
      className={[
        "h-full min-w-0 rounded-md border px-3 py-2",
        shrink ? "max-w-[44mm]" : "max-w-full",
        statColorClasses[color],
      ].join(" ")}
    >
      <div className="min-w-0 truncate text-xs font-medium uppercase text-current/75" title={typeof label === "string" ? label : undefined}>
        {label}
      </div>
      <div className={["mt-1 min-w-0 truncate font-semibold leading-none", valueClassName ?? "text-xl"].join(" ")} title={typeof value === "string" ? value : undefined}>
        {value}
      </div>
      {sublabel && (
        <div className="mt-1 min-w-0 truncate text-xs text-current/70" title={typeof sublabel === "string" ? sublabel : undefined}>
          {sublabel}
        </div>
      )}
    </div>
  );
}

export function SeverityStatCard({
  label,
  value,
  color,
}: {
  label: ReactNode;
  value: ReactNode;
  color: Exclude<StatColor, "green" | "gray">;
}) {
  return <StatCard label={label} value={value} color={color} />;
}
