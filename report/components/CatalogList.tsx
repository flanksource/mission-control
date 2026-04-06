import React from 'react';
import { Section, ListTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportEntry } from '../catalog-report-types.ts';
import ConfigTreeSection from './ConfigTreeSection.tsx';
import RBACMatrixSection from './RBACMatrixSection.tsx';

function isUnknown(v?: string): boolean {
  return !v || v.toLowerCase() === 'unknown';
}

function entryToRow(entry: CatalogReportEntry): Record<string, any> {
  const ci = entry.configItem;
  const row: Record<string, any> = {
    name: ci.name,
    type: ci.type,
  };
  if (!isUnknown(ci.health)) row.health = ci.health;
  if (!isUnknown(ci.status)) row.status = ci.status;
  if (entry.changeCount > 0) row.changes = `${entry.changeCount} changes`;
  if (entry.insightCount > 0) row.insights = `${entry.insightCount} insights`;
  if (entry.accessCount > 0) row.access = `${entry.accessCount} access`;
  return row;
}

function EntryDetail({ entry }: { entry: CatalogReportEntry }) {
  const hasTree = entry.relationshipTree && (entry.relationshipTree.children || []).length > 0;
  const hasRbac = (entry.rbacResources || []).length > 0;
  if (!hasTree && !hasRbac) return null;

  return (
    <div className="ml-[4mm] mb-[2mm]">
      {hasTree && <ConfigTreeSection tree={entry.relationshipTree!} />}
      {hasRbac && entry.rbacResources!.map((resource, idx) => (
        <RBACMatrixSection key={resource.configId || idx} resource={resource} />
      ))}
    </div>
  );
}

interface CatalogListProps {
  entries: CatalogReportEntry[];
}

export default function CatalogList({ entries }: CatalogListProps) {
  return (
    <Section variant="hero" title={`Config Items (${entries.length})`} size="md">
      <ListTable
        rows={entries.map(entryToRow)}
        subject="name"
        subtitle="type"
        icon="type"
        iconMap={(type: string) => type ? <Icon name={type} size={12} /> : null}
        primaryTags={['health', 'status']}
        secondaryTags={['changes', 'insights', 'access']}
        size="sm"
        density="compact"
      />
      {entries.map((entry, idx) => (
        <EntryDetail key={entry.configItem?.id || idx} entry={entry} />
      ))}
    </Section>
  );
}
