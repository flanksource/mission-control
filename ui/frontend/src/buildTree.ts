export type TypeNode = {
  /** The segment label for this node (e.g. "AWS", "EC2", "Instance"). */
  label: string;
  /** Config type prefix represented by this node (e.g. "AWS::EC2"). */
  typePath: string;
  /** Best parent type prefix to use as an icon fallback. */
  parentTypePath?: string;
  /** Full config_type string — only set on leaves (e.g. "AWS::EC2::Instance"). */
  fullType?: string;
  /** Aggregated count for this subtree (leaf = its own count; branch = sum of descendants). */
  count: number;
  children: TypeNode[];
};

/**
 * Groups flat config_type entries (e.g. "AWS::EC2::Instance") into a nested
 * hierarchy by splitting on "::". Siblings are sorted alphabetically.
 */
export function buildTypeTree(
  entries: Array<{ type: string; count: number }>,
): TypeNode[] {
  const root: TypeNode = { label: "", typePath: "", count: 0, children: [] };

  for (const { type, count } of entries) {
    if (!type) continue;
    const segments = type.split("::");
    let cursor = root;
    for (let i = 0; i < segments.length; i++) {
      const label = segments[i];
      const typePath = segments.slice(0, i + 1).join("::");
      const parentTypePath = cursor.typePath || undefined;
      let child = cursor.children.find((c) => c.label === label);
      if (!child) {
        child = { label, typePath, parentTypePath, count: 0, children: [] };
        cursor.children.push(child);
      }
      child.count += count;
      if (i === segments.length - 1) {
        child.fullType = type;
      }
      cursor = child;
    }
    root.count += count;
  }

  sortTree(root);
  return root.children.map(collapseFolderOnlyEdges);
}

function sortTree(node: TypeNode) {
  node.children.sort((a, b) => a.label.localeCompare(b.label));
  for (const child of node.children) sortTree(child);
}

function collapseFolderOnlyEdges(node: TypeNode): TypeNode {
  const collapsedChildren = node.children.map(collapseFolderOnlyEdges);
  let current: TypeNode = { ...node, children: collapsedChildren };

  while (!current.fullType && current.children.length === 1) {
    const child = current.children[0];
    current = {
      ...child,
      label: [current.label, child.label].filter(Boolean).join(" / "),
      parentTypePath: child.parentTypePath ?? current.typePath ?? current.parentTypePath,
    };
  }

  return current;
}
