import React from 'react';
import { Icon } from '@flanksource/icons/icon';
import type { RBACResource, RBACUserRole } from '../rbac-types.ts';
import { Badge, MatrixTable, Dot, CompactTable } from '@flanksource/facet';
import { ACCESS_COLORS, STALE_COLORS, ReviewOverdueBadge, ReviewOverdueLegendSwatch, IdentityIcon, groupAnchor, groupNameFromRoleSource, userAnchor } from './rbac-visual.tsx';

interface Props {
  resource: RBACResource;
}

interface UserRow {
  userId: string;
  userName: string;
  email: string;
  kind: 'direct' | 'group' | 'member';
  groupName?: string;
  memberCount?: number;
  roles: Map<string, RBACUserRole>;
}

interface MatrixData {
  allRoles: string[];
  combinedRows: UserRow[];
  directRoles: string[];
  directRows: UserRow[];
  indirectRoles: string[];
  indirectRows: UserRow[];
}

interface RoleColumn {
  role: string;
  label: string;
}

interface SparseEntry {
  row: UserRow;
  entry: RBACUserRole;
}

const NIL_UUID = '00000000-0000-0000-0000-000000000000';
const PT_TO_MM = 0.3528;

function principalKey(u: RBACUserRole): string {
  // Group-only grants share userId=00000000… (no resolved member); fall back to
  // the group name so each group gets its own matrix row.
  return u.userId && u.userId !== NIL_UUID ? u.userId : `group:${u.userName}`;
}

function rowKey(row: UserRow): string {
  return row.kind === 'group' ? `group:${row.userName}` : row.userId;
}

function buildMatrix(resource: RBACResource): MatrixData {
  const roleSet = new Set<string>();
  const directRoleSet = new Set<string>();
  const indirectRoleSet = new Set<string>();
  const directUsers = new Map<string, UserRow>();
  const groups = new Map<string, { header: UserRow; members: Map<string, UserRow> }>();

  const directRow = (u: RBACUserRole) => {
    const key = principalKey(u);
    let row = directUsers.get(key);
    if (!row) {
      row = { userId: u.userId, userName: u.userName, email: u.email, kind: 'direct', roles: new Map() };
      directUsers.set(key, row);
    }
    return row;
  };

  const groupRows = (groupName: string) => {
    let group = groups.get(groupName);
    if (!group) {
      group = {
        header: { userId: NIL_UUID, userName: groupName, email: '', kind: 'group', groupName, roles: new Map() },
        members: new Map(),
      };
      groups.set(groupName, group);
    }
    return group;
  };

  for (const u of resource.users || []) {
    roleSet.add(u.role);

    const groupName = groupNameFromRoleSource(u.roleSource);
    if (!groupName) {
      directRoleSet.add(u.role);
      directRow(u).roles.set(u.role, u);
      continue;
    }

    indirectRoleSet.add(u.role);
    const group = groupRows(groupName);
    group.header.roles.set(u.role, u);

    if (u.userId && u.userId !== NIL_UUID) {
      let member = group.members.get(u.userId);
      if (!member) {
        member = { userId: u.userId, userName: u.userName, email: u.email, kind: 'member', groupName, roles: new Map() };
        group.members.set(u.userId, member);
      }
      member.roles.set(u.role, u);
    }
  }

  const allRoles = [...roleSet].sort();
  const directRoles = [...directRoleSet].sort();
  const indirectRoles = [...indirectRoleSet].sort();
  const directRows: UserRow[] = [...directUsers.values()].sort((a, b) => a.userName.localeCompare(b.userName));
  const indirectRows: UserRow[] = [];
  for (const group of [...groups.values()].sort((a, b) => a.header.userName.localeCompare(b.header.userName))) {
    const members = [...group.members.values()].sort((a, b) => a.userName.localeCompare(b.userName));
    group.header.memberCount = members.length;
    indirectRows.push(group.header, ...members);
  }
  return {
    allRoles,
    combinedRows: [...directRows, ...indirectRows],
    directRoles,
    directRows,
    indirectRoles,
    indirectRows,
  };
}

function roleHeaderFontPt(role: string): number {
  const maxFit = 18;
  return role.length <= maxFit ? 7 : Math.max(4.5, 7 * (maxFit / role.length));
}

