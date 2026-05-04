import React from 'react';
import { Badge, StatCard } from './facet-components.tsx';
import { Icon } from './icon.tsx';
import { fnv1a32 } from '@flanksource/clicky-ui';
import type { ConfigChange, ConfigTypedChange, ApplicationChange } from './types.ts';
import {
  classifyDeploymentChange,
  filterDeploymentChanges,
  getChangeActor,
  getChangeEventIconName,
  getResolvedTypedChange,
  type DeploymentCategory,
} from './change-section-utils.ts';
import { formatDateTime, formatTime, getTimeBucket } from './utils.ts';
import { ActorAvatar, ChangeEntry } from './config-change-entry.tsx';

interface Props {
  changes: ApplicationChange[];
}

interface ApprovalAnnotation {
  id: string;
  actor?: string;
  label: string;
  phase: 'pre' | 'post';
  status: string;
  comment?: string;
}

interface StageSubmission {
  actor: string;
  date: string;
}

interface PromotionStage {
  name: string;
  latestDate?: string;
  preApprovals: ApprovalAnnotation[];
  postApprovals: ApprovalAnnotation[];
  submissions: StageSubmission[];
}

interface PromotionGroup {
  key: string;
  artifact: string;
  version: string;
  latestDate: string;
  stages: PromotionStage[];
  promotionCount: number;
  approvalCount: number;
  relatedEvents: ApplicationChange[];
}

const COUNT_VALUE_CLASS = 'text-[16pt] leading-[18pt]';
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const STAT_CARD_CLASS = 'flex-[1_1_28mm] min-w-[22mm] max-w-[44mm]';

type StageStatus = 'approved' | 'pending' | 'rejected' | 'default';

const STAGE_STATUS: Record<StageStatus, { icon: string; cellBg: string; badgeBg: string; badgeRing: string; text: string }> = {
  approved: { icon: 'check',     cellBg: 'bg-emerald-50', badgeBg: 'bg-emerald-100', badgeRing: 'ring-emerald-300', text: 'text-emerald-700' },
  pending:  { icon: 'hourglass', cellBg: 'bg-amber-50',   badgeBg: 'bg-amber-100',   badgeRing: 'ring-amber-300',   text: 'text-amber-700'   },
  rejected: { icon: 'x',         cellBg: 'bg-rose-50',    badgeBg: 'bg-rose-100',    badgeRing: 'ring-rose-300',    text: 'text-rose-700'    },
  default:  { icon: 'minus',     cellBg: 'bg-slate-50',   badgeBg: 'bg-slate-100',   badgeRing: 'ring-slate-300',   text: 'text-slate-600'   },
};

function StatusBadgeIcon({ status }: { status: StageStatus }) {
  const spec = STAGE_STATUS[status];
  return (
    <span
      aria-label={status}
      title={status}
      role="img"
      className={`inline-flex h-[3mm] w-[3mm] items-center justify-center rounded-full ring-1 ring-inset ${spec.badgeBg} ${spec.badgeRing} ${spec.text}`}
    >
      <Icon name={spec.icon} size={8} />
    </span>
  );
}

function approvalKey(status: string): StageStatus {
  const normalized = status.trim().toLowerCase();
  if (normalized.includes('approved')) return 'approved';
  if (normalized.includes('rejected') || normalized.includes('denied')) return 'rejected';
  if (normalized.includes('pending')) return 'pending';
  return 'default';
}

function isGroupActor(actor: string): boolean {
  return actor.includes('\\') || actor.trim().startsWith('[');
}

function stageCellBorderColor(actor: string): string {
  if (isGroupActor(actor)) return '#b8b3a7';
  const hue = fnv1a32(actor) % 360;
  return `oklch(0.55 0.14 ${hue} / 0.25)`;
}

function StageCellPill({
  actor,
  status,
}: {
  actor: string;
  status: StageStatus;
}) {
  const spec = STAGE_STATUS[status];
  return (
    <div
      className={`flex max-w-full min-w-0 items-center gap-[1.2mm] rounded-full border py-[0.5mm] pl-[0.5mm] pr-[1mm] ${spec.cellBg}`}
      style={{ borderColor: stageCellBorderColor(actor) }}
      title={actor}
    >
      <ActorAvatar actor={actor} size="xs" />
      <span className="min-w-0 max-w-[30mm] flex-1 truncate text-xs leading-none text-slate-800">{actor}</span>
      <StatusBadgeIcon status={status} />
    </div>
  );
}

