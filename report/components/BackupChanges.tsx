import React from 'react';
import { StatCard, ListTable } from '@flanksource/facet';
import type { ApplicationChange } from '../types.ts';
import { formatDateTime } from './utils.ts';
import BackupActivityCalendar from './BackupActivityCalendar.tsx';
import {
  filterBackupChanges,
  getBackupCalendarStatus,
  getChangeActor,
  isRestoreChange,
  toBackupCalendarEntries,
} from './change-section-utils.ts';

interface Props {
  changes: ApplicationChange[];
}

const COUNT_VALUE_CLASS = 'text-[16pt] leading-[18pt]';
const TIMESTAMP_VALUE_CLASS = 'text-[8pt] leading-[10pt]';
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const BACKUP_TAG_MAPPING = (key: string, value: unknown): string => {
  if (key !== 'state' && key !== 'type') {
    return '';
  }

  const normalized = String(value).toLowerCase();
  if (normalized.includes('fail')) {
    return 'text-red-700 bg-red-50 border-red-200';
  }
  if (normalized.includes('running') || normalized.includes('progress') || normalized.includes('started') || normalized.includes('queued')) {
    return 'text-orange-700 bg-orange-50 border-orange-200';
  }
  if (normalized.includes('complete') || normalized.includes('success')) {
    return 'text-green-700 bg-green-50 border-green-200';
  }
  if (normalized.includes('restore')) {
    return 'text-blue-700 bg-blue-50 border-blue-200';
  }
  return 'text-gray-600 bg-gray-50 border-gray-200';
};

function attentionLabel(change: ApplicationChange): string {
  const status = getBackupCalendarStatus(change);
  if (status === 'failed') {
    return 'Failed';
  }
  if (status === 'warning') {
    return 'In Progress';
  }
  return change.changeType ?? 'Backup';
}

export default function BackupChanges({ changes }: Props) {
  const relevant = filterBackupChanges(changes);
  if (!relevant.length) {
    return null;
  }

  const backupEvents = relevant.filter((change) => !isRestoreChange(change));
  const restoreEvents = relevant.filter(isRestoreChange);
  const completed = backupEvents.filter((change) => getBackupCalendarStatus(change) === 'success');
  const failed = backupEvents.filter((change) => getBackupCalendarStatus(change) === 'failed');
  const inProgress = backupEvents.filter((change) => getBackupCalendarStatus(change) === 'warning');
  const attentionEvents = [...failed, ...inProgress];
  const latestSuccessful = completed.reduce<ApplicationChange | null>((latest, change) => {
    if (!latest) return change;
    return new Date(change.date).getTime() > new Date(latest.date).getTime() ? change : latest;
  }, null);
  const latestSuccessfulValue = latestSuccessful ? formatDateTime(latestSuccessful.date) : 'None';
  const latestSuccessfulColor = latestSuccessful
    ? 'green'
    : backupEvents.length > 0
      ? 'red'
      : 'gray';

  return (
    <>
      <div className="flex flex-wrap items-stretch gap-[3mm] mb-[4mm]" style={NO_BREAK_STYLE}>
        <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Failed Backups"
            value={String(failed.length)}
            sublabel={failed.length > 0 ? 'Needs attention' : 'No failures'}
            variant="summary"
            size="sm"
            color={failed.length > 0 ? 'red' : 'gray'}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Running Backups"
            value={String(inProgress.length)}
            sublabel="Started, running, or queued"
            variant="summary"
            size="sm"
            color={inProgress.length > 0 ? 'orange' : 'gray'}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
          <StatCard
            label="Latest Successful Backup"
            value={latestSuccessfulValue}
            sublabel={`${completed.length} completed`}
            variant="summary"
            size="sm"
            color={latestSuccessfulColor}
            shrink
            valueClassName={TIMESTAMP_VALUE_CLASS}
          />
        </div>
        {restoreEvents.length > 0 && (
          <div className="flex-1 min-w-[28mm]" style={NO_BREAK_STYLE}>
            <StatCard
              label="Restore Events"
              value={String(restoreEvents.length)}
              sublabel="Recovery activity"
              variant="summary"
              size="sm"
              color="blue"
              shrink
              valueClassName={COUNT_VALUE_CLASS}
            />
          </div>
        )}
      </div>

      {backupEvents.length > 0 && (
        <div className="mb-[4mm]" style={NO_BREAK_STYLE}>
          <BackupActivityCalendar entries={toBackupCalendarEntries(backupEvents)} />
        </div>
      )}

      {attentionEvents.length > 0 && (
        <div className="mb-[4mm]">
          <h3 className="text-[11pt] font-normal text-slate-800 mb-[2mm]">Exceptions & Running Jobs</h3>
          <ListTable
            rows={attentionEvents.map((change) => ({
              date: change.date,
              subject: change.description,
              subtitle: `Changed by ${getChangeActor(change)}`,
              state: attentionLabel(change),
              sourceLabel: `Source: ${change.source || '-'}`,
            }))}
            subject="subject"
            subtitle="subtitle"
            date="date"
            dateFormat="long"
            primaryTags={['state']}
            keys={['sourceLabel']}
            tagMapping={BACKUP_TAG_MAPPING}
            size="xs"
            density="compact"
            wrap
            cellClassName="text-[8pt]"
          />
        </div>
      )}

      {restoreEvents.length > 0 && (
        <div className="mb-[4mm]">
          <h3 className="text-[11pt] font-normal text-slate-800 mb-[2mm]">Restore Jobs</h3>
          <ListTable
            rows={restoreEvents.map((change) => ({
              date: change.date,
              subject: change.description,
              subtitle: `Changed by ${getChangeActor(change)}`,
              type: change.changeType ?? 'Restore',
              sourceLabel: `Source: ${change.source || '-'}`,
            }))}
            subject="subject"
            subtitle="subtitle"
            date="date"
            dateFormat="long"
            primaryTags={['type']}
            keys={['sourceLabel']}
            tagMapping={BACKUP_TAG_MAPPING}
            size="xs"
            density="compact"
            wrap
            cellClassName="text-[8pt]"
          />
        </div>
      )}

      <div>
        <h3 className="text-[11pt] font-normal text-slate-800 mb-[2mm]">Event Stream</h3>
        <ListTable
          rows={relevant.map((change) => ({
            id: change.id,
            date: change.date,
            subject: change.description,
            subtitle: `Changed by ${getChangeActor(change)}`,
            type: change.changeType ?? 'Event',
            sourceLabel: `Source: ${change.source || '-'}`,
          }))}
          subject="subject"
          subtitle="subtitle"
          date="date"
          dateFormat="long"
          primaryTags={['type']}
          keys={['sourceLabel']}
          tagMapping={BACKUP_TAG_MAPPING}
          groups={[{ by: 'date' }]}
          size="xs"
          density="compact"
          wrap
          cellClassName="text-[8pt]"
        />
      </div>
    </>
  );
}
