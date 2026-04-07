import React from 'react';
import { StatCard } from '@flanksource/facet';
import type { ApplicationChange } from '../types.ts';
import ConfigLink from './ConfigLink.tsx';
import { IdentityIcon } from './rbac-visual.tsx';
import { formatDateTime } from './utils.ts';
import { filterRBACChanges, groupRBACChanges, type RBACChangeRow } from './change-section-utils.ts';

interface Props {
  changes: ApplicationChange[];
}

const COUNT_VALUE_CLASS = 'text-[16pt] leading-[18pt]';
const ACTION_BADGE_CLASSES: Record<'Granted' | 'Revoked', string> = {
  Granted: 'text-green-700 bg-green-50 border-green-200',
  Revoked: 'text-red-700 bg-red-50 border-red-200',
};

function PermissionRow({ row }: { row: RBACChangeRow }) {
  const changedBy = row.changedBy !== '-' ? row.changedBy : row.source;
  const roleLabel = row.role || 'Access';
  const identityRoleSource = row.subjectKind === 'group' ? `group:${row.subject}` : undefined;

  return (
    <div className="py-[1mm] border-b border-gray-100 last:border-b-0">
      <div className="grid grid-cols-[1fr_auto] gap-x-[2mm] gap-y-[0.5mm] items-start">
        <div className="flex flex-wrap items-center gap-[0.8mm] min-w-0">
          <span className={`text-[6.5pt] leading-none px-[1mm] py-[0.35mm] rounded border font-semibold ${ACTION_BADGE_CLASSES[row.action]}`}>
            {row.action}
          </span>
          <span className="text-[7.5pt] font-semibold text-slate-900">{roleLabel}</span>
          <span className="text-[7pt] text-gray-500">to</span>
          <span className="inline-flex items-center gap-[0.8mm] min-w-0">
            <IdentityIcon userId={row.subject} roleSource={identityRoleSource} size={9} />
            <span className="text-[7.5pt] font-medium text-slate-800 break-all">{row.subject}</span>
          </span>
          {row.viaGroup && (
            <span className="text-[6.5pt] text-gray-500">via {row.viaGroup}</span>
          )}
          <span className="text-[6.5pt] font-mono text-gray-400 whitespace-nowrap">
            {formatDateTime(row.date)}
          </span>
        </div>
        <div className="text-[6.5pt] text-gray-500 whitespace-nowrap text-right">
          Changed by {changedBy}
        </div>
      </div>
      {row.notes && (
        <div className="mt-[0.5mm] ml-[7.5mm] text-[6.5pt] text-gray-500 leading-snug">
          {row.notes}
        </div>
      )}
    </div>
  );
}

export default function RBACChanges({ changes }: Props) {
  const relevant = filterRBACChanges(changes);
  if (!relevant.length) {
    return null;
  }

  const groups = groupRBACChanges(relevant);
  const grantedCount = groups.reduce((total, group) => total + group.rows.filter((row) => row.action === 'Granted').length, 0);
  const revokedCount = groups.reduce((total, group) => total + group.rows.filter((row) => row.action === 'Revoked').length, 0);
  const netCount = grantedCount - revokedCount;
  const netColor = netCount > 0 ? 'orange' : netCount < 0 ? 'green' : 'gray';

  return (
    <>
      <div className="grid grid-cols-3 gap-[3mm] mb-[3mm]">
        <StatCard
          label="Granted"
          value={String(grantedCount)}
          sublabel="Access granted"
          variant="summary"
          size="sm"
          color={grantedCount > 0 ? 'orange' : 'gray'}
          valueClassName={COUNT_VALUE_CLASS}
        />
        <StatCard
          label="Revoked"
          value={String(revokedCount)}
          sublabel="Access revoked"
          variant="summary"
          size="sm"
          color={revokedCount > 0 ? 'green' : 'gray'}
          valueClassName={COUNT_VALUE_CLASS}
        />
        <StatCard
          label="Net"
          value={netCount > 0 ? `+${netCount}` : String(netCount)}
          sublabel="Granted minus revoked"
          variant="summary"
          size="sm"
          color={netColor}
          valueClassName={COUNT_VALUE_CLASS}
        />
      </div>
      <div className="flex flex-col gap-[3mm]">
        {groups.map((group) => (
          <div key={group.key}>
            <div className="flex items-center gap-[1.2mm] mb-[1.2mm]">
              <div className="text-[8pt] font-semibold text-slate-900">
                <ConfigLink config={{ name: group.configName, type: group.configType }} />
              </div>
              {group.configType && (
                <span className="text-[6.5pt] text-gray-500">
                  {group.configType}
                </span>
              )}
              <span className="text-[6.5pt] text-gray-400">
                {group.rows.length} change{group.rows.length === 1 ? '' : 's'}
              </span>
            </div>
            <div className="ml-[4mm] border-l border-gray-200 pl-[2.5mm]">
              {group.rows.map((row) => (
                <PermissionRow key={row.id} row={row} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </>
  );
}
