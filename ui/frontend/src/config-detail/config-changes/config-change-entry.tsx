import React from 'react';
import { Badge } from './facet-components.tsx';
import { Avatar, JsonView, Modal } from '@flanksource/clicky-ui';
import { Icon } from './icon.tsx';
import type { ConfigChange, ConfigSeverity } from './types.ts';
import {
  getChangeEventIconName,
  getChangeTypeLabel,
  getConfigChangeActor,
  getResolvedTypedChange,
  getTypedChangeDisplay,
  type TypedChangeDiff,
} from './change-section-utils.ts';
import { formatDateTime, formatEntryDate, humanizeSize, type TimeBucketFormat } from './utils.ts';

const SEVERITY_ICON_STYLE: Record<ConfigSeverity, { className: string; shape: 'triangle' | 'diamond' | 'square' | 'circle' | 'ring' }> = {
  critical: { className: 'text-red-600', shape: 'triangle' },
  high: { className: 'text-orange-600', shape: 'diamond' },
  medium: { className: 'text-yellow-700', shape: 'square' },
  low: { className: 'text-blue-600', shape: 'circle' },
  info: { className: 'text-slate-400', shape: 'ring' },
};

type ChangeBadgeStyle = { color: string; textColor: string; borderColor: string };
const LABEL_BADGE_CLASS = 'max-w-full';

const CHANGE_BADGE_STYLES: Record<string, ChangeBadgeStyle> = {
  default: { color: 'bg-slate-100', textColor: 'text-slate-700', borderColor: 'border-slate-200' },
  diff: { color: 'bg-indigo-50', textColor: 'text-indigo-700', borderColor: 'border-indigo-200' },
  policy: { color: 'bg-orange-50', textColor: 'text-orange-700', borderColor: 'border-orange-200' },
  scale: { color: 'bg-blue-50', textColor: 'text-blue-700', borderColor: 'border-blue-200' },
  backup: { color: 'bg-emerald-50', textColor: 'text-emerald-700', borderColor: 'border-emerald-200' },
  permission: { color: 'bg-violet-50', textColor: 'text-violet-700', borderColor: 'border-violet-200' },
  release: { color: 'bg-indigo-50', textColor: 'text-indigo-700', borderColor: 'border-indigo-200' },
  artifact: { color: 'bg-sky-50', textColor: 'text-sky-700', borderColor: 'border-sky-200' },
  cost: { color: 'bg-amber-50', textColor: 'text-amber-700', borderColor: 'border-amber-200' },
};

function getChangeAccent(change: ConfigChange, label: string): ChangeBadgeStyle {
  const kind = getResolvedTypedChange(change)?.kind ?? '';
  const type = (change.changeType || '').toLowerCase();
  const category = (change.category || '').toLowerCase();
  const normalizedLabel = label.toLowerCase();

  if (kind === 'Screenshot/v1' || type.includes('screenshot')) return CHANGE_BADGE_STYLES.artifact;
  if (kind === 'PermissionChange/v1' || category.startsWith('rbac') || type.includes('permission')) return CHANGE_BADGE_STYLES.permission;
  if (kind === 'Backup/v1' || kind === 'Restore/v1' || category.startsWith('backup') || type.includes('backup') || type.includes('restore')) return CHANGE_BADGE_STYLES.backup;
  if (kind === 'CostChange/v1' || type.includes('cost')) return CHANGE_BADGE_STYLES.cost;
  if (kind === 'Promotion/v1' || kind === 'Approval/v1' || kind === 'Rollback/v1' || kind === 'PipelineRun/v1' || kind === 'PlaybookExecution/v1') return CHANGE_BADGE_STYLES.release;
  if (kind === 'Scale/v1' || kind === 'Scaling/v1' || type.includes('replica') || type.includes('scaling')) return CHANGE_BADGE_STYLES.scale;
  if (kind === 'ConfigChange/v1' || kind === 'Change/v1' || kind === 'Deployment/v1' || type === 'diff' || category.startsWith('deployment')) return CHANGE_BADGE_STYLES.diff;
  if (type.includes('policy') || normalizedLabel.includes('policy')) return CHANGE_BADGE_STYLES.policy;
  return CHANGE_BADGE_STYLES.default;
}

