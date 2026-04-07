import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ConfigChangesSection from '../components/ConfigChangesSection.tsx';
import RBACChanges from '../components/RBACChanges.tsx';
import BackupChanges from '../components/BackupChanges.tsx';
import DeploymentChanges from '../components/DeploymentChanges.tsx';
import { categorizeChanges, configChangeToApplicationChange } from '../components/change-section-utils.ts';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function ChangesPage({ data, pageProps }: Props) {
  const rbacChanges = data.rbacChanges ?? [];
  const backupChanges = data.backupChanges ?? [];
  const deploymentChanges = data.deploymentChanges ?? [];
  const categoryMappings = (data as any).categoryMappings as Record<string, string[]> | undefined;
  const categorized = categorizeChanges(data.changes ?? [], categoryMappings);

  return (
    <Page {...pageProps}>
      <Section variant="hero" title="Auto-Categorized Changes" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          A single changes array auto-split into specialized sections using categoryMappings. RBAC, backup, and deployment changes get their own renderers; the rest falls through to ConfigChangesSection.
        </div>
        {categorized.rbac.length > 0 && (
          <Section variant="hero" title="Permission Changes" size="md">
            <RBACChanges changes={categorized.rbac.map(({ change, category }) => configChangeToApplicationChange(change, category))} />
          </Section>
        )}
        {categorized.backup.length > 0 && (
          <Section variant="hero" title="Backup Activity" size="md">
            <BackupChanges changes={categorized.backup.map(({ change, category }) => configChangeToApplicationChange(change, category))} />
          </Section>
        )}
        {categorized.deployment.length > 0 && (
          <Section variant="hero" title="Deployment Changes" size="md">
            <DeploymentChanges changes={categorized.deployment.map(({ change, category }) => configChangeToApplicationChange(change, category))} />
          </Section>
        )}
        {categorized.uncategorized.length > 0 && (
          <ConfigChangesSection changes={categorized.uncategorized} />
        )}
      </Section>

      <ConfigChangesSection changes={data.changes} />

      <Section variant="hero" title="RBACChanges" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Groups permission changes by date and resource, shows config type icons in resource headers, and renders compact granted/revoked audit rows with role, principal, timestamp, and changed-by attribution.
        </div>
        <RBACChanges changes={rbacChanges} />
      </Section>

      <Section variant="hero" title="BackupChanges" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Backup calendar/heatmap pattern with event stream. Filters out non-backup change types.
        </div>
        <BackupChanges changes={backupChanges} />
      </Section>

      <Section variant="hero" title="DeploymentChanges" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Highlights deployment-relevant spec, scaling, and policy changes.
        </div>
        <DeploymentChanges changes={deploymentChanges} />
      </Section>
    </Page>
  );
}
