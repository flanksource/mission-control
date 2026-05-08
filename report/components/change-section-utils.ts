import type { ApplicationChange, ApplicationPermissionChange } from '../types.ts';
import type { ConfigChange, ConfigTypedChange } from '../config-types.ts';
import { humanizeSize } from './utils.ts';

export type BackupCalendarStatus = 'success' | 'failed' | 'warning';
export type RBACChangeAction = 'added' | 'removed';
export type DeploymentCategory = 'promotion' | 'scale' | 'policy' | 'spec';

export type BackupType = 'full' | 'incremental' | 'differential' | 'unknown';
export type BackupTrigger = 'scheduled' | 'manual' | 'automated';

export interface BackupCalendarEntry {
  id?: string;
  date: string;
  status: BackupCalendarStatus;
  label?: string;
  startTime?: string;
  duration?: string;
  size?: string;
  backupType?: BackupType;
  trigger?: BackupTrigger;
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
const DEPLOYMENT_RELEASE_TYPES = new Set(['Promotion', 'Approved', 'Rejected', 'Rollback', 'Deployment', 'PipelineRunCompleted', 'PipelineRunStarted', 'PipelineRunFailed', 'PlaybookCompleted', 'PlaybookStarted', 'PlaybookFailed']);
const DEPLOYMENT_RELEASE_KINDS = new Set(['Promotion/v1', 'Approval/v1', 'Rollback/v1', 'PipelineRun/v1', 'PlaybookExecution/v1', 'Deployment/v1']);
const DEPLOYMENT_SCALE_KINDS = new Set(['Scaling/v1', 'Scale/v1']);
const CHANGE_EVENT_ICON_NAMES: Record<string, string> = {
  'Approval/v1': 'approval',
  'Backup/v1': 'add-snapshot',
  'Certificate/v1': 'certificate',
  'Change/v1': 'diff',
  'ConfigChange/v1': 'diff',
  'CostChange/v1': 'add-cost',
  'DatabaseSource/v1': 'database',
  'Deployment/v1': 'deployment',
  'Dimension/v1': 'scale-up',
  'Environment/v1': 'world',
  'Event/v1': 'azure-event-hub',
  'GitSource/v1': 'git',
  'GroupMembership/v1': 'group',
  'HelmSource/v1': 'helm',
  'Identity/v1': 'user',
  'ImageSource/v1': 'azure-image',
  'PermissionChange/v1': 'shield-lock',
  'PipelineRun/v1': 'azure-devops-pipeline',
  'PlaybookExecution/v1': 'playbook',
  'Promotion/v1': 'gcp-cloud-deploy',
  'Restore/v1': 'restore',
  'Rollback/v1': 'rollback',
  'Scale/v1': 'scale-up',
  'Scaling/v1': 'scale-up',
  'Screenshot/v1': 'add-snapshot',
  'Source/v1': 'source',
  'Test/v1': 'test',
  'UserChange/v1': 'user',
  Approval: 'approval',
  Approved: 'approval',
  Rejected: 'approval',
  Backup: 'add-snapshot',
  BackupFailed: 'add-snapshot',
  BackupCompleted: 'add-snapshot',
  BackupStarted: 'add-snapshot',
  Restore: 'restore',
  RestoreCompleted: 'restore',
  PermissionGranted: 'shield-lock',
  PermissionRevoked: 'shield-lock',
  PermissionAdded: 'shield-lock',
  PermissionRemoved: 'shield-lock',
  Promotion: 'gcp-cloud-deploy',
  Rollback: 'rollback',
  Scaling: 'scale-up',
  ScalingReplicaSet: 'scale-up',
  PolicyUpdate: 'diff',
  diff: 'diff',
  CodeDeployment: 'deployment',
  Deployment: 'deployment',
  PipelineRunStarted: 'azure-devops-pipeline',
  PipelineRunCompleted: 'azure-devops-pipeline',
  PipelineRunFailed: 'azure-devops-pipeline',
  PlaybookStarted: 'playbook',
  PlaybookCompleted: 'playbook',
  PlaybookFailed: 'playbook',
};

const CHANGE_SOURCE_ICON_NAMES: Record<string, string> = {
  diff: 'diff',
  'config-db': 'config-db',
  cloudtrail: 'aws-cloudtrail',
  kubernetes: 'k8s',
  kubernetescrd: 'k8s',
  playbook: 'playbook',
  'aws backup': 'add-snapshot',
  awsbackup: 'add-snapshot',
  accessanalyzer: 'shield-lock',
  push: 'source',
  ui: 'source',
  crd: 'source',
  file: 'source',
  exec: 'source',
  azuredevops: 'azure-devops',
  'azure-devops': 'azure-devops',
  ado: 'azure-devops',
};

export function getChangeSourceIconName(source?: string): string | undefined {
  if (!source) return undefined;
  return CHANGE_SOURCE_ICON_NAMES[source.toLowerCase()];
}

type TypedChangeCarrier = {
  changeType?: string;
  details?: Record<string, any>;
  typedChange?: ConfigTypedChange;
};

function normalizedType(change: ApplicationChange): string {
  return change.changeType ?? '';
}

function typedChangeFromDetails(details?: Record<string, any>): ConfigTypedChange | undefined {
  if (!details || typeof details.kind !== 'string' || !details.kind) {
    return undefined;
  }

  return details as ConfigTypedChange;
}

export function getResolvedTypedChange(change: TypedChangeCarrier): ConfigTypedChange | undefined {
  if (change.typedChange?.kind) {
    return change.typedChange;
  }

  return typedChangeFromDetails(change.details);
}

function typedActorLabel(typedChange?: ConfigTypedChange): string | undefined {
  switch (typedChange?.kind) {
    case 'Approval/v1':
      return identityLabel(typedChange.approver)
        || asText(typedChange.approved_by)
        || asText(typedChange.rejected_by)
        || identityLabel(typedChange.submitted_by)
        || asText(typedChange.submitted_by);
    case 'Backup/v1':
      return identityLabel(typedChange.created_by);
    case 'ConfigChange/v1':
      return identityLabel(typedChange.author);
    default:
      return undefined;
  }
}

export function getChangeEventIconName(change: TypedChangeCarrier): string {
  const typedChange = getResolvedTypedChange(change);
  if (typedChange?.kind) {
    return CHANGE_EVENT_ICON_NAMES[typedChange.kind] || (typedChange.kind.split('/')[0] ?? typedChange.kind);
  }

  const changeType = change.changeType?.trim();
  if (!changeType) {
    return 'Change';
  }

  return CHANGE_EVENT_ICON_NAMES[changeType] || changeType;
}

export function getChangeActor(change: ApplicationChange): string {
  return typedActorLabel(getResolvedTypedChange(change))
    || cleanField(change.createdBy)
    || cleanField(change.source)
    || '-';
}

export function getConfigChangeActor(change: ConfigChange): string | undefined {
  return typedActorLabel(getResolvedTypedChange(change))
    || cleanField(change.createdBy)
    || cleanField(change.externalCreatedBy)
    || cleanField(change.source);
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

export function classifyDeploymentChange(change: ApplicationChange): DeploymentCategory | null {
  const category = getCategoryKey(change);
  if (category.startsWith('deployment.')) {
    const suffix = category.slice('deployment.'.length);
    if (suffix === 'promotion' || suffix === 'release' || suffix === 'success' || suffix === 'failed') {
      return 'promotion';
    }
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

  const typedChange = getResolvedTypedChange(change);
  if (typedChange?.kind) {
    if (DEPLOYMENT_RELEASE_KINDS.has(typedChange.kind)) {
      return 'promotion';
    }
    if (DEPLOYMENT_SCALE_KINDS.has(typedChange.kind)) {
      return 'scale';
    }
  }

  const type = normalizedType(change);
  const lowerType = type.toLowerCase();

  if (type === 'ScalingReplicaSet' || lowerType.includes('replicaset')) {
    return 'scale';
  }

  if (type === 'PolicyUpdate') {
    return 'policy';
  }

  if (DEPLOYMENT_RELEASE_TYPES.has(type)) {
    return 'promotion';
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

function formatStartTime(date: string): string | undefined {
  if (!date) {
    return undefined;
  }
  const parsed = new Date(date);
  if (Number.isNaN(parsed.getTime())) {
    return undefined;
  }
  const hours = String(parsed.getUTCHours()).padStart(2, '0');
  const minutes = String(parsed.getUTCMinutes()).padStart(2, '0');
  return `${hours}:${minutes}`;
}

function classifyBackupType(value: unknown): BackupType | undefined {
  const text = asText(value)?.toLowerCase();
  if (!text) {
    return undefined;
  }
  if (text.includes('incremental') || text === 'incr') return 'incremental';
  if (text.includes('differential') || text === 'diff') return 'differential';
  if (text.includes('full')) return 'full';
  return 'unknown';
}

const AUTOMATED_KEYWORDS = ['pre-upgrade', 'preupgrade', 'post-upgrade', 'drill', 'disaster', 'dr ', ' dr'];
const SCHEDULED_KEYWORDS = ['scheduled', 'nightly', 'hourly', 'weekly', 'daily', 'cron'];
const MANUAL_KEYWORDS = ['manual', 'on-demand', 'adhoc', 'ad-hoc'];

function classifyBackupTrigger(change: ApplicationChange, typedChange?: ConfigTypedChange): BackupTrigger {
  const triggerField = asText(typedChange?.trigger)?.toLowerCase();
  if (triggerField) {
    if (MANUAL_KEYWORDS.some((k) => triggerField.includes(k))) return 'manual';
    if (AUTOMATED_KEYWORDS.some((k) => triggerField.includes(k))) return 'automated';
    if (SCHEDULED_KEYWORDS.some((k) => triggerField.includes(k))) return 'scheduled';
  }

  const description = change.description?.toLowerCase() ?? '';
  if (AUTOMATED_KEYWORDS.some((k) => description.includes(k))) return 'automated';
  if (MANUAL_KEYWORDS.some((k) => description.includes(k))) return 'manual';
  if (SCHEDULED_KEYWORDS.some((k) => description.includes(k))) return 'scheduled';

  const createdBy = asRecord(typedChange?.created_by);
  if (createdBy) {
    const type = asText(createdBy.type)?.toLowerCase();
    if (type === 'user' || type === 'human') return 'manual';
    if (type === 'bot' || type === 'system' || type === 'service') return 'automated';
  }

  const source = change.source?.toLowerCase() ?? '';
  if (source.includes('playbook') || source.includes('automation')) return 'automated';
  if (source.includes('manual') || source.includes('user')) return 'manual';

  return 'scheduled';
}

export function toBackupCalendarEntries(changes: ApplicationChange[]): BackupCalendarEntry[] {
  return filterBackupChanges(changes)
    .filter((change) => !isRestoreChange(change))
    .map((change) => {
      const status = getBackupCalendarStatus(change);
      if (!status) {
        return null;
      }

      const typedChange = getResolvedTypedChange(change);
      const startSource = asText(typedChange?.start) || change.date;
      const duration = asText(typedChange?.duration);
      const size = humanizeSize(typedChange?.size) || asText(typedChange?.size) || extractBackupLabel(change);
      const backupType = classifyBackupType(typedChange?.backup_type);
      const trigger = classifyBackupTrigger(change, typedChange);

      return {
        id: change.id,
        date: change.date,
        status,
        label: extractBackupLabel(change),
        startTime: formatStartTime(startSource),
        duration,
        size,
        backupType,
        trigger,
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

function permissionDedupeKey(change: ConfigChange): string | null {
  const action = change.category === 'rbac.granted'
    ? 'granted'
    : change.category === 'rbac.revoked'
      ? 'revoked'
      : RBAC_ADDED_TYPES.has(change.changeType)
        ? 'granted'
        : RBAC_REMOVED_TYPES.has(change.changeType)
          ? 'revoked'
          : null;
  if (!action) return null;

  const asApp = configChangeToApplicationChange(change);
  const parsed = parsePermissionSummary(asApp.description);
  const permission = asApp.permission ?? {};
  const user = cleanField(permission.user) || parsed.user || '';
  const group = cleanField(permission.group) || parsed.group || '';
  const role = cleanField(permission.role) || parsed.role || '';
  const configKey = cleanField(change.configID) || cleanField(change.configName) || '';

  return [action, configKey, user, group, role].join('|');
}

export function dedupePermissionChanges(changes: ConfigChange[]): ConfigChange[] {
  const byKey = new Map<string, ConfigChange>();
  const passthrough: ConfigChange[] = [];

  for (const change of changes) {
    const key = permissionDedupeKey(change);
    if (!key) {
      passthrough.push(change);
      continue;
    }

    const existing = byKey.get(key);
    if (!existing) {
      byKey.set(key, { ...change, count: change.count ?? 1 });
      continue;
    }

    const existingTime = existing.createdAt ? new Date(existing.createdAt).getTime() : 0;
    const candidateTime = change.createdAt ? new Date(change.createdAt).getTime() : 0;
    const keepLatest = candidateTime > existingTime
      ? { ...change, count: (existing.count ?? 1) + (change.count ?? 1) }
      : { ...existing, count: (existing.count ?? 1) + (change.count ?? 1) };
    byKey.set(key, keepLatest);
  }

  return [...byKey.values(), ...passthrough];
}

export function categorizeChanges(
  changes: ConfigChange[],
): CategorizedChanges {
  const result: CategorizedChanges = { rbac: [], backup: [], deployment: [], uncategorized: [] };

  for (const change of changes) {
    const category = change.category ?? '';

    if (category === 'rbac' || category.startsWith('rbac.')) { result.rbac.push(change); continue; }
    if (category === 'backup' || category.startsWith('backup.')) { result.backup.push(change); continue; }
    if (category === 'deployment' || category.startsWith('deployment.')) { result.deployment.push(change); continue; }

    if (!category) {
      const asApp = configChangeToApplicationChange(change);
      if (isRBACChange(asApp)) { result.rbac.push(change); continue; }
      if (isBackupChange(asApp)) { result.backup.push(change); continue; }
      if (isDeploymentChange(asApp)) { result.deployment.push(change); continue; }
    }

    result.uncategorized.push(change);
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

function asRecord(value: unknown): Record<string, unknown> | undefined {
  if (typeof value === 'object' && value !== null && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return undefined;
}

function compactMeta(values: Array<string | undefined>): string[] {
  return values.filter((value): value is string => Boolean(value));
}

function joinText(values: Array<string | undefined>, separator = ', '): string | undefined {
  const filtered = compactMeta(values);
  return filtered.length > 0 ? filtered.join(separator) : undefined;
}

function formattedLabel(label: string): string {
  return humanizeLabel(label);
}

function labelValue(label: string, value: unknown): string | undefined {
  const text = asText(value);
  return text ? `${formattedLabel(label)}: ${text}` : undefined;
}

function transition(label: string, from: unknown, to: unknown): string | undefined {
  const displayLabel = formattedLabel(label);
  const fromText = asText(from);
  const toText = asText(to);
  if (!fromText && !toText) {
    return undefined;
  }
  if (!fromText) {
    return `${displayLabel}: ${toText}`;
  }
  if (!toText) {
    return `${displayLabel}: ${fromText}`;
  }
  return `${displayLabel}: ${fromText} -> ${toText}`;
}

function toDiff(label: string, from: unknown, to: unknown): TypedChangeDiff | undefined {
  const fromText = asText(from);
  const toText = asText(to);
  if (!fromText || !toText || fromText === toText) {
    return undefined;
  }

  return { label: formattedLabel(label), from: fromText, to: toText };
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

function identityLabel(value: unknown): string | undefined {
  const record = asRecord(value);
  if (!record) {
    return undefined;
  }
  return asText(record.name) || asText(record.id) || asText(record.type);
}

function environmentLabel(value: unknown): string | undefined {
  const record = asRecord(value);
  if (!record) {
    return undefined;
  }
  return asText(record.name) || asText(record.identifier);
}

function dimensionLabel(value: unknown): string | undefined {
  const record = asRecord(value);
  if (!record) {
    return asText(value);
  }

  const desired = asText(record.desired);
  if (desired) {
    return desired;
  }

  const min = asText(record.min);
  const max = asText(record.max);
  if (min || max) {
    return joinText([min, max], '..');
  }

  return undefined;
}

function formatObjectPreview(value: unknown): string | undefined {
  const record = asRecord(value);
  if (record) {
    const entries = Object.entries(record);
    if (entries.length === 1) {
      const [key, nested] = entries[0];
      const nestedText = asText(nested) || formatObjectPreview(nested);
      return nestedText ? `${key}: ${nestedText}` : key;
    }

    try {
      return JSON.stringify(record);
    } catch {
      return undefined;
    }
  }

  if (Array.isArray(value)) {
    try {
      return JSON.stringify(value);
    } catch {
      return undefined;
    }
  }

  return asText(value);
}

function arrayCountLabel(label: string, value: unknown): string | undefined {
  return Array.isArray(value) && value.length > 0 ? `${formattedLabel(label)}: ${value.length}` : undefined;
}

function objectCountLabel(label: string, value: unknown): string | undefined {
  const record = asRecord(value);
  return record && Object.keys(record).length > 0 ? `${formattedLabel(label)}: ${Object.keys(record).length}` : undefined;
}

function sourceSummary(value: unknown): string | undefined {
  const source = asRecord(value);
  if (!source) {
    return undefined;
  }

  const git = asRecord(source.git) ?? asRecord(source.kustomization) ?? asRecord(source.argocd);
  if (git) {
    return joinText(['Git', asText(git.url) || asText(git.branch) || asText(git.commit_sha)], ': ');
  }

  const helm = asRecord(source.helm);
  if (helm) {
    return joinText(['Helm', asText(helm.chart_name) || asText(helm.repo_url)], ': ');
  }

  const image = asRecord(source.image);
  if (image) {
    const imageRef = joinText([asText(image.registry), asText(image.image)], '/');
    return joinText(['Image', imageRef || asText(image.version)], ': ');
  }

  const database = asRecord(source.database);
  if (database) {
    return joinText(['Database', asText(database.name) || asText(database.endpoint)], ': ');
  }

  const other = asText(source.other);
  if (other) {
    return joinText(['Other', other], ': ');
  }

  return undefined;
}

function changePathsLabel(value: unknown): string | undefined {
  if (!Array.isArray(value)) {
    return undefined;
  }

  const paths = value
    .map((item) => asText(asRecord(item)?.path))
    .filter((item): item is string => Boolean(item));

  if (!paths.length) {
    return undefined;
  }

  const preview = paths.slice(0, 2).join(', ');
  return `Paths: ${preview}${paths.length > 2 ? ` +${paths.length - 2} more` : ''}`;
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
    summary: asText(typedChange.user_name) || asText(typedChange.user_id),
    meta: compactMeta([
      asText(typedChange.user_email),
      labelValue('Group', typedChange.group_name || typedChange.group_id),
      labelValue('Type', typedChange.user_type),
      labelValue('Tenant', typedChange.tenant),
    ]),
  }),
  'Screenshot/v1': (typedChange) => ({
    summary: asText(typedChange.url) || asText(typedChange.artifact_id),
    meta: compactMeta([
      labelValue('Artifact', typedChange.artifact_id),
      labelValue('Type', typedChange.content_type),
      labelValue('Size', formatDimensions(typedChange.width, typedChange.height)),
      labelValue('URL', typedChange.url),
    ]),
  }),
  'PermissionChange/v1': (typedChange) => ({
    summary: asText(typedChange.user_name) || asText(typedChange.group_name) || asText(typedChange.user_id) || asText(typedChange.group_id),
    meta: compactMeta([
      labelValue('Role', typedChange.role_name || typedChange.role_id),
      labelValue('Role Type', typedChange.role_type),
      labelValue('Scope', typedChange.scope),
    ]),
  }),
  'GroupMembership/v1': (typedChange) => {
    const action = asText(typedChange.action);
    const member = identityLabel(typedChange.member);
    return {
      summary: joinText([action, member], ' ') || member || identityLabel(typedChange.group),
      meta: compactMeta([
        labelValue('Group', identityLabel(typedChange.group)),
        labelValue('Tenant', typedChange.tenant),
      ]),
    };
  },
  'Identity/v1': (typedChange) => ({
    summary: identityLabel(typedChange),
    meta: compactMeta([
      labelValue('Type', typedChange.type),
      labelValue('Comment', typedChange.comment),
    ]),
  }),
  'GitSource/v1': (typedChange) => ({
    summary: asText(typedChange.url),
    meta: compactMeta([
      labelValue('Branch', typedChange.branch),
      labelValue('Commit', typedChange.commit_sha),
      labelValue('Version', typedChange.version),
      labelValue('Tags', typedChange.tags),
    ]),
  }),
  'HelmSource/v1': (typedChange) => ({
    summary: asText(typedChange.chart_name),
    meta: compactMeta([
      labelValue('Version', typedChange.chart_version),
      labelValue('Repo', typedChange.repo_url),
    ]),
  }),
  'ImageSource/v1': (typedChange) => ({
    summary: joinText([asText(typedChange.registry), asText(typedChange.image)], '/'),
    meta: compactMeta([
      labelValue('Version', typedChange.version),
      labelValue('SHA', typedChange.sha),
    ]),
  }),
  'DatabaseSource/v1': (typedChange) => ({
    summary: asText(typedChange.name) || asText(typedChange.endpoint),
    meta: compactMeta([
      labelValue('Type', typedChange.type),
      labelValue('Schema', typedChange.schema),
      labelValue('Version', typedChange.version),
      labelValue('Endpoint', typedChange.endpoint),
    ]),
  }),
  'Source/v1': (typedChange) => ({
    summary: sourceSummary(typedChange),
    meta: compactMeta([
      labelValue('Path', typedChange.path),
      labelValue('Other', typedChange.other),
    ]),
  }),
  'Environment/v1': (typedChange) => ({
    summary: environmentLabel(typedChange),
    meta: compactMeta([
      labelValue('Type', typedChange.type),
      labelValue('Stage', typedChange.stage),
      labelValue('Identifier', typedChange.identifier),
      objectCountLabel('Tags', typedChange.tags),
    ]),
  }),
  'Event/v1': (typedChange) => ({
    summary: asText(typedChange.id),
    meta: compactMeta([
      labelValue('URL', typedChange.url),
      labelValue('Timestamp', typedChange.timestamp),
      objectCountLabel('Tags', typedChange.tags),
      objectCountLabel('Properties', typedChange.properties),
    ]),
  }),
  'Deployment/v1': (typedChange) => {
    const imageDiff = toDiff('Image', typedChange.previous_image, typedChange.new_image);
    return {
      summary: asText(typedChange.container),
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
    const fromEnvironment = environmentLabel(typedChange.from) || asText(typedChange.from_environment);
    const toEnvironment = environmentLabel(typedChange.to) || asText(typedChange.to_environment);
    const environmentDiff = toDiff('Environment', fromEnvironment, toEnvironment);
    return {
      summary: asText(typedChange.artifact) || asText(typedChange.version),
      meta: compactMeta([
        environmentDiff ? undefined : transition('Environment', fromEnvironment, toEnvironment),
        labelValue('Version', typedChange.version),
        labelValue('Artifact', typedChange.artifact),
        labelValue('Source', sourceSummary(typedChange.source)),
        arrayCountLabel('Approvals', typedChange.approvals),
      ]),
      diff: environmentDiff,
    };
  },
  'Approval/v1': (typedChange) => {
    const submittedBy = identityLabel(typedChange.submitted_by) || asText(typedChange.submitted_by);
    const approver = identityLabel(typedChange.approver) || asText(typedChange.approved_by) || asText(typedChange.rejected_by);
    const status = asText(typedChange.status)
      || (asText(typedChange.approved_by) ? 'Approved' : undefined)
      || (asText(typedChange.rejected_by) ? 'Rejected' : undefined);
    const summary = approver && status
      ? `${status} by ${approver}`
      : submittedBy
        ? `Submitted by ${submittedBy}`
        : status
          ? `${status} approval`
          : 'Approval decision';
    return {
      summary,
      meta: compactMeta([
        labelValue('Submitted By', submittedBy),
        labelValue('Approver', approver),
        labelValue('Stage', typedChange.stage),
        labelValue('Status', status),
        labelValue('Playbook', typedChange.playbook_id),
        labelValue('Run', typedChange.run_id),
        labelValue('Reason', typedChange.reason),
      ]),
    };
  },
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
    summary: environmentLabel(typedChange.environment) || asText(typedChange.target) || asText(typedChange.backup_type),
    meta: compactMeta([
      labelValue('Status', typedChange.status),
      labelValue('Type', typedChange.backup_type),
      labelValue('Created By', identityLabel(typedChange.created_by)),
      labelValue('Environment', environmentLabel(typedChange.environment)),
      labelValue('Target', typedChange.target),
      labelValue('Size', humanizeSize(typedChange.size) ?? typedChange.size),
      labelValue('Delta', humanizeSize(typedChange.delta) ?? typedChange.delta),
      labelValue('Duration', typedChange.duration),
      labelValue('End', typedChange.end),
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
      summary: asText(typedChange.resource_type),
      meta: compactMeta([
        labelValue('Resource', typedChange.resource_type),
        replicaDiff ? undefined : transition('Replicas', typedChange.from_replicas, typedChange.to_replicas),
        labelValue('Trigger', typedChange.trigger),
      ]),
      diff: replicaDiff,
    };
  },
  'Scale/v1': (typedChange) => {
    const previousValue = dimensionLabel(typedChange.previous_value);
    const currentValue = dimensionLabel(typedChange.value);
    const label = asText(typedChange.dimension) || 'Value';
    const scaleDiff = toDiff(label, previousValue, currentValue);
    return {
      summary: typedChange.dimension ? `${typedChange.dimension} scaling` : 'Scale change',
      meta: compactMeta([
        scaleDiff ? undefined : transition(label, previousValue, currentValue),
      ]),
      diff: scaleDiff,
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
    const pipeline = asText(typedChange.pipeline_name) || asText(typedChange.pipeline_id) || environmentLabel(typedChange.environment);
    return {
      summary: pipeline,
      meta: compactMeta([
        labelValue('Run', typedChange.run_number ?? typedChange.run_id),
        labelValue('Branch', typedChange.branch),
        labelValue('Environment', environmentLabel(typedChange.environment)),
        labelValue('Status', typedChange.status),
        labelValue('Duration', typedChange.duration),
        labelValue('Error', typedChange.error),
      ]),
    };
  },
  'Change/v1': (typedChange) => {
    const changeDiff = toDiff('Value', formatObjectPreview(typedChange.from), formatObjectPreview(typedChange.to));
    return {
      summary: asText(typedChange.path) || 'Field change',
      meta: compactMeta([
        labelValue('Type', typedChange.type),
        changeDiff ? undefined : transition('Value', formatObjectPreview(typedChange.from), formatObjectPreview(typedChange.to)),
      ]),
      diff: changeDiff,
    };
  },
  'ConfigChange/v1': (typedChange) => {
    const changeCount = Array.isArray(typedChange.changes) ? typedChange.changes.length : 0;
    return {
      summary: changeCount > 0 ? `${changeCount} field change${changeCount === 1 ? '' : 's'}` : 'Config change',
      meta: compactMeta([
        labelValue('Author', identityLabel(typedChange.author)),
        labelValue('Environment', environmentLabel(typedChange.environment)),
        labelValue('Source', sourceSummary(typedChange.source)),
        changePathsLabel(typedChange.changes),
      ]),
    };
  },
  'Restore/v1': (typedChange) => {
    const fromEnvironment = environmentLabel(typedChange.from);
    const toEnvironment = environmentLabel(typedChange.to);
    const environmentDiff = toDiff('Environment', fromEnvironment, toEnvironment);
    return {
      summary: sourceSummary(typedChange.source) || asText(typedChange.status) || 'Restore job',
      meta: compactMeta([
        environmentDiff ? undefined : transition('Environment', fromEnvironment, toEnvironment),
        labelValue('Source', sourceSummary(typedChange.source)),
        labelValue('Status', typedChange.status),
      ]),
      diff: environmentDiff,
    };
  },
  'Test/v1': (typedChange) => ({
    summary: asText(typedChange.name) || asText(typedChange.id),
    meta: compactMeta([
      labelValue('Type', typedChange.type),
      labelValue('Status', typedChange.status),
      labelValue('Result', typedChange.result),
      labelValue('Description', typedChange.description),
    ]),
  }),
  'Dimension/v1': (typedChange) => ({
    summary: dimensionLabel(typedChange),
    meta: compactMeta([
      labelValue('Min', typedChange.min),
      labelValue('Max', typedChange.max),
      labelValue('Desired', typedChange.desired),
    ]),
  }),
};

export function getTypedChangeDisplay(change: ConfigChange): TypedChangeDisplay | undefined {
  const typedChange = getResolvedTypedChange(change);
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

export function applicationChangeToConfigChange(c: ApplicationChange): ConfigChange {
  const severity = (['critical', 'high', 'medium', 'low', 'info'] as const).includes(c.status as any)
    ? (c.status as ConfigChange['severity'])
    : undefined;
  return {
    id: c.id || undefined,
    configID: c.configId,
    configName: c.configName,
    configType: c.configType,
    changeType: c.changeType || 'Change',
    category: c.category,
    severity,
    source: c.source,
    summary: c.description,
    details: c.details,
    typedChange: c.typedChange,
    createdBy: c.createdBy,
    createdAt: c.createdAt || c.date,
  };
}

export function configChangeToApplicationChange(c: ConfigChange, category?: string): ApplicationChange {
  const typedChange = getResolvedTypedChange(c);
  const permission = (c.details?.permission as ApplicationPermissionChange | undefined) ?? permissionFromTypedChange(typedChange);
  const typedDisplay = getTypedChangeDisplay({ ...c, typedChange });
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
    details: c.details,
    typedChange,
    description: c.summary ?? typedDisplay?.summary ?? humanizeLabel(c.changeType || 'Change'),
    status: c.severity ?? 'info',
    createdAt: c.createdAt ?? '',
  };
}
