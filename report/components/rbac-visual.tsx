import React from 'react';
import { Icon } from '@flanksource/icons/icon';

// --- Identity Types ---

export type IdentityType = 'user' | 'group' | 'service' | 'bot';

export interface IdentityInfo {
  type: IdentityType;
  icon: string;
  color: string;
  label: string;
}

const IDENTITY_COLOR = '#64748B';

const IDENTITY_ICON_NAMES: Record<IdentityType, string> = {
  user: 'user',
  group: 'group',
  service: 'server',
  bot: 'bot',
};

const IDENTITY_LABELS: Record<IdentityType, string> = {
  user: 'User',
  group: 'Group',
  service: 'Service Account',
  bot: 'Bot',
};

const NIL_UUID = '00000000-0000-0000-0000-000000000000';

export function identityType(userId: string, roleSource?: string): IdentityInfo {
  const resolve = (type: IdentityType): IdentityInfo => ({
    type, icon: type, color: IDENTITY_COLOR, label: IDENTITY_LABELS[type],
  });
  if ((!userId || userId === NIL_UUID) && roleSource?.startsWith('group:')) return resolve('group');
  if (/svc[-_]|service[-_]/i.test(userId)) return resolve('service');
  if (/bot[-_]|automation[-_]|pipeline[-_]/i.test(userId)) return resolve('bot');
  return resolve('user');
}

// --- Access Pattern ---

export const ACCESS_COLORS = {
  direct: '#2563EB',
  group: '#7C3AED',
};

export function isDirect(roleSource: string): boolean {
  return !roleSource.startsWith('group:');
}

export function groupAnchor(name: string): string {
  return `group-${name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')}`;
}

export function userAnchor(userId: string, name: string): string {
  const key = userId || name;
  return `user-${key.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '')}`;
}

export function groupNameFromRoleSource(roleSource: string): string | null {
  return roleSource.startsWith('group:') ? roleSource.slice(6) : null;
}

// --- Staleness ---

export const STALE_COLORS = {
  stale7d: '#EAB308',
  stale30d: '#DC2626',
};

// --- Review Status ---

export const REVIEW_OVERDUE_COLOR = '#DC2626';

export function ReviewOverdueBadge() {
  return (
    <div style={{
      position: 'absolute',
      top: 0,
      right: 0,
      width: 0,
      height: 0,
      borderLeft: '2mm solid transparent',
      borderTop: `2mm solid ${REVIEW_OVERDUE_COLOR}`,
    }} />
  );
}

export function ReviewOverdueLegendSwatch() {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '1mm' }}>
      <span style={{
        display: 'inline-block',
        width: '3.5mm',
        height: '3.5mm',
        position: 'relative',
        border: '1px solid #E2E8F0',
      }}>
        <span style={{
          position: 'absolute',
          top: 0,
          right: 0,
          width: 0,
          height: 0,
          borderLeft: '2mm solid transparent',
          borderTop: `2mm solid ${REVIEW_OVERDUE_COLOR}`,
        }} />
      </span>
      Review Overdue
    </span>
  );
}

// --- Reusable Visual Components ---

export function IdentityIcon({ userId, roleSource, size = 14 }: { userId: string; roleSource?: string; size?: number }) {
  const info = identityType(userId, roleSource);
  return <Icon name={IDENTITY_ICON_NAMES[info.type]} size={size} />;
}

export function AccessIndicator({ direct, color, size = 2.5 }: { direct: boolean; color: string; size?: number }) {
  return (
    <div style={{
      width: `${size}mm`,
      height: `${size}mm`,
      borderRadius: '50%',
      backgroundColor: direct ? color : 'transparent',
      border: direct ? 'none' : `0.4mm solid #64748B`,
    }} />
  );
}
