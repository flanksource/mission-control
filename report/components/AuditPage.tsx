import React from 'react';
import { Section, ListTable } from '@flanksource/facet';
import type { CatalogReportAudit } from '../catalog-report-types.ts';
import ScraperCard from './ScraperCard.tsx';

interface Props {
  audit: CatalogReportAudit;
}

function MetadataRow({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return (
    <tr>
      <td className="text-xs text-gray-500 pr-[4mm] py-[0.5mm] align-top whitespace-nowrap">{label}</td>
      <td className="text-xs text-slate-800 py-[0.5mm] font-mono">{value}</td>
    </tr>
  );
}

function SectionBadge({ label, enabled }: { label: string; enabled: boolean }) {
  return (
    <span className={`inline-flex items-center px-[1.5mm] py-[0.3mm] rounded text-[5.5pt] font-medium border ${enabled ? 'bg-blue-50 text-blue-700 border-blue-200' : 'bg-gray-50 text-gray-400 border-gray-200'}`}>
      {label}
    </span>
  );
}

export default function AuditPage({ audit }: Props) {
  const opts = audit.options;
  const sections = opts.sections;

  return (
    <>
      <Section variant="hero" title="Audit" size="md">
        <table>
          <tbody>
            <MetadataRow label="Version" value={audit.buildVersion} />
            <MetadataRow label="Commit" value={audit.buildCommit} />
            <MetadataRow label="Title" value={opts.title} />
            <MetadataRow label="Since" value={opts.since} />
            <MetadataRow label="Group By" value={opts.groupBy} />
            <MetadataRow label="Recursive" value={opts.recursive ? 'yes' : 'no'} />
            <MetadataRow label="Artifacts" value={opts.changeArtifacts ? 'yes' : 'no'} />
            {opts.thresholds && (
              <>
                <MetadataRow label="Stale Days" value={String(opts.thresholds.staleDays)} />
                <MetadataRow label="Review Overdue" value={String(opts.thresholds.reviewOverdueDays)} />
              </>
            )}
          </tbody>
        </table>

        <div className="flex gap-[1.5mm] mt-[2mm] flex-wrap">
          <SectionBadge label="Changes" enabled={sections.changes} />
          <SectionBadge label="Insights" enabled={sections.insights} />
          <SectionBadge label="Relationships" enabled={sections.relationships} />
          <SectionBadge label="Access" enabled={sections.access} />
          <SectionBadge label="Access Logs" enabled={sections.accessLogs} />
          <SectionBadge label="Config JSON" enabled={sections.configJSON} />
        </div>

        {(opts.filters || []).length > 0 && (
          <div className="mt-[2mm]">
            <div className="text-xs text-gray-500 mb-[0.5mm]">Filters</div>
            {opts.filters!.map((f, i) => (
              <div key={i} className="text-xs font-mono text-slate-700">{f}</div>
            ))}
          </div>
        )}

        {opts.categoryMappings && Object.keys(opts.categoryMappings).length > 0 && (
          <div className="mt-[2mm]">
            <div className="text-xs text-gray-500 mb-[0.5mm]">Category Mappings</div>
            {Object.entries(opts.categoryMappings).map(([key, values]) => (
              <div key={key} className="text-xs font-mono text-slate-700">
                <span className="text-blue-600">{key}</span>: {values.join(', ')}
              </div>
            ))}
          </div>
        )}
      </Section>

      {audit.gitStatus && (
        <Section variant="hero" title="Git Status" size="md">
          <pre className="text-xs font-mono bg-gray-50 border border-gray-200 rounded p-[2mm] whitespace-pre-wrap break-all leading-tight">
            {audit.gitStatus}
          </pre>
        </Section>
      )}

      {audit.queries.length > 0 && (
        <Section variant="hero" title="Queries" size="md">
          <ListTable
            rows={audit.queries.map((q, i) => ({
              id: String(i),
              subject: q.pretty,
              count: String(q.count),
            }))}
            subject="subject"
            keys={['count']}
            size="xs"
            density="compact"
            wrap
            cellClassName="text-[8pt] font-mono"
          />
        </Section>
      )}

      {audit.scrapers.length > 0 && (
        <Section variant="hero" title="Scrapers" size="md">
          <div className="flex flex-col gap-[2mm]">
            {audit.scrapers.map((s) => (
              <ScraperCard key={s.id} scraper={s} />
            ))}
          </div>
        </Section>
      )}
    </>
  );
}
