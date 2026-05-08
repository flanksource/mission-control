import React from 'react';
import { Section, StatCard } from '@flanksource/facet';
import type { ApplicationBackup } from '../types.ts';
import { humanizeSize } from './utils.ts';
import BackupActivityCalendar from './BackupActivityCalendar.tsx';
import type { BackupCalendarStatus } from './change-section-utils.ts';

interface Props {
  backups: ApplicationBackup[];
}

const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };

export default function BackupsSection({ backups }: Props) {
  const isFailed = (status: string) => String(status).toLowerCase().includes('fail');
  const successCount = backups.filter((b) => b.status === 'success').length;
  const failedCount = backups.filter((b) => isFailed(b.status)).length;
  const calendarEntries = backups.map((backup) => ({
    date: backup.date,
    status: (backup.status === 'success' ? 'success' : isFailed(backup.status) ? 'failed' : 'warning') as BackupCalendarStatus,
    label: humanizeSize(backup.size) || backup.size || undefined,
  }));

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
      <div>
        <BackupActivityCalendar entries={calendarEntries} />
      </div>
    </Section>
  );
}
