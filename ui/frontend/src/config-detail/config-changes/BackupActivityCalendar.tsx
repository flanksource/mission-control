import { Icon as ClickyIcon } from '@flanksource/clicky-ui';
import type {
  BackupCalendarEntry,
  BackupCalendarStatus,
  BackupTrigger,
  BackupType,
} from './change-section-utils.ts';
import { humanizeSize } from './utils.ts';

interface Props {
  entries: BackupCalendarEntry[];
}

const DAY_HEADERS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

const CELL_CLASSES: Record<BackupCalendarStatus | 'none', string> = {
  success: 'bg-green-50 border border-green-500',
  failed: 'bg-red-50 border border-red-500',
  warning: 'bg-amber-50 border border-amber-400',
  none: 'bg-slate-100 border border-slate-200',
};

const DOT_CLASSES: Record<BackupCalendarStatus, string> = {
  success: 'bg-green-600',
  failed: 'bg-red-600',
  warning: 'bg-amber-500',
};

const STATUS_RANK: Record<BackupCalendarStatus, number> = {
  success: 1,
  warning: 2,
  failed: 3,
};

const TYPE_ICON: Record<BackupType, string> = {
  full: 'ph:database-thin',
  incremental: 'ph:stack-plus-thin',
  differential: 'ph:arrows-clockwise-thin',
  unknown: 'ph:database-thin',
};

const TYPE_LABEL: Record<BackupType, string> = {
  full: 'Full',
  incremental: 'Incremental',
  differential: 'Differential',
  unknown: 'Unknown',
};

const TRIGGER_ICON: Record<BackupTrigger, string> = {
  scheduled: 'ph:calendar-dots-thin',
  manual: 'ph:user-thin',
  automated: 'ph:robot-thin',
};

const TRIGGER_LABEL: Record<BackupTrigger, string> = {
  scheduled: 'Scheduled',
  manual: 'Manual',
  automated: 'Automated',
};

interface DayGroup {
  date: string;
  worstStatus: BackupCalendarStatus;
  entries: BackupCalendarEntry[];
}

function groupEntriesByDay(entries: BackupCalendarEntry[]): Map<string, DayGroup> {
  const byDay = new Map<string, DayGroup>();

  for (const entry of entries) {
    const key = entry.date.slice(0, 10);
    const current = byDay.get(key);

    if (!current) {
      byDay.set(key, {
        date: entry.date,
        worstStatus: entry.status,
        entries: [entry],
      });
      continue;
    }

    current.entries.push(entry);
    if (new Date(entry.date).getTime() >= new Date(current.date).getTime()) {
      current.date = entry.date;
    }
    if (STATUS_RANK[entry.status] >= STATUS_RANK[current.worstStatus]) {
      current.worstStatus = entry.status;
    }
  }

  for (const group of byDay.values()) {
    group.entries.sort((a, b) => new Date(a.date).getTime() - new Date(b.date).getTime());
  }

  return byDay;
}

function EntryRow({ entry }: { entry: BackupCalendarEntry }) {
  const typeIcon = TYPE_ICON[entry.backupType ?? 'unknown'];
  const triggerIcon = entry.trigger ? TRIGGER_ICON[entry.trigger] : undefined;
  const sizeOrDuration = humanizeSize(entry.size) || entry.size || entry.duration;

  return (
    <div className="flex items-center gap-[0.8mm] text-[6.5pt] leading-[8pt] text-slate-700">
      <span className={`inline-block w-[1.3mm] h-[1.3mm] rounded-full shrink-0 ${DOT_CLASSES[entry.status]}`} />
      <ClickyIcon name={typeIcon} width={8} height={8} className="shrink-0 text-slate-600" />
      {triggerIcon && <ClickyIcon name={triggerIcon} width={8} height={8} className="shrink-0 text-slate-500" />}
      {entry.startTime && (
        <span className="font-mono text-slate-600">{entry.startTime}</span>
      )}
      {sizeOrDuration && (
        <span className="text-slate-500 truncate">{sizeOrDuration}</span>
      )}
    </div>
  );
}

