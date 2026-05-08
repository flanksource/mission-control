import './icon-setup.ts';
import React from 'react';
import { Document, Page, Header, Footer, Section, PageNo } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import { MissionControlLogo } from '@flanksource/icons/mi';
import type { CatalogReportData, CatalogReportConfigGroup } from './catalog-report-types.ts';
import type { ConfigChange } from './config-types.ts';
import ConfigChangesSection from './components/ConfigChangesSection.tsx';
import ConfigInsightsSection from './components/ConfigInsightsSection.tsx';
import { defaultConfigChangesExtensions } from './components/config-changes-builtin-extensions.tsx';
import CatalogAccessLogsSection from './components/CatalogAccessLogsSection.tsx';
import RBACMatrixSection from './components/RBACMatrixSection.tsx';
import ArtifactAppendix from './components/ArtifactAppendix.tsx';
import AuditPage from './components/AuditPage.tsx';
import CoverPage from './components/CoverPage.tsx';
import CatalogList from './components/CatalogList.tsx';
import { formatDateTime } from './components/utils.ts';

const catalogReportCss = `
  .datasheet-header--solid .header-meta p { font-size:9pt; color:#cbd5e1; margin:0; }
`;

function CatalogCoverPage({ data }: { data: CatalogReportData }) {
  const ci = data.configItem || {};
  const stats: Array<{ label: string; value: number }> = [];
  if (data.sections?.changes) stats.push({ label: 'changes', value: (data.changes || []).length });
  if (data.sections?.insights) stats.push({ label: 'insights', value: (data.analyses || []).length });
  if (data.sections?.relationships) stats.push({ label: 'relationships', value: (data.relatedConfigs || []).length });
  if (data.sections?.accessLogs) stats.push({ label: 'access logs', value: (data.accessLogs || []).length });

  return (
    <>
      <div className="flex justify-center pt-[10mm] pb-[6mm]">
        <MissionControlLogo className="h-[20mm] w-auto" />
      </div>
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
    </>
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

function CategorizedChangesSection({ changes, hideConfigName }: {
  changes?: ConfigChange[];
  hideConfigName?: boolean;
}) {
  return (
    <ConfigChangesSection
      changes={changes}
      hideConfigName={hideConfigName}
      extensions={defaultConfigChangesExtensions}
    />
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
  const generated = formatDateTime(data.generatedAt ?? new Date().toISOString());
  return (
    <Document pageSize="a4" margins={{ top: 1, bottom: 1, left: 5, right: 5 }} css={catalogReportCss}>
      <Header type="first" height={0}>
        <></>
      </Header>
      <Header
        variant="solid"
        className="bg-slate-800"
        height={14}
        logo={<MissionControlLogo className="filter grayscale brightness-[250] contrast-100 mix-blend-screen h-[6mm] w-auto" />}
        subtitle="Catalog Report"
      />
      <Footer height={8}>
        <div className="px-[5mm] py-[1mm] border-t border-gray-200 text-xs text-gray-400 flex items-center justify-between gap-[4mm]">
          <span>Generated {generated}</span>
          <PageNo />
          {data.publicURL && (
            <a href={data.publicURL} className="text-blue-500" style={{ textDecoration: 'none' }}>{data.publicURL}</a>
          )}
        </div>
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <CatalogCoverPage data={data} />
      </Page>

      <Page>
        <CatalogList entries={data.entries} groupBy={data.groupBy} relationshipTree={data.relationshipTree} />

        {data.groupBy === 'config' && (data.entries || []).map((entry, idx) => (
          <React.Fragment key={entry.configItem?.id || idx}>
            <ConfigGroupHeader group={{ configItem: entry.configItem as any, changes: entry.changes, analyses: entry.analyses, access: entry.access, accessLogs: entry.accessLogs }} />
            <CategorizedChangesSection changes={entry.changes} hideConfigName />
            <ConfigInsightsSection analyses={entry.analyses} />
            {(entry.rbacResources || []).map((resource, rIdx) => (
              <RBACMatrixSection key={resource.configId || rIdx} resource={resource} />
            ))}
          </React.Fragment>
        ))}

        {data.groupBy !== 'config' && (
          <>
            {data.sections?.changes && <CategorizedChangesSection changes={data.changes} />}
            {data.sections?.insights && <ConfigInsightsSection analyses={data.analyses} />}
          </>
        )}

        {data.groupBy !== 'config' && data.sections?.accessLogs && (
          <CatalogAccessLogsSection logs={data.accessLogs} />
        )}

        {data.groupBy === 'config' && (data.configGroups || []).map((group, idx) => (
          <React.Fragment key={group.configItem.id || idx}>
            <ConfigGroupHeader group={group} />
            {data.sections?.changes && <CategorizedChangesSection changes={group.changes} hideConfigName />}
            {data.sections?.insights && <ConfigInsightsSection analyses={group.analyses} />}
            {data.sections?.accessLogs && <CatalogAccessLogsSection logs={group.accessLogs} />}
          </React.Fragment>
        ))}

        {data.sections?.configJSON && data.configJSON && <ConfigJSONSection json={data.configJSON} />}

        <ArtifactAppendix changes={[
          ...(data.changes ?? []),
          ...(data.entries ?? []).flatMap((e) => e.changes ?? []),
          ...(data.configGroups ?? []).flatMap((g) => g.changes ?? []),
        ]} />
      </Page>

      {data.audit && (
        <Page>
          <AuditPage audit={data.audit} />
        </Page>
      )}
    </Document>
  );
}
