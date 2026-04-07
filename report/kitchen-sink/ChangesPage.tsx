import React from 'react';
import { Page, Section } from '@flanksource/facet';
import type { KitchenSinkData } from './KitchenSinkTypes.ts';
import ConfigChangesSection from '../components/ConfigChangesSection.tsx';
import RBACChanges from '../components/RBACChanges.tsx';
import BackupChanges from '../components/BackupChanges.tsx';
import DeploymentChanges from '../components/DeploymentChanges.tsx';

interface Props {
  data: KitchenSinkData;
  pageProps: any;
}

export default function ChangesPage({ data, pageProps }: Props) {
  const rbacChanges = data.rbacChanges ?? [];
  const backupChanges = data.backupChanges ?? [];
  const deploymentChanges = data.deploymentChanges ?? [];

  return (
    <Page {...pageProps}>
      <ConfigChangesSection changes={data.changes} />

      <Section variant="hero" title="RBACChanges" size="md">
        <div className="text-xs text-gray-500 mb-[2mm]">
          Groups permission changes by config and renders granted/revoked audit rows with role, principal, timestamp, and changed-by attribution.
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
