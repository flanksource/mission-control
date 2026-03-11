export type ViewColumnType =
  | 'string' | 'number' | 'boolean' | 'datetime' | 'duration'
  | 'health' | 'status' | 'gauge' | 'bytes' | 'decimal'
  | 'millicore' | 'config_item' | 'labels' | 'row_attributes' | 'grants';

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

export interface ViewSectionResult {
  title: string;
  icon?: string;
  view?: ViewReportData;
}

export interface ViewReportData {
  namespace?: string;
  name: string;
  title: string;
  icon?: string;
  columns?: ViewColumnDef[];
  rows?: any[][];
  variables?: ViewVariable[];
  sectionResults?: ViewSectionResult[];
}

export interface MultiViewReportData {
  views: ViewReportData[];
}
