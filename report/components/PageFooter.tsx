import React from 'react';
import { formatDateTime } from './utils.ts';

interface PageFooterProps {
  publicURL?: string;
  generatedAt?: string;
  children?: React.ReactNode;
}

export default function PageFooter({ publicURL, generatedAt, children }: PageFooterProps) {
  const date = generatedAt ? formatDateTime(generatedAt) : formatDateTime(new Date().toISOString());

  return (
    <div className="px-[5mm] py-[1mm] border-t border-gray-200 text-xs text-gray-400">
      {children}
      <div className="flex items-center justify-between">
        <span>Generated {date}</span>
        {publicURL && (
          <a href={publicURL} className="text-blue-500" style={{ textDecoration: 'none' }}>{publicURL}</a>
        )}
      </div>
    </div>
  );
}
