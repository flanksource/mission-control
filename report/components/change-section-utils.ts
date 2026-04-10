import type { ApplicationChange, ApplicationPermissionChange } from '../types.ts';
import type { ConfigChange, ConfigTypedChange } from '../config-types.ts';
import type { CatalogReportCategoryMapping } from '../catalog-report-types.ts';

export type ChangeSectionVariant = 'generic' | 'rbac' | 'backup' | 'deployment';
export type BackupCalendarStatus = 'success' | 'failed' | 'warning';
export type RBACChangeAction = 'added' | 'removed';

export interface BackupCalendarEntry {
  date: string;
  status: BackupCalendarStatus;
  label?: string;
}

export interface RBACChangeRow {
  id: string;
  date: string;
  configId?: string;
  configName: string;
  configType?: string;
  action: 'Granted' | 'Revoked';
  subject: string;
  subjectKind: 'user' | 'group';
  role?: string;
  viaGroup?: string;
  changedBy: string;
  source: string;
  notes?: string;
}

export interface RBACChangeGroup {
  key: string;
  configId?: string;
  configName: string;
  configType?: string;
  latestDate: string;
  rows: RBACChangeRow[];
}

export interface CategorizedChanges {
  rbac: ConfigChange[];
  backup: ConfigChange[];
  deployment: ConfigChange[];
  uncategorized: ConfigChange[];
}

export interface TypedChangeDisplay {
  label?: string;
  summary?: string;
  meta: string[];
  diff?: TypedChangeDiff;
}

export interface TypedChangeDiff {
  label?: string;
  from: string;
  to: string;
}

const RBAC_ADDED_TYPES = new Set(['PermissionGranted', 'PermissionAdded']);
const RBAC_REMOVED_TYPES = new Set(['PermissionRevoked', 'PermissionRemoved']);
const BACKUP_SUCCESS_TYPES = new Set(['BackupCompleted', 'BackupSuccessful']);
const BACKUP_FAILED_TYPES = new Set(['BackupFailed']);
const BACKUP_PROGRESS_TYPES = new Set(['BackupStarted', 'BackupRunning', 'BackupEnqueued']);
const RESTORE_CHANGE_TYPES = new Set(['BackupRestored', 'RestoreCompleted']);
const DEPLOYMENT_CHANGE_TYPES = new Set(['ScalingReplicaSet', 'PolicyUpdate', 'CodeDeployment', 'diff']);

function normalizedType(change: ApplicationChange): string {
  return change.changeType ?? '';
}

export function getChangeActor(change: ApplicationChange): string {
  return change.createdBy || change.source || '-';
}

function getCategoryKey(change: ApplicationChange): string {
  return change.category ?? '';
}

function normalizeRBACAction(change: ApplicationChange): RBACChangeAction | null {
  const category = getCategoryKey(change);
  if (category === 'rbac.granted') {
    return 'added';
  }
  if (category === 'rbac.revoked') {
    return 'removed';
  }

  const type = normalizedType(change);
  if (RBAC_ADDED_TYPES.has(type)) {
    return 'added';
  }
  if (RBAC_REMOVED_TYPES.has(type)) {
    return 'removed';
  }
  return null;
}

export function isRBACChange(change: ApplicationChange): boolean {
  return normalizeRBACAction(change) !== null;
}

export function isBackupChange(change: ApplicationChange): boolean {
  if (getCategoryKey(change).startsWith('backup.')) {
    return true;
  }

  const type = normalizedType(change);
  return BACKUP_SUCCESS_TYPES.has(type) || BACKUP_FAILED_TYPES.has(type) || BACKUP_PROGRESS_TYPES.has(type) || RESTORE_CHANGE_TYPES.has(type);
}

export function isRestoreChange(change: ApplicationChange): boolean {
  if (getCategoryKey(change) === 'backup.restore') {
    return true;
  }

  return RESTORE_CHANGE_TYPES.has(normalizedType(change));
}