function asText(value: unknown): string | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value === 'string') {
    const trimmed = value.trim();
    return trimmed || undefined;
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  return undefined;
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return undefined;
}

function identityLabel(value: unknown): string | undefined {
  const record = asRecord(value);
  if (!record) {
    return undefined;
  }
  return asText(record.name) || asText(record.id) || asText(record.type);
}

function environmentLabel(value: unknown): string | undefined {
  const record = asRecord(value);
  if (!record) {
    return undefined;
  }
  return asText(record.name) || asText(record.identifier);
}

function extractImageTag(value: unknown): string | undefined {
  const text = asText(value);
  if (!text) {
    return undefined;
  }

  const withoutDigest = text.split('@')[0];
  const idx = withoutDigest.lastIndexOf(':');
  if (idx === -1 || idx === withoutDigest.length - 1) {
    return undefined;
  }
  return withoutDigest.slice(idx + 1);
}

function extractVersionCandidates(change: ApplicationChange, typedChange?: ConfigTypedChange): string[] {
  const candidates = new Set<string>();

  if (typedChange?.kind === 'Promotion/v1') {
    const version = asText(typedChange.version);
    if (version) candidates.add(version);
  }

  if (typedChange?.kind === 'Deployment/v1') {
    const newTag = extractImageTag(typedChange.new_image);
    const previousTag = extractImageTag(typedChange.previous_image);
    if (newTag) candidates.add(newTag);
    if (previousTag) candidates.add(previousTag);
  }

  if (typedChange?.kind === 'Rollback/v1') {
    const fromVersion = asText(typedChange.from_version);
    const toVersion = asText(typedChange.to_version);
    if (fromVersion) candidates.add(fromVersion);
    if (toVersion) candidates.add(toVersion);
  }

  const fromDescription = change.description.match(/\bv\d+(?:\.\d+)+(?:[-a-z0-9.]*)?\b/gi) ?? [];
  for (const version of fromDescription) {
    candidates.add(version);
  }

  return [...candidates];
}

function approvalPhase(stage?: string): 'pre' | 'post' {
  return stage?.toLowerCase().includes('post') ? 'post' : 'pre';
}

function approvalStatus(approval: Record<string, unknown>): string {
  return asText(approval.status)
    || (asText(approval.approved_by) ? 'Approved' : undefined)
    || (asText(approval.rejected_by) ? 'Rejected' : undefined)
    || 'Pending';
}

function extractApprovalAnnotations(typedChange: ConfigTypedChange): ApprovalAnnotation[] {
  const approvals = Array.isArray(typedChange.approvals) ? typedChange.approvals : [];
  const annotations: ApprovalAnnotation[] = [];

  approvals.forEach((approval, index) => {
    const record = asRecord(approval);
    if (!record) {
      return;
    }

    const status = approvalStatus(record);
    const actor = identityLabel(record.approver)
      || asText(record.approved_by)
      || asText(record.rejected_by)
      || identityLabel(record.submitted_by)
      || asText(record.submitted_by);
    const stage = asText(record.stage);
    const comment = asText(record.reason) || asText(record.comment);

    annotations.push({
      id: asText(record.id) || `${typedChange.kind}-approval-${index + 1}`,
      actor,
      label: actor ? `${status} · ${actor}` : status,
      phase: approvalPhase(stage),
      status,
      comment,
    });
  });

  return annotations;
}

function ensureStage(stages: PromotionStage[], environment: string): PromotionStage {
  const existing = stages.find((stage) => stage.name === environment);
  if (existing) {
    return existing;
  }

  const created: PromotionStage = { name: environment, latestDate: undefined, preApprovals: [], postApprovals: [], submissions: [] };
  stages.push(created);
  return created;
}

function recordStageDate(stage: PromotionStage, date: string) {
  if (!stage.latestDate || new Date(date).getTime() > new Date(stage.latestDate).getTime()) {
    stage.latestDate = date;
  }
}

