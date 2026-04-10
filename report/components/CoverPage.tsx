import React from 'react';
import { Badge } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import { formatDate, formatDateTime } from './utils.ts';

interface CoverPageSubject {
  name?: string;
  type?: string;
  status?: string;
  health?: string;
  description?: string;
  tags?: Record<string, string>;
  labels?: Record<string, string>;
}

interface CoverPageProps {
  title: string;
  subtitle?: string;
  icon?: string;
  query?: string;
  breadcrumbs?: Array<{ id: string; name?: string; type?: string }>;
  subjects?: CoverPageSubject[];
  tags?: Record<string, string>;
  stats?: Array<{ label: string; value: string | number }>;
  dateRange?: { from?: string; to?: string };
  generatedAt?: string;
  children?: React.ReactNode;
}

function SubjectBadge({ subject }: { subject: CoverPageSubject }) {
  return (
    <div className="inline-flex items-center gap-[1mm] text-sm text-slate-700">
      {subject.type && <Icon name={subject.type} size={16} />}
      <span className="font-semibold">{subject.name}</span>
      <span className="text-gray-400 text-xs">{subject.type}</span>
      {subject.status && (
        <Badge variant="custom" size="xs" shape="rounded" label={subject.status} color="bg-gray-100" textColor="text-gray-600" borderColor="border-gray-200" className="ml-[1mm] font-medium" />
      )}
    </div>
  );
}

function TagBadges({ tags }: { tags: Record<string, string> }) {
  if (Object.keys(tags).length === 0) return null;
  return (
    <div className="flex flex-wrap justify-center gap-[1mm] mt-[2mm]">
      {Object.entries(tags).map(([k, v]) => (
        <Badge
          key={k}
          variant="label"
          size="xs"
          shape="rounded"
          label={k}
          value={v || '-'}
          color="bg-blue-100"
          textColor="text-slate-700"
          className="bg-white"
        />
      ))}
    </div>
  );
}

export default function CoverPage({ title, subtitle, icon, query, breadcrumbs, subjects, tags, stats, dateRange, generatedAt, children }: CoverPageProps) {
  const allTags = tags || {};
  if (!tags && subjects?.length === 1) {
    Object.assign(allTags, subjects[0].tags || {}, subjects[0].labels || {});
  }

  return (
    <div className="flex flex-col justify-center items-center h-full min-h-[140mm] text-center px-[20mm]">
      <div className="mb-[8mm]">
        {subtitle && (
          <div className="text-sm font-medium text-blue-600 uppercase tracking-widest mb-[3mm]">
            {subtitle}
          </div>
        )}

        {icon && (
          <div className="mb-[4mm] flex justify-center">
            <Icon name={icon} className="w-[16mm] h-[16mm]" />
          </div>
        )}

        <h1 className="text-4xl font-bold text-slate-900 leading-tight mb-[4mm]">{title}</h1>

        {query && (
          <div className="text-base text-gray-500 font-mono bg-gray-50 rounded px-[4mm] py-[2mm] mt-[4mm]">
            {query}
          </div>
        )}
      </div>

      {breadcrumbs && breadcrumbs.length > 0 && (
        <div className="flex items-center justify-center gap-[1mm] text-xs text-gray-400 mb-[2mm]">
          {breadcrumbs.map((p, i) => (
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

      {subjects && subjects.length > 0 && (
        <div className="mb-[6mm]">
          {subjects.map((s, i) => (
            <SubjectBadge key={i} subject={s} />
          ))}
          {subjects.length === 1 && subjects[0].description && (
            <div className="text-sm text-gray-500 italic mt-[2mm] max-w-[120mm]">
              {subjects[0].description}
            </div>
          )}
        </div>
      )}

      {Object.keys(allTags).length > 0 && <TagBadges tags={allTags} />}

      <div className="w-[40mm] h-[0.3mm] mb-[6mm] mt-[4mm] bg-blue-600" />

      {stats && stats.length > 0 && (
        <div className="flex gap-[4mm] text-xs text-gray-500 mb-[2mm]">
          {stats.map((s) => (
            <span key={s.label}>{s.value} {s.label}</span>
          ))}
        </div>
      )}

      {dateRange && (dateRange.from || dateRange.to) && (
        <div className="text-xs text-gray-500 mb-[2mm]">
          Period: {formatDate(dateRange.from || generatedAt || new Date().toISOString())} – {formatDate(dateRange.to || generatedAt || new Date().toISOString())}
        </div>
      )}

      <div className="text-sm text-gray-400">
        Generated {generatedAt ? formatDateTime(generatedAt) : new Date().toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric' })}
      </div>

      {children}
    </div>
  );
}
