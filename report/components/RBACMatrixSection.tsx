import React from 'react';
import { Icon } from '@flanksource/icons/icon';
import type { RBACResource, RBACUserRole } from '../rbac-types.ts';
import { Badge, MatrixTable, Dot } from '@flanksource/facet';
import { ACCESS_COLORS, STALE_COLORS, ReviewOverdueBadge, ReviewOverdueLegendSwatch, IdentityIcon } from './rbac-visual.tsx';

interface Props {
  resource: RBACResource;
}

interface UserRow {
  userId: string;
  userName: string;
  email: string;
  roles: Map<string, RBACUserRole>;
}

function buildMatrix(resource: RBACResource) {
  const roleSet = new Set<string>();
  const userMap = new Map<string, UserRow>();

  for (const u of resource.users || []) {
    roleSet.add(u.role);
    let row = userMap.get(u.userId);
    if (!row) {
      row = { userId: u.userId, userName: u.userName, email: u.email, roles: new Map() };
      userMap.set(u.userId, row);
    }
    row.roles.set(u.role, u);
  }

  const roles = [...roleSet].sort();
  const users = [...userMap.values()].sort((a, b) => a.userName.localeCompare(b.userName));
  return { roles, users };
}

function loginAgeDays(lastSignedInAt?: string | null): number | null {
  if (!lastSignedInAt) return null;
  return Math.floor((Date.now() - new Date(lastSignedInAt).getTime()) / 86400000);
}

function staleColor(lastSignedInAt?: string | null): string | null {
  const days = loginAgeDays(lastSignedInAt);
  if (days === null || days > 30) return STALE_COLORS.stale30d;
  if (days > 7) return STALE_COLORS.stale7d;
  return null;
}

function Indicator({ entry }: { entry?: RBACUserRole }) {
  if (!entry) return null;
  const indirect = entry.roleSource.startsWith('group:');
  const color = indirect ? ACCESS_COLORS.group : ACCESS_COLORS.direct;
  return (
    <div style={{ position: 'relative', display: 'flex', justifyContent: 'center', alignItems: 'center', width: '100%', height: '100%' }}>
      <Dot color={color} outline={indirect} />
      {entry.isReviewOverdue && <ReviewOverdueBadge />}
    </div>
  );
}

export function MatrixLegend() {
  return (
    <div className="flex flex-wrap items-center gap-[4mm] text-gray-500">
      <span className="font-semibold">Legend:</span>
      <span className="inline-flex items-center gap-[1mm]">
        <Dot color={ACCESS_COLORS.direct} /> Direct
      </span>
      <span className="inline-flex items-center gap-[1mm]">
        <Dot color={ACCESS_COLORS.group} outline /> Indirect
      </span>
      <span className="inline-flex items-center gap-[1mm]">
        <span style={{ display: 'inline-block', width: '2mm', height: '3mm', borderLeft: `2px solid ${STALE_COLORS.stale7d}` }} />
        Last login &gt; 7d
      </span>
      <span className="inline-flex items-center gap-[1mm]">
        <span style={{ display: 'inline-block', width: '2mm', height: '3mm', borderLeft: `2px solid ${STALE_COLORS.stale30d}` }} />
        Last login &gt; 30d
      </span>
      <ReviewOverdueLegendSwatch />
    </div>
  );
}

export default function RBACMatrixSection({ resource }: Props) {
  const { roles, users } = buildMatrix(resource);
  if (users.length === 0) return null;

  const matrixRows = users.map((user) => {
    const worstStale = [...user.roles.values()].reduce<string | null>((worst, r) => {
      const c = staleColor(r.lastSignedInAt);
      if (c === STALE_COLORS.stale30d) return c;
      return worst ?? c;
    }, null);
    const firstRole = [...user.roles.values()][0];
    const roleSource = firstRole?.roleSource;
    return {
      label: (
        <span style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '1mm',
          fontWeight: 500,
          borderLeft: `2px solid ${worstStale || 'transparent'}`,
          paddingLeft: '1mm',
        }}>
          <IdentityIcon userId={user.userId} roleSource={roleSource} size={10} />
          {user.userName}
        </span>
      ),
      cells: roles.map((role) => <Indicator key={role} entry={user.roles.get(role)} />),
    };
  });

  const tags = { ...(resource.tags || {}), ...(resource.labels || {}) };
  const pathParts = resource.path?.split('.').filter(Boolean) ?? [];
  const corner = (
    <div>
      {pathParts.length > 0 && (
        <div className="text-[5pt] text-gray-400 mb-[0.5mm]">
          {pathParts.map((p, i) => (
            <span key={i}>
              {i > 0 && <span className="mx-[0.5mm]">/</span>}
              {p}
            </span>
          ))}
        </div>
      )}
      <div className="flex items-center gap-[1mm] text-[8pt] font-semibold text-slate-900">
        <Icon name={resource.configType} size={14} />
        {resource.configName}
      </div>
      {Object.keys(tags).length > 0 && (
        <div className="flex flex-wrap gap-[0.5mm] mt-[1mm]">
          {Object.entries(tags).map(([k, v]) => (
            <Badge
              key={k}
              variant="label"
              size="xs"
              shape="rounded"
              label={k}
              value={v || '-'}
              color="bg-blue-50"
              textColor="text-slate-600"
              className="bg-white"
            />
          ))}
        </div>
      )}
      <div className="mt-[1.5mm]">
        <MatrixLegend />
      </div>
    </div>
  );

  return (
    <div className="mb-[4mm]">
      <MatrixTable columns={roles} rows={matrixRows} columnWidth={10} headerHeight={20} cornerContent={corner} />
    </div>
  );
}
