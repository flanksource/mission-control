import type { ConfigTypedChange } from './config-types.ts';

// TypeScript interfaces mirroring api/application-data.yaml

export interface ApplicationProperty {
  label?: string;
  name?: string;
  tooltip?: string;
  icon?: string;
  text?: string;
  order?: number;
  type?: string;
  color?: string;
  value?: number;
  unit?: string; // milliseconds, bytes, millicores, epoch
}

export interface UserAndRole {
  id: string;
  name: string;
  email: string;
  role: string;
  authType: string;
  created: string;
  lastLogin?: string | null;
  lastAccessReview?: string | null;
}

export interface AuthMethodMFA {
  type?: string;
  enforced?: string;
}

export interface AuthMethod {
  name: string;
  type: string;
  mfa?: AuthMethodMFA;
  properties: Record<string, string>;
}

export interface ApplicationAccessControl {
  users: UserAndRole[];
  authentication: AuthMethod[];
}

export interface ApplicationIncident {
  id: string;
  date: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
  description: string;
  status: string;
  resolvedDate?: string | null;
}

export interface ApplicationLocation {
  account: string;
  name: string;
  type: string;
  purpose: string;
  region: string;
  provider: string;
  resourceCount: number;
}

export interface ApplicationBackup {
  id: string;
  database: string;
  type: string;
  source: string;
  date: string;
  size: string;
  status: string;
}

export interface ApplicationBackupRestore {
  id: string;
  database: string;
  date: string;
  source: string;
  status: string;
  completedAt: string;
}

export interface ApplicationFinding {
  id: string;
  type: string;
  severity: 'low' | 'medium' | 'high' | 'critical';
  title: string;
  description: string;
  date: string;
  lastObserved: string;
  status: string;
  remediation?: string;
}

export type ViewColumnType =
  | 'string' | 'number' | 'boolean' | 'datetime' | 'duration'
  | 'health' | 'status' | 'gauge' | 'bytes' | 'decimal'
  | 'millicore' | 'config_item' | 'labels';

export interface ViewColumnDef {
  name: string;
  type: ViewColumnType;
  primaryKey?: boolean;
  hidden?: boolean;
  width?: string;
  description?: string;
}

export interface ViewVariable {
  key: string;
  label: string;
  default?: string;
  options: string[];
}

export interface ColumnFilterOptions {
  list?: string[];
  labels?: Record<string, string[]>;
}

export interface ApplicationViewData {
  refreshStatus?: 'fresh' | 'cache' | 'error';
  lastRefreshedAt?: string;
  columns?: ViewColumnDef[];
  rows?: any[][];
  panels?: object[];
  variables?: ViewVariable[];
  columnOptions?: Record<string, ColumnFilterOptions>;
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

export interface ApplicationConfigItem {
  id: string;
  name: string;
  type?: string;
  status?: string;
  health?: string;
  labels?: Record<string, string>;
}

export interface ApplicationSection {
  type: 'view' | 'changes' | 'configs';
  title: string;
  icon?: string;
  view?: ApplicationViewData;
  changes?: ApplicationChange[];
  configs?: ApplicationConfigItem[];
}

export interface Application {
  id: string;
  name: string;
  type: string;
  namespace: string;
  description?: string;
  createdAt?: string;
  properties?: ApplicationProperty[];
  accessControl: ApplicationAccessControl;
  incidents: ApplicationIncident[];
  locations: ApplicationLocation[];
  backups: ApplicationBackup[];
  restores: ApplicationBackupRestore[];
  findings: ApplicationFinding[];
  sections: ApplicationSection[];
}
