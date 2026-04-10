import React from 'react';
import { Badge, Section, SeverityStatCard } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigChange, ConfigSeverity } from '../config-types.ts';
import { getChangeTypeLabel, getTypedChangeDisplay } from './change-section-utils.ts';
import { getTimeBucket, formatEntryDate, type TimeBucketFormat } from './utils.ts';

interface Props {
  changes?: ConfigChange[];
  hideConfigName?: boolean;
}

const SEVERITY_ORDER: ConfigSeverity[] = ['critical', 'high', 'medium', 'low', 'info'];
const SEVERITY_COLOR: Record<string, 'red' | 'orange' | 'yellow' | 'blue'> = {
  critical: 'red',
  high: 'orange',
  medium: 'yellow',
  low: 'blue',
  info: 'blue',
};
const SEVERITY_TEXT: Record<string, string> = {
  critical: 'text-red-700 bg-red-50 border-red-200',
  high: 'text-orange-700 bg-orange-50 border-orange-200',
  medium: 'text-yellow-700 bg-yellow-50 border-yellow-200',
  low: 'text-blue-700 bg-blue-50 border-blue-200',
  info: 'text-gray-600 bg-gray-50 border-gray-200',
};
const SEVERITY_ACCENT_TEXT: Record<string, string> = {
  critical: 'text-red-600',
  high: 'text-orange-600',
  medium: 'text-yellow-700',
  low: 'text-blue-600',
  info: 'text-gray-500',
};
type ChangeBadgeStyle = { color: string; textColor: string; borderColor: string };
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };

const CHANGE_BADGE_STYLES: Record<string, ChangeBadgeStyle> = {
  default: { color: 'bg-slate-100', textColor: 'text-slate-700', borderColor: 'border-slate-200' },
  diff: { color: 'bg-indigo-50', textColor: 'text-indigo-700', borderColor: 'border-indigo-200' },
  policy: { color: 'bg-orange-50', textColor: 'text-orange-700', borderColor: 'border-orange-200' },
  scale: { color: 'bg-sky-50', textColor: 'text-sky-700', borderColor: 'border-sky-200' },
  backup: { color: 'bg-emerald-50', textColor: 'text-emerald-700', borderColor: 'border-emerald-200' },
  permission: { color: 'bg-violet-50', textColor: 'text-violet-700', borderColor: 'border-violet-200' },
  release: { color: 'bg-fuchsia-50', textColor: 'text-fuchsia-700', borderColor: 'border-fuchsia-200' },
  artifact: { color: 'bg-cyan-50', textColor: 'text-cyan-700', borderColor: 'border-cyan-200' },
  cost: { color: 'bg-amber-50', textColor: 'text-amber-700', borderColor: 'border-amber-200' },
};

function getChangeAccent(change: ConfigChange, label: string): ChangeBadgeStyle {
  const kind = change.typedChange?.kind ?? '';
  const type = (change.changeType || '').toLowerCase();
  const category = (change.category || '').toLowerCase();
  const normalizedLabel = label.toLowerCase();

  if (kind === 'Screenshot/v1' || type.includes('screenshot')) return CHANGE_BADGE_STYLES.artifact;
  if (kind === 'PermissionChange/v1' || category.startsWith('rbac') || type.includes('permission')) return CHANGE_BADGE_STYLES.permission;
  if (kind === 'Backup/v1' || category.startsWith('backup') || type.includes('backup') || type.includes('restore')) return CHANGE_BADGE_STYLES.backup;
  if (kind === 'CostChange/v1' || type.includes('cost')) return CHANGE_BADGE_STYLES.cost;
  if (kind === 'Promotion/v1' || kind === 'Rollback/v1' || kind === 'PipelineRun/v1' || kind === 'PlaybookExecution/v1') return CHANGE_BADGE_STYLES.release;
  if (kind === 'Scaling/v1' || type.includes('replica') || type.includes('scaling')) return CHANGE_BADGE_STYLES.scale;
  if (kind === 'Deployment/v1' || type === 'diff' || category.startsWith('deployment')) return CHANGE_BADGE_STYLES.diff;
  if (type.includes('policy') || normalizedLabel.includes('policy')) return CHANGE_BADGE_STYLES.policy;
  return CHANGE_BADGE_STYLES.default;
}