function roleHeaderHeightMm(roles: string[]): number {
  if (roles.length === 0) return 20;
  const required = Math.max(
    ...roles.map((role) => (role.length + 2) * roleHeaderFontPt(role) * 0.6 * Math.SQRT1_2 * PT_TO_MM),
  ) + 3;
  return Math.min(42, Math.max(20, Math.ceil(required)));
}

function RoleHeader({ column }: { column: RoleColumn }) {
  return (
    <span
      title={column.role}
      style={{
        display: 'inline-block',
        fontSize: `${roleHeaderFontPt(column.label).toFixed(2)}pt`,
        lineHeight: 1,
      }}
    >
      {column.label}
    </span>
  );
}

function columnsForRoles(roles: string[]): RoleColumn[] {
  return roles.map((role) => ({
    role,
    label: role,
  }));
}

function roleReferences(rows: UserRow[], roles: RoleColumn[]) {
  return roles.map((role) => {
    const externalIds = new Set<string>();
    for (const row of rows) {
      const entry = row.roles.get(role.role);
      if (!entry) continue;
      for (const externalID of entry.roleExternalIds || []) {
        if (externalID) externalIds.add(externalID);
      }
    }
    return { role, externalIds: [...externalIds].sort() };
  });
}

function RoleReferenceTable({ rows, roles }: { rows: UserRow[]; roles: RoleColumn[] }) {
  if (roles.length === 0) return null;
  const tableRows = roleReferences(rows, roles).map(({ role, externalIds }) => [
    <span
      className="inline-flex items-start gap-[0.75mm] text-slate-800"
      title={role.role}
      style={{ overflowWrap: 'anywhere', wordBreak: 'break-word' }}
    >
      <Icon name="shield-user" size={9} />
      <span>{role.label}</span>
    </span>,
    externalIds.length > 0 ? (
      <div className="flex flex-col gap-[0.25mm] font-mono text-[4.8pt] leading-tight text-slate-600">
        {externalIds.map((externalID) => (
          <span key={externalID} style={{ overflowWrap: 'anywhere', wordBreak: 'break-all' }}>
            {externalID}
          </span>
        ))}
      </div>
    ) : (
      <span className="text-slate-400">-</span>
    ),
  ]);
  return (
    <div className="mt-[1.25mm]">
      <div className="mb-[0.5mm] text-[5.8pt] font-semibold uppercase tracking-wide text-slate-500">
        Role external IDs
      </div>
      <CompactTable
        size="xs"
        variant="reference"
        columns={['Role name', 'External IDs']}
        data={tableRows}
      />
    </div>
  );
}

function loginAgeDays(lastSignedInAt?: string | null): number | null {
  if (!lastSignedInAt) return null;
  return Math.floor((Date.now() - new Date(lastSignedInAt).getTime()) / 86400000);
}

