export type ConfigHealth = "healthy" | "warning" | "unhealthy" | "unknown" | string;
export type ConfigSeverity = "info" | "low" | "medium" | "high" | "critical" | string;

export type ConfigProperty = {
  name?: string;
  label?: string;
  text?: string;
  value?: number | string | null;
  icon?: string;
  color?: string;
  links?: Array<{ label?: string; url?: string }>;
};

export type ConfigItem = {
  id: string;
  name: string;
  namespace?: string | null;
  type?: string;
  config_class?: string;
  description?: string | null;
  status?: string | null;
  health?: ConfigHealth | null;
  labels?: Record<string, string> | null;
  tags?: Record<string, string> | null;
  properties?: ConfigProperty[] | null;
  config?: unknown;
  external_id?: string[] | null;
  path?: string;
  source?: string | null;
  scraper_id?: string | null;
  scraper_name?: string | null;
  agent_id?: string | null;
  agent_name?: string | null;
  ready?: boolean;
  parent_id?: string | null;
  created_at?: string;
  updated_at?: string | null;
  deleted_at?: string | null;
  delete_reason?: string | null;
  last_scraped_time?: string | null;
  cost_per_minute?: number | null;
  cost_total_1d?: number | null;
  cost_total_7d?: number | null;
  cost_total_30d?: number | null;
  changes?: number;
  analysis?: Record<string, unknown> | null;
};

export type ConfigChildItem = {
  id: string;
  type: string;
  name: string;
};

export type ConfigRelationshipTreeNode = ConfigItem & {
  edgeType?: "parent" | "target" | "child" | "related" | string;
  relation?: string;
  children?: ConfigRelationshipTreeNode[];
};

export type ConfigChange = {
  id: string;
  config_id: string;
  config_name?: string | null;
  config_type?: string | null;
  permalink?: string;
  external_change_id?: string;
  change_type: string;
  category?: string;
  severity?: ConfigSeverity | null;
  source?: string;
  summary?: string;
  patches?: string;
  diff?: string;
  details?: unknown;
  typed_change?: unknown;
  typedChange?: unknown;
  external_created_by?: string | null;
  created_by?: string | null;
  created_at?: string;
  first_observed?: string;
  count?: number;
  artifacts?: Array<{
    id?: string;
    filename?: string;
    content_type?: string;
    size?: number;
    checksum?: string;
    path?: string;
    created_at?: string;
    data_uri?: string;
  }>;
};

export type ConfigAnalysis = {
  id: string;
  config_id: string;
  analyzer?: string;
  analysis_type?: string;
  summary?: string;
  message?: string;
  severity?: ConfigSeverity | null;
  status?: string;
  source?: string | null;
  properties?: ConfigProperty[] | null;
  analysis?: unknown;
  first_observed?: string | null;
  last_observed?: string | null;
};

export type ConfigAccessSummary = {
  config_id?: string;
  config_name?: string | null;
  config_type?: string | null;
  external_user_id?: string;
  user?: string;
  email?: string;
  role?: string | null;
  user_type?: string | null;
  external_group_id?: string | null;
  group_name?: string | null;
  created_at?: string;
  last_signed_in_at?: string | null;
  last_reviewed_at?: string | null;
};

export type ConfigAccessLog = {
  config_id: string;
  external_user_id: string;
  created_at: string;
  mfa?: boolean | null;
  count?: number | null;
  properties?: Record<string, unknown> | null;
  external_users?: {
    name?: string;
    user_email?: string | null;
  } | null;
};

export type ExternalUser = {
  id: string;
  name: string;
  email?: string | null;
  user_type?: string | null;
  account_id?: string | null;
  aliases?: string[] | null;
  created_at?: string;
  updated_at?: string | null;
};

export type ExternalGroup = {
  id: string;
  name: string;
  group_type?: string | null;
  account_id?: string | null;
  aliases?: string[] | null;
  members_count?: number | null;
  permissions_count?: number | null;
  created_at?: string;
  updated_at?: string | null;
};

export type ExternalUserGroupMembership = {
  external_user_id: string;
  external_group_id: string;
  created_at?: string;
  deleted_at?: string | null;
  external_users?: ExternalUser | null;
  external_groups?: ExternalGroup | null;
};

export type ConfigRelationshipsResponse = {
  id: string;
  incoming: ConfigRelationshipTreeNode | null;
  outgoing: ConfigRelationshipTreeNode | null;
};

export type CatalogReportRoot = {
  id: string;
  includeChildren: boolean;
};

export type CatalogReportFormat = "facet-pdf" | "facet-html" | "json";

