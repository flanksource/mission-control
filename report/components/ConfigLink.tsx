import React from 'react';
import { Icon } from '@flanksource/icons/icon';
import { HEALTH_COLORS } from './utils.ts';
import type { ConfigItem } from '../config-types.ts';

interface Props {
  config: Pick<ConfigItem, 'name' | 'type' | 'health'>;
  showHealth?: boolean;
}

export default function ConfigLink({ config, showHealth }: Props) {
  return (
    <span className="inline-flex items-center gap-[0.5mm]">
      {config.type && <Icon name={config.type} className="w-[3mm] h-[3mm]" />}
      {showHealth && config.health && (
        <span
          className="inline-block w-[1.5mm] h-[1.5mm] rounded-full"
          style={{ backgroundColor: HEALTH_COLORS[config.health] ?? '#6B7280' }}
        />
      )}
      {config.name}
    </span>
  );
}
