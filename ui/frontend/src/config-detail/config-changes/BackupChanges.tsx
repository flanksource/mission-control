import { StatCard } from './facet-components.tsx';
import type { ApplicationChange } from './types.ts';
import { formatDateTime } from './utils.ts';
import BackupActivityCalendar from './BackupActivityCalendar.tsx';
import {
  filterBackupChanges,
  getBackupCalendarStatus,
  isRestoreChange,
  toBackupCalendarEntries,
} from './change-section-utils.ts';

interface Props {
  changes: ApplicationChange[];
}

const COUNT_VALUE_CLASS = 'text-[16pt] leading-[18pt]';
const TIMESTAMP_VALUE_CLASS = 'text-[8pt] leading-[10pt]';
const NO_BREAK_STYLE = { pageBreakInside: 'avoid' as const, breakInside: 'avoid' as const };
const STAT_CARD_CLASS = 'flex-[1_1_28mm] min-w-[22mm] max-w-[44mm]';

function byDateDesc(a: ApplicationChange, b: ApplicationChange): number {
  return new Date(b.date).getTime() - new Date(a.date).getTime();
}

export default function BackupChanges({ changes }: Props) {
  const relevant = [...filterBackupChanges(changes)].sort(byDateDesc);
  if (!relevant.length) {
    return null;
  }

  const backupEvents = relevant.filter((change) => !isRestoreChange(change));
  const restoreEvents = relevant.filter(isRestoreChange);
  const completed = backupEvents.filter((change) => getBackupCalendarStatus(change) === 'success');
  const failed = backupEvents.filter((change) => getBackupCalendarStatus(change) === 'failed');
  const inProgress = backupEvents.filter((change) => getBackupCalendarStatus(change) === 'warning');
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
        <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
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
        <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
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
        <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
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
          <div className={STAT_CARD_CLASS} style={NO_BREAK_STYLE}>
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
        <div style={NO_BREAK_STYLE}>
          <BackupActivityCalendar entries={toBackupCalendarEntries(backupEvents)} />
        </div>
      )}
    </>
  );
}
