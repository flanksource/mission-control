import React from 'react';
import { Section } from '@flanksource/facet';
import type { RBACChangeEntry } from '../rbac-types.ts';
import { formatDate } from './utils.ts';

const CHANGELOG_TYPE_COLORS: Record<string, { bg: string; fg: string }> = {
  PermissionGranted: { bg: '#DCFCE7', fg: '#166534' },
  PermissionRevoked: { bg: '#FEE2E2', fg: '#991B1B' },
  AccessReviewed:    { bg: '#DBEAFE', fg: '#1E40AF' },
};

function ChangeTypeBadge({ type }: { type: string }) {
  const colors = CHANGELOG_TYPE_COLORS[type] || { bg: '#E2E8F0', fg: '#334155' };
  return (
    <span
      className="inline-flex px-[1.5mm] py-[0.3mm] rounded text-[5pt] font-semibold"
      style={{ backgroundColor: colors.bg, color: colors.fg, whiteSpace: 'nowrap' }}
    >
      {type}
    </span>
  );
}

interface Props {
  changelog?: RBACChangeEntry[];
}

export default function RBACChangelogSection({ changelog }: Props) {
  if (!changelog?.length) return null;

  return (
    <Section variant="hero" title="Permission Changelog" size="md">
      <div className="flex flex-wrap gap-x-[4mm] gap-y-[1mm] mb-[2mm] text-[5pt] text-gray-500">
        <span className="font-semibold mr-[1mm]">Legend:</span>
        {Object.entries(CHANGELOG_TYPE_COLORS).map(([key, colors]) => (
          <span key={key} className="inline-flex items-center gap-[0.5mm]">
            <span className="inline-block w-[2mm] h-[2mm] rounded-sm" style={{ backgroundColor: colors.bg, border: `1px solid ${colors.fg}` }} />
            {key}
          </span>
        ))}
      </div>
      <div className="flex flex-col gap-[1mm]">
        {changelog.map((entry, i) => (
          <div key={i} className="flex items-baseline gap-[2mm] text-[6pt] text-gray-600">
            <span className="text-gray-400 font-mono" style={{ whiteSpace: 'nowrap' }}>{formatDate(entry.date)}</span>
            <ChangeTypeBadge type={entry.changeType} />
            <span className="font-medium text-gray-800">{entry.user}</span>
            <span className="text-gray-400">&rarr;</span>
            <span>{entry.role}</span>
            <span className="text-gray-500">{entry.configName}</span>
            {entry.source && <span className="text-gray-400">({entry.source})</span>}
            {entry.description && <span className="text-gray-400 italic">{entry.description}</span>}
          </div>
        ))}
      </div>
    </Section>
  );
}
