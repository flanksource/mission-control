import React from 'react';
import { Icon } from '@flanksource/icons/icon';
import type { RBACReport, ConfigItem } from '../rbac-types.ts';

function ConfigBadge({ config }: { config: ConfigItem }) {
  return (
    <div className="inline-flex items-center gap-[1mm] text-[9pt] text-slate-700">
      {config.type && <Icon name={config.type} size={16} />}
      <span className="font-semibold">{config.name}</span>
      <span className="text-gray-400 text-[7pt]">{config.type}</span>
      {config.status && (
        <span className="ml-[1mm] px-[1.5mm] py-[0.3mm] rounded text-[6pt] font-medium bg-gray-100 text-gray-600">
          {config.status}
        </span>
      )}
    </div>
  );
}

function TagBadges({ tags, labels }: { tags?: Record<string, string>; labels?: Record<string, string> }) {
  const all = { ...tags, ...labels };
  if (Object.keys(all).length === 0) return null;
  return (
    <div className="flex flex-wrap justify-center gap-[1mm] mt-[2mm]">
      {Object.entries(all).map(([k, v]) => (
        <span key={k} className="inline-flex items-center border border-blue-200 rounded overflow-hidden text-[5.5pt]" style={{ whiteSpace: 'nowrap' }}>
          <span className="px-[0.5mm] font-medium bg-blue-50 text-slate-600">{k}</span>
          <span className="px-[0.5mm] bg-white text-slate-900">{v || '-'}</span>
        </span>
      ))}
    </div>
  );
}

interface Props {
  report: RBACReport;
  subtitle: string;
}

export default function RBACCoverContent({ report, subtitle }: Props) {
  const now = new Date().toISOString().replace('T', ' ').replace(/\.\d+Z$/, ' UTC');
  const { subject, parents } = report;

  return (
    <div className="flex flex-col justify-center items-center h-full min-h-[140mm] text-center px-[20mm]">
      <div className="mb-[8mm]">
        <div className="text-[8pt] font-medium text-blue-600 uppercase tracking-widest mb-[3mm]">
          {subtitle}
        </div>
        <h1 className="text-[36pt] font-bold text-slate-900 leading-tight mb-[4mm]">
          {report.title}
        </h1>
        {report.query && (
          <div className="text-[10pt] text-gray-500 font-mono bg-gray-50 rounded px-[4mm] py-[2mm] mt-[4mm]">
            {report.query}
          </div>
        )}
      </div>

      {subject && (
        <div className="mb-[6mm]">
          {parents && parents.length > 0 && (
            <div className="flex items-center justify-center gap-[1mm] text-[7pt] text-gray-400 mb-[2mm]">
              {parents.map((p, i) => (
                <React.Fragment key={p.id}>
                  {i > 0 && <span className="mx-[0.5mm]">&rsaquo;</span>}
                  <span className="inline-flex items-center gap-[0.5mm]">
                    {p.type && <Icon name={p.type} size={10} />}
                    {p.name}
                  </span>
                </React.Fragment>
              ))}
            </div>
          )}
          <ConfigBadge config={subject} />
          {subject.description && (
            <div className="text-[8pt] text-gray-500 italic mt-[2mm] max-w-[120mm]">
              {subject.description}
            </div>
          )}
          <TagBadges tags={subject.tags} labels={subject.labels} />
        </div>
      )}

      <div className="w-[40mm] h-[0.3mm] mb-[6mm] bg-blue-600" />

      <div className="text-[8pt] text-gray-400 mb-[2mm]">
        {report.summary.totalResources} resources · {report.summary.totalUsers} users · {report.summary.staleAccessCount} stale
      </div>
      <div className="text-[8pt] text-gray-400">Generated {now}</div>
    </div>
  );
}
