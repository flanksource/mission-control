import React from 'react';
import { Page, PageBreak, Section } from '@flanksource/facet';
import type { ConfigReportData } from './config-types.ts';
import ConfigLink from './components/ConfigLink.tsx';
import ConfigChangesSection from './components/ConfigChangesSection.tsx';
import ConfigInsightsSection from './components/ConfigInsightsSection.tsx';
import ConfigRelationshipGraph from './components/ConfigRelationshipGraph.tsx';
import ConfigItemCard from './components/ConfigItemCard.tsx';
import ScraperCard from './components/ScraperCard.tsx';
import { MatrixTable, Dot } from '@flanksource/facet';
import type { ScraperInfo } from './scraper-types.ts';

const defaultData: ConfigReportData = {
  configItem: {
    id: 'cfg-eks-001', name: 'prod-eks-cluster', type: 'AWS::EKS::Cluster',
    configClass: 'Cluster', status: 'Active', health: 'healthy',
    description: 'Production EKS cluster running Mission Control workloads in us-east-1',
    labels: { env: 'production', team: 'platform', region: 'us-east-1' },
    costTotal30d: 4280.50, createdAt: '2025-03-15T09:00:00Z', updatedAt: '2026-03-28T12:00:00Z',
  },
  changes: [
    { id: 'chg-001', configID: 'cfg-eks-001', changeType: 'diff', severity: 'info', source: 'kubernetes', summary: 'Node pool autoscaler adjusted desired count from 3 to 5', createdBy: 'cluster-autoscaler', createdAt: '2026-03-30T08:15:00Z', count: 1 },
    { id: 'chg-002', configID: 'cfg-eks-001', changeType: 'Pulled', severity: 'info', source: 'kubernetes', summary: 'Image flanksource/incident-commander:v1.4.200 pulled on node ip-10-0-1-42', createdAt: '2026-03-30T07:30:00Z', count: 3 },
    { id: 'chg-003', configID: 'cfg-eks-001', changeType: 'ScalingReplicaSet', severity: 'low', source: 'kubernetes', summary: 'Deployment incident-commander scaled from 2 to 3 replicas', externalCreatedBy: 'hpa-controller', createdAt: '2026-03-29T22:00:00Z' },
    { id: 'chg-004', configID: 'cfg-eks-001', changeType: 'diff', severity: 'medium', source: 'terraform', summary: 'EKS cluster version upgraded from 1.28 to 1.29', createdBy: 'alice@flanksource.com', createdAt: '2026-03-29T14:00:00Z' },
    { id: 'chg-005', configID: 'cfg-eks-001', changeType: 'PolicyUpdate', severity: 'high', source: 'argocd', summary: 'Network policy updated: restricted egress to 10.0.0.0/8 for namespace mc', createdBy: 'bob@flanksource.com', createdAt: '2026-03-28T16:00:00Z' },
    { id: 'chg-006', configID: 'cfg-eks-001', changeType: 'diff', severity: 'critical', source: 'aws-config', summary: 'IAM role policy detached: eks-admin-access removed from cluster role', createdBy: 'security-automation', createdAt: '2026-03-28T10:00:00Z' },
    { id: 'chg-007', configID: 'cfg-eks-001', changeType: 'FieldsV1', severity: 'info', source: 'kubernetes', summary: 'ConfigMap kube-proxy updated with new CIDR ranges', createdAt: '2026-03-27T18:00:00Z', count: 2 },
    { id: 'chg-008', configID: 'cfg-eks-001', changeType: 'diff', severity: 'low', source: 'terraform', summary: 'Added tag cost-center=platform-engineering to cluster', createdBy: 'carol@flanksource.com', createdAt: '2026-03-27T09:00:00Z' },
  ],
  analyses: [
    { id: 'ana-001', configID: 'cfg-eks-001', analyzer: 'Trivy', message: 'Container image flanksource/incident-commander:v1.4.200 has 3 high CVEs', status: 'open', severity: 'high', analysisType: 'security', source: 'trivy-operator', firstObserved: '2026-03-28T09:00:00Z', lastObserved: '2026-03-30T09:00:00Z' },
    { id: 'ana-002', configID: 'cfg-eks-001', analyzer: 'Trivy', message: 'Base image golang:1.23-alpine has known vulnerability in libcrypto (CVE-2026-0891)', status: 'open', severity: 'critical', analysisType: 'security', source: 'trivy-operator', firstObserved: '2026-03-25T09:00:00Z', lastObserved: '2026-03-30T09:00:00Z' },
    { id: 'ana-003', configID: 'cfg-eks-001', analyzer: 'OPA/Gatekeeper', message: 'Pod incident-commander-7f8b9c running as root user in namespace mc', status: 'open', severity: 'medium', analysisType: 'compliance', source: 'gatekeeper', firstObserved: '2026-03-20T09:00:00Z', lastObserved: '2026-03-30T09:00:00Z' },
    { id: 'ana-004', configID: 'cfg-eks-001', analyzer: 'OPA/Gatekeeper', message: 'Namespace mc missing required label: data-classification', status: 'silenced', severity: 'low', analysisType: 'compliance', source: 'gatekeeper', firstObserved: '2026-03-15T09:00:00Z', lastObserved: '2026-03-30T09:00:00Z' },
    { id: 'ana-005', configID: 'cfg-eks-001', analyzer: 'AWS Cost Optimizer', message: 'EKS node group i3.xlarge instances are underutilized (avg CPU 18%)', status: 'open', severity: 'medium', analysisType: 'cost', source: 'aws-cost-explorer', firstObserved: '2026-03-01T09:00:00Z', lastObserved: '2026-03-30T09:00:00Z' },
    { id: 'ana-007', configID: 'cfg-eks-001', analyzer: 'Prometheus Advisor', message: 'P99 API response latency exceeded 500ms threshold 12 times in the last 7 days', status: 'open', severity: 'high', analysisType: 'performance', source: 'prometheus', firstObserved: '2026-03-23T09:00:00Z', lastObserved: '2026-03-30T09:00:00Z' },
    { id: 'ana-010', configID: 'cfg-eks-001', analyzer: 'Prometheus Advisor', message: 'Node ip-10-0-2-18 memory utilization consistently above 85%', status: 'open', severity: 'high', analysisType: 'reliability', source: 'prometheus', firstObserved: '2026-03-26T09:00:00Z', lastObserved: '2026-03-30T09:00:00Z' },
  ],
  relationships: [
    { configID: 'cfg-eks-001', relatedID: 'cfg-vpc-001', relation: 'RunsIn', direction: 'outgoing' },
    { configID: 'cfg-eks-001', relatedID: 'cfg-iam-001', relation: 'ManagedBy', direction: 'outgoing' },
    { configID: 'cfg-eks-001', relatedID: 'cfg-sg-001', relation: 'DependsOn', direction: 'outgoing' },
    { configID: 'cfg-eks-001', relatedID: 'cfg-rds-001', relation: 'DependsOn', direction: 'outgoing' },
    { configID: 'cfg-deploy-001', relatedID: 'cfg-eks-001', relation: 'RunsOn', direction: 'incoming' },
    { configID: 'cfg-deploy-002', relatedID: 'cfg-eks-001', relation: 'RunsOn', direction: 'incoming' },
    { configID: 'cfg-deploy-003', relatedID: 'cfg-eks-001', relation: 'RunsOn', direction: 'incoming' },
    { configID: 'cfg-ns-001', relatedID: 'cfg-eks-001', relation: 'ChildOf', direction: 'incoming' },
    { configID: 'cfg-node-001', relatedID: 'cfg-eks-001', relation: 'ChildOf', direction: 'incoming' },
    { configID: 'cfg-node-002', relatedID: 'cfg-eks-001', relation: 'ChildOf', direction: 'incoming' },
  ],
  relatedConfigs: [
    { id: 'cfg-vpc-001', name: 'prod-vpc', type: 'AWS::EC2::VPC', configClass: 'Network', status: 'available', health: 'healthy', labels: { env: 'production' } },
    { id: 'cfg-iam-001', name: 'eks-cluster-role', type: 'AWS::IAM::Role', configClass: 'IAM', status: 'active', health: 'healthy' },
    { id: 'cfg-sg-001', name: 'eks-cluster-sg', type: 'AWS::EC2::SecurityGroup', configClass: 'Network', status: 'active', health: 'warning', labels: { env: 'production' } },
    { id: 'cfg-rds-001', name: 'mission-control-db', type: 'AWS::RDS::Instance', configClass: 'Database', status: 'available', health: 'healthy', labels: { env: 'production', engine: 'postgresql' } },
    { id: 'cfg-deploy-001', name: 'incident-commander', type: 'Kubernetes::Deployment', configClass: 'Deployment', status: 'Running', health: 'healthy', labels: { app: 'incident-commander' } },
    { id: 'cfg-deploy-002', name: 'canary-checker', type: 'Kubernetes::Deployment', configClass: 'Deployment', status: 'Running', health: 'healthy', labels: { app: 'canary-checker' } },
    { id: 'cfg-deploy-003', name: 'config-db', type: 'Kubernetes::Deployment', configClass: 'Deployment', status: 'Running', health: 'unhealthy', labels: { app: 'config-db' } },
    { id: 'cfg-ns-001', name: 'mc', type: 'Kubernetes::Namespace', configClass: 'Namespace', status: 'Active', health: 'healthy' },
    { id: 'cfg-node-001', name: 'ip-10-0-1-42', type: 'Kubernetes::Node', configClass: 'Node', status: 'Ready', health: 'healthy' },
    { id: 'cfg-node-002', name: 'ip-10-0-2-18', type: 'Kubernetes::Node', configClass: 'Node', status: 'Ready', health: 'warning', labels: { 'instance-type': 'i3.xlarge' } },
  ],
};

