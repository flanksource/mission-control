import type { ApplicationChange, ApplicationPermissionChange } from '../types.ts';

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

const RBAC_ADDED_TYPES = new Set(['PermissionGranted', 'PermissionAdded']);
const RBAC_REMOVED_TYPES = new Set(['PermissionRevoked', 'PermissionRemoved']);
const BACKUP_SUCCESS_TYPES = new Set(['BackupCompleted', 'BackupSuccessful']);
const BACKUP_FAILED_TYPES = new Set(['BackupFailed']);
const BACKUP_PROGRESS_TYPES = new Set(['BackupStarted', 'BackupRunning', 'BackupEnqueued']);
const RESTORE_CHANGE_TYPES = new Set(['BackupRestored', 'RestoreCompleted']);
const DEPLOYMENT_CHANGE_TYPES = new Set(['ScalingReplicaSet', 'PolicyUpdate']);

function normalizedType(change: ApplicationChange): string {
  return change.changeType ?? '';
}

export function getChangeActor(change: ApplicationChange): string {
  return change.createdBy || change.source || '-';
}

function normalizeRBACAction(change: ApplicationChange): RBACChangeAction | null {
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
  const type = normalizedType(change);
  return BACKUP_SUCCESS_TYPES.has(type) || BACKUP_FAILED_TYPES.has(type) || BACKUP_PROGRESS_TYPES.has(type) || RESTORE_CHANGE_TYPES.has(type);
}

export function isRestoreChange(change: ApplicationChange): boolean {
  return RESTORE_CHANGE_TYPES.has(normalizedType(change));
}

export function classifyDeploymentChange(change: ApplicationChange): 'scale' | 'policy' | 'spec' | null {
  const type = normalizedType(change);
  const description = change.description.toLowerCase();
  const lowerType = type.toLowerCase();

  if (type === 'ScalingReplicaSet' || lowerType.includes('replicaset') || /scaled|scaling|replica/.test(description)) {
    return 'scale';
  }

  if (type === 'PolicyUpdate' || description.includes('policy')) {
    return 'policy';
  }

  if (
    DEPLOYMENT_CHANGE_TYPES.has(type) ||
    (type === 'diff' && (
      description.includes('deployment') ||
      description.includes('rollout') ||
      description.includes('image updated')
    )) ||
    description.includes('deployment') ||
    description.includes('rollout') ||
    description.includes('image updated')
  ) {
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

export function inferChangeSectionVariant(title: string, changes: ApplicationChange[]): ChangeSectionVariant {
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

  const rbacCount = filterRBACChanges(changes).length;
  const backupCount = filterBackupChanges(changes).length;
  const deploymentCount = filterDeploymentChanges(changes).length;

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
