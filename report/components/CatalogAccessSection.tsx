import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { CatalogReportAccess } from '../catalog-report-types.ts';
import { formatMonthDay } from './utils.ts';

interface Props {
  access?: CatalogReportAccess[];
}

function StaleBadge({ lastSignedInAt }: { lastSignedInAt?: string }) {
  if (!lastSignedInAt) {
    return <span className="text-xs text-red-600 bg-red-50 border border-red-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">never</span>;
  }
  const days = Math.floor((Date.now() - new Date(lastSignedInAt).getTime()) / 86400000);
  if (days > 90) {
    return <span className="text-xs text-red-600 bg-red-50 border border-red-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">stale</span>;
  }
  if (days > 30) {
    return <span className="text-xs text-yellow-600 bg-yellow-50 border border-yellow-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">aging</span>;
  }
  return null;
}

export default function CatalogAccessSection({ access }: Props) {
  if (!access?.length) return null;

  const hasMultipleConfigs = access.some((a) => a.configName);
  const rows = access.map((a) => {
    const row = [
      <span className="font-medium text-slate-800">{a.userName}</span>,
      a.role,
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
