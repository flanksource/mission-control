import React from 'react';
import { Section, SeverityStatCard } from './facet-components.tsx';
import type { ConfigChange, ConfigSeverity } from './types.ts';
import { getTimeBucket, type TimeBucketFormat } from './utils.ts';
import { ChangeEntry } from './config-change-entry.tsx';
import { ConfigTypeIcon } from './configTypeIcon.tsx';
import type { ConfigChangesExtension } from './config-changes-extension.ts';

interface Props {
  changes?: ConfigChange[];
  hideConfigName?: boolean;
  extensions?: ConfigChangesExtension[];
}

const SEVERITY_ORDER: ConfigSeverity[] = ['critical', 'high', 'medium', 'low', 'info'];
const SEVERITY_COLOR: Record<string, 'red' | 'orange' | 'yellow' | 'blue'> = {
  critical: 'red',
  high: 'orange',
  medium: 'yellow',
  low: 'blue',
  info: 'blue',
};
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const STAT_CARD_CLASS = 'flex-[1_1_24mm] min-w-[18mm] max-w-[42mm]';

interface BucketGroup {
  key: string;
  label: string;
  dateFormat: TimeBucketFormat;
  changes: ConfigChange[];
}

interface ConfigGroup {
  key: string;
  configName: string;
  configType?: string;
  latestAt: number;
  buckets: BucketGroup[];
}

interface ExtensionMatch {
  ext: ConfigChangesExtension;
  changes: ConfigChange[];
}

function partitionWithExtensions(
  changes: ConfigChange[],
  extensions: ConfigChangesExtension[],
): { main: ConfigChange[]; matches: ExtensionMatch[] } {
  const matches: ExtensionMatch[] = extensions.map((ext) => ({ ext, changes: [] }));
  const main: ConfigChange[] = [];

  for (const change of changes) {
    let claimed = false;
    for (const match of matches) {
      if (!match.ext.filter(change)) continue;
      match.changes.push(change);
      if (match.ext.drop !== false) {
        claimed = true;
        break;
      }
    }
    if (!claimed) main.push(change);
  }

  return { main, matches };
}

function groupByTimeBucket(changes: ConfigChange[]): BucketGroup[] {
  const sorted = [...changes].sort((a, b) => {
    const ta = a.createdAt ? new Date(a.createdAt).getTime() : 0;
    const tb = b.createdAt ? new Date(b.createdAt).getTime() : 0;
    return tb - ta;
  });

  const groups: BucketGroup[] = [];
  const groupMap = new Map<string, BucketGroup>();

  for (const change of sorted) {
    const bucket = change.createdAt
      ? getTimeBucket(change.createdAt)
      : { key: 'unknown', label: 'Unknown', dateFormat: 'monthDay' as TimeBucketFormat };
    let group = groupMap.get(bucket.key);
    if (!group) {
      group = { key: bucket.key, label: bucket.label, dateFormat: bucket.dateFormat, changes: [] };
      groupMap.set(bucket.key, group);
      groups.push(group);
    }
    group.changes.push(change);
  }

  return groups;
}

function groupByConfig(changes: ConfigChange[]): ConfigGroup[] {
  const byKey = new Map<string, ConfigChange[]>();
  const meta = new Map<string, { configName: string; configType?: string }>();

  for (const change of changes) {
    const key = change.configID || change.configName || 'unknown';
    if (!byKey.has(key)) {
      byKey.set(key, []);
      meta.set(key, {
        configName: change.configName || 'Unknown',
        configType: change.configType,
      });
    }
    byKey.get(key)!.push(change);
  }

  const result: ConfigGroup[] = [];
  for (const [key, bucketChanges] of byKey) {
    const buckets = groupByTimeBucket(bucketChanges);
    const latestAt = bucketChanges.reduce((acc, c) => {
      const t = c.createdAt ? new Date(c.createdAt).getTime() : 0;
      return t > acc ? t : acc;
    }, 0);
    const m = meta.get(key)!;
    result.push({ key, configName: m.configName, configType: m.configType, latestAt, buckets });
  }

  result.sort((a, b) => b.latestAt - a.latestAt);
  return result;
}