function recordSubmission(stage: PromotionStage, change: ApplicationChange) {
  const actor = getChangeActor(change);
  if (actor === '-') return;
  const date = change.date;
  if (stage.submissions.some((s) => s.actor === actor && s.date === date)) return;
  stage.submissions.push({ actor, date });
}

function buildPromotionGroup(change: ApplicationChange, typedChange: ConfigTypedChange): PromotionGroup {
  const artifact = asText(typedChange.artifact) || change.configName || 'Artifact';
  const version = asText(typedChange.version) || 'Unknown version';
  const stages: PromotionStage[] = [];
  const fromEnvironment = environmentLabel(typedChange.from) || asText(typedChange.from_environment) || 'Source';
  const toEnvironment = environmentLabel(typedChange.to) || asText(typedChange.to_environment) || 'Target';

  const source = ensureStage(stages, fromEnvironment);
  const destination = ensureStage(stages, toEnvironment);
  recordStageDate(source, change.date);
  recordStageDate(destination, change.date);
  recordSubmission(source, change);
  const approvals = extractApprovalAnnotations(typedChange);
  for (const approval of approvals) {
    if (approval.phase === 'post') {
      destination.postApprovals.push(approval);
    } else {
      destination.preApprovals.push(approval);
    }
  }

  return {
    key: `${artifact}::${version}`,
    artifact,
    version,
    latestDate: change.date,
    stages,
    promotionCount: 1,
    approvalCount: approvals.length,
    relatedEvents: [],
  };
}

function mergePromotionIntoGroup(group: PromotionGroup, change: ApplicationChange, typedChange: ConfigTypedChange) {
  if (new Date(change.date).getTime() > new Date(group.latestDate).getTime()) {
    group.latestDate = change.date;
  }

  group.promotionCount += 1;

  const fromEnvironment = environmentLabel(typedChange.from) || asText(typedChange.from_environment) || 'Source';
  const toEnvironment = environmentLabel(typedChange.to) || asText(typedChange.to_environment) || 'Target';
  const source = ensureStage(group.stages, fromEnvironment);
  const destination = ensureStage(group.stages, toEnvironment);
  recordStageDate(source, change.date);
  recordStageDate(destination, change.date);
  recordSubmission(source, change);

  const approvals = extractApprovalAnnotations(typedChange);
  group.approvalCount += approvals.length;
  for (const approval of approvals) {
    if (approval.phase === 'post') {
      destination.postApprovals.push(approval);
    } else {
      destination.preApprovals.push(approval);
    }
  }
}

function buildPromotionGroups(changes: ApplicationChange[]): PromotionGroup[] {
  const groups = new Map<string, PromotionGroup>();
  const promotions = changes
    .filter((change) => getResolvedTypedChange(change)?.kind === 'Promotion/v1')
    .sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime());

  for (const change of promotions) {
    const typedChange = getResolvedTypedChange(change);
    if (!typedChange || typedChange.kind !== 'Promotion/v1') {
      continue;
    }

    const artifact = asText(typedChange.artifact) || change.configName || 'Artifact';
    const version = asText(typedChange.version) || 'Unknown version';
    const key = `${artifact}::${version}`;

    if (!groups.has(key)) {
      groups.set(key, buildPromotionGroup(change, typedChange));
      continue;
    }

    mergePromotionIntoGroup(groups.get(key)!, change, typedChange);
  }

  return [...groups.values()].sort((a, b) => new Date(b.latestDate).getTime() - new Date(a.latestDate).getTime());
}

function matchPromotionGroup(change: ApplicationChange, groupsByVersion: Map<string, PromotionGroup[]>): PromotionGroup | undefined {
  const versions = extractVersionCandidates(change, getResolvedTypedChange(change));
  for (const version of versions) {
    const groups = groupsByVersion.get(version);
    if (groups && groups.length === 1) {
      return groups[0];
    }
  }
  return undefined;
}

