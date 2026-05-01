import React from 'react';
import { Badge } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { ScraperInfo } from '../scraper-types.ts';
import { formatDate } from './utils.ts';
import { GitRefFromSource } from './GitRef.tsx';

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

function Tag({ children }: { children: React.ReactNode }) {
  return (
    <Badge
      variant="custom"
      size="xs"
      shape="rounded"
      label={String(children)}
      color="bg-gray-100"
      textColor="text-gray-500"
      borderColor="border-gray-200"
      className="font-mono"
    />
  );
}

interface Props {
  scraper: ScraperInfo;
}

export default function ScraperCard({ scraper }: Props) {
  return (
    <div className="flex items-center gap-[1.5mm] flex-wrap py-[0.5mm] border-b border-gray-100 leading-none">
      {(scraper.types || []).map((t) => (
        <Icon key={t} name={TYPE_ICONS[t] || t} size={12} />
      ))}
      <span className="text-xs font-semibold text-slate-900">{scraper.name}</span>
      {scraper.source && (
        <Badge
          variant="custom"
          size="xs"
          shape="rounded"
          label={scraper.source}
          color="bg-blue-50"
          textColor="text-blue-700"
          borderColor="border-blue-200"
          className="font-medium"
        />
      )}
      <Tag>{scraper.id.slice(0, 8)}</Tag>
      {scraper.createdBy && <Tag>{scraper.createdBy}</Tag>}
      {scraper.updatedAt
        ? <Tag>modified {formatDate(scraper.updatedAt)}</Tag>
        : scraper.createdAt && <Tag>created {formatDate(scraper.createdAt)}</Tag>
      }
      <GitRefFromSource gitops={scraper.gitops} />
    </div>
  );
}