function DateBuckets({ buckets, dateFormat: _unused }: { buckets: BucketGroup[]; dateFormat?: TimeBucketFormat }) {
  return (
    <>
      {buckets.map((group) => (
        <div key={group.key} className="mb-[1.5mm]">
          <div className="text-xs font-semibold text-slate-500 border-b border-slate-200 pb-[0.3mm] mb-[0.5mm]">
            {group.label}
            <span className="font-normal text-slate-400 ml-[1mm]">({group.changes.length})</span>
          </div>
          <div className="flex flex-col">
            {group.changes.map((change) => (
              <ChangeEntry
                key={change.id}
                change={change}
                dateFormat={group.dateFormat}
                hideConfigName
              />
            ))}
          </div>
        </div>
      ))}
    </>
  );
}

function MainChangesList({ changes, hideConfigName }: { changes: ConfigChange[]; hideConfigName?: boolean }) {
  const uniqueConfigs = new Set(changes.map((change) => change.configID || change.configName).filter(Boolean));
  const flatten = hideConfigName || uniqueConfigs.size <= 1;
  const bySeverity = Object.fromEntries(
    SEVERITY_ORDER.map((severity) => [severity, changes.filter((change) => (change.severity ?? 'info') === severity).length]),
  );

  return (
    <Section variant="hero" title="Config Changes" size="md">
      <div className="flex flex-wrap gap-[2mm] mb-[2mm]" style={NO_BREAK_STYLE}>
        {SEVERITY_ORDER.filter((severity) => bySeverity[severity] > 0).map((severity) => (
          <div key={severity} className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
            <SeverityStatCard
              color={SEVERITY_COLOR[severity]}
              value={bySeverity[severity]}
              label={severity.charAt(0).toUpperCase() + severity.slice(1)}
            />
          </div>
        ))}
      </div>
      {flatten ? (
        <DateBuckets buckets={groupByTimeBucket(changes)} />
      ) : (
        groupByConfig(changes).map((configGroup) => (
          <div key={configGroup.key} className="mb-[2mm]" style={NO_BREAK_STYLE}>
            <div className="flex items-center gap-[1mm] border-b border-slate-300 pb-[0.3mm] mb-[0.8mm]">
              {configGroup.configType && <ConfigTypeIcon configType={configGroup.configType} size={10} />}
              <span className="text-xs font-semibold text-slate-900">{configGroup.configName}</span>
              {configGroup.configType && (
                <span className="text-xs text-slate-500">({configGroup.configType})</span>
              )}
              <span className="text-xs font-normal text-slate-400 ml-auto">
                {configGroup.buckets.reduce((n, b) => n + b.changes.length, 0)}
              </span>
            </div>
            <DateBuckets buckets={configGroup.buckets} />
          </div>
        ))
      )}
    </Section>
  );
}

export default function ConfigChangesSection({ changes, hideConfigName, extensions }: Props) {
  if (!changes?.length) return null;

  const exts = extensions ?? [];
  const { main, matches } = partitionWithExtensions(changes, exts);

  const activeMatches = matches.filter((m) => m.changes.length > 0);
  if (activeMatches.length === 0 && main.length === 0) return null;

  return (
    <>
      {activeMatches.map(({ ext, changes: extChanges }) => {
        const PanelBody = <ext.panel changes={extChanges} />;
        if (ext.title) {
          return (
            <Section key={ext.key} variant="hero" title={ext.title} size="md">
              {PanelBody}
            </Section>
          );
        }
        return <React.Fragment key={ext.key}>{PanelBody}</React.Fragment>;
      })}
      {main.length > 0 && <MainChangesList changes={main} hideConfigName={hideConfigName} />}
    </>
  );
}
