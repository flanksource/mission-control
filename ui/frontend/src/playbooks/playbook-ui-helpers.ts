import type {
  PlaybookListItem,
  PlaybookRun,
  PlaybookRunAction,
  PlaybookRunTarget,
} from "../api/types";

export type PlaybookStatusVisual = {
  tone: "success" | "danger" | "neutral" | "info" | "warning";
  icon: string;
  label: string;
};

export type PlaybookRunTargetSummary = {
  key: string;
  label: string;
  detail?: string;
  icon: string;
  target: PlaybookRunTarget;
  lastRun?: PlaybookRun;
  count: number;
};

export type PlaybookSection = {
  id: string;
  label: string;
  icon: string;
  playbooks: PlaybookListItem[];
};

export function displayPlaybookName(playbook: Pick<PlaybookListItem, "name" | "title">) {
  return playbook.title || playbook.name || "Playbook";
}

export function normalizeCategory(category?: string | null) {
  const trimmed = category?.trim();
  return trimmed || "Uncategorized";
}

export function categoryId(category: string) {
  return category.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "uncategorized";
}

export function categoryIcon(category?: string | null) {
  const normalized = normalizeCategory(category).toLowerCase();
  if (normalized.includes("kubernetes")) return "k8s-deployment";
  if (normalized.includes("flux")) return "flux";
  if (normalized.includes("helm")) return "helm";
  if (normalized.includes("sql") || normalized.includes("database")) return "sqlserver";
  if (normalized.includes("aws") || normalized.includes("cloud")) return "aws";
  if (normalized.includes("diagnostic")) return "logs";
  if (normalized.includes("security")) return "aws-shield";
  return "playbook";
}

export function playbookFallbackIcon(playbook: Pick<PlaybookListItem, "icon" | "category" | "name" | "title">) {
  if (playbook.icon) return playbook.icon;
  const text = `${playbook.name} ${playbook.title ?? ""}`.toLowerCase();
  if (text.includes("restart") || text.includes("reconcile")) return "restart";
  if (text.includes("rollback")) return "rollback";
  if (text.includes("scale")) return "scale-up";
  if (text.includes("drain")) return "k8s-pod";
  if (text.includes("backup") || text.includes("bundle")) return "package-rollback";
  if (text.includes("cert") || text.includes("security")) return "aws-shield";
  if (text.includes("log")) return "logs";
  return categoryIcon(playbook.category);
}

export function playbookMatchesSearch(playbook: PlaybookListItem, query: string) {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return [
    playbook.name,
    playbook.title,
    playbook.description,
    playbook.category,
    playbook.source,
    playbook.namespace,
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase()
    .includes(q);
}

export function buildPlaybookSections(
  playbooks: PlaybookListItem[],
  runs: PlaybookRun[],
  query = "",
): PlaybookSection[] {
  const filtered = playbooks.filter((playbook) => playbookMatchesSearch(playbook, query));
  const runCounts = countRunsByPlaybook(runs);
  const favoriteIds = new Set(
    Array.from(runCounts.entries())
      .filter(([, count]) => count > 0)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 8)
      .map(([id]) => id),
  );
  const sections: PlaybookSection[] = [];
  const favorites = sortByRunCount(filtered.filter((playbook) => favoriteIds.has(playbook.id)), runCounts);
  sections.push({ id: "favorites", label: "Favorites", icon: "heart-checkmark", playbooks: favorites });

  const grouped = new Map<string, PlaybookListItem[]>();
  for (const playbook of filtered) {
    const category = normalizeCategory(playbook.category);
    grouped.set(category, [...(grouped.get(category) ?? []), playbook]);
  }

  for (const category of grouped.keys()) {
    sections.push({
      id: `category-${categoryId(category)}`,
      label: category,
      icon: categoryIcon(category),
      playbooks: grouped.get(category) ?? [],
    });
  }

  return sections;
}

