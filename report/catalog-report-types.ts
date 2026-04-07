import type { ConfigChange, ConfigAnalysis, ConfigRelationship, ConfigItem } from './config-types.ts';
import type { RBACResource } from './rbac-types.ts';

export interface CatalogReportSections {
  changes: boolean;
  insights: boolean;
  relationships: boolean;
  access: boolean;
  accessLogs: boolean;
  configJSON: boolean;
}

export interface CatalogReportAccess {
  configId?: string;
  configName?: string;
  configType?: string;
  permalink?: string;
  userId: string;
  userName: string;
  email: string;
  role: string;
  userType: string;
  createdAt: string;
  lastSignedInAt?: string;
  lastReviewedAt?: string;
}

export interface CatalogReportAccessLog {
  configId?: string;
  permalink?: string;
  userId: string;
  userName: string;
  configName: string;
  configType: string;
  createdAt: string;
  mfa: boolean;
  count: number;
  properties?: Record<string, string>;
}

export interface CatalogReportTreeNode extends ConfigItem {
  edgeType?: 'parent' | 'child' | 'related' | 'target';
  relation?: string;
  permalink?: string;
  children?: CatalogReportTreeNode[];
}

export interface CatalogReportConfigGroup {
  configItem: ConfigItem;
  changes: ConfigChange[];
  analyses: ConfigAnalysis[];
  access: CatalogReportAccess[];
  accessLogs: CatalogReportAccessLog[];
}

export interface CatalogReportData {
  title: string;
  generatedAt: string;
  publicURL?: string;
  from?: string;
  to?: string;
  recursive?: boolean;
  groupBy?: string;
  configItem: ConfigItem & {
    config?: string;
    name: string;
    type?: string;
    id: string;
    tags?: Record<string, string>;
    labels?: Record<string, string>;
    parent_id?: string;
    created_at?: string;
    updated_at?: string;
  };
  parents: Array<{
    id: string;
    name: string;
    type?: string;
  }>;
  sections: CatalogReportSections;
  changes: ConfigChange[];
  analyses: ConfigAnalysis[];
  relationships: ConfigRelationship[];
  relatedConfigs: ConfigItem[];
  access: CatalogReportAccess[];
  accessLogs: CatalogReportAccessLog[];
  configJSON?: string;
  configGroups?: CatalogReportConfigGroup[];
  relationshipTree?: CatalogReportTreeNode;
  entries?: CatalogReportEntry[];
}

export interface CatalogReportEntry {
  configItem: ConfigItem & { permalink?: string };
  parents?: Array<ConfigItem & { permalink?: string }>;
  relationshipTree?: CatalogReportTreeNode;
  changeCount: number;
  insightCount: number;
  accessCount: number;
  rbacResources?: RBACResource[];
  changes: ConfigChange[];
  analyses: ConfigAnalysis[];
  access: CatalogReportAccess[];
  accessLogs: CatalogReportAccessLog[];
}
