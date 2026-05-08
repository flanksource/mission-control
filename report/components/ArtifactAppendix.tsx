import React from 'react';
import { Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigChange, ConfigChangeArtifact } from '../config-types.ts';
import { formatMonthDay, formatTime, humanizeSize } from './utils.ts';
import { getChangeSourceIconName } from './change-section-utils.ts';

interface Props {
  changes?: ConfigChange[];
}

function ArtifactMetadata({ artifact }: { artifact: ConfigChangeArtifact }) {
  const parts: string[] = [artifact.contentType, humanizeSize(artifact.size) ?? ''].filter(Boolean);
  const checksum = artifact.checksum ? `sha:${artifact.checksum.slice(0, 12)}` : undefined;
  if (checksum) parts.push(checksum);
  if (artifact.createdAt) parts.push(`${formatMonthDay(artifact.createdAt)} ${formatTime(artifact.createdAt)}`);

  return (
    <div className="text-xs text-gray-500 mb-[2mm] flex flex-wrap gap-x-[2mm] gap-y-[0.5mm]">
      <span className="font-semibold text-slate-700">{artifact.filename}</span>
      {parts.map((part) => (
        <span key={part} className="font-mono">{part}</span>
      ))}
      {artifact.path && <span className="font-mono text-gray-400 break-all">{artifact.path}</span>}
    </div>
  );
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
    <Section variant="hero" title="Appendix: Attachments" size="md">
      {[...grouped.entries()].map(([key, group]) => (
        <div key={key} className="mb-[4mm]">
          <div className="flex items-center gap-[1.5mm] mb-[2mm]">
            {group.configType && <Icon name={group.configType} size={14} />}
            <span className="text-sm font-bold text-slate-800">{group.configName}</span>
            {group.configType && <span className="text-xs text-gray-500">({group.configType})</span>}
          </div>

          {group.changes.map((change) => {
            const sourceIcon = getChangeSourceIconName(change.source);
            return (
            <div key={change.id} id={`artifact-${change.id}`} className="mb-[3mm]">
              <div className="flex items-center gap-[1.5mm] text-xs mb-[1mm]">
                <span className="text-gray-400 font-mono">{change.createdAt ? `${formatMonthDay(change.createdAt)} ${formatTime(change.createdAt)}` : ''}</span>
                {sourceIcon && <Icon name={sourceIcon} size={12} />}
                <Icon name={change.changeType} size={10} />
                <span className="font-semibold text-slate-800">{change.changeType}</span>
                <span className="text-gray-600 truncate">{change.summary ?? ''}</span>
              </div>
              {(change.artifacts || []).map((a) => (
                <div key={a.id} id={`artifact-${a.id}`} className="mb-[2mm]">
                  {a.dataUri && a.contentType.startsWith('image/') && (
                    <img src={a.dataUri} alt={a.filename} style={{ maxWidth: '100%', display: 'block', marginBottom: '1mm' }} />
                  )}
                  <ArtifactMetadata artifact={a} />
                </div>
              ))}
            </div>
            );
          })}
        </div>
      ))}
    </Section>
  );
}
