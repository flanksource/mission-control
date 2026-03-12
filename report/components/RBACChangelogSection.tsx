import React from 'react';
import { Section, CompactTable } from '@flanksource/facet';
import type { RBACChangeEntry } from '../rbac-types.ts';
import { formatDate } from './utils.ts';

interface Props {
  changelog: RBACChangeEntry[];
}

export default function RBACChangelogSection({ changelog }: Props) {
  if (changelog.length === 0) return null;

  const rows = changelog.map((entry) => [
    formatDate(entry.date),
    entry.changeType,
    entry.user,
    entry.role,
    entry.configName,
    entry.source,
    entry.description,
  ]);

  return (
    <Section variant="hero" title="Permission Changelog" size="md">
      <CompactTable
        variant="reference"
        columns={['Date', 'Type', 'User', 'Role', 'Resource', 'Source', 'Description']}
        data={rows}
      />
    </Section>
  );
}