function toConfigChange(change: ApplicationChange): ConfigChange {
  return {
    id: change.id,
    configID: change.configId,
    configName: change.configName,
    configType: change.configType,
    changeType: change.changeType || 'Change',
    category: change.category,
    source: change.source,
    summary: change.description,
    details: change.details,
    typedChange: change.typedChange,
    createdBy: change.createdBy,
    createdAt: change.createdAt || change.date,
  };
}

function StageSubmissions({ submissions }: { submissions: StageSubmission[] }) {
  if (!submissions.length) return null;
  return (
    <div className="mt-[1mm]">
      <div className="mb-[0.6mm] text-xs uppercase tracking-[0.03em] text-slate-500">
        Submitted by
      </div>
      <div className="flex flex-col gap-[0.6mm]">
        {submissions.map((s, idx) => (
          <StageCellPill
            key={`${s.actor}-${s.date}-${idx}`}
            actor={s.actor}
            status="default"
          />
        ))}
      </div>
    </div>
  );
}

function ApprovalRow({ approval }: { approval: ApprovalAnnotation }) {
  const status = approvalKey(approval.status);
  const statusWord = approval.status.trim() || 'Pending';
  return (
    <div>
      {approval.actor ? (
        <StageCellPill actor={approval.actor} status={status} />
      ) : (
        <div className="flex items-center gap-[1mm] text-xs text-slate-700">
          <StatusBadgeIcon status={status} />
          <span>{statusWord}</span>
        </div>
      )}
      {approval.comment && (
        <div className="mt-[0.5mm] pl-[3.6mm] text-xs italic text-slate-500 break-words">
          &ldquo;{approval.comment}&rdquo;
        </div>
      )}
    </div>
  );
}

function ApprovalList({ title, approvals }: { title: string; approvals: ApprovalAnnotation[] }) {
  if (!approvals.length) {
    return null;
  }

  return (
    <div className="mt-[1mm]">
      <div className="mb-[0.6mm] text-xs uppercase tracking-[0.03em] text-slate-500">{title}</div>
      <div className="flex flex-col gap-[0.6mm]">
        {approvals.map((approval) => (
          <ApprovalRow key={approval.id} approval={approval} />
        ))}
      </div>
    </div>
  );
}

function isSameDay(a: string, b: string): boolean {
  const d1 = new Date(a);
  const d2 = new Date(b);
  return d1.getFullYear() === d2.getFullYear()
    && d1.getMonth() === d2.getMonth()
    && d1.getDate() === d2.getDate();
}

function StageCard({ stage, referenceDate }: { stage: PromotionStage; referenceDate: string }) {
  const dateLabel = stage.latestDate
    ? (isSameDay(stage.latestDate, referenceDate) ? formatTime(stage.latestDate) : formatDateTime(stage.latestDate))
    : undefined;

  return (
    <div className="min-w-[24mm] max-w-[54mm] flex-[1_1_32mm] rounded-[1.5mm] border border-slate-200 bg-slate-50 px-[1.8mm] py-[1.6mm]">
      <div className="flex min-w-0 items-center gap-[1mm]">
        <div className="min-w-0 truncate text-sm font-semibold text-slate-800" title={stage.name}>{stage.name}</div>
        {dateLabel && (
          <div className="ml-auto shrink-0 text-xs text-slate-400">{dateLabel}</div>
        )}
      </div>
      <StageSubmissions submissions={stage.submissions} />
      <ApprovalList title="Pre" approvals={stage.preApprovals} />
      <ApprovalList title="Post" approvals={stage.postApprovals} />
    </div>
  );
}

