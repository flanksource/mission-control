import React from 'react';
import { Section } from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import type { CatalogReportTreeNode } from '../catalog-report-types.ts';

const EDGE_STYLES: Record<string, { border: string }> = {
  parent: { border: 'border-l-blue-300' },
  child: { border: 'border-l-green-300' },
  related: { border: 'border-l-purple-300' },
  target: { border: 'border-l-blue-500' },
};

function TreeNodeRow({ node, depth = 0 }: { node: CatalogReportTreeNode; depth?: number }) {
  const style = EDGE_STYLES[node.edgeType || 'child'] || EDGE_STYLES.child;
  const isTarget = node.edgeType === 'target';
  const children = node.children || [];

  return (
    <div style={{ marginLeft: `${depth * 4}mm` }}>
      <div className={`flex items-center gap-[1.5mm] py-[0.4mm] border-l-2 pl-[1.5mm] ${style.border}`}>
        {node.type && <Icon name={node.type} size={10} />}
        <span className={`text-xs ${isTarget ? 'font-bold text-blue-700' : 'font-medium text-slate-800'}`}>
          {node.name}
        </span>
        {node.type && (
          <span className="text-xs text-gray-500">({node.type})</span>
        )}
        {node.relation && (
          <span className="text-xs text-purple-400 italic">{node.relation}</span>
        )}
      </div>
      {children.map((child, idx) => (
        <TreeNodeRow key={child.id || idx} node={child} depth={depth + 1} />
      ))}
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
      <TreeNodeRow node={tree} />
    </Section>
  );
}
