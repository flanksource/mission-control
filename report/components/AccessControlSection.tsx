import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { ApplicationAccessControl } from '../types.ts';
import { formatDate } from './utils.ts';

interface Props {
  accessControl: ApplicationAccessControl;
}

function neverOrDate(val?: string | null): string {
  return val ? formatDate(val) : 'Never';
}

export default function AccessControlSection({ accessControl }: Props) {
  const userRows = accessControl.users.map((u) => [
    u.name,
    u.email,
    u.role,
    u.authType,
    formatDate(u.created),
    neverOrDate(u.lastLogin),
    neverOrDate(u.lastAccessReview),
  ]);

  const authRows = accessControl.authentication.map((a) => [
    a.name,
    a.type,
    a.mfa?.type ?? '-',
    a.mfa?.enforced === 'true' ? 'Enforced' : 'Optional',
  ]);

  return (
    <Section variant="hero" title="Access Control" size="md">
      <div className="mb-[4mm]">
        <h3 className="text-[11pt] font-semibold text-slate-800 mb-[2mm]">Users</h3>
        <CompactTable
          variant="reference"
          columns={['Name', 'Email', 'Role', 'Auth Type', 'Created', 'Last Login', 'Last Access Review']}
          data={userRows}
        />
      </div>
      {authRows.length > 0 && (
        <div>
          <h3 className="text-[11pt] font-semibold text-slate-800 mb-[2mm]">Authentication Methods</h3>
          <CompactTable
            variant="reference"
            columns={['Name', 'Type', 'MFA Type', 'MFA Status']}
            data={authRows}
          />
        </div>
      )}
    </Section>
  );
}