export function SeverityIcon({ severity }: { severity: ConfigSeverity }) {
  const style = SEVERITY_ICON_STYLE[severity] ?? SEVERITY_ICON_STYLE.info;

  return (
    <span className={`inline-flex h-[3mm] w-[3mm] shrink-0 items-center justify-center ${style.className}`}>
      <svg viewBox="0 0 12 12" className="h-[2.3mm] w-[2.3mm]" aria-hidden="true">
        {style.shape === 'triangle' && <path d="M6 1.2L11 10.8H1z" fill="currentColor" />}
        {style.shape === 'diamond' && <path d="M6 1L11 6L6 11L1 6z" fill="currentColor" />}
        {style.shape === 'square' && <rect x="2" y="2" width="8" height="8" rx="1.2" fill="currentColor" />}
        {style.shape === 'circle' && <circle cx="6" cy="6" r="4.2" fill="currentColor" />}
        {style.shape === 'ring' && <circle cx="6" cy="6" r="3.8" fill="none" stroke="currentColor" strokeWidth="1.6" />}
      </svg>
    </span>
  );
}

function ChangeIcon({ change }: { change: ConfigChange }) {
  return (
    <span className="inline-flex h-[3.5mm] w-[3.5mm] shrink-0 items-center justify-center text-slate-500">
      <Icon name={getChangeEventIconName(change)} size={10} />
    </span>
  );
}

function ChangeTypeBadge({ change, label }: { change: ConfigChange; label: string }) {
  const accent = getChangeAccent(change, label);

  return (
    <Badge
      variant="custom"
      size="xxs"
      shape="rounded"
      label={label}
      color={accent.color}
      textColor={accent.textColor}
      borderColor={accent.borderColor}
      className="font-medium"
    />
  );
}

function GitDiffBadge({ diff }: { diff: TypedChangeDiff }) {
  return (
    <Badge
      variant="label"
      size="xxs"
      shape="rounded"
      label={diff.label}
      value={(
        <>
          <span className="font-mono text-red-700 line-through break-all">{diff.from}</span>
          <span className="text-slate-400">→</span>
          <span className="font-mono text-green-700 break-all">{diff.to}</span>
        </>
      ) as any}
      wrap
      className="max-w-full"
      labelClassName="uppercase tracking-[0.03em]"
      valueClassName="gap-[0.6mm] font-medium text-slate-700"
    />
  );
}

function mergedConfigLabel(change: ConfigChange, hidden: boolean): string | undefined {
  if (hidden) {
    return undefined;
  }

  const name = change.configName?.trim();
  const type = change.configType?.trim();
  if (name && type) {
    return `${name} (${type})`;
  }
  return name || type;
}

export function ActorAvatar({ actor, size = 'xs' }: { actor: string; size?: 'xs' | 'sm' | 'md' | 'lg' }) {
  return <Avatar alt={actor} size={size} variant="duotone" />;
}

export function ActorIdentity({ actor }: { actor: string }) {
  return (
    <span className="inline-flex max-w-full min-w-0 items-center justify-end gap-[0.7mm] text-[7.5pt] leading-[9pt] text-slate-500" title={actor}>
      <ActorAvatar actor={actor} size="xs" />
      <span className="min-w-0 truncate text-right">{actor}</span>
    </span>
  );
}

function splitFieldValue(meta: string): { label: string; value: string } | undefined {
  const separatorIndex = meta.indexOf(':');
  if (separatorIndex === -1) {
    return undefined;
  }

  const label = meta.slice(0, separatorIndex).trim();
  const value = meta.slice(separatorIndex + 1).trim();
  if (!label || !value) {
    return undefined;
  }

  return { label, value };
}

