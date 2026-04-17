import type { ConfigReportData } from '../config-types.ts';
import type { ApplicationChange, ApplicationSection, Application } from '../types.ts';
import type { RBACReport } from '../rbac-types.ts';
import type { CatalogReportData } from '../catalog-report-types.ts';
import type { ViewReportData } from '../view-types.ts';
import type { ScraperInfo } from '../scraper-types.ts';

export interface KitchenSinkData extends ConfigReportData {
  rbacChanges?: ApplicationChange[];
  backupChanges?: ApplicationChange[];
  deploymentChanges?: ApplicationChange[];
  dynamicSections?: ApplicationSection[];
  genericChangesSection?: ApplicationSection;
  dynamicViewSection?: ApplicationSection;
  dynamicConfigsSection?: ApplicationSection;
  scrapers?: ScraperInfo[];
  application?: Application;
  rbacReport?: RBACReport;
  catalogReport?: Partial<CatalogReportData>;
  viewReport?: ViewReportData;
}