function PageHeader() {
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] bg-[#1e293b] text-white text-[9pt]">
      <span className="font-semibold">Config Components</span>
      <span className="text-gray-300">Kitchen Sink</span>
    </div>
  );
}

function PageFooter() {
  const date = new Date().toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric',
  });
  return (
    <div className="flex items-center justify-between px-[10mm] py-[2mm] border-t border-gray-200 text-[8pt] text-gray-400">
      <span>Generated {date}</span>
    </div>
  );
}

function CoverContent() {
  const date = new Date().toLocaleDateString('en-US', {
    year: 'numeric', month: 'long', day: 'numeric',
  });
  return (
    <div className="flex flex-col justify-center items-center h-full min-h-[200mm] text-center px-[20mm]">
      <div className="mb-[8mm]">
        <div className="text-[8pt] font-medium text-blue-600 uppercase tracking-widest mb-[3mm]">
          Component Showcase
        </div>
        <h1 className="text-[36pt] font-bold text-slate-900 leading-tight mb-[4mm]">
          Config Components
        </h1>
        <div className="text-[14pt] text-gray-500 mb-[8mm]">
          Kitchen Sink
        </div>
        <p className="text-[11pt] text-gray-600 max-w-[120mm] mx-auto mb-[8mm]">
          PDF-compatible components for rendering config items, changes, insights, and relationships.
        </p>
      </div>
      <div className="w-[40mm] h-[1px] mb-[8mm]" style={{ backgroundColor: '#2563EB' }} />
      <div className="text-[10pt] text-gray-400">Generated on {date}</div>
    </div>
  );
}

