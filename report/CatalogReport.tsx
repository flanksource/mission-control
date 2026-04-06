import React from 'react';
import { Page, PageBreak, Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportData, CatalogReportConfigGroup } from './catalog-report-types.ts';
import ConfigChangesSection from './components/ConfigChangesSection.tsx';
import ConfigInsightsSection from './components/ConfigInsightsSection.tsx';
import ConfigRelationshipGraph from './components/ConfigRelationshipGraph.tsx';
import ConfigTreeSection from './components/ConfigTreeSection.tsx';
import CatalogAccessSection from './components/CatalogAccessSection.tsx';
import CatalogAccessLogsSection from './components/CatalogAccessLogsSection.tsx';
import RBACMatrixSection from './components/RBACMatrixSection.tsx';
import ArtifactAppendix from './components/ArtifactAppendix.tsx';
import CoverPage from './components/CoverPage.tsx';
import CatalogList from './components/CatalogList.tsx';

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

function CatalogCoverPage({ data }: { data: CatalogReportData }) {
  const ci = data.configItem || {};
  const stats: Array<{ label: string; value: number }> = [];
  if (data.sections?.changes) stats.push({ label: 'changes', value: (data.changes || []).length });
  if (data.sections?.insights) stats.push({ label: 'insights', value: (data.analyses || []).length });
  if (data.sections?.relationships) stats.push({ label: 'relationships', value: (data.relatedConfigs || []).length });
  if (data.sections?.access) stats.push({ label: 'access entries', value: (data.access || []).length });
  if (data.sections?.accessLogs) stats.push({ label: 'access logs', value: (data.accessLogs || []).length });

  return (
    <CoverPage
      title={data.title || 'Catalog Report'}
      subtitle="Catalog Report"
      breadcrumbs={data.parents}
      subjects={[ci]}
      stats={stats}
      dateRange={data.from || data.to ? { from: data.from, to: data.to } : undefined}
      generatedAt={data.generatedAt}
    >
      {data.recursive && (
        <div className="text-xs text-blue-600 font-medium mt-[2mm]">
          Including all descendant config items
          {data.groupBy === 'config' && ` · Grouped by config (${(data.configGroups || []).length} items)`}
        </div>
      )}
    </CoverPage>
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
        <CatalogCoverPage data={data} />
      </Page>

      {(data.entries || []).length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <CatalogList entries={data.entries!} />
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
