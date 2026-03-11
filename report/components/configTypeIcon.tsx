import React from 'react';
import { Icon } from '@flanksource/icons/icon';

export function ConfigTypeIcon({ configType, size = 14 }: { configType: string; size?: number }) {
  return <Icon name={configType} size={size} />;
}