function PromotionPipeline({ group }: { group: PromotionGroup }) {
  const sourceAbove = group.stages.length > 3;
  const [source, ...targets] = group.stages;
  const stagesInline = sourceAbove ? targets : group.stages;

  return (
    <div className="rounded-[2mm] border border-slate-200 bg-white px-[2.5mm] py-[2mm] mb-[3mm]" style={NO_BREAK_STYLE}>
      <div className="mb-[2mm] flex min-w-0 flex-wrap items-center gap-[1.2mm]">
        <Icon name={getChangeEventIconName({ changeType: 'Promotion', typedChange: { kind: 'Promotion/v1' } })} size={10} className="text-indigo-600" />
        <span className="min-w-0 max-w-[72mm] truncate text-sm font-semibold text-slate-800" title={group.artifact}>{group.artifact}</span>
        <Badge
          variant="custom"
          size="xxs"
          shape="rounded"
          label={group.version}
          maxWidth="22mm"
          truncate="auto"
          color="bg-indigo-50"
          textColor="text-indigo-700"
          borderColor="border-indigo-200"
          className="min-w-0 font-medium"
        />
        <span className="text-xs text-slate-500">
          {group.promotionCount} promotion{group.promotionCount === 1 ? '' : 's'}
        </span>
        {group.approvalCount > 0 && (
          <span className="text-xs text-slate-500">
            {group.approvalCount} approval{group.approvalCount === 1 ? '' : 's'}
          </span>
        )}
        <span className="ml-auto shrink-0 text-xs text-slate-400">{formatDateTime(group.latestDate)}</span>
      </div>

      {sourceAbove && source && (
        <div className="mb-[2mm]">
          <StageCard stage={source} referenceDate={group.latestDate} />
        </div>
      )}

      <div className="flex min-w-0 flex-wrap items-stretch gap-[2mm]">
        {stagesInline.map((stage, index) => (
          <React.Fragment key={`${group.key}-${stage.name}`}>
            {index > 0 && (
              <div className="shrink-0 flex items-center text-slate-300 text-[11pt]">→</div>
            )}
            <StageCard stage={stage} referenceDate={group.latestDate} />
          </React.Fragment>
        ))}
      </div>

      {group.relatedEvents.length > 0 && (
        <div className="mt-[2mm] border-t border-slate-100 pt-[1.5mm]">
          <div className="text-[8pt] font-semibold text-slate-700 mb-[0.8mm]">Related Events</div>
          <div className="flex flex-col">
            {group.relatedEvents
              .sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime())
              .map((change) => {
                const cfg = toConfigChange(change);
                const bucket = cfg.createdAt ? getTimeBucket(cfg.createdAt) : undefined;
                return (
                  <ChangeEntry
                    key={change.id}
                    change={cfg}
                    dateFormat={bucket?.dateFormat ?? 'monthDay'}
                  />
                );
              })}
          </div>
        </div>
      )}
    </div>
  );
}

export default function DeploymentChanges({ changes }: Props) {
  const relevant = filterDeploymentChanges(changes).sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());
  if (!relevant.length) {
    return null;
  }

  const counts = relevant.reduce(
    (acc, change) => {
      const category = classifyDeploymentChange(change);
      if (category) {
        acc[category] += 1;
      }
      return acc;
    },
    { promotion: 0, scale: 0, policy: 0, spec: 0 } as Record<DeploymentCategory, number>,
  );

  const promotionGroups = buildPromotionGroups(relevant);
  const groupsByVersion = promotionGroups.reduce((map, group) => {
    if (!map.has(group.version)) {
      map.set(group.version, []);
    }
    map.get(group.version)!.push(group);
    return map;
  }, new Map<string, PromotionGroup[]>());

  for (const change of relevant) {
    if (getResolvedTypedChange(change)?.kind === 'Promotion/v1') {
      continue;
    }
    const group = matchPromotionGroup(change, groupsByVersion);
    if (group) {
      group.relatedEvents.push(change);
    }
  }

  return (
    <>
      <div className="flex flex-wrap items-stretch gap-[3mm] mb-[4mm]" style={NO_BREAK_STYLE}>
        <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
          <StatCard
            label="Promotion Versions"
            value={String(promotionGroups.length)}
            sublabel={`${counts.promotion} release events`}
            variant="summary"
            size="sm"
            color="blue"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
          <StatCard
            label="Spec Updates"
            value={String(counts.spec)}
            variant="summary"
            size="sm"
            color="gray"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
          <StatCard
            label="Scaling Events"
            value={String(counts.scale)}
            variant="summary"
            size="sm"
            color="blue"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
          <StatCard
            label="Policy Updates"
            value={String(counts.policy)}
            variant="summary"
            size="sm"
            color="orange"
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
      </div>

      {promotionGroups.map((group) => (
        <PromotionPipeline key={group.key} group={group} />
      ))}
    </>
  );
}