export function LabelBadge({
  label,
  value,
  color = '#e2e8f0',
  textColor = '#475569',
  className,
  valueClassName = 'font-medium text-slate-800',
  href,
}: {
  label: string;
  value: React.ReactNode;
  color?: string;
  textColor?: string;
  className?: string;
  valueClassName?: string;
  href?: string;
}) {
  return (
    <Badge
      variant="label"
      size="xxs"
      shape="rounded"
      label={label}
      value={value as any}
      color={color}
      textColor={textColor}
      wrap
      href={href}
      className={className ?? LABEL_BADGE_CLASS}
      labelClassName="uppercase tracking-[0.03em]"
      valueClassName={valueClassName}
    />
  );
}

function PlainMetaBadge({ label }: { label: string }) {
  return (
    <Badge
      variant="custom"
      size="xxs"
      shape="rounded"
      label={label}
      color="bg-slate-50"
      textColor="text-slate-600"
      borderColor="border-slate-200"
      wrap
      className="max-w-full"
    />
  );
}

function isInteractiveTarget(target: EventTarget | null): boolean {
  return target instanceof Element && Boolean(target.closest('a,button,input,select,textarea'));
}

function stringifyDetail(value: unknown): string | undefined {
  if (value === undefined || value === null || value === '') return undefined;
  if (typeof value === 'string') return value;
  if (typeof value === 'number' || typeof value === 'boolean') return String(value);
  return undefined;
}

function extractRawDiff(change: ConfigChange): string | undefined {
  const direct = stringifyDetail(change.diff);
  if (direct) return direct;
  const details = change.details;
  return stringifyDetail(details?.diff) || stringifyDetail(details?.patch) || stringifyDetail(details?.patches);
}

function DetailRow({ label, value }: { label: string; value?: React.ReactNode }) {
  if (value === undefined || value === null || value === '') return null;
  return (
    <div className="min-w-0 rounded-md border border-border bg-background px-3 py-2">
      <div className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="mt-1 min-w-0 break-words text-sm text-foreground">{value}</div>
    </div>
  );
}

function DetailSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="min-w-0">
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{title}</h3>
      {children}
    </section>
  );
}

function DialogDiff({ diff }: { diff: TypedChangeDiff }) {
  return (
    <div className="rounded-md border border-border bg-background">
      <div className="border-b border-border px-3 py-2 text-xs font-semibold text-muted-foreground">{diff.label}</div>
      <div className="grid gap-0 md:grid-cols-2">
        <div className="min-w-0 border-b border-border p-3 md:border-b-0 md:border-r">
          <div className="mb-1 text-[11px] font-medium uppercase text-red-700">Before</div>
          <pre className="whitespace-pre-wrap break-words font-mono text-xs text-red-800">{diff.from}</pre>
        </div>
        <div className="min-w-0 p-3">
          <div className="mb-1 text-[11px] font-medium uppercase text-green-700">After</div>
          <pre className="whitespace-pre-wrap break-words font-mono text-xs text-green-800">{diff.to}</pre>
        </div>
      </div>
    </div>
  );
}

function RawDiff({ diff }: { diff: string }) {
  return (
    <pre className="max-h-[22rem] overflow-auto rounded-md border border-border bg-muted/30 p-3 font-mono text-xs leading-relaxed text-foreground whitespace-pre-wrap">
      {diff}
    </pre>
  );
}

function JsonTreePanel({ title, data }: { title: string; data: unknown }) {
  if (data === undefined || data === null) return null;
  return (
    <DetailSection title={title}>
      <div className="max-h-[24rem] overflow-auto rounded-md border border-border bg-background p-3">
        <JsonView data={data} defaultOpenDepth={2} />
      </div>
    </DetailSection>
  );
}