function getChangeIconName(change: ConfigChange): string {
  return change.typedChange?.kind ? change.typedChange.kind.split('/')[0] : change.changeType;
}

function ChangeIcon({ change }: { change: ConfigChange }) {
  return (
    <span className="w-[3.5mm] h-[3.5mm] shrink-0 flex items-center justify-center">
      <Icon name={getChangeIconName(change)} size={10} />
    </span>
  );
}

function ChangeTypeBadge({ change, label }: { change: ConfigChange; label: string }) {
  const accent = getChangeAccent(change, label);

  return (
    <Badge
      variant="custom"
      size="xs"
      shape="rounded"
      label={label}
      color={accent.color}
      textColor={accent.textColor}
      borderColor={accent.borderColor}
      className="shrink-0"
    />
  );
}

function SecondaryMeta({ label, className = 'text-gray-500' }: { label: string; className?: string }) {
  return (
    <span className={`text-[9px] leading-none whitespace-nowrap shrink-0 ${className}`}>
      {label}
    </span>
  );
}

function ChangeEntry({ change, dateFormat, hideConfigName }: { change: ConfigChange; dateFormat: TimeBucketFormat; hideConfigName?: boolean }) {
  const sev = change.severity ?? 'info';
  const author = change.createdBy || change.externalCreatedBy || change.source || '';
  const artifactCount = (change.artifacts || []).length;
  const typedDisplay = getTypedChangeDisplay(change);
  const summary = change.summary || typedDisplay?.summary;
  const changeTypeLabel = getChangeTypeLabel(change, typedDisplay);
  const hasSecondaryMeta = sev !== 'info' || Boolean(author);
  const hasPrimaryMeta = Boolean(
    typedDisplay?.diff
    || (typedDisplay?.meta && typedDisplay.meta.length > 0)
    || (!hideConfigName && change.configName)
    || (change.count ?? 0) > 1
    || artifactCount > 0,
  );
  return (
    <div className="flex items-start gap-[1.5mm] py-[0.45mm] border-b border-gray-50 last:border-b-0 text-xs">
      <span className="text-gray-400 font-mono whitespace-nowrap w-[12mm] text-right shrink-0">
        {change.createdAt ? formatEntryDate(change.createdAt, dateFormat) : '-'}
      </span>
      <ChangeIcon change={change} />
      <div className="flex-1 min-w-0 flex items-start gap-[1.5mm]">
        <div className="flex-1 min-w-0">
          <div className="flex flex-wrap items-start gap-[0.8mm] min-w-0">
            <ChangeTypeBadge change={change} label={changeTypeLabel} />
            {summary && (
              <div className="min-w-0 flex-1 text-slate-700 leading-tight break-words">
                {summary}
              </div>
            )}
          </div>
          {hasPrimaryMeta && (
            <div className="flex flex-wrap items-center gap-[0.8mm] min-w-0 mt-[0.35mm]">
              {typedDisplay?.diff && (
                <span className="inline-flex items-center flex-wrap gap-[0.5mm] min-w-0">
                  {typedDisplay.diff.label && (
                    <span className="text-[9px] font-medium text-slate-500 uppercase tracking-[0.03em]">
                      {typedDisplay.diff.label}
                    </span>
                  )}
                  <span className="text-[9px] font-mono text-red-700 bg-red-50 border border-red-200 px-[0.6mm] rounded break-all">
                    {typedDisplay.diff.from}
                  </span>
                  <span className="text-[9px] text-slate-400">→</span>
                  <span className="text-[9px] font-mono text-green-700 bg-green-50 border border-green-200 px-[0.6mm] rounded break-all">
                    {typedDisplay.diff.to}
                  </span>
                </span>
              )}
              {!hideConfigName && change.configName && (
                <Badge
                  variant="custom"
                  size="xs"
                  shape="rounded"
                  label={change.configName}
                  color="bg-blue-50"
                  textColor="text-blue-700"
                  borderColor="border-blue-200"
                  className="shrink-0"
                />
              )}
              {typedDisplay?.meta?.map((meta) => (
                <Badge
                  key={meta}
                  variant="custom"
                  size="xs"
                  shape="rounded"
                  label={meta}
                  color="bg-slate-50"
                  textColor="text-slate-600"
                  borderColor="border-slate-200"
                  className="shrink-0"
                />
              ))}
              {(change.count ?? 0) > 1 && (
                <Badge
                  variant="custom"
                  size="xs"
                  shape="rounded"
                  label={`×${change.count}`}
                  color="bg-gray-100"
                  textColor="text-gray-600"
                  borderColor="border-gray-200"
                  className="shrink-0"
                />
              )}
              {artifactCount > 0 && (
                <a href={`#artifact-${change.id}`} style={{ textDecoration: 'none' }}>
                  <Badge
                    variant="custom"
                    size="xs"
                    shape="rounded"
                    label={`${artifactCount} screenshot${artifactCount > 1 ? 's' : ''}`}
                    color="bg-purple-50"
                    textColor="text-purple-700"
                    borderColor="border-purple-200"
                    className="shrink-0"
                  />
                </a>
              )}
            </div>
          )}
        </div>
        {hasSecondaryMeta && (
          <div className="ml-auto shrink-0 flex items-center gap-[0.8mm] pl-[1.5mm] text-right whitespace-nowrap">
            {sev !== 'info' && (
              <SecondaryMeta
                label={sev}
                className={`${SEVERITY_ACCENT_TEXT[sev] ?? SEVERITY_ACCENT_TEXT.info} lowercase`}
              />
            )}
            {author && (
              <SecondaryMeta label={`by ${author}`} />
            )}
          </div>
        )}
      </div>
    </div>
  );
}

