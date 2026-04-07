import React from 'react';
import { Icon } from '@flanksource/icons/icon';

interface PageHeaderProps {
  subtitle?: string;
}

export default function PageHeader({ subtitle }: PageHeaderProps) {
  return (
    <div className="flex items-center justify-between px-[5mm] py-[1mm] bg-[#1e293b] text-white text-xs">
      <Icon name="mission-control-logo-white" size={120} className='h-full w-auto' />
      {subtitle && <span className="text-gray-300">{subtitle}</span>}
    </div>
  );
}