export function classifyDeploymentChange(change: ApplicationChange): 'scale' | 'policy' | 'spec' | null {
  const category = getCategoryKey(change);
  if (category.startsWith('deployment.')) {
    const suffix = category.slice('deployment.'.length);
    if (suffix === 'scale' || suffix === 'scaling') {
      return 'scale';
    }
    if (suffix === 'policy') {
      return 'policy';
    }
    return 'spec';
  }
  if (category === 'deployment') {
    return 'spec';
  }

  const type = normalizedType(change);
  const lowerType = type.toLowerCase();

  if (type === 'ScalingReplicaSet' || lowerType.includes('replicaset')) {
    return 'scale';
  }

  if (type === 'PolicyUpdate') {
    return 'policy';
  }

  if (DEPLOYMENT_CHANGE_TYPES.has(type)) {
    return 'spec';
  }

  return null;
}

export function isDeploymentChange(change: ApplicationChange): boolean {
  return classifyDeploymentChange(change) !== null;
}

export function filterRBACChanges(changes: ApplicationChange[]): ApplicationChange[] {
  return changes.filter(isRBACChange);
}

export function filterBackupChanges(changes: ApplicationChange[]): ApplicationChange[] {
  return changes.filter(isBackupChange);
}

export function filterDeploymentChanges(changes: ApplicationChange[]): ApplicationChange[] {
  return changes.filter(isDeploymentChange);
}

export function inferChangeSectionVariant(
  title: string,
  changes: ApplicationChange[],
  _categoryMappings?: CatalogReportCategoryMapping[],
): ChangeSectionVariant {
  const lowerTitle = title.toLowerCase();

  if (/\brbac\b|\bpermission/.test(lowerTitle)) {
    return 'rbac';
  }

  if (/\bbackup\b|\brestore\b/.test(lowerTitle)) {
    return 'backup';
  }

  if (/\bdeployment\b|\brollout\b/.test(lowerTitle)) {
    return 'deployment';
  }

  let rbacCount = 0;
  let backupCount = 0;
  let deploymentCount = 0;
  for (const change of changes) {
    const category = getCategoryKey(change);
    if (!category) {
      continue;
    }

    if (category === 'rbac' || category.startsWith('rbac.')) {
      rbacCount += 1;
    } else if (category === 'backup' || category.startsWith('backup.')) {
      backupCount += 1;
    } else if (category === 'deployment' || category.startsWith('deployment.')) {
      deploymentCount += 1;
    }
  }

  if (rbacCount > 0 && rbacCount === changes.length) {
    return 'rbac';
  }
  if (backupCount > 0 && backupCount === changes.length) {
    return 'backup';
  }
  if (deploymentCount > 0 && deploymentCount >= Math.ceil(changes.length / 2)) {
    return 'deployment';
  }

  return 'generic';
}

export function getBackupCalendarStatus(change: ApplicationChange): BackupCalendarStatus | null {
  const category = getCategoryKey(change);
  if (category === 'backup.success') {
    return 'success';
  }
  if (category === 'backup.failed') {
    return 'failed';
  }
  if (category === 'backup.progress') {
    return 'warning';
  }

  const type = normalizedType(change);
  if (BACKUP_SUCCESS_TYPES.has(type)) {
    return 'success';
  }
  if (BACKUP_FAILED_TYPES.has(type)) {
    return 'failed';
  }
  if (BACKUP_PROGRESS_TYPES.has(type)) {
    return 'warning';
  }
  return null;
}

export function extractBackupLabel(change: ApplicationChange): string | undefined {
  const match = change.description.match(/(\d+(?:\.\d+)?)\s*([KMGT]i?B|bytes?)/i);
  if (!match) {
    return undefined;
  }

  return `${match[1]} ${match[2].toUpperCase()}`;
}

export function toBackupCalendarEntries(changes: ApplicationChange[]): BackupCalendarEntry[] {
  return filterBackupChanges(changes)
    .filter((change) => !isRestoreChange(change))
    .map((change) => {
      const status = getBackupCalendarStatus(change);
      if (!status) {
        return null;
      }

      return {
        date: change.date,
        status,
        label: extractBackupLabel(change),
      };
    })
    .filter(Boolean) as BackupCalendarEntry[];
}

