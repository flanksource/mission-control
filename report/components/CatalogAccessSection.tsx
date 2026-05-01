import React from 'react';
import { Badge, Section, CompactTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportAccess } from '../catalog-report-types.ts';
import { formatMonthDay } from './utils.ts';

interface Props {
  access?: CatalogReportAccess[];
}

function StaleBadge({ lastSignedInAt }: { lastSignedInAt?: string }) {
  if (!lastSignedInAt) {
    return <Badge variant="custom" size="xs" shape="rounded" label="never" color="bg-red-50" textColor="text-red-600" borderColor="border-red-200" className="font-semibold" />;
  }
  const days = Math.floor((Date.now() - new Date(lastSignedInAt).getTime()) / 86400000);
  if (days > 90) {
    return <Badge variant="custom" size="xs" shape="rounded" label="stale" color="bg-red-50" textColor="text-red-600" borderColor="border-red-200" className="font-semibold" />;
  }
  if (days > 30) {
    return <Badge variant="custom" size="xs" shape="rounded" label="aging" color="bg-yellow-50" textColor="text-yellow-600" borderColor="border-yellow-200" className="font-semibold" />;
  }
  return null;
}

export default function CatalogAccessSection({ access }: Props) {
  if (!access?.length) return null;

  const hasMultipleConfigs = access.some((a) => a.configName);
  const rows = access.map((a) => {
    const row = [
      <span className="inline-flex items-center gap-[1mm] font-medium text-slate-800">
        <Icon name={a.userType === 'group' ? 'group' : 'user'} size={10} />
        {a.userName}
      </span>,
      <div className="flex flex-col gap-[0.5mm]">
        <span style={{ overflowWrap: 'anywhere', wordBreak: 'break-word' }}>{a.role}</span>
        {(a.roleExternalIds || []).length > 0 && (
          <span className="font-mono text-[5pt] leading-tight text-slate-500" style={{ overflowWrap: 'anywhere', wordBreak: 'break-all' }}>
            {a.roleExternalIds!.join(', ')}
          </span>
        )}
      </div>,
      <span className="text-gray-600">{a.email}</span>,
      <span className="text-gray-500">{a.userType}</span>,
      a.lastSignedInAt ? (
        <span className="inline-flex items-center gap-[1mm]">
          {formatMonthDay(a.lastSignedInAt)}
          <StaleBadge lastSignedInAt={a.lastSignedInAt} />
        </span>
      ) : (
        <span className="inline-flex items-center gap-[1mm]">
          <span className="text-gray-400">-</span>
          <StaleBadge />
        </span>
      ),
      a.lastReviewedAt ? formatMonthDay(a.lastReviewedAt) : <span className="text-gray-400">-</span>,
    ];
    if (hasMultipleConfigs) {
      row.splice(0, 0, <span className="text-blue-600 text-xs">{a.configName || '-'}</span>);
    }
    return row;
  });

  const columns = hasMultipleConfigs
    ? ['Config', 'User', 'Role', 'Email', 'Type', 'Last Sign In', 'Last Reviewed']
    : ['User', 'Role', 'Email', 'Type', 'Last Sign In', 'Last Reviewed'];

  return (
    <Section variant="hero" title={`Access Control (${access.length})`} size="md">
      <CompactTable
        variant="reference"
        columns={columns}
        data={rows}
      />
    </Section>
  );
}
