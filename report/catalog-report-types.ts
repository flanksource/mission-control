import type { ConfigChange, ConfigAnalysis, ConfigRelationship, ConfigItem } from './config-types.ts';
import type { RBACResource } from './rbac-types.ts';
import type { ScraperInfo } from './scraper-types.ts';

export interface CatalogReportCategoryMapping {
  category?: string;
  filter: string;
  transform?: string;
}

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

export interface QueryLogEntry {
  name: string;
  args?: string;
  count: number;
  duration: number;
  error?: string;
  summary?: string;
  pretty: string;
}

export interface CatalogReportOptions {
  title: string;
  since: string;
  sections: CatalogReportSections;
  recursive: boolean;
  groupBy: string;
  changeArtifacts: boolean;
  filters?: string[];
  thresholds?: { staleDays: number; reviewOverdueDays: number };
  categoryMappings?: CatalogReportCategoryMapping[];
}

export interface CatalogReportGroupMember {
  userId: string;
  name: string;
  email?: string;
  userType?: string;
  lastSignedInAt?: string;
  membershipAddedAt: string;
  membershipDeletedAt?: string;
}

export interface CatalogReportGroup {
  id: string;
  name: string;
  groupType?: string;
  members: CatalogReportGroupMember[];
}

export interface CatalogReportAudit {
  buildCommit: string;
  buildVersion: string;
  gitStatus?: string;
  options: CatalogReportOptions;
  scrapers: ScraperInfo[];
  queries: QueryLogEntry[];
  groups: CatalogReportGroup[];
}

export interface CatalogReportData {
  title: string;
  generatedAt: string;
  publicURL?: string;
  from?: string;
  to?: string;
  recursive?: boolean;
  groupBy?: string;
  categoryMappings?: CatalogReportCategoryMapping[];
  thresholds?: { staleDays?: number; reviewOverdueDays?: number };
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
  audit?: CatalogReportAudit;
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
