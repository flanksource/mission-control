import React from 'react';
import { Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportTreeNode } from '../catalog-report-types.ts';

function TreeNodeRow({ node, isRoot = false }: { node: CatalogReportTreeNode; isRoot?: boolean }) {
  const isTarget = node.edgeType === 'target';
  const children = node.children || [];

  return (
    <div>
      <div className="flex items-center gap-[1.2mm] py-[0.15mm] min-h-[4.5mm]">
        {!isRoot && <span className="inline-block h-px w-[2.5mm] shrink-0 bg-slate-300" />}
        {node.type && <Icon name={node.type} size={11} />}
        <span className={`text-sm ${isTarget ? 'font-semibold text-slate-900' : 'font-medium text-slate-800'}`}>
          {node.name}
        </span>
        {node.type && (
          <span className="text-[8pt] text-slate-500">({node.type})</span>
        )}
        {node.relation && (
          <span className="text-[8pt] text-slate-400 italic">{node.relation}</span>
        )}
      </div>
      {children.length > 0 && (
        <div className={`${isRoot ? 'ml-[1.5mm]' : 'ml-[4mm]'} border-l border-slate-300 pl-[2.5mm] space-y-[0.2mm]`}>
          {children.map((child, idx) => (
            <TreeNodeRow key={child.id || idx} node={child} />
          ))}
        </div>
      )}
    </div>
  );
}

interface Props {
  tree: CatalogReportTreeNode;
}

export default function ConfigTreeSection({ tree }: Props) {
  if (!tree || !(tree.children || []).length) return null;

  return (
    <Section variant="hero" title="Config Relationships" size="md">
      <TreeNodeRow node={tree} isRoot />
    </Section>
  );
}
