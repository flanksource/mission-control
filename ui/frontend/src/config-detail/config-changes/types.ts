export type ConfigSeverity = "info" | "low" | "medium" | "high" | "critical";

export interface ConfigChangeArtifact {
  id: string;
  filename: string;
  contentType: string;
  size: number;
  checksum?: string;
  path?: string;
  createdAt?: string;
  dataUri?: string;
}

export interface ConfigTypedChange {
  kind: string;
  [key: string]: any;
}

export interface ConfigChange {
  id?: string;
  configID?: string;
  configName?: string;
  configType?: string;
  permalink?: string;
  changeType: string;
  category?: string;
  severity?: ConfigSeverity;
  source?: string;
  summary?: string;
  diff?: string;
  details?: Record<string, any>;
  typedChange?: ConfigTypedChange;
  createdBy?: string;
  externalCreatedBy?: string;
  createdAt?: string;
  firstObserved?: string;
  count?: number;
  artifacts?: ConfigChangeArtifact[];
}

export interface ApplicationPermissionChange {
  user?: string;
  role?: string;
  group?: string;
}

export interface ApplicationChange {
  id: string;
  date: string;
  changeType?: string;
  category?: string;
  source?: string;
  createdBy?: string;
  configId?: string;
  configName?: string;
  configType?: string;
  permission?: ApplicationPermissionChange;
  details?: Record<string, any>;
  typedChange?: ConfigTypedChange;
  description: string;
  status: string;
  createdAt: string;
  updatedAt?: string;
}
