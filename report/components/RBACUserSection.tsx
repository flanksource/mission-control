import React from 'react';
import { CompactTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { RBACUserReport, RBACUserResource } from '../rbac-types.ts';
import { ConfigTypeIcon } from './configTypeIcon.tsx';

function fmtDate(iso: string): string {
  const d = new Date(iso);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

function age(iso?: string | null): string {
  if (!iso) return 'Never';
  const days = Math.floor((Date.now() - new Date(iso).getTime()) / 86400000);
  if (days < 1) return '<1d';
  if (days < 30) return `${days}d`;
  if (days < 365) return `${Math.floor(days / 30)}mo`;
  return `${Math.floor(days / 365)}y ${Math.floor((days % 365) / 30)}mo`;
}

function ReviewAge({ r }: { r: RBACUserResource }) {
  const text = age(r.lastReviewedAt);
  if (r.isReviewOverdue && text !== 'Never') {
    return <span style={{ color: '#EA580C', fontWeight: 600 }}>{text}</span>;
  }
  if (text === 'Never') {
    return <span style={{ color: '#DC2626', fontWeight: 600 }}>Never</span>;
  }
  return <>{text}</>;
}

function roleColumn(r: RBACUserResource): string {
  const parts = [r.role];
  if (r.roleSource && r.roleSource !== 'direct') {
    parts.push(`via ${r.roleSource}`);
  }
  return parts.join(' ');
}

interface Props {
  user: RBACUserReport;
}

function groupByConfigType(resources: RBACUserResource[]): Map<string, RBACUserResource[]> {
  const groups = new Map<string, RBACUserResource[]>();
  for (const r of resources) {
    const list = groups.get(r.configType) || [];
    list.push(r);
    groups.set(r.configType, list);
  }
  return groups;
}

export default function RBACUserSection({ user }: Props) {
  const lastSignIn = user.lastSignedInAt ? age(user.lastSignedInAt) : 'Never';
  const grouped = groupByConfigType(user.resources);

  const title = (
    <span className="inline-flex items-center gap-[1mm]">
      <Icon name="person" size={14} />
      {user.userName}
      <span className="text-[7pt] font-normal text-gray-500 ml-[0.5mm]">
        ({user.email})
      </span>
    </span>
  );

  return (
    <div>
      <div className="text-[10pt] font-bold text-slate-800 border-b border-gray-300 pb-[0.5mm] mb-[0.5mm]">
        {title}
      </div>
      <div className="flex flex-wrap items-baseline gap-x-[3mm] text-[7pt] text-gray-500 mb-[0.5mm]">
        <span>
          <span className="font-medium text-gray-400">Source: </span>
          {user.sourceSystem}
        </span>
        <span className="border-l border-gray-300 pl-[3mm]">
          <span className="font-medium text-gray-400">Last Sign In: </span>
          {lastSignIn}
        </span>
        <span className="border-l border-gray-300 pl-[3mm]">
          <span className="font-medium text-gray-400">Resources: </span>
          {user.resources.length}
        </span>
      </div>
      {[...grouped.entries()].map(([configType, resources]) => {
        const rows = resources.map((r) => [
          <span key={r.configId} className="inline-flex items-center gap-[0.5mm]">
            <ConfigTypeIcon configType={r.configType} size={10} />
            {r.configName}
          </span>,
          roleColumn(r),
          fmtDate(r.createdAt),
          age(r.lastSignedInAt),
          <ReviewAge key={`${r.configId}-review`} r={r} />,
        ]);
        return (
          <div key={configType} className="mt-[0.5mm]">
            <div className="text-[7pt] font-semibold text-gray-600">
              <span className="inline-flex items-center gap-[0.5mm]">
                <ConfigTypeIcon configType={configType} size={10} />
                {configType}
                <span className="font-normal text-gray-400 ml-[0.5mm]">({resources.length})</span>
              </span>
            </div>
            <CompactTable
              variant="reference"
              columns={['Resource', 'Role', 'Created', 'Last Sign In', 'Last Review']}
              data={rows}
            />
          </div>
        );
      })}
    </div>
  );
}