function MonthGrid({ year, month, groupsByKey, todayKey }: { year: number; month: number; groupsByKey: Record<string, DayGroup>; todayKey: string }) {
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const firstDow = new Date(year, month, 1).getDay();
  const monthLabel = new Date(Date.UTC(year, month, 1)).toLocaleString('en-US', {
    month: 'long', year: 'numeric', timeZone: 'UTC',
  });
  const cells: (number | null)[] = [
    ...Array(firstDow).fill(null),
    ...Array.from({ length: daysInMonth }, (_, index) => index + 1),
  ];

  return (
    <div style={{ pageBreakInside: 'avoid', breakInside: 'avoid' }}>
      <p className="text-[9pt] font-semibold text-slate-700 mb-[2mm]">{monthLabel}</p>
      <div className="grid grid-cols-7 gap-[1mm]">
        {DAY_HEADERS.map((day) => (
          <div key={day} className="text-center text-[8pt] text-slate-500 pb-[1mm]">{day}</div>
        ))}
        {cells.map((day, index) => {
          if (day === null) {
            return <div key={`empty-${index}`} />;
          }

          const key = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
          if (key > todayKey) {
            return <div key={key} />;
          }

          const group = groupsByKey[key];
          const cellClass = group ? CELL_CLASSES[group.worstStatus] : CELL_CLASSES.none;

          return (
            <div
              key={key}
              className={`${cellClass} rounded-[1mm] p-[1mm] min-h-[12mm] flex flex-col gap-[0.5mm]`}
            >
              <span className="text-[7pt] text-slate-700 font-semibold">{day}</span>
              {group && (
                <div className="flex flex-col gap-[0.5mm]">
                  {group.entries.map((entry, idx) => (
                    <EntryRow key={entry.id ?? `${key}-${idx}`} entry={entry} />
                  ))}
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function Legend() {
  const statusItems: Array<{ status: BackupCalendarStatus | 'none'; label: string }> = [
    { status: 'success', label: 'Success' },
    { status: 'failed', label: 'Failed' },
    { status: 'warning', label: 'In Progress' },
    { status: 'none', label: 'No backup' },
  ];
  const types: BackupType[] = ['full', 'incremental', 'differential', 'unknown'];
  const triggers: BackupTrigger[] = ['scheduled', 'manual', 'automated'];

  return (
    <div className="flex flex-col gap-[1.5mm] text-[8pt] text-slate-600">
      <div className="flex flex-wrap gap-[3mm]">
        {statusItems.map((item) => (
          <span key={item.status} className="flex items-center gap-1">
            <span className={`inline-block w-[3mm] h-[3mm] rounded-[0.5mm] ${CELL_CLASSES[item.status]}`} />
            {item.label}
          </span>
        ))}
      </div>
      <div className="flex flex-wrap gap-[3mm]">
        <span className="text-[7.5pt] text-slate-500 font-semibold uppercase tracking-wide">Type</span>
        {types.map((type) => (
          <span key={type} className="flex items-center gap-1">
            <ClickyIcon name={TYPE_ICON[type]} width={10} height={10} className="text-slate-600" />
            {TYPE_LABEL[type]}
          </span>
        ))}
      </div>
      <div className="flex flex-wrap gap-[3mm]">
        <span className="text-[7.5pt] text-slate-500 font-semibold uppercase tracking-wide">Trigger</span>
        {triggers.map((trigger) => (
          <span key={trigger} className="flex items-center gap-1">
            <ClickyIcon name={TRIGGER_ICON[trigger]} width={10} height={10} className="text-slate-600" />
            {TRIGGER_LABEL[trigger]}
          </span>
        ))}
      </div>
    </div>
  );
}

export default function BackupActivityCalendar({ entries }: Props) {
  if (!entries.length) {
    return null;
  }

  const grouped = groupEntriesByDay(entries);
  const groupsByKey: Record<string, DayGroup> = {};
  const monthSet = new Set<string>();
  for (const [key, group] of grouped) {
    groupsByKey[key] = group;
    monthSet.add(key.slice(0, 7));
  }

  const now = new Date();
  const todayKey = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  const currentMonthKey = todayKey.slice(0, 7);

  const months = [...monthSet]
    .filter((ym) => ym <= currentMonthKey)
    .sort()
    .map((ym) => {
      const [y, m] = ym.split('-');
      return { year: Number(y), month: Number(m) - 1 };
    });

  return (
    <div className="flex flex-col gap-[3mm]" style={{ pageBreakInside: 'avoid', breakInside: 'avoid' }}>
      {months.map(({ year, month }) => (
        <MonthGrid
          key={`${year}-${month}`}
          year={year}
          month={month}
          groupsByKey={groupsByKey}
          todayKey={todayKey}
        />
      ))}
      <Legend />
    </div>
  );
}