interface ParsedPermissionSummary {
  user?: string;
  role?: string;
  group?: string;
  resourceName?: string;
}

function cleanField(value?: string): string | undefined {
  const trimmed = value?.trim();
  return trimmed ? trimmed : undefined;
}

function parseStructuredPermissionSummary(description: string): ParsedPermissionSummary {
  const match = description.match(/^Permission(?:Added|Removed):\s*(.+)$/i);
  if (!match) {
    return {};
  }

  const parsed: ParsedPermissionSummary = {};
  for (const part of match[1].split(/\s*,\s*/)) {
    const userMatch = part.match(/^user\s+(.+)$/i);
    if (userMatch) {
      parsed.user = cleanField(userMatch[1]);
      continue;
    }

    const roleMatch = part.match(/^role\s+(.+)$/i);
    if (roleMatch) {
      parsed.role = cleanField(roleMatch[1]);
      continue;
    }

    const groupMatch = part.match(/^group\s+(.+)$/i);
    if (groupMatch) {
      parsed.group = cleanField(groupMatch[1]);
    }
  }

  return parsed;
}

function parseLegacyPermissionSummary(description: string): ParsedPermissionSummary {
  const grantedWithPermissions = description.match(/^Granted\s+(.+?)\s+permissions?\s+to\s+(.+?)\s+on\s+(.+)$/i);
  if (grantedWithPermissions) {
    return {
      role: cleanField(grantedWithPermissions[1]),
      user: cleanField(grantedWithPermissions[2]),
      resourceName: cleanField(grantedWithPermissions[3]),
    };
  }

  const granted = description.match(/^Granted\s+(.+?)\s+to\s+(.+?)\s+on\s+(.+)$/i);
  if (granted) {
    return {
      role: cleanField(granted[1]),
      user: cleanField(granted[2]),
      resourceName: cleanField(granted[3]),
    };
  }

  const revoked = description.match(/^Revoked\s+(.+?)\s+access\s+for\s+(.+?)\s+on\s+(.+)$/i);
  if (revoked) {
    return {
      role: cleanField(revoked[1]),
      user: cleanField(revoked[2]),
      resourceName: cleanField(revoked[3]),
    };
  }

  return {};
}

function parsePermissionSummary(description: string): ParsedPermissionSummary {
  const structured = parseStructuredPermissionSummary(description);
  if (structured.user || structured.role || structured.group) {
    return structured;
  }
  return parseLegacyPermissionSummary(description);
}

function isRedundantPermissionDescription(change: ApplicationChange, permission: ApplicationPermissionChange, parsed: ParsedPermissionSummary): boolean {
  const description = change.description.trim();
  if (!description) {
    return true;
  }

  if (/^Permission(?:Added|Removed):/i.test(description)) {
    return true;
  }

  if (/^(Granted|Revoked)\b/i.test(description)) {
    const matchedUser = cleanField(permission.user) || parsed.user;
    const matchedRole = cleanField(permission.role) || parsed.role;
    const matchedResource = cleanField(change.configName) || parsed.resourceName;

    if (
      (!matchedUser || description.includes(matchedUser)) &&
      (!matchedRole || description.includes(matchedRole)) &&
      (!matchedResource || description.includes(matchedResource))
    ) {
      return true;
    }
  }

  return false;
}

