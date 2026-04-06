import React from 'react';
import { Page, PageBreak, Section, ListTable } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportData, CatalogReportConfigGroup, CatalogReportEntry } from './catalog-report-types.ts';
import ConfigChangesSection from './components/ConfigChangesSection.tsx';
import ConfigInsightsSection from './components/ConfigInsightsSection.tsx';
import ConfigRelationshipGraph from './components/ConfigRelationshipGraph.tsx';
import ConfigTreeSection from './components/ConfigTreeSection.tsx';
import CatalogAccessSection from './components/CatalogAccessSection.tsx';
import CatalogAccessLogsSection from './components/CatalogAccessLogsSection.tsx';
import RBACMatrixSection, { MatrixLegend } from './components/RBACMatrixSection.tsx';
import ArtifactAppendix from './components/ArtifactAppendix.tsx';
import { formatDate, formatDateTime } from './components/utils.ts';

function PageHeader({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-between px-[5mm] py-[1mm] bg-[#1e293b] text-white text-xs">
      <span className="font-semibold">{title}</span>
      <span className="text-gray-300">Catalog Report</span>
    </div>
  );
}

function PageFooter() {
  const now = new Date().toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC');
  return (
    <div className="px-[5mm] py-[1mm] border-t border-gray-200 text-xs text-gray-400 flex items-center justify-between">
      <span>Generated {now}</span>
    </div>
  );
}

function CoverPage({ data }: { data: CatalogReportData }) {
  const ci = data.configItem || {};
  const tags = { ...(ci.tags || {}), ...(ci.labels || {}) };

  return (
    <div className="flex flex-col items-center justify-center h-full text-center gap-[4mm]">
      <div className="text-xl font-bold text-slate-900">{data.title || 'Catalog Report'}</div>

      {(data.parents || []).length > 0 && (
        <div className="text-xs text-gray-400">
          {data.parents.map((p, i) => (
            <span key={p.id}>
              {i > 0 && ' › '}
              {p.name}
            </span>
          ))}
        </div>
      )}

      <div className="flex items-center gap-[2mm] mt-[4mm]">
        {ci.type && <Icon name={ci.type} size={24} />}
        <span className="text-lg font-semibold text-slate-800">{ci.name}</span>
      </div>

      {ci.type && <div className="text-sm text-gray-500">{ci.type}</div>}

      {Object.keys(tags).length > 0 && (
        <div className="flex items-center gap-[1mm] flex-wrap justify-center mt-[2mm]">
          {Object.entries(tags).map(([k, v]) => (
            <span key={k} className="inline-flex items-center border border-blue-300 rounded overflow-hidden text-xs leading-none">
              <span className="px-[0.7mm] py-[0.2mm] font-medium bg-blue-100 text-slate-700">{k}</span>
              <span className="px-[0.7mm] py-[0.2mm] bg-white text-slate-800">{v || '-'}</span>
            </span>
          ))}
        </div>
      )}

      <div className="flex gap-[6mm] text-xs text-gray-500 mt-[4mm]">
        {!isUnknown(ci.health) && <span>Health: <span className="font-semibold">{ci.health}</span></span>}
        {!isUnknown(ci.status) && <span>Status: <span className="font-semibold">{ci.status}</span></span>}
        {ci.created_at && <span>Created: {formatDate(ci.created_at)}</span>}
      </div>

      {(data.from || data.to) && (
        <div className="text-xs text-gray-500 mt-[2mm]">
          Period: {formatDate(data.from || data.generatedAt)} – {formatDate(data.to || data.generatedAt)}
        </div>
      )}

      <div className="text-xs text-gray-400 mt-[6mm]">
        Generated {data.generatedAt ? formatDateTime(data.generatedAt) : new Date().toLocaleDateString()}
      </div>

      {data.recursive && (
        <div className="text-xs text-blue-600 font-medium mt-[2mm]">
          Including all descendant config items
          {data.groupBy === 'config' && ` · Grouped by config (${(data.configGroups || []).length} items)`}
        </div>
      )}

      <div className="flex gap-[4mm] text-xs text-gray-500 mt-[2mm]">
        {data.sections?.changes && <span>{(data.changes || []).length} changes</span>}
        {data.sections?.insights && <span>{(data.analyses || []).length} insights</span>}
        {data.sections?.relationships && <span>{(data.relatedConfigs || []).length} relationships</span>}
        {data.sections?.access && <span>{(data.access || []).length} access entries</span>}
        {data.sections?.accessLogs && <span>{(data.accessLogs || []).length} access logs</span>}
      </div>
    </div>
  );
}

function ConfigJSONSection({ json }: { json: string }) {
  let formatted = json;
  try {
    formatted = JSON.stringify(JSON.parse(json), null, 2);
  } catch { }

  return (
    <Section variant="hero" title="Config JSON" size="md">
      <pre className="text-xs font-mono bg-gray-50 border border-gray-200 rounded p-[2mm] overflow-hidden whitespace-pre-wrap break-all leading-tight">
        {formatted}
      </pre>
    </Section>
  );
}

function ConfigGroupHeader({ group }: { group: CatalogReportConfigGroup }) {
  const ci = group.configItem;
  return (
    <div className="flex items-center gap-[2mm] mb-[2mm] pb-[1mm] border-b-2 border-blue-200">
      {ci.type && <Icon name={ci.type} size={14} />}
      <span className="text-sm font-bold text-slate-800">{ci.name}</span>
      {ci.type && <span className="text-xs text-gray-500">{ci.type}</span>}
      {ci.permalink && (
        <span className="text-xs text-blue-500 font-mono ml-auto">{ci.permalink}</span>
      )}
    </div>
  );
}

function isUnknown(v?: string): boolean {
  return !v || v.toLowerCase() === 'unknown';
}

function entryToRow(entry: CatalogReportEntry): Record<string, any> {
  const ci = entry.configItem || {};
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

interface CatalogReportProps {
  data: CatalogReportData;
}

export default function CatalogReportPage({ data }: CatalogReportProps) {
  const header = <PageHeader title={data.title || 'Catalog Report'} />;
  const footer = <PageFooter />;
  const pageProps = {
    pageSize: 'a4' as const,
    margins: { top: 1, bottom: 1, left: 5, right: 5 },
    header,
    headerHeight: 8,
    footer,
    footerHeight: 8,
  };

  const configItem = {
    id: data.configItem.id,
    name: data.configItem.name,
    type: data.configItem.type,
    configClass: data.configItem.configClass,
    status: data.configItem.status,
    health: data.configItem.health,
    description: data.configItem.description,
    labels: data.configItem.labels,
    tags: data.configItem.tags,
    createdAt: data.configItem.created_at,
    updatedAt: data.configItem.updated_at,
  };

  return (
    <>
      <Page pageSize="a4" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <CoverPage data={data} />
      </Page>

      {(data.entries || []).length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <Section variant="hero" title={`Config Items (${data.entries!.length})`} size="md">
              <ListTable
                rows={data.entries!.map(entryToRow)}
                subject="name"
                subtitle="type"
                icon="type"
                iconMap={(type: string) => type ? <Icon name={type} size={12} /> : null}
                primaryTags={['health', 'status']}
                secondaryTags={['changes', 'insights', 'access']}
                size="sm"
                density="compact"
              />
              {data.entries!.map((entry, idx) => (
                <EntryDetail key={entry.configItem?.id || idx} entry={entry} />
              ))}
            </Section>
          </Page>
        </>
      )}

      {data.groupBy === 'config' && (data.entries || []).map((entry, idx) => (
        <React.Fragment key={entry.configItem?.id || idx}>
          {(entry.changes || []).length > 0 && (
            <>
              <PageBreak />
              <Page {...pageProps}>
                <ConfigGroupHeader group={{ configItem: entry.configItem as any, changes: entry.changes, analyses: entry.analyses, access: entry.access, accessLogs: entry.accessLogs }} />
                <ConfigChangesSection changes={entry.changes} hideConfigName />
              </Page>
            </>
          )}
          {(entry.analyses || []).length > 0 && (
            <>
              <PageBreak />
              <Page {...pageProps}>
                <ConfigGroupHeader group={{ configItem: entry.configItem as any, changes: entry.changes, analyses: entry.analyses, access: entry.access, accessLogs: entry.accessLogs }} />
                <ConfigInsightsSection analyses={entry.analyses} />
              </Page>
            </>
          )}
          {(entry.rbacResources || []).length > 0 && (
            <>
              <PageBreak />
              <Page {...pageProps}>
                <ConfigGroupHeader group={{ configItem: entry.configItem as any, changes: entry.changes, analyses: entry.analyses, access: entry.access, accessLogs: entry.accessLogs }} />
                {entry.rbacResources!.map((resource, rIdx) => (
                  <RBACMatrixSection key={resource.configId || rIdx} resource={resource} />
                ))}
              </Page>
            </>
          )}
        </React.Fragment>
      ))}

      {data.groupBy !== 'config' && data.sections?.changes && (data.changes || []).length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigChangesSection changes={data.changes} />
          </Page>
        </>
      )}

      {data.groupBy !== 'config' && data.sections?.insights && (data.analyses || []).length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigInsightsSection analyses={data.analyses} />
          </Page>
        </>
      )}

      {data.sections?.relationships && data.relationshipTree && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigTreeSection tree={data.relationshipTree} />
          </Page>
        </>
      )}

      {data.sections?.relationships && !data.relationshipTree && (data.relatedConfigs || []).length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigRelationshipGraph
              centralConfig={configItem}
              relationships={data.relationships}
              relatedConfigs={data.relatedConfigs}
            />
          </Page>
        </>
      )}

      {data.groupBy !== 'config' && data.sections?.access && (data.access || []).length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <CatalogAccessSection access={data.access} />
          </Page>
        </>
      )}

      {data.groupBy !== 'config' && data.sections?.accessLogs && (data.accessLogs || []).length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <CatalogAccessLogsSection logs={data.accessLogs} />
          </Page>
        </>
      )}

      {data.groupBy === 'config' && (data.configGroups || []).map((group, idx) => (
        <React.Fragment key={group.configItem.id || idx}>
          <Page {...pageProps}>
            <ConfigGroupHeader group={group} />
            {(group.changes || []).length > 0 && (
              <ConfigChangesSection changes={group.changes} hideConfigName />
            )}
            {(group.analyses || []).length > 0 && (
              <ConfigInsightsSection analyses={group.analyses} />
            )}
            {(group.access || []).length > 0 && (
              <CatalogAccessSection access={group.access} />
            )}
            {(group.accessLogs || []).length > 0 && (
              <CatalogAccessLogsSection logs={group.accessLogs} />
            )}
          </Page>
        </React.Fragment>
      ))}

      {data.sections?.configJSON && data.configJSON && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigJSONSection json={data.configJSON} />
          </Page>
        </>
      )}

      {(() => {
        const allChanges = (data.entries || []).flatMap((e) => e.changes || []);
        const withArtifacts = allChanges.filter((c) => (c.artifacts || []).length > 0);
        if (withArtifacts.length === 0) return null;
        return (
          <>
            <PageBreak />
            <Page {...pageProps}>
              <ArtifactAppendix changes={allChanges} />
            </Page>
          </>
        );
      })()}
    </>
  );
}
