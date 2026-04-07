import React from 'react';
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
    <span className={`inline-flex items-center px-[1mm] py-[0.2mm] rounded bg-gray-100 text-gray-600 font-mono border border-gray-200 leading-none ${className}`}>
      {children}
    </span>
  );
}

export default function GitRef({ url, branch, file, dir, link, size = 'xs' }: GitRefProps) {
  if (!url && !file) return null;
  const textClass = SIZE_CLASSES[size];

  return (
    <span className={`inline-flex items-center gap-[1mm] flex-wrap ${textClass}`}>
      <Icon name="Git" size={size === 'xs' ? 10 : 12} />
      {url && <Tag className={textClass}>{url}{branch ? ` @ ${branch}` : ''}</Tag>}
      {dir && <Tag className={`text-gray-400 ${textClass}`}>{dir}/</Tag>}
      {file && <Tag className={`text-gray-400 ${textClass}`}>{file}</Tag>}
    </span>
  );
}

export function GitRefFromSource({ gitops, size }: { gitops?: { git: { url: string; branch: string; file: string; dir: string; link: string } }; size?: 'xs' | 'sm' }) {
  if (!gitops?.git?.url) return null;
  return <GitRef url={gitops.git.url} branch={gitops.git.branch} file={gitops.git.file} dir={gitops.git.dir} link={gitops.git.link} size={size} />;
}
