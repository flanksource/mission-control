import React from 'react';
import { Badge } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';

interface GitRefProps {
  url?: string;
  branch?: string;
  file?: string;
  dir?: string;
  link?: string;
  size?: 'xs' | 'sm';
}

const SIZE_CLASSES = {
  xs: 'text-[5pt]',
  sm: 'text-xs',
};

function Tag({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return (
    <Badge
      variant="custom"
      size="xs"
      shape="rounded"
      label={String(children)}
      color="bg-gray-100"
      textColor="text-gray-600"
      borderColor="border-gray-200"
      className={`font-mono ${className}`}
    />
  );
}

export default function GitRef({ url, branch, file, dir, link, size = 'xs' }: GitRefProps) {
  if (!url && !file) return null;
  const textClass = SIZE_CLASSES[size];

  const content = (
    <span className={`inline-flex items-center gap-[1mm] flex-wrap ${textClass}`}>
      <Icon name="Git" size={size === 'xs' ? 10 : 12} />
      {url && <Tag className={textClass}>{url}{branch ? ` @ ${branch}` : ''}</Tag>}
      {dir && <Tag className={`text-gray-400 ${textClass}`}>{dir}/</Tag>}
      {file && <Tag className={`text-gray-400 ${textClass}`}>{file}</Tag>}
    </span>
  );

  if (link) {
    return <a href={link} target="_blank" rel="noopener noreferrer">{content}</a>;
  }
  return content;
}

export function GitRefFromSource({ gitops, size }: { gitops?: { git: { url: string; branch: string; file: string; dir: string; link: string } }; size?: 'xs' | 'sm' }) {
  if (!gitops?.git?.url) return null;
  return <GitRef url={gitops.git.url} branch={gitops.git.branch} file={gitops.git.file} dir={gitops.git.dir} link={gitops.git.link} size={size} />;
}
