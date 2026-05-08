import { Icon } from './icon.tsx';

export function ConfigTypeIcon({ configType, size = 14 }: { configType: string; size?: number }) {
  return <Icon name={configType} size={size} />;
}
