import React from 'react';
import { Section } from '@flanksource/facet';
import type { ConfigChange } from '../config-types.ts';
import ConfigChangesSection from './ConfigChangesSection.tsx';

interface Props {
  changes?: ConfigChange[];
}

function pickMatching(changes: ConfigChange[], predicate: (change: ConfigChange) => boolean, limit: number): ConfigChange[] {
  return changes.filter(predicate).slice(0, limit);
}

export default function ConfigChangesExamples({ changes }: Props) {
  if (!changes?.length) {
    return null;
  }

  const singleLine = pickMatching(
    changes,
    (change) => !change.summary || change.summary.length <= 72 || Boolean(change.typedChange?.kind),
    6,
  );
  const typedDiffs = pickMatching(
    changes,
    (change) => ['Deployment/v1', 'Promotion/v1', 'Rollback/v1', 'Scaling/v1', 'CostChange/v1'].includes(change.typedChange?.kind ?? ''),
    5,
  );
  const visualStates = pickMatching(
    changes,
    (change) => (
      (change.severity && change.severity !== 'info')
      || Boolean(change.artifacts?.length)
      || (change.changeType || '').toLowerCase().includes('backup')
      || (change.changeType || '').toLowerCase().includes('permission')
      || change.typedChange?.kind === 'Screenshot/v1'
    ),
    6,
  );

  return (
    <>
      {singleLine.length > 0 && (
        <Section variant="hero" title="ConfigChangesExamples" size="md">
          <div className="text-xs text-gray-500 mb-[2mm]">
            Compact rows optimized for one-line scanning. Change type, diff chips, config, actor, counters, and severity stay inline whenever the summary is short enough.
          </div>
          <ConfigChangesSection changes={singleLine} hideConfigName />
        </Section>
      )}

      {typedDiffs.length > 0 && (
        <Section variant="hero" title="Typed Diff Variations" size="md">
          <div className="text-xs text-gray-500 mb-[2mm]">
            Typed changes show richer before/after chips for images, environments, versions, replicas, and costs instead of a generic diff label.
          </div>
          <ConfigChangesSection changes={typedDiffs} />
        </Section>
      )}

      {visualStates.length > 0 && (
        <Section variant="hero" title="Badge Color Variations" size="md">
          <div className="text-xs text-gray-500 mb-[2mm]">
            Permission, backup, artifact, release, and higher-severity changes now use distinct badge accents to separate activity types at a glance.
          </div>
          <ConfigChangesSection changes={visualStates} />
        </Section>
      )}
    </>
  );
}
