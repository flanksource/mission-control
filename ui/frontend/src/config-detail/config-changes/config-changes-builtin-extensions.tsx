import { Section } from './facet-components.tsx';
import { Icon } from './icon.tsx';
import type { ConfigChange } from './types.ts';
import BackupChanges from './BackupChanges.tsx';
import DeploymentChanges from './DeploymentChanges.tsx';
import ConfigChangesSection from './ConfigChangesSection.tsx';
import {
  configChangeToApplicationChange,
  dedupePermissionChanges,
  isBackupChange,
  isDeploymentChange,
  isRBACChange,
} from './change-section-utils.ts';
import type {
  ConfigChangesExtension,
  ConfigChangesExtensionProps,
} from './config-changes-extension.ts';

function hasCategoryPrefix(change: ConfigChange, prefix: string): boolean {
  const category = change.category ?? '';
  return category === prefix || category.startsWith(`${prefix}.`);
}

function matchesCategoryOr(change: ConfigChange, prefix: string, predicate: (c: ConfigChange) => boolean): boolean {
  if (hasCategoryPrefix(change, prefix)) return true;
  if (change.category) return false;
  return predicate(change);
}

function rbacFilter(change: ConfigChange): boolean {
  return matchesCategoryOr(change, 'rbac', (c) => isRBACChange(configChangeToApplicationChange(c)));
}

function backupFilter(change: ConfigChange): boolean {
  return matchesCategoryOr(change, 'backup', (c) => isBackupChange(configChangeToApplicationChange(c)));
}

function deploymentFilter(change: ConfigChange): boolean {
  return matchesCategoryOr(change, 'deployment', (c) => isDeploymentChange(configChangeToApplicationChange(c)));
}

interface BackupConfigGroup {
  configId: string;
  configName: string;
  configType?: string;
  changes: ConfigChange[];
}

function groupChangesByConfig(changes: ConfigChange[]): BackupConfigGroup[] {
  const byID = new Map<string, BackupConfigGroup>();
  for (const c of changes) {
    const id = c.configID || c.configName || '';
    let group = byID.get(id);
    if (!group) {
      group = {
        configId: c.configID || '',
        configName: c.configName || 'Unknown',
        configType: c.configType,
        changes: [],
      };
      byID.set(id, group);
    }
    if (!group.configType && c.configType) group.configType = c.configType;
    group.changes.push(c);
  }
  return [...byID.values()];
}

function BackupActivityPanel({ changes }: ConfigChangesExtensionProps) {
  const groups = groupChangesByConfig(changes);
  return (
    <Section variant="hero" title="Backup Activity" size="md">
      {groups.map((g, idx) => (
        <div key={g.configId || idx} className={idx > 0 ? 'mt-[4mm]' : ''}>
          <div className="flex items-center gap-[2mm] mb-[2mm]">
            {g.configType && <Icon name={g.configType} size={12} />}
            <span className="text-sm font-semibold text-slate-800">{g.configName}</span>
            {g.configType && <span className="text-xs text-gray-500">{g.configType}</span>}
          </div>
          <BackupChanges changes={g.changes.map((c) => configChangeToApplicationChange(c))} />
        </div>
      ))}
    </Section>
  );
}

function DeploymentChangesPanel({ changes }: ConfigChangesExtensionProps) {
  return (
    <Section variant="hero" title="Deployment Changes" size="md">
      <DeploymentChanges changes={changes.map((c) => configChangeToApplicationChange(c))} />
    </Section>
  );
}

function RBACChangesPanel({ changes }: ConfigChangesExtensionProps) {
  const deduped = dedupePermissionChanges(changes);
  return (
    <Section variant="hero" title="Permission Changes" size="md">
      <ConfigChangesSection changes={deduped} />
    </Section>
  );
}

export const rbacExtension: ConfigChangesExtension = {
  key: 'rbac',
  filter: rbacFilter,
  panel: RBACChangesPanel,
  drop: true,
};

export const backupExtension: ConfigChangesExtension = {
  key: 'backup',
  filter: backupFilter,
  panel: BackupActivityPanel,
  drop: true,
};

export const deploymentExtension: ConfigChangesExtension = {
  key: 'deployment',
  filter: deploymentFilter,
  panel: DeploymentChangesPanel,
  drop: true,
};

export const defaultConfigChangesExtensions: ConfigChangesExtension[] = [
  rbacExtension,
  backupExtension,
  deploymentExtension,
];