function ChangeDetailDialog({
  change,
  open,
  onClose,
  hideConfigName,
}: {
  change: ConfigChange;
  open: boolean;
  onClose: () => void;
  hideConfigName?: boolean;
}) {
  const severity = change.severity ?? 'info';
  const actor = getConfigChangeActor(change);
  const typedChange = getResolvedTypedChange(change);
  const typedDisplay = getTypedChangeDisplay(change);
  const changeTypeLabel = getChangeTypeLabel(change, typedDisplay);
  const configLabel = mergedConfigLabel(change, Boolean(hideConfigName));
  const summary = change.summary || typedDisplay?.summary;
  const rawDiff = extractRawDiff(change);
  const artifactCount = change.artifacts?.length ?? 0;

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={summary || changeTypeLabel}
      size="xl"
      className="max-h-[92vh]"
    >
      <div className="flex min-w-0 flex-col gap-5">
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          <ChangeTypeBadge change={change} label={changeTypeLabel} />
          <Badge tone={severity === 'critical' || severity === 'high' ? 'danger' : severity === 'medium' ? 'warning' : 'info'} size="xs">
            {severity}
          </Badge>
          {typedChange?.kind && <Badge size="xs" maxWidth="20rem" truncate="auto">{typedChange.kind}</Badge>}
          {change.permalink && (
            <Badge variant="outlined" size="xs" href={change.permalink} target="_blank" rel="noreferrer" label="Permalink" />
          )}
        </div>

        <DetailSection title="Who, What, When">
          <div className="grid min-w-0 grid-cols-1 gap-2 md:grid-cols-2 xl:grid-cols-3">
            <DetailRow label="Actor" value={actor !== '-' ? actor : undefined} />
            <DetailRow label="Source" value={change.source} />
            <DetailRow label="Config" value={configLabel || change.configName || change.configID} />
            <DetailRow label="Config Type" value={change.configType} />
            <DetailRow label="Change Type" value={changeTypeLabel} />
            <DetailRow label="Category" value={change.category} />
            <DetailRow label="Created" value={change.createdAt ? formatDateTime(change.createdAt) : undefined} />
            <DetailRow label="First Observed" value={change.firstObserved ? formatDateTime(change.firstObserved) : undefined} />
            <DetailRow label="Count" value={change.count && change.count > 1 ? String(change.count) : undefined} />
            <DetailRow label="Artifacts" value={artifactCount > 0 ? String(artifactCount) : undefined} />
          </div>
        </DetailSection>

        {typedDisplay?.meta?.length ? (
          <DetailSection title="Typed Metadata">
            <div className="flex min-w-0 flex-wrap gap-2">
              {typedDisplay.meta.map((meta) => {
                const fieldValue = splitFieldValue(meta);
                return fieldValue ? (
                  <LabelBadge key={meta} label={fieldValue.label} value={fieldValue.value} className="max-w-[24rem]" />
                ) : (
                  <PlainMetaBadge key={meta} label={meta} />
                );
              })}
            </div>
          </DetailSection>
        ) : null}

        {(typedDisplay?.diff || rawDiff) && (
          <DetailSection title="Diff">
            <div className="flex min-w-0 flex-col gap-3">
              {typedDisplay?.diff && <DialogDiff diff={typedDisplay.diff} />}
              {rawDiff && <RawDiff diff={rawDiff} />}
            </div>
          </DetailSection>
        )}

        {change.artifacts?.length ? (
          <DetailSection title="Artifacts">
            <div className="flex min-w-0 flex-wrap gap-2">
              {change.artifacts.map((artifact) => (
                <LabelBadge
                  key={artifact.id}
                  label={artifact.filename || 'Artifact'}
                  value={humanizeSize(artifact.size) ?? artifact.contentType}
                  color="#cffafe"
                  textColor="#0f766e"
                  href={artifact.dataUri || artifact.path}
                  className="max-w-[24rem]"
                />
              ))}
            </div>
          </DetailSection>
        ) : null}

        <JsonTreePanel title="Typed Change JSON" data={typedChange} />
        <JsonTreePanel title="Details JSON" data={change.details} />
        <JsonTreePanel title="Full Change JSON" data={change} />
      </div>
    </Modal>
  );
}