interface KitchenSinkProps {
  data?: ConfigReportData;
}

export default function KitchenSink({ data: externalData }: KitchenSinkProps) {
  const data = externalData?.configItem ? externalData : defaultData;
  const header = <PageHeader />;
  const footer = <PageFooter />;
  const pageProps = {
    pageSize: 'a4' as const,
    margins: { top: 5, bottom: 5, left: 5, right: 5 },
    header,
    headerHeight: 10,
    footer,
    footerHeight: 10,
  };

  const sampleConfigs = [data.configItem, ...data.relatedConfigs.slice(0, 5)];

  const sampleScrapers: ScraperInfo[] = [
    {
      id: 'scr-001', name: 'mc/aws-production', namespace: 'mc',
      source: 'KubernetesCRD', types: ['aws', 'kubernetes'],
      specHash: 'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6abcd',
      createdBy: 'alice@flanksource.com', createdAt: '2025-06-10T09:00:00Z', updatedAt: '2026-03-28T14:30:00Z',
      gitops: {
        git: { url: 'https://github.com/flanksource/mission-control-demo', branch: 'main', file: 'clusters/prod/scrapers/aws.yaml', dir: 'clusters/prod/scrapers', link: 'https://github.com/flanksource/mission-control-demo/tree/main/clusters/prod/scrapers/aws.yaml' },
        kustomize: { path: 'clusters/prod/scrapers', file: 'clusters/prod/scrapers/kustomization.yaml' },
      },
    },
    {
      id: 'scr-002', name: 'mc/azure-entra', namespace: 'mc',
      source: 'KubernetesCRD', types: ['azure'],
      specHash: 'ff00aa11bb22cc33dd44ee55ff00aa11bb22cc33dd44ee55ff00aa11bb22cc33',
      createdAt: '2025-09-01T10:00:00Z', updatedAt: '2026-03-30T08:00:00Z',
    },
    {
      id: 'scr-003', name: 'local-file-scraper',
      source: 'ConfigFile', types: ['file', 'sql'],
      specHash: '1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef',
      createdBy: 'bob@flanksource.com', createdAt: '2026-01-15T11:00:00Z',
    },
  ];

  return (
    <>
      <Page pageSize="a4" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
        <CoverContent />
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <Section variant="hero" title="ConfigLink" size="md">
          <div className="mb-[3mm]">
            <div className="text-[8pt] text-gray-500 mb-[2mm]">
              Renders a config item as Icon + Name with optional health indicator.
            </div>
            <div className="flex flex-col gap-[3mm]">
              {sampleConfigs.map((config) => (
                <div key={config.id} className="flex items-center gap-[4mm] py-[1mm] border-b border-gray-100">
                  <div className="w-[60mm]">
                    <ConfigLink config={config} />
                  </div>
                  <div className="w-[60mm]">
                    <ConfigLink config={config} showHealth />
                  </div>
                  <span className="text-[7pt] text-gray-400">{config.type}</span>
                </div>
              ))}
            </div>
          </div>
        </Section>
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <Section variant="hero" title="ConfigItemCard" size="md">
          <div className="text-[8pt] text-gray-500 mb-[2mm]">
            Renders a config item with icon, name, tags, and metadata.
          </div>
          <div className="flex flex-col gap-[3mm]">
            {sampleConfigs.map((config) => (
              <div key={config.id} className="py-[1mm] border-b border-gray-100">
                <ConfigItemCard config={{ ...config, id: config.id, created_at: data.configItem.createdAt, updated_at: data.configItem.updatedAt }} />
              </div>
            ))}
          </div>
        </Section>
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <Section variant="hero" title="ScraperCard" size="md">
          <div className="text-[8pt] text-gray-500 mb-[2mm]">
            Renders a scraper with type icons, source badge, spec hash, created by, dates, and GitOps provenance.
          </div>
          <div className="flex flex-col gap-[3mm]">
            {sampleScrapers.map((scraper) => (
              <ScraperCard key={scraper.id} scraper={scraper} />
            ))}
          </div>
        </Section>
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <ConfigChangesSection changes={data.changes} />
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <ConfigInsightsSection analyses={data.analyses} />
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <ConfigRelationshipGraph
          centralConfig={data.configItem}
          relationships={data.relationships}
          relatedConfigs={data.relatedConfigs}
        />
      </Page>

      <PageBreak />

      <Page {...pageProps}>
        <Section variant="hero" title="MatrixTable" size="md">
          <div className="text-[8pt] text-gray-500 mb-[3mm]">
            Rotated column headers using CSS-Tricks translate+rotate pattern.
          </div>
          <MatrixTable
            columnWidth={10} headerHeight={12}
            columns={['Read', 'Write', 'Execute', 'Admin', 'Delete', 'Audit']}
            rows={[
              { label: <span className="font-medium">alice@example.com</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, <Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null] },
              { label: <span className="font-medium">bob@example.com</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null, null, null] },
              { label: <span className="font-medium">charlie@example.com</span>, cells: [<Dot color="#2563EB" />, null, null, null, null, <Dot color="#7C3AED" />] },
              { label: <span className="font-medium">deploy-bot</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null, null] },
              { label: <span className="font-medium">monitoring-svc</span>, cells: [<Dot color="#EA580C" />, null, null, null, null, <Dot color="#EA580C" />] },
            ]}
          />
          <div className="mt-[6mm] text-[8pt] text-gray-500 mb-[3mm]">
            With longer column names and more rows.
          </div>
          <MatrixTable
            columnWidth={10} headerHeight={25}
            columns={['db_datareader', 'db_datawriter', 'db_owner', 'db_securityadmin', 'db_backupoperator', 'db_ddladmin', 'db_accessadmin']}
            rows={[
              { label: <span className="font-medium">design-studio-pas</span>, cells: [null, null, <Dot color="#2563EB" />, null, null, null, null] },
              { label: <span className="font-medium">monitoring_ro</span>, cells: [<Dot color="#2563EB" />, null, null, null, null, null, null] },
              { label: <span className="font-medium">oipa-qa-bot</span>, cells: [null, null, <Dot color="#EA580C" />, null, null, null, null] },
              { label: <span className="font-medium">omasa</span>, cells: [null, null, <Dot color="#2563EB" />, null, null, null, null] },
              { label: <span className="font-medium">SG-OMAR Shared Dev DB</span>, cells: [null, <Dot color="#7C3AED" />, null, null, <Dot color="#7C3AED" />, null, null] },
              { label: <span className="font-medium">SG-OMAR Shared RO</span>, cells: [<Dot color="#7C3AED" />, null, null, null, null, null, <Dot color="#7C3AED" />] },
              { label: <span className="font-medium">svc_mission_control</span>, cells: [<Dot color="#2563EB" />, <Dot color="#2563EB" />, null, null, null, null, null] },
            ]}
          />
        </Section>
      </Page>
    </>
  );
}
