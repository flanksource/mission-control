import React from 'react';
import { Badge, StatCard } from '@flanksource/facet';
import type { ApplicationChange } from '../types.ts';
import { ConfigTypeIcon } from './configTypeIcon.tsx';
import { IdentityIcon } from './rbac-visual.tsx';
import { ActorIdentity } from './config-change-entry.tsx';
import { formatEntryDate, getTimeBucket, type TimeBucketFormat } from './utils.ts';
import { filterRBACChanges, groupRBACChanges, type RBACChangeRow } from './change-section-utils.ts';

interface Props {
  changes: ApplicationChange[];
}

const COUNT_VALUE_CLASS = 'text-sm leading-none';
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const ACTION_BADGE_COLORS: Record<'Granted' | 'Revoked', { color: string; textColor: string; borderColor: string; symbol: string }> = {
  Granted: {
    color: 'bg-green-50',
    textColor: 'text-green-700',
    borderColor: 'border-green-200',
    symbol: '+',
  },
  Revoked: {
    color: 'bg-red-50',
    textColor: 'text-red-700',
    borderColor: 'border-red-200',
    symbol: '-',
  },
};

interface ConfigBucket {
  configId?: string;
  configName: string;
  configType?: string;
  rows: RBACChangeRow[];
  latestAt: number;
}

function bucketByConfig(rows: RBACChangeRow[]): ConfigBucket[] {
  const byKey = new Map<string, ConfigBucket>();
  for (const row of rows) {
    const key = row.configId || row.configName;
    let bucket = byKey.get(key);
    if (!bucket) {
      bucket = {
        configId: row.configId,
        configName: row.configName,
        configType: row.configType,
        rows: [],
        latestAt: 0,
      };
      byKey.set(key, bucket);
    }
    if (!bucket.configType && row.configType) bucket.configType = row.configType;
    const t = row.date ? new Date(row.date).getTime() : 0;
    if (t > bucket.latestAt) bucket.latestAt = t;
    bucket.rows.push(row);
  }
  const result = [...byKey.values()];
  for (const bucket of result) {
    bucket.rows.sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());
  }
  result.sort((a, b) => b.latestAt - a.latestAt);
  return result;
}

function RBACRow({ row, dateFormat }: { row: RBACChangeRow; dateFormat: TimeBucketFormat }) {
  const identityRoleSource = row.subjectKind === 'group' ? `group:${row.subject}` : undefined;
  const action = ACTION_BADGE_COLORS[row.action];
  const changedBy = row.changedBy !== '-' ? row.changedBy : row.source;

  return (
    <div className="flex items-start gap-[1.5mm] py-[0.4mm] border-b border-slate-50 last:border-b-0 text-xs">
      <span className="w-[12mm] shrink-0 whitespace-nowrap text-right font-mono text-xs text-slate-400">
        {row.date ? formatEntryDate(row.date, dateFormat) : '-'}
      </span>
      <div className="flex-1 min-w-0 flex items-start gap-[1.4mm]">
        <div className="min-w-0 flex-1 flex flex-wrap items-center gap-[0.6mm] text-xs text-slate-700">
          <Badge
            variant="custom"
            size="xxs"
            shape="rounded"
            label={action.symbol}
            color={action.color}
            textColor={action.textColor}
            borderColor={action.borderColor}
            className="font-normal"
          />
          <span className="text-xs font-semibold text-slate-900">{row.role || 'Access'}</span>
          <span className="text-xs text-slate-500">to</span>
          <span className="inline-flex items-center gap-[0.5mm] min-w-0">
            <IdentityIcon userId={row.subject} roleSource={identityRoleSource} size={8} />
            <span className="text-xs font-medium text-slate-800 break-all">{row.subject}</span>
          </span>
          {row.viaGroup && <span className="text-xs text-slate-400">via {row.viaGroup}</span>}
          {row.notes && <span className="text-xs text-slate-500 break-words">{row.notes}</span>}
        </div>
        {changedBy && changedBy !== '-' && (
          <div className="shrink-0 self-start pl-[1mm] max-w-[42mm]">
            <ActorIdentity actor={changedBy} />
          </div>
        )}
      </div>
    </div>
  );
}

export default function RBACChanges({ changes }: Props) {
  const relevant = filterRBACChanges(changes);
  if (!relevant.length) {
    return null;
  }

  const groups = groupRBACChanges(relevant);
  const rows: RBACChangeRow[] = groups.flatMap((group) =>
    group.rows.map((row) => ({ ...row, configName: group.configName, configType: group.configType })),
  );
  const grantedCount = rows.filter((row) => row.action === 'Granted').length;
  const revokedCount = rows.filter((row) => row.action === 'Revoked').length;
  const netCount = grantedCount - revokedCount;
  const netColor = netCount > 0 ? 'orange' : netCount < 0 ? 'green' : 'gray';

  const buckets = bucketByConfig(rows);

  return (
    <>
      <div className="flex flex-wrap items-stretch gap-[1.5mm] mb-[1.5mm]" style={NO_BREAK_STYLE}>
        <div className="flex-1 min-w-[22mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Granted"
            value={String(grantedCount)}
            variant="summary"
            size="xs"
            color={grantedCount > 0 ? 'orange' : 'gray'}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[22mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Revoked"
            value={String(revokedCount)}
            variant="summary"
            size="xs"
            color={revokedCount > 0 ? 'green' : 'gray'}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[22mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Net"
            value={netCount > 0 ? `+${netCount}` : String(netCount)}
            variant="summary"
            size="xs"
            color={netColor}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
      </div>
      {buckets.map((bucket) => (
        <div key={bucket.configId || bucket.configName} className="mb-[1.5mm]" style={NO_BREAK_STYLE}>
          <div className="flex items-center gap-[1mm] border-b border-slate-300 pb-[0.3mm] mb-[0.5mm]">
            {bucket.configType && <ConfigTypeIcon configType={bucket.configType} size={9} />}
            <span className="text-xs font-semibold text-slate-900">{bucket.configName}</span>
            {bucket.configType && (
              <span className="text-xs text-slate-500">({bucket.configType})</span>
            )}
            <span className="text-xs font-normal text-slate-400 ml-auto">{bucket.rows.length}</span>
          </div>
          <div className="flex flex-col">
            {bucket.rows.map((row) => {
              const bucketFmt = row.date ? getTimeBucket(row.date).dateFormat : 'monthDay';
              return <RBACRow key={row.id} row={row} dateFormat={bucketFmt} />;
            })}
          </div>
        </div>
      ))}
    </>
  );
}
