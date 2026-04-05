import React from 'react';
import { Icon } from '@flanksource/icons/icon';
import type { ScraperInfo } from '../scraper-types.ts';
import { formatDate } from './utils.ts';

const TYPE_ICONS: Record<string, string> = {
  kubernetes: 'Kubernetes',
  aws: 'AWS',
  azure: 'Azure',
  gcp: 'GCP',
  file: 'file',
  sql: 'database',
  http: 'http',
  trivy: 'trivy',
  terraform: 'Terraform',
  githubActions: 'GitHub',
  slack: 'Slack',
  kubernetesFile: 'Kubernetes',
};

interface Props {
  scraper: ScraperInfo;
}

export default function ScraperCard({ scraper }: Props) {
  const gitops = scraper.gitops;
  const hashShort = scraper.specHash ? scraper.specHash.slice(0, 12) : '';

  return (
    <div className="border border-gray-200 rounded-[1.5mm] p-[3mm]">
      {/* Header: icons + name */}
      <div className="flex items-center gap-[1.5mm] flex-wrap">
        {scraper.types.map((t) => (
          <Icon key={t} name={TYPE_ICONS[t] || t} size={16} />
        ))}
        <span className="text-[9pt] font-semibold text-slate-900">{scraper.name}</span>
        {scraper.source && (
          <span className="inline-flex items-center px-[1.5mm] py-[0.3mm] rounded text-[5.5pt] font-medium bg-blue-50 text-blue-700 border border-blue-200">
            {scraper.source}
          </span>
        )}
      </div>

      {/* Metadata row */}
      <div className="flex items-center gap-[1.5mm] mt-[1.5mm] flex-wrap leading-none">
        {hashShort && (
          <span className="inline-flex items-center px-[1mm] py-[0.3mm] rounded bg-gray-100 text-gray-500 text-[5pt] font-mono border border-gray-200 leading-none">
            sha256:{hashShort}
          </span>
        )}
        {scraper.createdBy && (
          <span className="inline-flex items-center px-[1mm] py-[0.3mm] rounded bg-gray-100 text-gray-500 text-[5pt] border border-gray-200 leading-none">
            {scraper.createdBy}
          </span>
        )}
        {scraper.createdAt && (
          <span className="inline-flex items-center px-[1mm] py-[0.3mm] rounded bg-gray-100 text-gray-500 text-[5pt] border border-gray-200 leading-none">
            created {formatDate(scraper.createdAt)}
          </span>
        )}
        {scraper.updatedAt && (
          <span className="inline-flex items-center px-[1mm] py-[0.3mm] rounded bg-gray-100 text-gray-500 text-[5pt] border border-gray-200 leading-none">
            updated {formatDate(scraper.updatedAt)}
          </span>
        )}
      </div>

      {/* GitOps provenance */}
      {gitops && gitops.git.url && (
        <div className="flex items-center gap-[1.5mm] mt-[1mm] flex-wrap leading-none">
          <Icon name="Git" size={10} />
          <span className="inline-flex items-center px-[1mm] py-[0.3mm] rounded bg-gray-100 text-gray-600 text-[5pt] font-mono border border-gray-200 leading-none">
            {gitops.git.url}
          </span>
          {gitops.git.branch && (
            <span className="inline-flex items-center px-[1mm] py-[0.3mm] rounded bg-gray-100 text-gray-600 text-[5pt] font-mono border border-gray-200 leading-none">
              {gitops.git.branch}
            </span>
          )}
          {gitops.git.file && (
            <span className="inline-flex items-center px-[1mm] py-[0.3mm] rounded bg-gray-100 text-gray-400 text-[5pt] font-mono border border-gray-200 leading-none">
              {gitops.git.file}
            </span>
          )}
        </div>
      )}
    </div>
  );
}
