import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { RBACResource, RBACUserRole } from '../rbac-types.ts';
import { HEALTH_COLORS } from './utils.ts';
import { ConfigTypeIcon } from './configTypeIcon.tsx';

interface Props {
  resource: RBACResource;
}

function fmtDate(iso: string): string {
  const d = new Date(iso);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  const h = String(d.getHours()).padStart(2, '0');
  const min = String(d.getMinutes()).padStart(2, '0');
  return `${fmtDate(iso)}T${h}:${min}`;
}

function age(iso?: string | null): string {
  if (!iso) return 'Never';
  const diff = Date.now() - new Date(iso).getTime();
  const days = Math.floor(diff / 86400000);
  if (days < 1) return '<1d';
  if (days < 30) return `${days}d`;
  if (days < 365) return `${Math.floor(days / 30)}mo`;
  return `${Math.floor(days / 365)}y ${Math.floor((days % 365) / 30)}mo`;
}

function roleColumn(u: RBACUserRole): string {
  const parts = [u.role];
  if (u.roleSource && u.roleSource !== 'direct') {
    parts.push(`via ${u.roleSource}`);
  }
  if (u.sourceSystem && u.sourceSystem !== u.roleSource) {
    parts.push(`(${u.sourceSystem})`);
  }
  return parts.join(' ');
}

function ReviewAge({ u }: { u: RBACUserRole }) {
  const text = age(u.lastReviewedAt);
  if (u.isReviewOverdue && text !== 'Never') {
    return <span style={{ color: '#EA580C', fontWeight: 600 }}>{text}</span>;
  }
  if (text === 'Never') {
    return <span style={{ color: '#DC2626', fontWeight: 600 }}>Never</span>;
  }
  return <>{text}</>;
}

function LabelBadge({ label, value }: { label: string; value: string }) {
  return (
    <span className="inline-flex items-center mr-[1mm] mb-[0.5mm] border border-blue-200 rounded overflow-hidden text-[6pt]" style={{ whiteSpace: 'nowrap' }}>
      <span className="px-[1.5mm] py-[0.5mm] font-medium" style={{ backgroundColor: '#DBEAFE', color: '#475569' }}>
        {label}
      </span>
      <span className="px-[1.5mm] py-[0.5mm]" style={{ backgroundColor: '#FFFFFF', color: '#0F172A' }}>
        {value}
      </span>
    </span>
  );
}

function Pill({ label, color, icon }: { label: string; color?: string; icon?: React.ReactNode }) {
  return (
    <span
      className="inline-flex items-center gap-[0.5mm] px-[2mm] py-[0.5mm] rounded text-[6pt] font-bold mr-[1mm]"
      style={{
        backgroundColor: color || '#E2E8F0',
        color: color ? '#FFFFFF' : '#334155',
        whiteSpace: 'nowrap',
      }}
    >
      {icon}
      {label.toUpperCase()}
    </span>
  );
}

function SubHeader({ icon, children }: { icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-[1mm] text-[8pt] font-semibold text-slate-700 mb-[0.5mm]">
      {icon}
      {children}
    </div>
  );
}

function TagsRow({ tags, labels }: { tags?: Record<string, string>; labels?: Record<string, string> }) {
  const tagKeys = new Set(tags ? Object.keys(tags) : []);
  const entries: [string, string][] = [];
  if (tags) entries.push(...Object.entries(tags));
  if (labels) {
    for (const [k, v] of Object.entries(labels)) {
      if (!tagKeys.has(k)) entries.push([k, v]);
    }
  }
  if (entries.length === 0) return null;

  return (
    <div className="mb-[1mm]">
      {entries.map(([k, v]) => (
        <LabelBadge key={`${k}=${v}`} label={k} value={v || '-'} />
      ))}
    </div>
  );
}

