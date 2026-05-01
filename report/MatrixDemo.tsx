import React from 'react';
import { Document, Page } from '@flanksource/facet';
import { MatrixTable, Dot } from '@flanksource/facet';
import {
  AccessIndicator,
  IdentityIcon,
  identityType,
  ACCESS_COLORS,
  STALE_COLORS,
  ReviewOverdueBadge,
  ReviewOverdueLegendSwatch,
} from './components/rbac-visual.tsx';

function Cell({ direct, overdue }: { direct: boolean; overdue?: boolean }) {
  const color = direct ? ACCESS_COLORS.direct : ACCESS_COLORS.group;
  return (
    <div className="relative flex justify-center items-center w-full h-full">
      <Dot color={color} outline={!direct} />
      {overdue && <ReviewOverdueBadge />}
    </div>
  );
}

function UserLabel({ name, userId, roleSource, staleBorderColor }: { name: string; userId: string; roleSource?: string; staleBorderColor?: string }) {
  return (
    <span className="inline-flex items-center gap-[1mm] font-medium pl-[1mm]"
      style={{ borderLeft: `2px solid ${staleBorderColor || 'transparent'}` }}>
      <IdentityIcon userId={userId} roleSource={roleSource} size={10} />
      {name}
    </span>
  );
}

export default function MatrixDemo() {
  const roles = ['Read', 'Write', 'Execute', 'Admin', 'Delete', 'Audit'];

  return (
    <Document pageSize="a4" margins={{ top: 10, bottom: 10, left: 10, right: 10 }}>
    <Page>
      <h2 className="text-lg font-bold mb-[4mm] text-slate-900">
        RBAC Matrix - Visual System Demo
      </h2>

      {/* --- Reference Section --- */}
      <div className="mb-[6mm]">
        <div className="text-sm font-semibold text-slate-700 mb-[2mm]">
          Identity Types
        </div>
        <div className="flex gap-[6mm] text-xs text-slate-600">
          {(['user', 'group', 'service', 'bot'] as const).map((type) => (
            <span key={type} className="inline-flex items-center gap-[1mm]">
              <IdentityIcon
                userId={type === 'service' ? 'svc-x' : type === 'bot' ? 'bot-x' : 'x'}
                roleSource={type === 'group' ? 'group:x' : 'direct'}
                size={12}
              />
              {identityType(
                type === 'service' ? 'svc-x' : type === 'bot' ? 'bot-x' : 'x',
                type === 'group' ? 'group:x' : 'direct',
              ).label}
            </span>
          ))}
        </div>
      </div>

      <div className="mb-[6mm]">
        <div className="text-sm font-semibold text-slate-700 mb-[2mm]">
          Access Pattern - Filled vs Unfilled
        </div>
        <div className="flex gap-[6mm] text-xs text-slate-600 items-center">
          <span className="inline-flex items-center gap-[1mm]">
            <AccessIndicator direct={true} color={ACCESS_COLORS.direct} /> Direct (filled)
          </span>
          <span className="inline-flex items-center gap-[1mm]">
            <AccessIndicator direct={false} color={ACCESS_COLORS.group} /> Indirect / Group (unfilled)
          </span>
        </div>
      </div>

      {/* --- Matrix 1: Simple Permissions --- */}
      <div className="text-sm font-semibold text-slate-700 mb-[2mm]">
        Simple Permissions
      </div>
      <MatrixTable
        columnWidth={10} headerHeight={12}
        columns={roles}
        rows={[
          {
            label: <UserLabel name="alice@example.com" userId="alice" />,
            cells: [
              <Cell direct={true} />,
              <Cell direct={true} />,
              <Cell direct={true} />,
              <Cell direct={true} />,
              null,
              null,
            ],
          },
          {
            label: <UserLabel name="bob@example.com" userId="bob" />,
            cells: [
              <Cell direct={true} />,
              <Cell direct={true} />,
              null, null, null, null,
            ],
          },
          {
            label: <UserLabel name="charlie@example.com" userId="charlie" roleSource="group:viewers" />,
            cells: [
              <Cell direct={false} />,
              null, null, null, null,
              <Cell direct={false} />,
            ],
          },
          {
            label: <UserLabel name="deploy-bot" userId="bot-deploy" />,
            cells: [
              <Cell direct={true} />,
              <Cell direct={true} />,
              <Cell direct={true} />,
              null, null, null,
            ],
          },
          {
            label: <UserLabel name="monitoring-svc" userId="svc-monitoring" staleBorderColor={STALE_COLORS.stale30d} />,
            cells: [
              <Cell direct={true} />,
              null, null, null, null,
              <Cell direct={true} overdue />,
            ],
          },
          {
            label: <UserLabel name="dave@example.com" userId="dave" roleSource="group:admins" />,
            cells: [
              null, null, null,
              <Cell direct={false} overdue />,
              <Cell direct={false} />,
              null,
            ],
          },
          {
            label: <UserLabel name="temp-contractor" userId="temp-contractor" staleBorderColor={STALE_COLORS.stale7d} />,
            cells: [
              <Cell direct={true} />,
              <Cell direct={true} />,
              null, null, null, null,
            ],
          },
        ]}
      />

      {/* --- Matrix 2: Database Roles --- */}
      <div className="mt-[8mm] text-sm font-semibold text-slate-700 mb-[2mm]">
        Database Roles
      </div>
      {(() => {
        const dbRoles = ['db_datareader', 'db_datawriter', 'db_owner', 'db_securityadmin', 'db_backupoperator', 'db_ddladmin', 'db_accessadmin'];
        return (
          <MatrixTable
            columnWidth={10} headerHeight={25}
            columns={dbRoles}
            rows={[
              {
                label: <UserLabel name="design-studio-pas" userId="svc-design-studio" />,
                cells: [null, null, <Cell direct={true} />, null, null, null, null],
              },
              {
                label: <UserLabel name="monitoring_ro" userId="svc-monitoring" staleBorderColor={STALE_COLORS.stale7d} />,
                cells: [<Cell direct={true} />, null, null, null, null, null, null],
              },
              {
                label: <UserLabel name="workflow-qa-bot" userId="bot-workflow-qa" />,
                cells: [null, null, <Cell direct={true} />, null, null, null, null],
              },
              {
                label: <UserLabel name="SG-ACME Shared Developer" userId="sg-acme-dev" roleSource="group:acme-dev" />,
                cells: [null, <Cell direct={false} />, null, null, <Cell direct={false} />, null, null],
              },
              {
                label: <UserLabel name="SG-ACME Shared Read Only" userId="sg-acme-ro" roleSource="group:acme-ro" />,
                cells: [<Cell direct={false} />, null, null, null, null, null, <Cell direct={false} />],
              },
              {
                label: <UserLabel name="svc_mission_control" userId="svc_mission_control" />,
                cells: [<Cell direct={true} />, <Cell direct={true} />, null, null, null, null, null],
              },
            ]}
          />
        );
      })()}

      {/* --- Matrix 3: Very Long Column Names --- */}
      <div className="mt-[8mm] text-sm font-semibold text-slate-700 mb-[2mm]">
        IAM Policies (very long column names)
      </div>
      {(() => {
        const iamRoles = [
          'AmazonEKSClusterPolicyAdministratorFullAccess',
          'SecretsManagerReadWriteForIncidentCommanderWorkloads',
          'CloudWatchLogsReadOnlyAccessForAuditAndComplianceOperators',
          'AmazonRDSFullAccessWithCrossAccountBackupRestorePermissions',
          'ElasticContainerRegistryPushPullAccessForCIPipelines',
        ];
        // MatrixTable default header font is 7pt; scale down for long labels so
        // rotated text fits within headerHeight without clipping.
        const maxFit = 18;
        const fontPtFor = (len: number) =>
          len <= maxFit ? 7 : Math.max(4.5, 7 * (maxFit / len));
        const scaledColumns = iamRoles.map((role) => (
          <span style={{ fontSize: `${fontPtFor(role.length).toFixed(2)}pt` }}>{role}</span>
        ));
        // Labels are rotated -45deg, so vertical extent ≈ length × fontPt × 0.6 × sin(45°).
        // 0.6 approximates the per-character width-to-font-size ratio for sans-serif.
        const PT_TO_MM = 0.3528;
        const headerHeightMm = Math.ceil(
          Math.max(
            ...iamRoles.map((role) => (role.length + 2) * fontPtFor(role.length) * 0.6 * Math.SQRT1_2 * PT_TO_MM),
          ) + 2,
        );
        return (
          <MatrixTable
            columnWidth={10}
            headerHeight={headerHeightMm}
            columns={scaledColumns}
            rows={[
              {
                label: <UserLabel name="alice@example.com" userId="alice" />,
                cells: [
                  <Cell direct={true} />,
                  <Cell direct={false} />,
                  null,
                  null,
                  null,
                ],
              },
              {
                label: <UserLabel name="bob@example.com" userId="bob" roleSource="group:SG-DataEngineers" />,
                cells: [
                  null,
                  null,
                  <Cell direct={true} />,
                  <Cell direct={false} />,
                  null,
                ],
              },
              {
                label: <UserLabel name="deploy-bot" userId="bot-deploy" />,
                cells: [
                  <Cell direct={true} />,
                  null,
                  null,
                  null,
                  <Cell direct={true} />,
                ],
              },
              {
                label: <UserLabel name="carol@example.com" userId="carol" roleSource="group:SG-Analytics" staleBorderColor={STALE_COLORS.stale30d} />,
                cells: [
                  null,
                  null,
                  <Cell direct={false} overdue />,
                  null,
                  null,
                ],
              },
              {
                label: <UserLabel name="SG-IncidentCommanderBreakGlassOperators" userId="sg-break-glass" roleSource="group:SG-IncidentCommanderBreakGlassOperators" />,
                cells: [
                  <Cell direct={false} />,
                  <Cell direct={false} />,
                  null,
                  null,
                  null,
                ],
              },
            ]}
          />
        );
      })()}

      {/* --- Legend --- */}
      <div className="mt-[6mm] pt-[2mm] border-t border-gray-200 flex flex-wrap gap-[4mm] text-xs text-gray-500 items-center">
        <span className="font-semibold">Access:</span>
        <span className="inline-flex items-center gap-[1mm]">
          <Dot color={ACCESS_COLORS.direct} /> Direct
        </span>
        <span className="inline-flex items-center gap-[1mm]">
          <Dot color={ACCESS_COLORS.group} outline /> Indirect
        </span>

        <span className="font-semibold ml-[2mm]">Last Login:</span>
        <span className="inline-flex items-center gap-[1mm]">
          <span className="inline-block w-[2mm] h-[3mm]" style={{ borderLeft: `2px solid ${STALE_COLORS.stale7d}` }} />
          &gt; 7d
        </span>
        <span className="inline-flex items-center gap-[1mm]">
          <span className="inline-block w-[2mm] h-[3mm]" style={{ borderLeft: `2px solid ${STALE_COLORS.stale30d}` }} />
          &gt; 30d
        </span>

        <span className="font-semibold ml-[2mm]">Review:</span>
        <ReviewOverdueLegendSwatch />
      </div>
    </Page>
    </Document>
  );
}
