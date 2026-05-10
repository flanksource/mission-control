import type React from 'react';
import type { ConfigChange } from '../config-types.ts';

export interface ConfigChangesExtensionProps {
  changes: ConfigChange[];
}

export interface ConfigChangesExtension {
  key: string;
  title?: string;
  filter: (change: ConfigChange) => boolean;
  panel: React.ComponentType<ConfigChangesExtensionProps>;
  drop?: boolean;
}