function ResourceMeta({ resource }: Props) {
  const dateParts: string[] = [];
  if (resource.createdAt) dateParts.push(`Created: ${fmtDate(resource.createdAt)}`);
  if (resource.updatedAt) dateParts.push(`Updated: ${fmtDate(resource.updatedAt)}`);

  const hasPills = resource.status || resource.health;
  const hasTags = (resource.tags && Object.keys(resource.tags).length > 0) ||
                  (resource.labels && Object.keys(resource.labels).length > 0);

  return (
    <div className="mb-[1mm]">
      <div className="flex flex-wrap items-baseline gap-x-[3mm] text-[7pt] text-gray-500 mb-[1mm]">
        <span>
          <span className="font-medium text-gray-400">ID: </span>
          <a href={`/catalog/${resource.configId}`} className="text-blue-600 underline font-mono">
            {resource.configId}
          </a>
        </span>
        {dateParts.length > 0 && (
          <span className="text-[6.5pt] text-gray-400 border-l border-gray-300 pl-[3mm]">
            {dateParts.join('  \u2022  ')}
          </span>
        )}
      </div>

      {hasPills && (
        <div className="flex items-center mb-[1mm]">
          {resource.health && (
            <Pill
              label={resource.health}
              color={HEALTH_COLORS[resource.health.toLowerCase()] || '#6B7280'}
              icon={<Icon name="health" size={10} />}
            />
          )}
          {resource.status && <Pill label={resource.status} />}
        </div>
      )}

      {resource.description && (
        <div className="text-[7pt] text-gray-600 italic mb-[1mm] leading-tight">{resource.description}</div>
      )}
      {hasTags && <TagsRow tags={resource.tags} labels={resource.labels} />}
    </div>
  );
}

function ChangelogTable({ resource }: Props) {
  if (!resource.changelog || resource.changelog.length === 0) return null;

  const rows = resource.changelog.map((e) => [
    fmtDateTime(e.date),
    e.changeType,
    e.user,
    e.role,
    e.description,
  ]);

  return (
    <div className="mt-[2mm]">
      <SubHeader icon={<Icon name="changes" size={12} />}>Changelog</SubHeader>
      <CompactTable
        variant="reference"
        columns={['Date', 'Type', 'User', 'Role', 'Description']}
        data={rows}
      />
    </div>
  );
}

function TemporaryAccessTable({ resource }: Props) {
  if (!resource.temporaryAccess || resource.temporaryAccess.length === 0) return null;

  const rows = resource.temporaryAccess.map((e) => [
    e.user,
    e.role,
    e.source,
    fmtDateTime(e.grantedAt),
    fmtDateTime(e.revokedAt),
    e.duration,
  ]);

  return (
    <div className="mt-[2mm]">
      <SubHeader icon={<Icon name="shield-time" size={12} />}>Temporary Access (&lt;72h)</SubHeader>
      <CompactTable
        variant="reference"
        columns={['User', 'Role', 'Source', 'Granted', 'Revoked', 'Duration']}
        data={rows}
      />
    </div>
  );
}

export default function RBACResourceSection({ resource }: Props) {
  const rows = resource.users.map((u) => [
    u.userName,
    u.email,
    roleColumn(u),
    fmtDate(u.createdAt),
    age(u.lastSignedInAt),
    <ReviewAge key={u.userId} u={u} />,
  ]);

  const title = (
    <span className="inline-flex items-center gap-[1mm]">
      <ConfigTypeIcon configType={resource.configType} size={14} />
      {resource.configName} ({resource.configType})
    </span>
  );

  return (
    <Section
      variant="hero"
      title={title}
      size="md"
    >
      <ResourceMeta resource={resource} />
      <SubHeader icon={<Icon name="group" size={12} />}>Users</SubHeader>
      <CompactTable
        variant="reference"
        columns={['Name', 'Email', 'Role', 'Created', 'Last Sign In', 'Last Review']}
        data={rows}
      />
      <TemporaryAccessTable resource={resource} />
      <ChangelogTable resource={resource} />
    </Section>
  );
}
