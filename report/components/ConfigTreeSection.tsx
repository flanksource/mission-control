import React from 'react';
import { Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportTreeNode } from '../catalog-report-types.ts';

function TreeNodeRow({ node, isRoot = false }: { node: CatalogReportTreeNode; isRoot?: boolean }) {
  const isTarget = node.edgeType === 'target';
  const children = node.children || [];
  const shortID = node.id ? node.id.slice(0, 8) : '';

  return (
    <div>
      <div className="flex items-center gap-[0.8mm] leading-tight py-0">
        {!isRoot && <span className="inline-block h-px w-[1.5mm] shrink-0 bg-slate-300" />}
        {node.type && <Icon name={node.type} size={9} />}
        <span className={`text-xs ${isTarget ? 'font-semibold text-slate-900' : 'text-slate-800'}`}>
          {node.name}
        </span>
        {node.type && (
          <span className="text-xs text-slate-500">({node.type})</span>
        )}
        {shortID && (
          <span className="text-xs font-mono text-slate-400">{shortID}</span>
        )}
        {node.relation && (
          <span className="text-xs text-slate-400 italic">{node.relation}</span>
        )}
      </div>
      {children.length > 0 && (
        <div className={`${isRoot ? 'ml-[1mm]' : 'ml-[2.5mm]'} border-l border-slate-300 pl-[1.5mm]`}>
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
    <Section title="Config Relationships" size="sm">
      <TreeNodeRow node={tree} isRoot />
    </Section>
  );
}