export function ChangeEntry({ change, dateFormat, hideConfigName }: { change: ConfigChange; dateFormat: TimeBucketFormat; hideConfigName?: boolean }) {
  const [detailOpen, setDetailOpen] = React.useState(false);
  const severity = change.severity ?? 'info';
  const actor = getConfigChangeActor(change);
  const typedChange = getResolvedTypedChange(change);
  const typedDisplay = getTypedChangeDisplay(change);
  const summary = change.summary || typedDisplay?.summary;
  const changeTypeLabel = getChangeTypeLabel(change, typedDisplay);
  const artifactCount = (change.artifacts || []).length;
  const typedMeta = typedDisplay?.meta ?? [];
  const hasTypedDetails = Boolean(typedChange?.kind);
  const configLabel = mergedConfigLabel(change, Boolean(hideConfigName));

  return (
    <>
      <div
        role="button"
        tabIndex={0}
        className="flex cursor-pointer items-start gap-[1.5mm] border-b border-slate-50 py-[0.55mm] text-[8.5pt] leading-[10pt] last:border-b-0 hover:bg-slate-50/70 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-slate-300"
        onClick={(event) => {
          if (!isInteractiveTarget(event.target)) {
            setDetailOpen(true);
          }
        }}
        onKeyDown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            setDetailOpen(true);
          }
        }}
      >
        <span className="w-[12mm] shrink-0 whitespace-nowrap text-right font-mono text-[7.5pt] leading-[9pt] text-slate-400">
          {change.createdAt ? formatEntryDate(change.createdAt, dateFormat) : '-'}
        </span>
        <div className="flex shrink-0 items-center gap-[0.8mm] pt-[0.25mm]">
          <SeverityIcon severity={severity} />
          <ChangeIcon change={change} />
        </div>
        <div className="flex min-w-0 flex-1 items-start gap-[1.4mm]">
          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-[0.8mm] text-[8.5pt] leading-[10pt] text-slate-700">
            <ChangeTypeBadge change={change} label={changeTypeLabel} />
            {configLabel && (
              <LabelBadge
                label="Config"
                value={configLabel}
                color="#dbeafe"
                textColor="#1d4ed8"
              />
            )}
            {summary && <span className="min-w-0 break-words">{summary}</span>}
            {typedDisplay?.diff && <GitDiffBadge diff={typedDisplay.diff} />}
            {hasTypedDetails && typedMeta.map((meta) => {
              const fieldValue = splitFieldValue(meta);
              if (!fieldValue) {
                return <PlainMetaBadge key={meta} label={meta} />;
              }

              return (
                <LabelBadge
                  key={meta}
                  label={fieldValue.label}
                  value={fieldValue.value}
                />
              );
            })}
            {hasTypedDetails && (change.count ?? 0) > 1 && (
              <LabelBadge
                label="Count"
                value={String(change.count)}
                color="#e5e7eb"
                textColor="#4b5563"
              />
            )}
            {hasTypedDetails && artifactCount > 0 && (change.artifacts || []).map((artifact) => (
              <LabelBadge
                key={artifact.id}
                label={artifact.filename}
                value={humanizeSize(artifact.size) ?? artifact.contentType}
                color="#cffafe"
                textColor="#0f766e"
                href={`#artifact-${artifact.id}`}
              />
            ))}
          </div>
          {actor && (
            <div className="max-w-[42mm] shrink self-start pl-[1mm]">
              <ActorIdentity actor={actor} />
            </div>
          )}
        </div>
      </div>
      <ChangeDetailDialog
        change={change}
        open={detailOpen}
        onClose={() => setDetailOpen(false)}
        hideConfigName={hideConfigName}
      />
    </>
  );
}
