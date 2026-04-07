import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { CatalogReportAccessLog } from '../catalog-report-types.ts';
import { formatMonthDay, formatTime } from './utils.ts';

interface Props {
  logs?: CatalogReportAccessLog[];
}

function MFABadge({ mfa }: { mfa: boolean }) {
  if (mfa) {
    return <span className="text-xs text-green-700 bg-green-50 border border-green-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">MFA</span>;
  }
  return <span className="text-xs text-gray-500 bg-gray-50 border border-gray-200 px-[0.5mm] py-[0.15mm] rounded font-semibold">no MFA</span>;
}

export default function CatalogAccessLogsSection({ logs }: Props) {
  if (!logs?.length) return null;

  const rows = logs.map((log) => [
    <span className="font-medium text-slate-800">{log.userName}</span>,
    log.createdAt ? `${formatMonthDay(log.createdAt)} ${formatTime(log.createdAt)}` : '-',
    <MFABadge mfa={log.mfa} />,
    log.count > 1 ? (
      <span className="text-xs text-gray-500 bg-gray-100 px-[0.7mm] rounded">×{log.count}</span>
    ) : (
      '1'
    ),
    log.properties && Object.keys(log.properties).length > 0 ? (
      <span className="text-xs text-gray-500">
        {Object.entries(log.properties).map(([k, v]) => `${k}=${v}`).join(', ')}
      </span>
    ) : (
      <span className="text-gray-400">-</span>
    ),
  ]);

  return (
    <Section variant="hero" title={`Access Logs (${logs.length})`} size="md">
      <CompactTable
        variant="reference"
        columns={['User', 'Time', 'MFA', 'Count', 'Properties']}
        data={rows}
      />
    </Section>
  );
}