interface BucketGroup {
  key: string;
  label: string;
  dateFormat: TimeBucketFormat;
  changes: ConfigChange[];
}

function groupByTimeBucket(changes: ConfigChange[]): BucketGroup[] {
  const sorted = [...changes].sort((a, b) => {
    const ta = a.createdAt ? new Date(a.createdAt).getTime() : 0;
    const tb = b.createdAt ? new Date(b.createdAt).getTime() : 0;
    return tb - ta;
  });

  const groups: BucketGroup[] = [];
  const groupMap = new Map<string, BucketGroup>();

  for (const c of sorted) {
    const bucket = c.createdAt ? getTimeBucket(c.createdAt) : { key: 'unknown', label: 'Unknown', dateFormat: 'monthDay' as TimeBucketFormat };
    let group = groupMap.get(bucket.key);
    if (!group) {
      group = { key: bucket.key, label: bucket.label, dateFormat: bucket.dateFormat, changes: [] };
      groupMap.set(bucket.key, group);
      groups.push(group);
    }
    group.changes.push(c);
  }

  return groups;
}

export default function ConfigChangesSection({ changes, hideConfigName: hideConfigNameProp }: Props) {
  if (!changes?.length) return null;
  const uniqueConfigs = new Set(changes.map((c) => c.configID || c.configName).filter(Boolean));
  const hideConfigName = hideConfigNameProp || uniqueConfigs.size <= 1;
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((sev) => [sev, changes.filter((c) => (c.severity ?? 'info') === sev).length])
  );

  const groups = groupByTimeBucket(changes);

  return (
    <Section variant="hero" title="Config Changes" size="md">
      <div className="flex flex-wrap gap-[2mm] mb-[2mm]" style={NO_BREAK_STYLE}>
        {SEVERITY_ORDER.filter((sev) => bySeverity[sev] > 0).map((sev) => (
          <div key={sev} className="flex-1 min-w-[20mm]" style={NO_BREAK_STYLE}>
            <SeverityStatCard
              color={SEVERITY_COLOR[sev]}
              value={bySeverity[sev]}
              label={sev.charAt(0).toUpperCase() + sev.slice(1)}
            />
          </div>
        ))}
      </div>
      {groups.map((group) => (
        <div key={group.key} className="mb-[2mm]">
          <div className="text-xs font-semibold text-gray-500 border-b border-gray-200 pb-[0.3mm] mb-[0.5mm]">
            {group.label}
            <span className="font-normal text-gray-400 ml-[1mm]">({group.changes.length})</span>
          </div>
          <div className="flex flex-col">
            {group.changes.map((c) => <ChangeEntry key={c.id} change={c} dateFormat={group.dateFormat} hideConfigName={hideConfigName} />)}
          </div>
        </div>
      ))}
    </Section>
  );
}