export function groupRBACChanges(changes: ApplicationChange[]): RBACChangeGroup[] {
  const grouped = new Map<string, RBACChangeGroup>();

  for (const change of filterRBACChanges(changes)) {
    const action = normalizeRBACAction(change);
    if (!action) {
      continue;
    }

    const parsed = parsePermissionSummary(change.description);
    const permission = change.permission ?? {};
    const configName = cleanField(change.configName) || parsed.resourceName || 'Unknown resource';
    const configId = cleanField(change.configId);
    const key = configId || configName;
    const explicitUser = cleanField(permission.user) || parsed.user;
    const explicitGroup = cleanField(permission.group) || parsed.group;
    const subject = explicitUser || explicitGroup || '-';
    const subjectKind = explicitUser ? 'user' : explicitGroup ? 'group' : 'user';
    const role = cleanField(permission.role) || parsed.role;
    const viaGroup = explicitUser && explicitGroup ? explicitGroup : undefined;
    const notes = isRedundantPermissionDescription(change, permission, parsed)
      ? undefined
      : cleanField(change.description);

    if (!grouped.has(key)) {
      grouped.set(key, {
        key,
        configId,
        configName,
        configType: cleanField(change.configType),
        latestDate: change.date,
        rows: [],
      });
    }

    const group = grouped.get(key)!;
    if (!group.configType && change.configType) {
      group.configType = change.configType;
    }
    if (new Date(change.date).getTime() > new Date(group.latestDate).getTime()) {
      group.latestDate = change.date;
    }

    group.rows.push({
      id: change.id,
      date: change.date,
      configId,
      configName,
      configType: cleanField(change.configType),
      action: action === 'added' ? 'Granted' : 'Revoked',
      subject,
      subjectKind,
      role,
      viaGroup,
      changedBy: cleanField(change.createdBy) || '-',
      source: cleanField(change.source) || '-',
      notes,
    });
  }

  return [...grouped.values()]
    .map((group) => ({
      ...group,
      rows: group.rows.sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime()),
    }))
    .sort((a, b) => new Date(b.latestDate).getTime() - new Date(a.latestDate).getTime());
}

export function categorizeChanges(
  changes: ConfigChange[],
  _categoryMappings?: CatalogReportCategoryMapping[],
): CategorizedChanges {
  const result: CategorizedChanges = { rbac: [], backup: [], deployment: [], uncategorized: [] };

  for (const change of changes) {
    const category = change.category ?? '';
    if (!category) {
      result.uncategorized.push(change);
      continue;
    }

    if (category === 'rbac' || category.startsWith('rbac.')) result.rbac.push(change);
    else if (category === 'backup' || category.startsWith('backup.')) result.backup.push(change);
    else if (category === 'deployment' || category.startsWith('deployment.')) result.deployment.push(change);
    else result.uncategorized.push(change);
  }

  return result;
}

function asText(value: unknown): string | undefined {
  if (value === undefined || value === null) {
    return undefined;
  }
  if (typeof value === 'string') {
    return cleanField(value);
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value);
  }
  return undefined;
}

function compactMeta(values: Array<string | undefined>): string[] {
  return values.filter((value): value is string => Boolean(value));
}

function labelValue(label: string, value: unknown): string | undefined {
  const text = asText(value);
  return text ? `${label}: ${text}` : undefined;
}

function transition(label: string, from: unknown, to: unknown): string | undefined {
  const fromText = asText(from);
  const toText = asText(to);
  if (!fromText && !toText) {
    return undefined;
  }
  if (!fromText) {
    return `${label}: ${toText}`;
  }
  if (!toText) {
    return `${label}: ${fromText}`;
  }
  return `${label}: ${fromText} -> ${toText}`;
}

function toDiff(label: string, from: unknown, to: unknown): TypedChangeDiff | undefined {
  const fromText = asText(from);
  const toText = asText(to);
  if (!fromText || !toText || fromText === toText) {
    return undefined;
  }

  return { label, from: fromText, to: toText };
}

function formatDimensions(width: unknown, height: unknown): string | undefined {
  const widthText = asText(width);
  const heightText = asText(height);
  if (!widthText || !heightText) {
    return undefined;
  }
  return `${widthText}x${heightText}`;
}

function formatCurrencyAmount(value: unknown, currency: unknown): string | undefined {
  if (typeof value !== 'number') {
    return asText(value);
  }

  const code = asText(currency)?.toUpperCase();
  if (code && code.length === 3) {
    try {
      return new Intl.NumberFormat('en-US', { style: 'currency', currency: code }).format(value);
    } catch {
      return `${code} ${value}`;
    }
  }

  return String(value);
}

function humanizeLabel(value: string): string {
  return value
    .replace(/[_-]+/g, ' ')
    .replace(/([a-z0-9])([A-Z])/g, '$1 $2')
    .replace(/\s+/g, ' ')
    .trim()
    .replace(/^./, (char) => char.toUpperCase());
}

