import React from 'react';
import { Section, StatCard, CompactTable } from '@flanksource/facet';
import type { ApplicationBackup, ApplicationBackupRestore } from '../types.ts';
import { formatDateTime } from './utils.ts';

interface Props {
  backups: ApplicationBackup[];
  restores: ApplicationBackupRestore[];
}

const DAY_HEADERS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

const CELL_CLASSES: Record<string, string> = {
  success: 'bg-green-50 border border-green-600',
  failed:  'bg-red-50 border border-red-600',
  none:    'bg-gray-100 border border-gray-200',
};

const SIZE_TEXT_CLASSES: Record<string, string> = {
  success: 'text-green-700',
  failed:  'text-red-600',
};

function BackupCalendar({ backups }: { backups: ApplicationBackup[] }) {
  const referenceDate = backups.length > 0
    ? new Date(backups.reduce((a, b) => (a.date > b.date ? a : b)).date)
    : new Date();

  const year = referenceDate.getFullYear();
  const month = referenceDate.getMonth();
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const firstDow = new Date(year, month, 1).getDay();

  const dateMap: Record<string, ApplicationBackup> = {};
  for (const b of backups) {
    dateMap[b.date.slice(0, 10)] = b;
  }

  const monthLabel = referenceDate.toLocaleString('default', { month: 'long', year: 'numeric' });
  const cells: (number | null)[] = [
    ...Array(firstDow).fill(null),
    ...Array.from({ length: daysInMonth }, (_, i) => i + 1),
  ];

  return (
    <div>
      <p className="text-[9pt] font-semibold text-slate-700 mb-[2mm]">{monthLabel}</p>
      <div className="grid grid-cols-7 gap-[1mm]">
        {DAY_HEADERS.map((d) => (
          <div key={d} className="text-center text-[8pt] text-gray-500 pb-[1mm]">{d}</div>
        ))}
        {cells.map((day, idx) => {
          if (day === null) return <div key={`empty-${idx}`} />;
          const key = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
          const backup = dateMap[key];
          const cellClass = backup ? CELL_CLASSES[backup.status] ?? CELL_CLASSES.none : CELL_CLASSES.none;
          return (
            <div
              key={key}
              className={`${cellClass} rounded-[1mm] p-[1mm] min-h-[10mm] flex flex-col justify-between`}
            >
              <span className="text-[7pt] text-gray-700 font-semibold">{day}</span>
              {backup && (
                <span className={`text-[6pt] ${SIZE_TEXT_CLASSES[backup.status] ?? ''}`}>
                  {backup.size}
                </span>
              )}
            </div>
          );
        })}
      </div>
      <div className="flex gap-[3mm] mt-[2mm] text-[8pt] text-gray-600">
        <span className="flex items-center gap-1">
          <span className="inline-block w-[3mm] h-[3mm] bg-green-50 border border-green-600 rounded-[0.5mm]" />
          Success
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block w-[3mm] h-[3mm] bg-red-50 border border-red-600 rounded-[0.5mm]" />
          Failed
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block w-[3mm] h-[3mm] bg-gray-100 border border-gray-200 rounded-[0.5mm]" />
          No backup
        </span>
      </div>
    </div>
  );
}


export default function BackupsSection({ backups, restores }: Props) {
  const successCount = backups.filter((b) => b.status === 'success').length;
  const failedCount = backups.filter((b) => b.status !== 'success').length;

  const failedRows = backups
    .filter((b) => b.status !== 'success')
    .map((b) => [b.database, formatDateTime(b.date), b.size, b.status]);

  const restoreRows = restores.map((r) => [
    r.database,
    formatDateTime(r.date),
    r.status,
    formatDateTime(r.completedAt),
  ]);

  return (
    <Section variant="hero" title="Backups & Restores" size="md">
      <div className="grid grid-cols-3 gap-[3mm] mb-[4mm]">
        <StatCard label="Total Backups" value={String(backups.length)} variant="bordered" size="sm" />
        <StatCard label="Successful" value={String(successCount)} variant="bordered" size="sm" />
        <StatCard label="Failed" value={String(failedCount)} variant="bordered" size="sm" />
      </div>
      <div className="mb-[4mm]">
        <BackupCalendar backups={backups} />
      </div>
      {failedRows.length > 0 && (
        <div className="mb-[4mm]">
          <h3 className="text-[11pt] font-semibold text-slate-800 mb-[2mm]">Failed Backups</h3>
          <CompactTable variant="reference" columns={['Database', 'Date', 'Size', 'Status']} data={failedRows} />
        </div>
      )}
      {restoreRows.length > 0 && (
        <div>
          <h3 className="text-[11pt] font-semibold text-slate-800 mb-[2mm]">Restore History</h3>
          <CompactTable variant="reference" columns={['Database', 'Started', 'Status', 'Completed']} data={restoreRows} />
        </div>
      )}
    </Section>
  );
}
