import React from 'react';
import { Document, Page, Header, Footer } from '@flanksource/facet';
import type { RBACReport, RBACResource } from './rbac-types.ts';
import RBACSummarySection from './components/RBACSummarySection.tsx';
import RBACMatrixSection, { MatrixLegend } from './components/RBACMatrixSection.tsx';
import RBACChangelogSection from './components/RBACChangelogSection.tsx';
import RBACCoverContent from './components/RBACCoverContent.tsx';
import PageHeader from './components/PageHeader.tsx';
import PageFooter from './components/PageFooter.tsx';

const NIL_UUID = '00000000-0000-0000-0000-000000000000';

function estimateBlockHeight(rowRoles: Map<string, Set<string>>, roles: Set<string>): number {
  if (rowRoles.size === 0 || roles.size === 0) return 0;
  if (roles.size <= 10) {
    return 18 + rowRoles.size * 2.4 + 5;
  }

  const columnCounts = new Map<string, number>();
  for (const row of rowRoles.values()) {
    for (const role of row) {
      columnCounts.set(role, (columnCounts.get(role) || 0) + 1);
    }
  }

  let sparseCount = 0;
  let matrixRows = 0;
  const matrixRoles = new Set<string>();

  for (const row of rowRoles.values()) {
    let remainingInRow = 0;
    for (const role of row) {
      if (row.size === 1 && (columnCounts.get(role) || 0) === 1) {
        sparseCount += 1;
        continue;
      }
      remainingInRow += 1;
      matrixRoles.add(role);
    }
    if (remainingInRow > 0) matrixRows += 1;
  }

  return 5 + sparseCount * 2.8 + (matrixRows > 0 && matrixRoles.size > 0 ? 18 + matrixRows * 2.4 + 5 : 0);
}

function estimateHeight(resource: RBACResource): number {
  const allRoles = new Set<string>();
  const directRoles = new Set<string>();
  const indirectRoles = new Set<string>();
  const directRows = new Map<string, Set<string>>();
  const indirectRows = new Map<string, Set<string>>();

  const addRole = (rows: Map<string, Set<string>>, rowKey: string, role: string) => {
    const row = rows.get(rowKey) || new Set<string>();
    row.add(role);
    rows.set(rowKey, row);
  };

  for (const u of resource.users || []) {
    allRoles.add(u.role);
    if (u.roleSource?.startsWith('group:')) {
      const groupName = u.roleSource.slice(6);
      addRole(indirectRows, `group:${groupName}`, u.role);
      if (u.userId && u.userId !== NIL_UUID) {
        addRole(indirectRows, `group:${groupName}:user:${u.userId}`, u.role);
      }
      indirectRoles.add(u.role);
    } else {
      addRole(directRows, `user:${u.userId}`, u.role);
      directRoles.add(u.role);
    }
  }
  if (allRoles.size <= 10) {
    return 18 + (directRows.size + indirectRows.size) * 2.4 + 5;
  }

  let height = 5;
  height += estimateBlockHeight(directRows, directRoles);
  height += estimateBlockHeight(indirectRows, indirectRoles);
  return height;
}

function packResources(resources: RBACResource[], maxHeight: number): RBACResource[][] {
  const pages: RBACResource[][] = [];
  let current: RBACResource[] = [];
  let currentHeight = 0;

  for (const r of resources) {
    const h = estimateHeight(r);
    if (currentHeight + h > maxHeight && current.length > 0) {
      pages.push(current);
      current = [r];
      currentHeight = h;
    } else {
      current.push(r);
      currentHeight += h;
    }
  }
  if (current.length > 0) pages.push(current);
  return pages;
}

interface RBACMatrixReportProps {
  data: RBACReport;
}

export default function RBACMatrixReportPage({ data }: RBACMatrixReportProps) {
  const resourcePages = packResources(data.resources || [], 160);

  return (
    <Document pageSize="a4-landscape" margins={{ top: 1, bottom: 1, left: 5, right: 5 }}>
      <Header height={8}>
        <PageHeader subtitle="RBAC Matrix" />
      </Header>
      <Footer height={14}>
        <PageFooter generatedAt={data.generatedAt}><MatrixLegend /></PageFooter>
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <RBACCoverContent report={data} subtitle="RBAC Matrix Report" />
      </Page>

      <Page>
        <RBACSummarySection summary={data.summary} />

        {resourcePages.map((group, pageIdx) => (
          <div key={pageIdx} className="flex flex-col gap-[4mm]">
            {group.map((resource, idx) => (
              <RBACMatrixSection key={idx} resource={resource} />
            ))}
          </div>
        ))}

        <RBACChangelogSection changelog={data.changelog} />
      </Page>
    </Document>
  );
}
