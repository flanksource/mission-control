import React from 'react';
import { Section, StatCard, CompactTable } from '@flanksource/facet';
import type { ApplicationBackup, ApplicationBackupRestore } from '../types.ts';
import { formatDateTime } from './utils.ts';
import BackupActivityCalendar from './BackupActivityCalendar.tsx';
import type { BackupCalendarStatus } from './change-section-utils.ts';

interface Props {
  backups: ApplicationBackup[];
  restores: ApplicationBackupRestore[];
}

export default function BackupsSection({ backups, restores }: Props) {
  const successCount = backups.filter((b) => b.status === 'success').length;
  const failedCount = backups.filter((b) => b.status !== 'success').length;
  const calendarEntries = backups.map((backup) => ({
    date: backup.date,
    status: (backup.status === 'success' ? 'success' : backup.status === 'failed' ? 'failed' : 'warning') as BackupCalendarStatus,
    label: backup.size || undefined,
  }));

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
        <BackupActivityCalendar entries={calendarEntries} />
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
