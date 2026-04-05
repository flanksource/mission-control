import React from 'react';
import { Page, PageBreak, Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportData } from './catalog-report-types.ts';
import ConfigChangesSection from './components/ConfigChangesSection.tsx';
import ConfigInsightsSection from './components/ConfigInsightsSection.tsx';
import ConfigRelationshipGraph from './components/ConfigRelationshipGraph.tsx';
import CatalogAccessSection from './components/CatalogAccessSection.tsx';
import CatalogAccessLogsSection from './components/CatalogAccessLogsSection.tsx';
import { formatDate, formatDateTime } from './components/utils.ts';

function PageHeader({ title }: { title: string }) {
  return (
    <div className="flex items-center justify-between px-[5mm] py-[1mm] bg-[#1e293b] text-white text-[7pt]">
      <span className="font-semibold">{title}</span>
      <span className="text-gray-300">Catalog Report</span>
    </div>
  );
}

function PageFooter() {
  const now = new Date().toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC');
  return (
    <div className="px-[5mm] py-[1mm] border-t border-gray-200 text-[6pt] text-gray-400 flex items-center justify-between">
      <span>Generated {now}</span>
    </div>
  );
}

function CoverPage({ data }: { data: CatalogReportData }) {
  const ci = data.configItem;
  const tags = { ...ci.tags, ...ci.labels };

  return (
    <div className="flex flex-col items-center justify-center h-full text-center gap-[4mm]">
      <div className="text-[18pt] font-bold text-slate-900">{data.title || 'Catalog Report'}</div>

      {data.parents.length > 0 && (
        <div className="text-[7pt] text-gray-400">
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
        <span className="text-[14pt] font-semibold text-slate-800">{ci.name}</span>
      </div>

      {ci.type && <div className="text-[8pt] text-gray-500">{ci.type}</div>}

      {Object.keys(tags).length > 0 && (
        <div className="flex items-center gap-[1mm] flex-wrap justify-center mt-[2mm]">
          {Object.entries(tags).map(([k, v]) => (
            <span key={k} className="inline-flex items-center border border-blue-300 rounded overflow-hidden text-[5pt] leading-none">
              <span className="px-[0.7mm] py-[0.2mm] font-medium bg-blue-100 text-slate-700">{k}</span>
              <span className="px-[0.7mm] py-[0.2mm] bg-white text-slate-800">{v || '-'}</span>
            </span>
          ))}
        </div>
      )}

      <div className="flex gap-[6mm] text-[7pt] text-gray-500 mt-[4mm]">
        {ci.health && <span>Health: <span className="font-semibold">{ci.health}</span></span>}
        {ci.status && <span>Status: <span className="font-semibold">{ci.status}</span></span>}
        {ci.created_at && <span>Created: {formatDate(ci.created_at)}</span>}
      </div>

      <div className="text-[6pt] text-gray-400 mt-[6mm]">
        Generated {data.generatedAt ? formatDateTime(data.generatedAt) : new Date().toLocaleDateString()}
      </div>

      <div className="flex gap-[4mm] text-[7pt] text-gray-500 mt-[2mm]">
        {data.sections.changes && <span>{data.changes.length} changes</span>}
        {data.sections.insights && <span>{data.analyses.length} insights</span>}
        {data.sections.relationships && <span>{data.relatedConfigs.length} relationships</span>}
        {data.sections.access && <span>{data.access.length} access entries</span>}
        {data.sections.accessLogs && <span>{data.accessLogs.length} access logs</span>}
      </div>
    </div>
  );
}

function ConfigJSONSection({ json }: { json: string }) {
  let formatted = json;
  try {
    formatted = JSON.stringify(JSON.parse(json), null, 2);
  } catch {}

  return (
    <Section variant="hero" title="Config JSON" size="md">
      <pre className="text-[5pt] font-mono bg-gray-50 border border-gray-200 rounded p-[2mm] overflow-hidden whitespace-pre-wrap break-all leading-tight">
        {formatted}
      </pre>
    </Section>
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

      {data.sections.changes && data.changes.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigChangesSection changes={data.changes} />
          </Page>
        </>
      )}

      {data.sections.insights && data.analyses.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigInsightsSection analyses={data.analyses} />
          </Page>
        </>
      )}

      {data.sections.relationships && data.relatedConfigs.length > 0 && (
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

      {data.sections.access && data.access.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <CatalogAccessSection access={data.access} />
          </Page>
        </>
      )}

      {data.sections.accessLogs && data.accessLogs.length > 0 && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <CatalogAccessLogsSection logs={data.accessLogs} />
          </Page>
        </>
      )}

      {data.sections.configJSON && data.configJSON && (
        <>
          <PageBreak />
          <Page {...pageProps}>
            <ConfigJSONSection json={data.configJSON} />
          </Page>
        </>
      )}
    </>
  );
}
