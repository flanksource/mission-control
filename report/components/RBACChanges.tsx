import React from 'react';
import { Badge, ListTable, StatCard } from '@flanksource/facet';
import type { ApplicationChange } from '../types.ts';
import { ConfigTypeIcon } from './configTypeIcon.tsx';
import { IdentityIcon } from './rbac-visual.tsx';
import { filterRBACChanges, groupRBACChanges, type RBACChangeRow } from './change-section-utils.ts';

interface Props {
  changes: ApplicationChange[];
}

const COUNT_VALUE_CLASS = 'text-[16pt] leading-[18pt]';
const ACTION_BADGE_COLORS: Record<'Granted' | 'Revoked', { color: string; textColor: string; borderColor: string }> = {
  Granted: {
    color: 'bg-green-50',
    textColor: 'text-green-700',
    borderColor: 'border-green-200',
  },
  Revoked: {
    color: 'bg-red-50',
    textColor: 'text-red-700',
    borderColor: 'border-red-200',
  },
};

export default function RBACChanges({ changes }: Props) {
  const relevant = filterRBACChanges(changes);
  if (!relevant.length) {
    return null;
  }

  const groups = groupRBACChanges(relevant);
  const rows = groups
    .flatMap((group) => group.rows.map((row) => ({ ...row, configName: group.configName, configType: group.configType })))
    .sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());
  const grantedCount = groups.reduce((total, group) => total + group.rows.filter((row) => row.action === 'Granted').length, 0);
  const revokedCount = groups.reduce((total, group) => total + group.rows.filter((row) => row.action === 'Revoked').length, 0);
  const netCount = grantedCount - revokedCount;
  const netColor = netCount > 0 ? 'orange' : netCount < 0 ? 'green' : 'gray';

  return (
    <>
      <div className="flex flex-wrap items-stretch gap-[3mm] mb-[3mm]">
        <div className="flex-1 min-w-[28mm]">
          <StatCard
            label="Granted"
            value={String(grantedCount)}
            sublabel="Access granted"
            variant="summary"
            size="sm"
            color={grantedCount > 0 ? 'orange' : 'gray'}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[28mm]">
          <StatCard
            label="Revoked"
            value={String(revokedCount)}
            sublabel="Access revoked"
            variant="summary"
            size="sm"
            color={revokedCount > 0 ? 'green' : 'gray'}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
        <div className="flex-1 min-w-[28mm]">
          <StatCard
            label="Net"
            value={netCount > 0 ? `+${netCount}` : String(netCount)}
            sublabel="Granted minus revoked"
            variant="summary"
            size="sm"
            color={netColor}
            shrink
            valueClassName={COUNT_VALUE_CLASS}
          />
        </div>
      </div>
      <ListTable
        rows={rows.map((row: RBACChangeRow & { configName: string; configType?: string }) => {
          const identityRoleSource = row.subjectKind === 'group' ? `group:${row.subject}` : undefined;
          return {
            id: row.id,
            date: row.date,
            configName: row.configName,
            configType: row.configType,
            subject: (
              <span className="inline-flex items-center flex-wrap gap-[0.8mm] align-middle">
                <Badge
                  variant="custom"
                  size="xs"
                  shape="rounded"
                  label={row.action}
                  color={ACTION_BADGE_COLORS[row.action].color}
                  textColor={ACTION_BADGE_COLORS[row.action].textColor}
                  borderColor={ACTION_BADGE_COLORS[row.action].borderColor}
                  className="font-semibold"
                />
                <span className="text-[7.5pt] leading-none font-semibold text-slate-900">{row.role || 'Access'}</span>
                <span className="text-[7pt] leading-none text-gray-500">to</span>
                <span className="inline-flex items-center gap-[0.8mm] min-w-0">
                  <IdentityIcon userId={row.subject} roleSource={identityRoleSource} size={9} />
                  <span className="text-[7.5pt] leading-none font-medium text-slate-800 break-all">{row.subject}</span>
                </span>
              </span>
            ),
            subtitle: row.viaGroup ? `via ${row.viaGroup}` : undefined,
            changedByLabel: `Changed by ${row.changedBy !== '-' ? row.changedBy : row.source}`,
            notes: row.notes,
          };
        })}
        subject="subject"
        subtitle="subtitle"
        body="notes"
        date="date"
        keys={['changedByLabel']}
        groups={[{ by: 'date' }, { by: 'field', field: 'configName' }]}
        iconRenderer={(_value, context) => {
          if (context.kind !== 'group' || context.field !== 'configName') {
            return null;
          }

          const configType = context.group?.sampleRow?.configType;
          return typeof configType === 'string' && configType
            ? <ConfigTypeIcon configType={configType} size={10} />
            : null;
        }}
        size="xs"
        density="compact"
        wrap
        cellClassName="text-[8pt]"
      />
    </>
  );
}
