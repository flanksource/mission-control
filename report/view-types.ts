export type ViewColumnType =
  | 'string' | 'number' | 'boolean' | 'datetime' | 'duration'
  | 'health' | 'status' | 'gauge' | 'bytes' | 'decimal'
  | 'millicore' | 'config_item' | 'labels' | 'row_attributes' | 'grants';

export interface GaugeThreshold {
  percent: number;
  color: string;
}

export interface GaugeConfig {
  max?: string;
  min?: string;
  precision?: number;
  thresholds?: GaugeThreshold[];
}

export interface BadgeColorSource {
  auto?: boolean;
  map?: Record<string, string>;
}

export interface BadgeConfig {
  color?: BadgeColorSource;
}

export interface ViewColumnDef {
  name: string;
  type: ViewColumnType;
  primaryKey?: boolean;
  hidden?: boolean;
  width?: string;
  description?: string;
  gauge?: GaugeConfig;
  badge?: BadgeConfig;
  icon?: string;
  unit?: string;
  url?: any;
}

export interface CellAttributes {
  url?: string;
  icon?: string;
  config?: {
    id: string;
    health?: string;
    status?: string;
    type?: string;
    class?: string;
  };
  max?: number;
  min?: number;
}

export type RowAttributes = Record<string, CellAttributes>;

export interface ViewVariable {
  key: string;
  label: string;
  default?: string;
  options: string[];
}

export type HeatmapVariant = 'calendar' | 'compact';

export interface PanelResult {
  name: string;
  description?: string;
  type:
    | 'piechart'
    | 'number'
    | 'gauge'
    | 'properties'
    | 'bargauge'
    | 'text'
    | 'table'
    | 'duration'
    | 'timeseries'
    | 'heatmap';
  piechart?: { showLabels?: boolean; colors?: Record<string, string> };
  number?: { unit?: string };
  gauge?: GaugeConfig & { unit?: string };
  bargauge?: { unit?: string };
  heatmap?: { mode?: HeatmapVariant };
  rows?: Record<string, any>[];
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
  panels?: PanelResult[];
}

export interface MultiViewReportData {
  views: ViewReportData[];
}
