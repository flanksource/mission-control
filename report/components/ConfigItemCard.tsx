import React from 'react';
import { Icon } from '@flanksource/icons/icon';
import type { ConfigItem } from '../rbac-types.ts';
import { formatDate } from './utils.ts';

interface Props {
  config: ConfigItem;
}

export default function ConfigItemCard({ config }: Props) {
  const tags = { ...config.tags, ...config.labels };

  return (
    <div>
      <div className="flex items-center gap-[1mm] flex-wrap">
        {config.type && <Icon name={config.type} size={14} />}
        <span className="text-[8pt] font-semibold text-slate-900">{config.name}</span>
        {Object.entries(tags).map(([k, v]) => (
          <span key={k} className="inline-flex items-center self-center border border-blue-300 rounded overflow-hidden text-[5pt] leading-none" style={{ whiteSpace: 'nowrap' }}>
            <span className="px-[0.7mm] py-[0.2mm] font-medium bg-blue-100 text-slate-700">{k}</span>
            <span className="px-[0.7mm] py-[0.2mm] bg-white text-slate-800">{v || '-'}</span>
          </span>
        ))}
      </div>
      <div className="flex items-center gap-[3mm] text-[4.5pt] text-gray-500 mt-[1mm] ml-[5mm]">
        <span className="font-mono text-gray-400">{config.id}</span>
        {config.created_at && <span><span className="font-semibold">created:</span> {formatDate(config.created_at)}</span>}
        {config.updated_at && <span><span className="font-semibold">updated:</span> {formatDate(config.updated_at)}</span>}
      </div>
    </div>
  );
}
