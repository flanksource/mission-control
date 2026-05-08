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

export interface BarGaugeConfig {
  min?: number;
  max?: number;
  unit?: string;
  thresholds?: GaugeThreshold[];
  precision?: number;
  format?: 'percentage' | 'multiplier';
  group?: string;
}

export interface TimeseriesConfig {
  timeKey?: string;
  style?: 'lines' | 'area' | 'points';
  valueKey?: string;
  legend?: { enable?: boolean; layout?: 'vertical' | 'horizontal' };
}

export interface NumberConfig {
  unit?: string;
  precision?: number;
}
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
  number?: NumberConfig;
  gauge?: GaugeConfig & { unit?: string };
  bargauge?: BarGaugeConfig;
  timeseries?: TimeseriesConfig;
  heatmap?: { mode?: string };
  rows: Record<string, any>[];
}

export type HeatmapVariant = 'calendar' | 'compact';

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