export type CatalogReportRequest = {
  format?: CatalogReportFormat;
  roots?: CatalogReportRoot[];
  selectedIds?: string[];
  title?: string;
  since?: string;
  recursive?: boolean;
  groupBy?: "none" | "merged" | "config" | string;
  changeArtifacts?: boolean;
  audit?: boolean;
  expandGroups?: boolean;
  limit?: number;
  maxItems?: number;
  maxChanges?: number;
  maxItemArtifacts?: number;
  staleDays?: number;
  reviewOverdueDays?: number;
  filters?: string[];
  changes?: boolean;
  insights?: boolean;
  relationships?: boolean;
  access?: boolean;
  accessLogs?: boolean;
  configJSON?: boolean;
};

export type CatalogReportPreviewResponse = {
  roots: ConfigRelationshipTreeNode[];
  selectedIds: string[];
  count: number;
};

export type PlaybookRunStatus =
  | "cancelled"
  | "timed_out"
  | "completed"
  | "failed"
  | "pending_approval"
  | "running"
  | "scheduled"
  | "sleeping"
  | "retrying"
  | "waiting"
  | string;

export type PlaybookActionStatus =
  | "waiting_children"
  | "completed"
  | "failed"
  | "running"
  | "scheduled"
  | "waiting"
  | "skipped"
  | "sleeping"
  | string;

export type PlaybookParameterType =
  | "check"
  | "checkbox"
  | "code"
  | "component"
  | "config"
  | "duration"
  | "list"
  | "people"
  | "team"
  | "text"
  | "bytes"
  | "millicores"
  | "secret"
  | string;

export type PlaybookParameter = {
  name: string;
  default?: unknown;
  label?: string;
  required?: boolean;
  icon?: string;
  description?: string;
  type?: PlaybookParameterType;
  properties?: Record<string, unknown> | null;
  dependsOn?: string[];
};

export type Playbook = {
  id: string;
  namespace?: string;
  name: string;
  title?: string | null;
  icon?: string | null;
  description?: string | null;
  spec?: {
    parameters?: PlaybookParameter[];
    [key: string]: unknown;
  } | Record<string, unknown> | null;
  source?: string | null;
  category?: string | null;
  created_by?: string | null;
  created_at?: string;
  updated_at?: string | null;
  deleted_at?: string | null;
};

export type PlaybookListItem = {
  id: string;
  namespace?: string;
  name: string;
  title?: string | null;
  icon?: string | null;
  description?: string | null;
  source?: string | null;
  category?: string | null;
  created_at?: string;
  parameters?: PlaybookParameter[] | unknown;
  spec?: Playbook["spec"];
};

export type PlaybookRunResource = {
  id?: string;
  name?: string | null;
  icon?: string | null;
  type?: string | null;
  config_class?: string | null;
};

export type PlaybookRunActor = {
  id?: string;
  name?: string | null;
  email?: string | null;
  avatar?: string | null;
};

export type PlaybookRun = {
  id: string;
  playbook_id: string;
  status?: PlaybookRunStatus;
  spec?: Record<string, unknown> | null;
  created_at?: string;
  start_time?: string | null;
  scheduled_time?: string;
  end_time?: string | null;
  timeout?: number | string | null;
  created_by?: string | null;
  component_id?: string | null;
  check_id?: string | null;
  config_id?: string | null;
  error?: string | null;
  parameters?: Record<string, string> | null;
  request?: Record<string, unknown> | null;
  agent_id?: string | null;
  parent_id?: string | null;
  notification_send_id?: string | null;
  playbooks?: Playbook | null;
  component?: PlaybookRunResource | null;
  check?: PlaybookRunResource | null;
  config?: PlaybookRunResource | null;
  person?: PlaybookRunActor | null;
  created_by_person?: PlaybookRunActor | null;
};

export type PlaybookArtifact = {
  id?: string;
  filename?: string;
  path?: string;
  content_type?: string;
  size?: number;
  created_at?: string;
  [key: string]: unknown;
};

export type PlaybookRunAction = {
  id: string;
  name: string;
  playbook_run_id: string;
  status?: PlaybookActionStatus;
  synthetic?: boolean;
  spec_index?: number;
  scheduled_time?: string;
  start_time?: string;
  end_time?: string | null;
  result?: Record<string, unknown> | null;
  error?: string | null;
  agent_id?: string | null;
  retry_count?: number | null;
  agent?: { id?: string; name?: string | null } | null;
  artifacts?: PlaybookArtifact[] | null;
};

export type PlaybookRunWithActions = {
  run: PlaybookRun | null;
  childRuns: PlaybookRun[];
  actions: PlaybookRunAction[];
  actionDetailsError?: string;
};

export type PlaybookRunTarget = {
  config_id?: string;
  component_id?: string;
  check_id?: string;
};

export type PlaybookRunSubmitRequest = PlaybookRunTarget & {
  id: string;
  params?: Record<string, string>;
};

export type PlaybookRunSubmitResponse = {
  run_id: string;
  starts_at: string;
};