function staleColor(entry: RBACUserRole): string | null {
  if (!entry.isStale) return null;
  const days = loginAgeDays(entry.lastSignedInAt);
  if (days === null || days > 30) return STALE_COLORS.stale30d;
  return STALE_COLORS.stale7d;
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

function principalLabel(user: UserRow, roleSource?: string): React.ReactNode {
  const isGroupRow = user.kind === 'group';
  const labelInner = isGroupRow ? (
    <span className="inline-flex items-center gap-[0.65mm]">
      <Icon name="group" size={8} />
      <span>{user.userName}</span>
      {Boolean(user.memberCount) && (
        <span className="text-[5pt] font-normal text-slate-500">({user.memberCount})</span>
      )}
    </span>
  ) : (
    <span
      className="inline-flex items-center gap-[0.65mm]"
      style={{ paddingLeft: user.kind === 'member' ? '3mm' : 0 }}
    >
      <IdentityIcon userId={user.userId} roleSource={roleSource} size={user.kind === 'member' ? 7 : 8} />
      {user.userName}
    </span>
  );

  return (
    <a
      href={`#${isGroupRow ? groupAnchor(user.userName) : userAnchor(user.userId, user.userName)}`}
      className="no-underline text-inherit"
    >
      {labelInner}
    </a>
  );
}

function roleCell(entry: RBACUserRole): React.ReactNode {
  return (
    <div className="flex flex-col gap-[0.25mm]">
      <span style={{ overflowWrap: 'anywhere', wordBreak: 'break-word' }}>{entry.role}</span>
      {(entry.roleExternalIds || []).length > 0 && (
        <span className="font-mono text-[4.8pt] leading-tight text-slate-500" style={{ overflowWrap: 'anywhere', wordBreak: 'break-all' }}>
          {entry.roleExternalIds!.join(', ')}
        </span>
      )}
    </div>
  );
}

function extractSparseEntries(rows: UserRow[], roles: string[]) {
  if (roles.length <= 10) {
    return { sparseEntries: [] as SparseEntry[], matrixRows: rows, matrixRoles: roles };
  }

  const roleSet = new Set(roles);
  const rowCounts = new Map<UserRow, number>();
  const columnCounts = new Map<string, number>();

  for (const row of rows) {
    let count = 0;
    for (const role of roles) {
      if (!row.roles.has(role)) continue;
      count += 1;
      columnCounts.set(role, (columnCounts.get(role) || 0) + 1);
    }
    rowCounts.set(row, count);
  }

  const sparseKeys = new Set<string>();
  const sparseEntries: SparseEntry[] = [];
  for (const row of rows) {
    if ((rowCounts.get(row) || 0) !== 1) continue;
    for (const role of roles) {
      const entry = row.roles.get(role);
      if (!entry || (columnCounts.get(role) || 0) !== 1) continue;
      const key = `${rowKey(row)}:${role}`;
      sparseKeys.add(key);
      sparseEntries.push({ row, entry });
    }
  }

  if (sparseEntries.length === 0) {
    return { sparseEntries, matrixRows: rows, matrixRoles: roles };
  }

  const matrixRows: UserRow[] = [];
  const matrixRoleSet = new Set<string>();
  for (const row of rows) {
    const remainingRoles = new Map<string, RBACUserRole>();
    for (const [role, entry] of row.roles.entries()) {
      if (!roleSet.has(role) || sparseKeys.has(`${rowKey(row)}:${role}`)) continue;
      remainingRoles.set(role, entry);
      matrixRoleSet.add(role);
    }
    if (remainingRoles.size > 0) {
      matrixRows.push({ ...row, roles: remainingRoles });
    }
  }

  return {
    sparseEntries,
    matrixRows,
    matrixRoles: roles.filter((role) => matrixRoleSet.has(role)),
  };
}

function SparseAccessTable({ entries }: { entries: SparseEntry[] }) {
  if (entries.length === 0) return null;
  const rows = entries.map(({ row, entry }) => [
    <span className="inline-flex items-center text-slate-800">
      {principalLabel(row, entry.roleSource)}
    </span>,
    roleCell(entry),
  ]);
  return (
    <div className="mb-[1.25mm]">
      <CompactTable
        size="xs"
        variant="reference"
        columns={['Principal', 'Role']}
        data={rows}
      />
    </div>
  );
}

function buildMatrixRows(users: UserRow[], roles: RoleColumn[]) {
  const roleSet = new Set(roles.map((role) => role.role));
  return users.map((user) => {
    const entries = [...user.roles.entries()]
      .filter(([role]) => roleSet.has(role))
      .map(([, entry]) => entry);
    const worstStale = entries.reduce<string | null>((worst, r) => {
      const c = staleColor(r);
      if (c === STALE_COLORS.stale30d) return c;
      return worst ?? c;
    }, null);
    const firstRole = entries[0];
    const roleSource = firstRole?.roleSource;
    const isGroupRow = user.kind === 'group';
    return {
      label: (
        <span style={{
          display: 'inline-flex',
          alignItems: 'center',
          gap: '0.65mm',
          fontSize: user.kind === 'member' ? '5.4pt' : '5.8pt',
          lineHeight: 1.05,
          fontWeight: isGroupRow ? 650 : 500,
          borderLeft: `2px solid ${worstStale || 'transparent'}`,
          paddingLeft: '0.6mm',
        }}>
          {principalLabel(user, roleSource)}
        </span>
      ),
      cells: roles.map((role) => <Indicator key={role.role} entry={user.roles.get(role.role)} />),
      rowStyle: isGroupRow ? { backgroundColor: '#F1F5F9' } : undefined,
      labelStyle: isGroupRow ? { color: '#334155' } : undefined,
      cellStyle: isGroupRow ? { backgroundColor: '#F8FAFC' } : undefined,
    };
  });
}

function MatrixBlock({
  title,
  roles,
  rows,
  cornerContent,
}: {
  title?: string;
  roles: RoleColumn[];
  rows: UserRow[];
  cornerContent?: React.ReactNode;
}) {
  if (roles.length === 0 || rows.length === 0) return null;
  return (
    <div className="mb-[2mm]">
      {title && (
        <div className="mb-[0.6mm] text-[6.6pt] font-semibold uppercase tracking-wide text-slate-600">
          {title}
        </div>
      )}
      <MatrixTable
        columns={roles.map((role) => <RoleHeader key={role.role} column={role} />)}
        rows={buildMatrixRows(rows, roles)}
        columnWidth={9}
        headerHeight={roleHeaderHeightMm(roles.map((role) => role.label))}
        rowHeight={3.1}
        labelPadding="0.1mm 1.4mm 0.1mm 0.6mm"
        cornerContent={cornerContent}
      />
      <RoleReferenceTable rows={rows} roles={roles} />
    </div>
  );
}

function MatrixBlockGroup({
  title,
  roles,
  rows,
  cornerContent,
}: {
  title?: string;
  roles: string[];
  rows: UserRow[];
  cornerContent?: React.ReactNode;
}) {
  const { sparseEntries, matrixRows, matrixRoles } = extractSparseEntries(rows, roles);
  const matrixColumns = columnsForRoles(matrixRoles);
  return (
    <div className="mb-[2mm]">
      {title && (
        <div className="mb-[0.6mm] text-[6.6pt] font-semibold uppercase tracking-wide text-slate-600">
          {title}
        </div>
      )}
      <SparseAccessTable entries={sparseEntries} />
      <MatrixBlock
        roles={matrixColumns}
        rows={matrixRows}
        cornerContent={cornerContent}
      />
    </div>
  );
}

export function MatrixLegend({ showStale = true, showReviewOverdue = true }: { showStale?: boolean; showReviewOverdue?: boolean } = {}) {
  return (
    <div className="flex flex-wrap items-center gap-[3mm] text-gray-500">
      <span className="font-semibold">Legend:</span>
      <span className="inline-flex items-center gap-[1mm]">
        <Dot color={ACCESS_COLORS.direct} /> Direct
      </span>
      <span className="inline-flex items-center gap-[1mm]">
        <Dot color={ACCESS_COLORS.group} outline /> Indirect
      </span>
      {showStale && (
        <>
          <span className="inline-flex items-center gap-[1mm]">
            <span style={{ display: 'inline-block', width: '2mm', height: '3mm', borderLeft: `2px solid ${STALE_COLORS.stale7d}` }} />
            Last login &gt; 7d
          </span>
          <span className="inline-flex items-center gap-[1mm]">
            <span style={{ display: 'inline-block', width: '2mm', height: '3mm', borderLeft: `2px solid ${STALE_COLORS.stale30d}` }} />
            Last login &gt; 30d
          </span>
        </>
      )}
      {showReviewOverdue && <ReviewOverdueLegendSwatch />}
    </div>
  );
}

export default function RBACMatrixSection({ resource }: Props) {
  const matrix = buildMatrix(resource);
  if (matrix.combinedRows.length === 0) return null;

  const hasStale = (resource.users || []).some((u) => u.isStale);
  const hasReviewOverdue = (resource.users || []).some((u) => u.isReviewOverdue);

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
        <MatrixLegend showStale={hasStale} showReviewOverdue={hasReviewOverdue} />
      </div>
    </div>
  );

  const splitByAccessType = matrix.allRoles.length > 10;
  const splitBlocks = [
    { title: 'Direct access', roles: matrix.directRoles, rows: matrix.directRows },
    { title: 'Indirect access', roles: matrix.indirectRoles, rows: matrix.indirectRows },
  ].filter((block) => block.roles.length > 0 && block.rows.length > 0);

  return (
    <div className="mb-[3mm]">
      {splitByAccessType ? (
        splitBlocks.map((block, index) => (
          <MatrixBlockGroup
            key={block.title}
            title={block.title}
            roles={block.roles}
            rows={block.rows}
            cornerContent={index === 0 ? corner : undefined}
          />
        ))
      ) : (
        <MatrixBlockGroup
          roles={matrix.allRoles}
          rows={matrix.combinedRows}
          cornerContent={corner}
        />
      )}
    </div>
  );
}
