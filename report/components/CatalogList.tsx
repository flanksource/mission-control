import React from 'react';
import { Section, ListTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportEntry, CatalogReportTreeNode } from '../catalog-report-types.ts';
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

function Breadcrumb({ entry }: { entry: CatalogReportEntry }) {
  const crumbs = entry.breadcrumb || [];
  if (crumbs.length === 0) return null;
  return (
    <div className="flex items-center flex-wrap gap-[1mm] text-xs text-slate-500 mb-[1mm]">
      {crumbs.map((c, idx) => (
        <React.Fragment key={c.id || idx}>
          {c.type && <Icon name={c.type} size={10} />}
          <span>{c.name}</span>
          <span className="text-slate-400">›</span>
        </React.Fragment>
      ))}
      {entry.configItem.type && <Icon name={entry.configItem.type} size={10} />}
      <span className="font-medium text-slate-700">{entry.configItem.name}</span>
    </div>
  );
}

function EntryDetail({ entry }: { entry: CatalogReportEntry }) {
  const hasRbac = (entry.rbacResources || []).length > 0;
  const hasBreadcrumb = (entry.breadcrumb || []).length > 0;
  if (!hasRbac && !hasBreadcrumb) return null;

  return (
    <div className="ml-[4mm] mb-[2mm]">
      {hasBreadcrumb && <Breadcrumb entry={entry} />}
      {hasRbac && entry.rbacResources!.map((resource, idx) => (
        <RBACMatrixSection key={resource.configId || idx} resource={resource} />
      ))}
    </div>
  );
}

interface CatalogListProps {
  entries?: CatalogReportEntry[];
  groupBy?: string;
  relationshipTree?: CatalogReportTreeNode;
}

export default function CatalogList({ entries, groupBy, relationshipTree }: CatalogListProps) {
  if (!entries?.length) return null;

  const ordered = groupBy === 'none'
    ? [...entries].sort((a, b) => {
      const aRoot = a.isRoot ? 0 : 1;
      const bRoot = b.isRoot ? 0 : 1;
      return aRoot - bRoot;
    })
    : entries;

  return (
    <>
      {relationshipTree && <ConfigTreeSection tree={relationshipTree} />}
      <Section variant="hero" title={`Config Items (${ordered.length})`} size="md">
        <ListTable
          rows={ordered.map(entryToRow)}
          subject="name"
          subtitle="type"
          icon="type"
          iconMap={(type: string) => type ? <Icon name={type} size={12} /> : null}
          primaryTags={['health', 'status']}
          secondaryTags={['changes', 'insights', 'access']}
          size="sm"
          density="compact"
        />
        {ordered.map((entry, idx) => (
          <EntryDetail
            key={entry.configItem?.id || idx}
            entry={entry}
          />
        ))}
      </Section>
    </>
  );
}