export function recentTargetsForPlaybook(
  playbookId: string,
  runs: PlaybookRun[],
  limit = 4,
): PlaybookRunTargetSummary[] {
  const summaries = new Map<string, PlaybookRunTargetSummary>();
  for (const run of runs) {
    if (run.playbook_id !== playbookId) continue;
    const target = targetSummaryFromRun(run);
    if (!target) continue;
    const existing = summaries.get(target.key);
    if (existing) {
      existing.count += 1;
      continue;
    }
    summaries.set(target.key, target);
  }
  return Array.from(summaries.values()).slice(0, limit);
}

export function targetSummaryFromRun(run: PlaybookRun): PlaybookRunTargetSummary | null {
  if (run.config_id) {
    const label = run.config?.name || run.config_id;
    return {
      key: `config:${run.config_id}`,
      label,
      detail: run.config?.type ?? "Config",
      icon: run.config?.type || "config",
      target: { config_id: run.config_id },
      lastRun: run,
      count: 1,
    };
  }
  if (run.component_id) {
    const label = run.component?.name || run.component_id;
    return {
      key: `component:${run.component_id}`,
      label,
      detail: "Component",
      icon: run.component?.icon || "config",
      target: { component_id: run.component_id },
      lastRun: run,
      count: 1,
    };
  }
  if (run.check_id) {
    const label = run.check?.name || run.check_id;
    return {
      key: `check:${run.check_id}`,
      label,
      detail: "Check",
      icon: run.check?.icon || "checkmark",
      target: { check_id: run.check_id },
      lastRun: run,
      count: 1,
    };
  }
  return null;
}

export function countRunsByPlaybook(runs: PlaybookRun[]) {
  const counts = new Map<string, number>();
  for (const run of runs) {
    counts.set(run.playbook_id, (counts.get(run.playbook_id) ?? 0) + 1);
  }
  return counts;
}

export function actorName(run: PlaybookRun) {
  return run.person?.name || run.created_by_person?.name || run.created_by || "Unknown";
}

export function statusVisual(status?: string | null): PlaybookStatusVisual {
  switch (status) {
    case "completed":
    case "done":
    case "success":
      return { tone: "success", icon: "checkmark", label: "Completed" };
    case "failed":
      return { tone: "danger", icon: "scorecard-fail", label: "Failed" };
    case "timed_out":
      return { tone: "danger", icon: "alarm-clock-check", label: "Timed out" };
    case "cancelled":
      return { tone: "neutral", icon: "remove-badge", label: "Cancelled" };
    case "skipped":
      return { tone: "neutral", icon: "skip", label: "Skipped" };
    case "running":
    case "retrying":
      return { tone: "info", icon: "activity-feed", label: titleStatus(status) };
    case "pending_approval":
      return { tone: "warning", icon: "wait-for-approval", label: "Pending approval" };
    case "scheduled":
      return { tone: "warning", icon: "add-clock", label: "Scheduled" };
    case "waiting":
    case "waiting_children":
    case "sleeping":
      return { tone: "warning", icon: "hourglass", label: titleStatus(status) };
    default:
      return { tone: "neutral", icon: "playbook", label: status ? titleStatus(status) : "Unknown" };
  }
}

export function playbookStatusTone(status?: string | null) {
  return statusVisual(status).tone;
}

export function actionProgress(actions: PlaybookRunAction[]) {
  const total = actions.length;
  if (total === 0) return { complete: 0, total, percent: 0 };
  const complete = actions.filter((action) => action.status === "completed" || action.status === "skipped").length;
  return { complete, total, percent: Math.round((complete / total) * 100) };
}

function sortByRunCount(playbooks: PlaybookListItem[], runCounts: Map<string, number>) {
  return [...playbooks].sort((a, b) => {
    const runDelta = (runCounts.get(b.id) ?? 0) - (runCounts.get(a.id) ?? 0);
    if (runDelta !== 0) return runDelta;
    return displayPlaybookName(a).localeCompare(displayPlaybookName(b));
  });
}

function titleStatus(status: string) {
  return status.replace(/_/g, " ").replace(/\b\w/g, (match) => match.toUpperCase());
}
