import React from 'react';
import { Section, StatCard, ListTable } from '@flanksource/facet';
import type { ApplicationBackup, ApplicationBackupRestore } from '../types.ts';
import { formatDateTime } from './utils.ts';
import BackupActivityCalendar from './BackupActivityCalendar.tsx';
import type { BackupCalendarStatus } from './change-section-utils.ts';

interface Props {
  backups: ApplicationBackup[];
  restores: ApplicationBackupRestore[];
}

const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const BACKUP_TAG_MAPPING = (key: string, value: unknown): string => {
  if (key !== 'status') {
    return '';
  }

  const normalized = String(value).toLowerCase();
  if (normalized.includes('fail')) {
    return 'text-red-700 bg-red-50 border-red-200';
  }
  if (normalized.includes('success')) {
    return 'text-green-700 bg-green-50 border-green-200';
  }
  if (normalized.includes('running') || normalized.includes('progress') || normalized.includes('started') || normalized.includes('queued')) {
    return 'text-orange-700 bg-orange-50 border-orange-200';
  }
  return 'text-gray-600 bg-gray-50 border-gray-200';
};

export default function BackupsSection({ backups, restores }: Props) {
  const isFailed = (status: string) => String(status).toLowerCase().includes('fail');
  const successCount = backups.filter((b) => b.status === 'success').length;
  const failedCount = backups.filter((b) => isFailed(b.status)).length;
  const calendarEntries = backups.map((backup) => ({
    date: backup.date,
    status: (backup.status === 'success' ? 'success' : isFailed(backup.status) ? 'failed' : 'warning') as BackupCalendarStatus,
    label: backup.size || undefined,
  }));

  const failedRows = backups.filter((b) => isFailed(b.status));

  return (
    <Section variant="hero" title="Backups & Restores" size="md">
      <div className="grid grid-cols-3 gap-[3mm] mb-[4mm]" style={NO_BREAK_STYLE}>
        <div style={NO_BREAK_STYLE}>
          <StatCard label="Total Backups" value={String(backups.length)} variant="bordered" size="sm" />
        </div>
        <div style={NO_BREAK_STYLE}>
          <StatCard label="Successful" value={String(successCount)} variant="bordered" size="sm" />
        </div>
        <div style={NO_BREAK_STYLE}>
          <StatCard label="Failed" value={String(failedCount)} variant="bordered" size="sm" />
        </div>
      </div>
      <div className="mb-[4mm]">
        <BackupActivityCalendar entries={calendarEntries} />
      </div>
      {failedRows.length > 0 && (
        <div className="mb-[4mm]">
          <h3 className="text-[11pt] font-semibold text-slate-800 mb-[2mm]">Failed Backups</h3>
          <ListTable
            rows={failedRows.map((backup) => ({
              subject: backup.database,
              subtitle: backup.size || 'Size unavailable',
              date: backup.date,
              status: backup.status,
              sourceLabel: `Source: ${backup.source || '-'}`,
            }))}
            subject="subject"
            subtitle="subtitle"
            date="date"
            dateFormat="long"
            primaryTags={['status']}
            keys={['sourceLabel']}
            tagMapping={BACKUP_TAG_MAPPING}
            size="xs"
            density="compact"
            wrap
            cellClassName="text-[8pt]"
          />
        </div>
      )}
      {restores.length > 0 && (
        <div>
          <h3 className="text-[11pt] font-semibold text-slate-800 mb-[2mm]">Restore Jobs</h3>
          <ListTable
            rows={restores.map((restore) => ({
              subject: restore.database,
              subtitle: `Completed ${formatDateTime(restore.completedAt)}`,
              date: restore.date,
              status: restore.status,
              sourceLabel: `Source: ${restore.source || '-'}`,
            }))}
            subject="subject"
            subtitle="subtitle"
            date="date"
            dateFormat="long"
            primaryTags={['status']}
            keys={['sourceLabel']}
            tagMapping={BACKUP_TAG_MAPPING}
            size="xs"
            density="compact"
            wrap
            cellClassName="text-[8pt]"
          />
        </div>
      )}
    </Section>
  );
}
