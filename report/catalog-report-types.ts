import type { ConfigChange, ConfigAnalysis, ConfigRelationship, ConfigItem } from './config-types.ts';

export interface CatalogReportSections {
  changes: boolean;
  insights: boolean;
  relationships: boolean;
  access: boolean;
  accessLogs: boolean;
  configJSON: boolean;
}

export interface CatalogReportAccess {
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
  userId: string;
  userName: string;
  configName: string;
  configType: string;
  createdAt: string;
  mfa: boolean;
  count: number;
  properties?: Record<string, string>;
}

export interface CatalogReportData {
  title: string;
  generatedAt: string;
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
}
