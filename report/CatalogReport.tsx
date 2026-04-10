import React from 'react';
import { Document, Page, Header, Footer, Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportData, CatalogReportConfigGroup, CatalogReportCategoryMapping } from './catalog-report-types.ts';
import type { ConfigChange } from './config-types.ts';
import ConfigChangesSection from './components/ConfigChangesSection.tsx';
import ConfigInsightsSection from './components/ConfigInsightsSection.tsx';
import RBACChanges from './components/RBACChanges.tsx';
import BackupChanges from './components/BackupChanges.tsx';
import DeploymentChanges from './components/DeploymentChanges.tsx';
import { categorizeChanges, configChangeToApplicationChange } from './components/change-section-utils.ts';
import ConfigRelationshipGraph from './components/ConfigRelationshipGraph.tsx';
import ConfigTreeSection from './components/ConfigTreeSection.tsx';
import CatalogAccessSection from './components/CatalogAccessSection.tsx';
import CatalogAccessLogsSection from './components/CatalogAccessLogsSection.tsx';
import RBACMatrixSection from './components/RBACMatrixSection.tsx';
import ArtifactAppendix from './components/ArtifactAppendix.tsx';
import AuditPage from './components/AuditPage.tsx';
import CoverPage from './components/CoverPage.tsx';
import CatalogList from './components/CatalogList.tsx';
import PageHeader from './components/PageHeader.tsx';
import PageFooter from './components/PageFooter.tsx';

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
      {data.thresholds && (
        <div className="flex gap-[4mm] text-xs text-gray-400 mt-[2mm]">
          <span>Stale access: {data.thresholds.staleDays}d</span>
          <span>Review overdue: {data.thresholds.reviewOverdueDays}d</span>
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

function CategorizedChangesSection({ changes, categoryMappings, hideConfigName }: {
  changes?: ConfigChange[];
  categoryMappings?: CatalogReportCategoryMapping[];
  hideConfigName?: boolean;
}) {
  if (!changes?.length) return null;
  const { rbac, backup, deployment, uncategorized } = categorizeChanges(changes, categoryMappings);
  return (
    <>
      {rbac.length > 0 && (
        <Section variant="hero" title="Permission Changes" size="md">
          <RBACChanges changes={rbac.map((change) => configChangeToApplicationChange(change))} />
        </Section>
      )}
      {backup.length > 0 && (
        <Section variant="hero" title="Backup Activity" size="md">
          <BackupChanges changes={backup.map((change) => configChangeToApplicationChange(change))} />
        </Section>
      )}
      {deployment.length > 0 && (
        <Section variant="hero" title="Deployment Changes" size="md">
          <DeploymentChanges changes={deployment.map((change) => configChangeToApplicationChange(change))} />
        </Section>
      )}
      {uncategorized.length > 0 && (
        <ConfigChangesSection changes={uncategorized} hideConfigName={hideConfigName} />
      )}
    </>
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
    <Document pageSize="a4" margins={{ top: 1, bottom: 1, left: 5, right: 5 }}>
      <Header height={8}>
        <PageHeader subtitle="Catalog Report" />
      </Header>
      <Footer height={8}>
        <PageFooter publicURL={data.publicURL} generatedAt={data.generatedAt} />
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <CatalogCoverPage data={data} />
      </Page>

      <Page>
        <CatalogList entries={data.entries} />

        {data.groupBy === 'config' && (data.entries || []).map((entry, idx) => (
          <React.Fragment key={entry.configItem?.id || idx}>
            <ConfigGroupHeader group={{ configItem: entry.configItem as any, changes: entry.changes, analyses: entry.analyses, access: entry.access, accessLogs: entry.accessLogs }} />
            <CategorizedChangesSection changes={entry.changes} categoryMappings={data.categoryMappings} hideConfigName />
            <ConfigInsightsSection analyses={entry.analyses} />
            {(entry.rbacResources || []).map((resource, rIdx) => (
              <RBACMatrixSection key={resource.configId || rIdx} resource={resource} />
            ))}
          </React.Fragment>
        ))}

        {data.groupBy !== 'config' && (
          <>
            <CategorizedChangesSection changes={data.changes} categoryMappings={data.categoryMappings} />
            <ConfigInsightsSection analyses={data.analyses} />
          </>
        )}

        {data.relationshipTree
          ? <ConfigTreeSection tree={data.relationshipTree} />
          : <ConfigRelationshipGraph centralConfig={configItem} relationships={data.relationships} relatedConfigs={data.relatedConfigs} />
        }

        {data.groupBy !== 'config' && (
          <>
            <CatalogAccessSection access={data.access} />
            <CatalogAccessLogsSection logs={data.accessLogs} />
          </>
        )}

        {data.groupBy === 'config' && (data.configGroups || []).map((group, idx) => (
          <React.Fragment key={group.configItem.id || idx}>
            <ConfigGroupHeader group={group} />
            <CategorizedChangesSection changes={group.changes} categoryMappings={data.categoryMappings} hideConfigName />
            <ConfigInsightsSection analyses={group.analyses} />
            <CatalogAccessSection access={group.access} />
            <CatalogAccessLogsSection logs={group.accessLogs} />
          </React.Fragment>
        ))}

        {data.configJSON && <ConfigJSONSection json={data.configJSON} />}

        <ArtifactAppendix changes={(data.entries || []).flatMap((e) => e.changes || [])} />
      </Page>

      {data.audit && (
        <Page>
          <AuditPage audit={data.audit} />
        </Page>
      )}
    </Document>
  );
}
