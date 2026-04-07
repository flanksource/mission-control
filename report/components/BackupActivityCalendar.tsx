import React from 'react';
import type { BackupCalendarEntry, BackupCalendarStatus } from './change-section-utils.ts';

interface Props {
  entries: BackupCalendarEntry[];
}

const DAY_HEADERS = ['Su', 'Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa'];

const CELL_CLASSES: Record<BackupCalendarStatus | 'none', string> = {
  success: 'bg-green-50 border border-green-600',
  failed: 'bg-red-50 border border-red-600',
  warning: 'bg-amber-50 border border-amber-500',
  none: 'bg-gray-100 border border-gray-200',
};

const LABEL_CLASSES: Record<BackupCalendarStatus, string> = {
  success: 'text-green-700',
  failed: 'text-red-600',
  warning: 'text-amber-700',
};

const STATUS_RANK: Record<BackupCalendarStatus, number> = {
  success: 1,
  warning: 2,
  failed: 3,
};

interface AggregatedEntry {
  date: string;
  status: BackupCalendarStatus;
  label?: string;
  count: number;
}

function aggregateEntries(entries: BackupCalendarEntry[]): AggregatedEntry[] {
  const byDay = new Map<string, AggregatedEntry>();

  for (const entry of entries) {
    const key = entry.date.slice(0, 10);
    const current = byDay.get(key);

    if (!current) {
      byDay.set(key, {
        date: entry.date,
        status: entry.status,
        label: entry.label,
        count: 1,
      });
      continue;
    }

    current.count += 1;
    if (new Date(entry.date).getTime() >= new Date(current.date).getTime()) {
      current.date = entry.date;
      current.label = entry.label ?? current.label;
    }
    if (STATUS_RANK[entry.status] >= STATUS_RANK[current.status]) {
      current.status = entry.status;
    }
  }

  return [...byDay.values()];
}

export default function BackupActivityCalendar({ entries }: Props) {
  if (!entries.length) {
    return null;
  }

  const aggregated = aggregateEntries(entries);
  const referenceDate = new Date(aggregated.reduce((latest, entry) => (
    new Date(entry.date).getTime() > new Date(latest.date).getTime() ? entry : latest
  )).date);

  const year = referenceDate.getFullYear();
  const month = referenceDate.getMonth();
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const firstDow = new Date(year, month, 1).getDay();

  const dateMap: Record<string, AggregatedEntry> = {};
  for (const entry of aggregated) {
    dateMap[entry.date.slice(0, 10)] = entry;
  }

  const monthLabel = referenceDate.toLocaleString('default', { month: 'long', year: 'numeric' });
  const cells: (number | null)[] = [
    ...Array(firstDow).fill(null),
    ...Array.from({ length: daysInMonth }, (_, index) => index + 1),
  ];

  return (
    <div>
      <p className="text-[9pt] font-semibold text-slate-700 mb-[2mm]">{monthLabel}</p>
      <div className="grid grid-cols-7 gap-[1mm]">
        {DAY_HEADERS.map((day) => (
          <div key={day} className="text-center text-[8pt] text-gray-500 pb-[1mm]">{day}</div>
        ))}
        {cells.map((day, index) => {
          if (day === null) {
            return <div key={`empty-${index}`} />;
          }

          const key = `${year}-${String(month + 1).padStart(2, '0')}-${String(day).padStart(2, '0')}`;
          const entry = dateMap[key];
          const cellClass = entry ? CELL_CLASSES[entry.status] : CELL_CLASSES.none;
          const label = entry ? (entry.count > 1 ? `×${entry.count}` : entry.label ?? entry.status) : '';

          return (
            <div
              key={key}
              className={`${cellClass} rounded-[1mm] p-[1mm] min-h-[10mm] flex flex-col justify-between`}
            >
              <span className="text-[7pt] text-gray-700 font-semibold">{day}</span>
              {entry && (
                <span className={`text-[6pt] ${LABEL_CLASSES[entry.status]}`}>
                  {label}
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
          <span className="inline-block w-[3mm] h-[3mm] bg-amber-50 border border-amber-500 rounded-[0.5mm]" />
          In Progress
        </span>
        <span className="flex items-center gap-1">
          <span className="inline-block w-[3mm] h-[3mm] bg-gray-100 border border-gray-200 rounded-[0.5mm]" />
          No backup
        </span>
      </div>
    </div>
  );
}