function humanizeKind(kind: string): string {
  const base = kind.split('/')[0] ?? kind;
  return humanizeLabel(base);
}

export function getChangeTypeLabel(change: ConfigChange, typedDisplay?: TypedChangeDisplay): string {
  const typeLabel = humanizeLabel(change.changeType || 'Change');
  const normalizedType = (change.changeType || '').trim().toLowerCase();

  if (typedDisplay?.label && ['diff', 'change', 'changed', 'update', 'updated'].includes(normalizedType)) {
    return typedDisplay.label;
  }

  return typeLabel;
}

function permissionFromTypedChange(typedChange?: ConfigTypedChange): ApplicationPermissionChange | undefined {
  if (typedChange?.kind !== 'PermissionChange/v1') {
    return undefined;
  }

  const user = asText(typedChange.user_name);
  const group = asText(typedChange.group_name);
  const role = asText(typedChange.role_name);
  if (!user && !group && !role) {
    return undefined;
  }

  return { user, group, role };
}

const TYPED_CHANGE_RENDERERS: Record<string, (typedChange: ConfigTypedChange) => Omit<TypedChangeDisplay, 'label'>> = {
  'UserChange/v1': (typedChange) => ({
    meta: compactMeta([
      asText(typedChange.user_name) || asText(typedChange.user_id),
      asText(typedChange.user_email),
      labelValue('Group', typedChange.group_name || typedChange.group_id),
      labelValue('Type', typedChange.user_type),
      labelValue('Tenant', typedChange.tenant),
    ]),
  }),
  'Screenshot/v1': (typedChange) => ({
    meta: compactMeta([
      labelValue('Artifact', typedChange.artifact_id),
      labelValue('Type', typedChange.content_type),
      labelValue('Size', formatDimensions(typedChange.width, typedChange.height)),
      labelValue('URL', typedChange.url),
    ]),
  }),
  'PermissionChange/v1': (typedChange) => ({
    meta: compactMeta([
      asText(typedChange.user_name) || asText(typedChange.group_name) || asText(typedChange.user_id) || asText(typedChange.group_id),
      labelValue('Role', typedChange.role_name || typedChange.role_id),
      labelValue('Role Type', typedChange.role_type),
      labelValue('Scope', typedChange.scope),
    ]),
  }),
  'Deployment/v1': (typedChange) => {
    const imageDiff = toDiff('Image', typedChange.previous_image, typedChange.new_image);
    return {
      meta: compactMeta([
        labelValue('Container', typedChange.container),
        imageDiff ? undefined : transition('Image', typedChange.previous_image, typedChange.new_image),
        labelValue('Namespace', typedChange.namespace),
        labelValue('Strategy', typedChange.strategy),
      ]),
      diff: imageDiff,
    };
  },
  'Promotion/v1': (typedChange) => {
    const environmentDiff = toDiff('Environment', typedChange.from_environment, typedChange.to_environment);
    return {
      meta: compactMeta([
        environmentDiff ? undefined : transition('Environment', typedChange.from_environment, typedChange.to_environment),
        labelValue('Version', typedChange.version),
        labelValue('Artifact', typedChange.artifact),
      ]),
      diff: environmentDiff,
    };
  },
  'Approval/v1': (typedChange) => ({
    summary: asText(typedChange.approved_by) ? `Approved by ${typedChange.approved_by}` : asText(typedChange.rejected_by) ? `Rejected by ${typedChange.rejected_by}` : 'Approval decision',
    meta: compactMeta([
      labelValue('Playbook', typedChange.playbook_id),
      labelValue('Run', typedChange.run_id),
      labelValue('Reason', typedChange.reason),
    ]),
  }),
  'Rollback/v1': (typedChange) => {
    const versionDiff = toDiff('Version', typedChange.from_version, typedChange.to_version);
    return {
      summary: labelValue('Reason', typedChange.reason),
      meta: compactMeta([
        versionDiff ? undefined : transition('Version', typedChange.from_version, typedChange.to_version),
        labelValue('Trigger', typedChange.trigger),
      ]),
      diff: versionDiff,
    };
  },
  'Backup/v1': (typedChange) => ({
    meta: compactMeta([
      labelValue('Status', typedChange.status),
      labelValue('Type', typedChange.backup_type),
      labelValue('Target', typedChange.target),
      labelValue('Size', typedChange.size),
      labelValue('Duration', typedChange.duration),
      labelValue('Snapshot', typedChange.snapshot_id),
    ]),
  }),
  'PlaybookExecution/v1': (typedChange) => {
    const playbook = asText(typedChange.playbook_name) || asText(typedChange.playbook_id);
    return {
      summary: playbook,
      meta: compactMeta([
        labelValue('Run', typedChange.run_id),
        labelValue('Status', typedChange.status),
        labelValue('Duration', typedChange.duration),
        labelValue('Error', typedChange.error),
      ]),
    };
  },
  'Scaling/v1': (typedChange) => {
    const replicaDiff = toDiff('Replicas', typedChange.from_replicas, typedChange.to_replicas);
    return {
      meta: compactMeta([
        labelValue('Resource', typedChange.resource_type),
        replicaDiff ? undefined : transition('Replicas', typedChange.from_replicas, typedChange.to_replicas),
        labelValue('Trigger', typedChange.trigger),
      ]),
      diff: replicaDiff,
    };
  },
  'Certificate/v1': (typedChange) => ({
    summary: labelValue('Subject', typedChange.subject),
    meta: compactMeta([
      labelValue('Issuer', typedChange.issuer),
      labelValue('Valid To', typedChange.not_after),
      labelValue('Serial', typedChange.serial),
      labelValue('DNS', typedChange.dns_names),
    ]),
  }),
  'CostChange/v1': (typedChange) => {
    const costDiff = toDiff(
      'Cost',
      formatCurrencyAmount(typedChange.previous_cost, typedChange.currency),
      formatCurrencyAmount(typedChange.new_cost, typedChange.currency),
    );
    return {
      summary: labelValue('Reason', typedChange.reason),
      meta: compactMeta([
        costDiff ? undefined : transition('Cost', formatCurrencyAmount(typedChange.previous_cost, typedChange.currency), formatCurrencyAmount(typedChange.new_cost, typedChange.currency)),
        labelValue('Period', typedChange.period),
      ]),
      diff: costDiff,
    };
  },
  'PipelineRun/v1': (typedChange) => {
    const pipeline = asText(typedChange.pipeline_name) || asText(typedChange.pipeline_id);
    return {
      summary: pipeline,
      meta: compactMeta([
        labelValue('Run', typedChange.run_number ?? typedChange.run_id),
        labelValue('Branch', typedChange.branch),
        labelValue('Status', typedChange.status),
        labelValue('Duration', typedChange.duration),
        labelValue('Error', typedChange.error),
      ]),
    };
  },
};

export function getTypedChangeDisplay(change: ConfigChange): TypedChangeDisplay | undefined {
  const typedChange = change.typedChange;
  if (!typedChange?.kind) {
    return undefined;
  }

  const renderer = TYPED_CHANGE_RENDERERS[typedChange.kind];
  const display = renderer ? renderer(typedChange) : { meta: [] };
  return {
    label: humanizeKind(typedChange.kind),
    summary: display.summary,
    meta: display.meta ?? [],
    diff: display.diff,
  };
}

export function configChangeToApplicationChange(c: ConfigChange, category?: string): ApplicationChange {
  const permission = (c.details?.permission as ApplicationPermissionChange | undefined) ?? permissionFromTypedChange(c.typedChange);
  return {
    id: c.id ?? '',
    date: c.createdAt ?? '',
    changeType: c.changeType,
    category: category ?? c.category,
    source: c.source,
    createdBy: c.createdBy ?? c.externalCreatedBy,
    configId: c.configID,
    configName: c.configName,
    configType: c.configType,
    permission,
    description: c.summary ?? '',
    status: c.severity ?? 'info',
    createdAt: c.createdAt ?? '',
  };
}
