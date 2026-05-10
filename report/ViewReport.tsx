import React from 'react';
import { Document, Page, Header, Footer } from '@flanksource/facet';
import type { ViewReportData, MultiViewReportData } from './view-types.ts';
import ViewResultSection from './components/ViewResultSection.tsx';
import CoverPage from './components/CoverPage.tsx';
import PageHeader from './components/PageHeader.tsx';
import PageFooter from './components/PageFooter.tsx';

function ViewCoverPage({ data }: { data: ViewReportData }) {
  const variableTags = (data.variables || []).reduce((acc, v) => {
    acc[v.label || v.key] = v.default || '-';
    return acc;
  }, {} as Record<string, string>);

  return (
    <CoverPage
      title={data.title || data.name}
      icon={data.icon}
      query={data.namespace ? `${data.namespace}/${data.name}` : undefined}
      tags={Object.keys(variableTags).length > 0 ? variableTags : undefined}
    />
  );
}

function isMultiView(data: any): data is MultiViewReportData {
  return data && Array.isArray(data.views);
}

interface ViewReportProps {
  data: ViewReportData | MultiViewReportData;
}

export default function ViewReportPage({ data }: ViewReportProps) {
  const viewsList = isMultiView(data) ? data.views : [data];
  if (!viewsList.length) return null;
  const firstView = viewsList[0];

  return (
    <Document pageSize="a4" margins={{ top: 3, bottom: 3, left: 10, right: 10 }}>
      <Header height={10}>
        <PageHeader subtitle="View Report" />
      </Header>
      <Footer height={10}>
        <PageFooter />
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
        <ViewCoverPage data={firstView} />
      </Page>

      {viewsList.map((view, idx) => (
        <Page key={idx}>
          <div className="text-xs">
            <ViewResultSection data={view} />
          </div>
        </Page>
      ))}
    </Document>
  );
}
