import React from 'react';
import { Badge, Section, CompactTable } from '@flanksource/facet';
import type { CatalogReportAccessLog } from '../catalog-report-types.ts';
import { formatMonthDay, formatTime } from './utils.ts';

interface Props {
  logs?: CatalogReportAccessLog[];
}

function MFABadge({ mfa }: { mfa: boolean }) {
  if (mfa) {
    return <Badge variant="custom" size="xs" shape="rounded" label="MFA" color="bg-green-50" textColor="text-green-700" borderColor="border-green-200" className="font-semibold" />;
  }
  return <Badge variant="custom" size="xs" shape="rounded" label="no MFA" color="bg-gray-50" textColor="text-gray-500" borderColor="border-gray-200" className="font-semibold" />;
}

export default function CatalogAccessLogsSection({ logs }: Props) {
  if (!logs?.length) return null;

  const rows = logs.map((log) => [
    <span className="font-medium text-slate-800">{log.userName}</span>,
    log.createdAt ? `${formatMonthDay(log.createdAt)} ${formatTime(log.createdAt)}` : '-',
    <MFABadge mfa={log.mfa} />,
    log.count > 1 ? (
      <Badge variant="custom" size="xs" shape="rounded" label={`×${log.count}`} color="bg-gray-100" textColor="text-gray-500" borderColor="border-gray-200" />
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
