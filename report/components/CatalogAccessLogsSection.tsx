import React from 'react';
import { Badge, Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportAccessLog } from '../catalog-report-types.ts';
import { getTimeBucket, formatEntryDate, type TimeBucketFormat } from './utils.ts';
import { ActorIdentity, LabelBadge } from './config-change-entry.tsx';

interface Props {
  logs?: CatalogReportAccessLog[];
}

interface BucketGroup {
  key: string;
  label: string;
  dateFormat: TimeBucketFormat;
  logs: CatalogReportAccessLog[];
}

function groupByTimeBucket(logs: CatalogReportAccessLog[]): BucketGroup[] {
  const sorted = [...logs].sort((a, b) => {
    const ta = a.createdAt ? new Date(a.createdAt).getTime() : 0;
    const tb = b.createdAt ? new Date(b.createdAt).getTime() : 0;
    return tb - ta;
  });

  const groups: BucketGroup[] = [];
  const groupMap = new Map<string, BucketGroup>();

  for (const log of sorted) {
    const bucket = log.createdAt
      ? getTimeBucket(log.createdAt)
      : { key: 'unknown', label: 'Unknown', dateFormat: 'monthDay' as TimeBucketFormat };
    let group = groupMap.get(bucket.key);
    if (!group) {
      group = { key: bucket.key, label: bucket.label, dateFormat: bucket.dateFormat, logs: [] };
      groupMap.set(bucket.key, group);
      groups.push(group);
    }
    group.logs.push(log);
  }

  return groups;
}

function MFABadge({ mfa }: { mfa: boolean }) {
  return (
    <Badge
      variant="custom"
      size="xxs"
      shape="rounded"
      label={mfa ? 'MFA' : 'no MFA'}
      color={mfa ? 'bg-green-50' : 'bg-slate-50'}
      textColor={mfa ? 'text-green-700' : 'text-slate-500'}
      borderColor={mfa ? 'border-green-200' : 'border-slate-200'}
      className="font-normal"
    />
  );
}

function AccessLogEntry({ log, dateFormat, hideConfigName }: {
  log: CatalogReportAccessLog;
  dateFormat: TimeBucketFormat;
  hideConfigName: boolean;
}) {
  const configLabel = !hideConfigName && log.configName
    ? (log.configType ? `${log.configName} (${log.configType})` : log.configName)
    : undefined;
  const propEntries = log.properties ? Object.entries(log.properties) : [];

  return (
    <div className="flex items-start gap-[1.5mm] py-[0.55mm] border-b border-slate-50 last:border-b-0 text-xs">
      <span className="w-[12mm] shrink-0 whitespace-nowrap text-right font-mono text-xs text-slate-400">
        {log.createdAt ? formatEntryDate(log.createdAt, dateFormat) : '-'}
      </span>
      <span className="inline-flex h-[3.5mm] w-[3.5mm] shrink-0 items-center justify-center text-slate-500 pt-[0.25mm]">
        <Icon name="user" size={10} />
      </span>
      <div className="flex-1 min-w-0 flex items-start gap-[1.4mm]">
        <div className="min-w-0 flex-1 flex flex-wrap items-center gap-[0.8mm] text-xs text-slate-700">
          <span className="text-xs font-semibold text-slate-900">{log.userName}</span>
          <MFABadge mfa={log.mfa} />
          {log.count > 1 && (
            <LabelBadge label="Count" value={`×${log.count}`} color="#e5e7eb" textColor="#4b5563" />
          )}
          {configLabel && (
            <LabelBadge label="Config" value={configLabel} color="#dbeafe" textColor="#1d4ed8" />
          )}
          {propEntries.map(([k, v]) => (
            <LabelBadge key={k} label={k} value={String(v)} />
          ))}
        </div>
        {log.userName && (
          <div className="shrink-0 self-start pl-[1mm] max-w-[42mm]">
            <ActorIdentity actor={log.userName} />
          </div>
        )}
      </div>
    </div>
  );
}

export default function CatalogAccessLogsSection({ logs }: Props) {
  if (!logs?.length) return null;

  const uniqueConfigs = new Set(logs.map((log) => log.configId || log.configName).filter(Boolean));
  const hideConfigName = uniqueConfigs.size <= 1;
  const groups = groupByTimeBucket(logs);

  return (
    <Section variant="hero" title={`Access Logs (${logs.length})`} size="md">
      {groups.map((group) => (
        <div key={group.key} className="mb-[2mm]">
          <div className="text-xs font-semibold text-slate-500 border-b border-slate-200 pb-[0.3mm] mb-[0.5mm]">
            {group.label}
            <span className="font-normal text-slate-400 ml-[1mm]">({group.logs.length})</span>
          </div>
          <div className="flex flex-col">
            {group.logs.map((log, idx) => (
              <AccessLogEntry
                key={`${log.userId}-${log.createdAt}-${idx}`}
                log={log}
                dateFormat={group.dateFormat}
                hideConfigName={hideConfigName}
              />
            ))}
          </div>
        </div>
      ))}
    </Section>
  );
}
