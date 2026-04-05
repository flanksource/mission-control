import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { CatalogReportAccess } from '../catalog-report-types.ts';
import { formatRelative } from './utils.ts';

interface Props {
  access: CatalogReportAccess[];
}

function StaleBadge({ lastSignedInAt }: { lastSignedInAt?: string }) {
  if (!lastSignedInAt) {
    return <span className="text-[4.5pt] text-red-600 bg-red-50 border border-red-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">never</span>;
  }
  const days = Math.floor((Date.now() - new Date(lastSignedInAt).getTime()) / 86400000);
  if (days > 90) {
    return <span className="text-[4.5pt] text-red-600 bg-red-50 border border-red-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">stale</span>;
  }
  if (days > 30) {
    return <span className="text-[4.5pt] text-yellow-600 bg-yellow-50 border border-yellow-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">aging</span>;
  }
  return null;
}

export default function CatalogAccessSection({ access }: Props) {
  if (access.length === 0) {
    return (
      <Section variant="hero" title="Access Control" size="md">
        <p className="text-[8pt] text-gray-500 italic">No access entries found.</p>
      </Section>
    );
  }

  const rows = access.map((a) => [
    <span className="font-medium text-slate-800">{a.userName}</span>,
    a.role,
    <span className="text-gray-600">{a.email}</span>,
    <span className="text-gray-500">{a.userType}</span>,
    a.lastSignedInAt ? (
      <span className="inline-flex items-center gap-[1mm]">
        {formatRelative(a.lastSignedInAt)}
        <StaleBadge lastSignedInAt={a.lastSignedInAt} />
      </span>
    ) : (
      <span className="inline-flex items-center gap-[1mm]">
        <span className="text-gray-400">-</span>
        <StaleBadge />
      </span>
    ),
    a.lastReviewedAt ? formatRelative(a.lastReviewedAt) : <span className="text-gray-400">-</span>,
  ]);

  return (
    <Section variant="hero" title={`Access Control (${access.length})`} size="md">
      <CompactTable
        variant="reference"
        columns={['User', 'Role', 'Email', 'Type', 'Last Sign In', 'Last Reviewed']}
        data={rows}
      />
    </Section>
  );
}
