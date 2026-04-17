export type ConfigSeverity = 'info' | 'low' | 'medium' | 'high' | 'critical';
export type ConfigHealth = 'healthy' | 'warning' | 'unhealthy' | 'unknown';
export type AnalysisType =
  | 'security' | 'compliance' | 'cost' | 'performance'
  | 'reliability' | 'recommendation' | 'integration' | 'availability';

export interface ConfigItem {
  id: string;
  name: string;
  type?: string;
  configClass?: string;
  status?: string;
  health?: ConfigHealth;
  description?: string;
  permalink?: string;
  labels?: Record<string, string>;
  tags?: Record<string, string>;
  costTotal30d?: number;
  createdAt?: string;
  updatedAt?: string;
}

export interface ConfigChangeArtifact {
  id: string;
  filename: string;
  contentType: string;
  size: number;
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
  details?: Record<string, any>;
  typedChange?: ConfigTypedChange;
  createdBy?: string;
  externalCreatedBy?: string;
  createdAt?: string;
  firstObserved?: string;
  count?: number;
  artifacts?: ConfigChangeArtifact[];
}

export interface ConfigAnalysis {
  id?: string;
  configID?: string;
  configName?: string;
  configType?: string;
  permalink?: string;
  analyzer: string;
  message?: string;
  summary?: string;
  status?: string;
  severity?: ConfigSeverity;
  analysisType?: AnalysisType;
  source?: string;
  firstObserved?: string;
  lastObserved?: string;
}

export interface ConfigRelationship {
  configID: string;
  relatedID: string;
  relation: string;
  direction?: 'incoming' | 'outgoing';
}

export interface ConfigReportData {
  configItem: ConfigItem;
  changes: ConfigChange[];
  analyses: ConfigAnalysis[];
  relationships: ConfigRelationship[];
  relatedConfigs: ConfigItem[];
}
