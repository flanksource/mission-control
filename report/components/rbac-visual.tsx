import React from 'react';
import { LuUser, LuUsers, LuServer, LuBot } from 'react-icons/lu';

// --- Identity Types ---

export type IdentityType = 'user' | 'group' | 'service' | 'bot';

export interface IdentityInfo {
  type: IdentityType;
  icon: string;
  color: string;
  label: string;
}

const IDENTITY_COLOR = '#64748B';

const IDENTITY_ICONS: Record<IdentityType, React.ComponentType<{ size?: number; color?: string; style?: React.CSSProperties }>> = {
  user: LuUser,
  group: LuUsers,
  service: LuServer,
  bot: LuBot,
};

const IDENTITY_LABELS: Record<IdentityType, string> = {
  user: 'User',
  group: 'Group',
  service: 'Service Account',
  bot: 'Bot',
};

export function identityType(userId: string, roleSource?: string): IdentityInfo {
  const resolve = (type: IdentityType): IdentityInfo => ({
    type, icon: type, color: IDENTITY_COLOR, label: IDENTITY_LABELS[type],
  });
  if (roleSource?.startsWith('group:')) return resolve('group');
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
  const IconComponent = IDENTITY_ICONS[info.type];
  return <IconComponent size={size} color={IDENTITY_COLOR} style={{ display: 'inline-block', verticalAlign: 'middle' }} />;
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
