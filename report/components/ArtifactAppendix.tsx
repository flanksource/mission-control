import React from 'react';
import { Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigChange } from '../config-types.ts';
import { formatMonthDay, formatTime } from './utils.ts';

interface Props {
  changes?: ConfigChange[];
}

export default function ArtifactAppendix({ changes }: Props) {
  const withArtifacts = (changes || []).filter((c) => (c.artifacts || []).length > 0);
  if (withArtifacts.length === 0) return null;

  const grouped = new Map<string, { configName: string; configType?: string; changes: ConfigChange[] }>();
  for (const c of withArtifacts) {
    const key = c.configName || c.configID || 'unknown';
    if (!grouped.has(key)) {
      grouped.set(key, { configName: c.configName || 'Unknown', configType: c.configType, changes: [] });
    }
    grouped.get(key)!.changes.push(c);
  }

  return (
    <Section variant="hero" title="Appendix: Screenshots" size="md">
      {[...grouped.entries()].map(([key, group]) => (
        <div key={key} className="mb-[4mm]">
          <div className="flex items-center gap-[1.5mm] mb-[2mm]">
            {group.configType && <Icon name={group.configType} size={14} />}
            <span className="text-sm font-bold text-slate-800">{group.configName}</span>
            {group.configType && <span className="text-xs text-gray-500">({group.configType})</span>}
          </div>

          {group.changes.map((change) => (
            <div key={change.id} id={`artifact-${change.id}`} className="mb-[3mm]">
              <div className="flex items-center gap-[1.5mm] text-xs mb-[1mm]">
                <span className="text-gray-400 font-mono">{change.createdAt ? `${formatMonthDay(change.createdAt)} ${formatTime(change.createdAt)}` : ''}</span>
                <Icon name={change.changeType} size={10} />
                <span className="font-semibold text-slate-800">{change.changeType}</span>
                <span className="text-gray-600 truncate">{change.summary ?? ''}</span>
              </div>
              {(change.artifacts || []).map((a) => {
                if (a.dataUri && a.contentType.startsWith('image/')) {
                  return (
                    <React.Fragment key={a.id}>
                      <img src={a.dataUri} alt={a.filename} style={{ maxWidth: '100%', display: 'block', marginBottom: '1mm' }} />
                      <div className="text-xs text-gray-400 mb-[2mm]">{a.filename}</div>
                    </React.Fragment>
                  );
                }
                return (
                  <div key={a.id} className="text-xs text-gray-500 mb-[1mm]">
                    {a.filename} ({a.contentType}, {formatSize(a.size)})
                  </div>
                );
              })}
            </div>
          ))}
        </div>
      ))}
    </Section>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)}KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
}
