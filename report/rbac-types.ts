export interface RBACUserRole {
  userId: string;
  userName: string;
  email: string;
  role: string;
  roleExternalIds?: string[];
  roleSource: string;
  sourceSystem: string;
  createdAt: string;
  lastSignedInAt?: string | null;
  lastReviewedAt?: string | null;
  isStale: boolean;
  isReviewOverdue: boolean;
}

export interface RBACResource {
  configId: string;
  configName: string;
  configType: string;
  configClass?: string;
  parentId?: string;
  path?: string;
  status?: string;
  health?: string;
  description?: string;
  tags?: Record<string, string>;
  labels?: Record<string, string>;
  createdAt?: string;
  updatedAt?: string;
  users: RBACUserRole[];
  changelog: RBACChangeEntry[];
  temporaryAccess?: RBACTemporaryAccess[];
}

export interface RBACChangeEntry {
  configId: string;
  date: string;
  changeType: string;
  user: string;
  role: string;
  configName: string;
  source: string;
  description: string;
}

export interface RBACTemporaryAccess {
  configId: string;
  user: string;
  role: string;
  source: string;
  grantedAt: string;
  revokedAt: string;
  duration: string;
}

export interface RBACSummary {
  totalUsers: number;
  totalResources: number;
  staleAccessCount: number;
  overdueReviews: number;
  directAssignments: number;
  groupAssignments: number;
}

export interface RBACUserResource {
  configId: string;
  configName: string;
  configType: string;
  configClass?: string;
  path?: string;
  role: string;
  roleExternalIds?: string[];
  roleSource: string;
  createdAt: string;
  lastSignedInAt?: string | null;
  lastReviewedAt?: string | null;
  isStale: boolean;
  isReviewOverdue: boolean;
  status?: string;
  health?: string;
  tags?: Record<string, string>;
  labels?: Record<string, string>;
}

export interface RBACUserReport {
  userId: string;
  userName: string;
  email: string;
  sourceSystem: string;
  lastSignedInAt?: string | null;
  resources: RBACUserResource[];
}

export interface ConfigItem {
  id: string;
  name?: string;
  type?: string;
  config_class?: string;
  status?: string;
  health?: string;
  description?: string;
  path?: string;
  parent_id?: string;
  tags?: Record<string, string>;
  labels?: Record<string, string>;
  created_at?: string;
  updated_at?: string;
}

export interface RBACReport {
  title: string;
  query?: string;
  generatedAt: string;
  subject?: ConfigItem;
  parents?: ConfigItem[];
  resources: RBACResource[];
  changelog: RBACChangeEntry[];
  summary: RBACSummary;
  users?: RBACUserReport[];
}
