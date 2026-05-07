export type ChangeBadgeStyle = {
  color: string;
  textColor: string;
  borderColor: string;
};

export const CHANGE_BADGE_STYLES: Record<string, ChangeBadgeStyle> = {
  default: { color: "bg-slate-100", textColor: "text-slate-700", borderColor: "border-slate-200" },
  diff: { color: "bg-indigo-50", textColor: "text-indigo-700", borderColor: "border-indigo-200" },
  policy: { color: "bg-orange-50", textColor: "text-orange-700", borderColor: "border-orange-200" },
  scale: { color: "bg-blue-50", textColor: "text-blue-700", borderColor: "border-blue-200" },
  backup: { color: "bg-emerald-50", textColor: "text-emerald-700", borderColor: "border-emerald-200" },
  permission: { color: "bg-violet-50", textColor: "text-violet-700", borderColor: "border-violet-200" },
  release: { color: "bg-indigo-50", textColor: "text-indigo-700", borderColor: "border-indigo-200" },
  artifact: { color: "bg-sky-50", textColor: "text-sky-700", borderColor: "border-sky-200" },
  cost: { color: "bg-amber-50", textColor: "text-amber-700", borderColor: "border-amber-200" },
};

export type ChangeAccentInput = {
  kind?: string;
  changeType?: string;
  category?: string;
  label?: string;
};

export function resolveChangeAccent({ kind = "", changeType = "", category = "", label = "" }: ChangeAccentInput): ChangeBadgeStyle {
  const k = kind;
  const type = changeType.toLowerCase();
  const cat = category.toLowerCase();
  const lbl = label.toLowerCase();

  if (k === "Screenshot/v1" || type.includes("screenshot")) return CHANGE_BADGE_STYLES.artifact;
  if (k === "PermissionChange/v1" || cat.startsWith("rbac") || type.includes("permission")) return CHANGE_BADGE_STYLES.permission;
  if (k === "Backup/v1" || k === "Restore/v1" || cat.startsWith("backup") || type.includes("backup") || type.includes("restore")) return CHANGE_BADGE_STYLES.backup;
  if (k === "CostChange/v1" || type.includes("cost")) return CHANGE_BADGE_STYLES.cost;
  if (k === "Promotion/v1" || k === "Approval/v1" || k === "Rollback/v1" || k === "PipelineRun/v1" || k === "PlaybookExecution/v1") return CHANGE_BADGE_STYLES.release;
  if (k === "Scale/v1" || k === "Scaling/v1" || type.includes("replica") || type.includes("scaling")) return CHANGE_BADGE_STYLES.scale;
  if (k === "ConfigChange/v1" || k === "Change/v1" || k === "Deployment/v1" || type === "diff" || cat.startsWith("deployment")) return CHANGE_BADGE_STYLES.diff;
  if (type.includes("policy") || lbl.includes("policy")) return CHANGE_BADGE_STYLES.policy;
  return CHANGE_BADGE_STYLES.default;
}
