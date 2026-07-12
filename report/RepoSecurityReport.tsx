// Security report over catalog config items, organized around GitHub repositories.
// Derives OpenSSF scorecard checks and vulnerability alerts from config insights.
import './icon-setup.ts';
import React from 'react';
import {
  Document, Page, Header, Footer, PageNo, ScoreGauge,
  SeverityStatCard, Badge,
} from '@flanksource/facet';
import { Icon } from '@flanksource/icons/icon';
import { MissionControlLogo, Github as IconGithub } from '@flanksource/icons/mi';
import type { CatalogReportData, CatalogReportEntry } from './catalog-report-types.ts';
import type { ConfigAnalysis, ConfigItem, ConfigProperty, ConfigSeverity } from './config-types.ts';
import ConfigInsightsSection from './components/ConfigInsightsSection.tsx';
import CoverPage from './components/CoverPage.tsx';
import { formatDate, formatDateTime } from './components/utils.ts';

const GITHUB_REPO_TYPE = 'GitHub::Repository';
const SCORECARD_SOURCE = 'OpenSSF Scorecard';

type AlertKind = 'dependabot' | 'code-scanning' | 'secret-scanning';

const ALERT_SOURCES: Record<string, AlertKind> = {
  'GitHub Dependabot': 'dependabot',
  'GitHub Code Scanning': 'code-scanning',
  'GitHub Secret Scanning': 'secret-scanning',
};

const ALERT_KIND_LABELS: Record<AlertKind, string> = {
  dependabot: 'Dependabot',
  'code-scanning': 'Code Scanning',
  'secret-scanning': 'Secret Scanning',
};

const SEVERITY_ORDER: ConfigSeverity[] = ['critical', 'high', 'medium', 'low', 'info'];

const SEVERITY_BADGE: Record<string, string> = {
  critical: 'text-red-700 bg-red-50 border-red-200',
  high: 'text-orange-700 bg-orange-50 border-orange-200',
  medium: 'text-yellow-700 bg-yellow-50 border-yellow-200',
  low: 'text-blue-700 bg-blue-50 border-blue-200',
  info: 'text-gray-600 bg-gray-50 border-gray-200',
};

interface SeverityCounts {
  critical: number;
  high: number;
  medium: number;
  low: number;
}

interface ScorecardCheck {
  name: string;
  risk: ConfigSeverity;
  passing: boolean;
  reason: string;
  score?: number;
  docURL?: string;
}

interface AlertActivity {
  newCount: number;
  resolvedCount: number;
  avgCloseDays: number | null;
}

interface RepoSecurity {
  configItem: ConfigItem & { permalink?: string };
  org?: string;
  repo?: string;
  githubUrl?: string;
  checks: ScorecardCheck[];
  // Aggregate OpenSSF Scorecard score (0-10) as scraped from the OpenSSF API.
  scorecardScore: number | null;
  scorecardBadgeURL?: string;
  openAlerts: ConfigAnalysis[];
  resolvedAlerts: ConfigAnalysis[];
  activity: AlertActivity;
  alertsByKind: Record<AlertKind, ConfigAnalysis[]>;
  severity: SeverityCounts;
  otherInsights: ConfigAnalysis[];
  lastObserved?: string;
}

function isOpen(a: ConfigAnalysis): boolean {
  return !a.status || a.status === 'open';
}

function findProperty(props: ConfigProperty[] | undefined, name: string): ConfigProperty | undefined {
  return props?.find((p) => p.name === name);
}

function propertyURL(props: ConfigProperty[] | undefined, name: string): string | undefined {
  const p = findProperty(props, name);
  return p?.links?.find((l) => l.url)?.url || (p?.type === 'url' ? p.text : undefined);
}

// Direct link to the insight's source: the GitHub alert for scanning alerts,
// the check documentation for OpenSSF scorecard checks.
function insightSourceURL(a: ConfigAnalysis): string | undefined {
  return propertyURL(a.properties, 'URL') || propertyURL(a.properties, 'Documentation');
}

function dayDiff(fromISO?: string, toISO?: string): number | null {
  if (!fromISO || !toISO) return null;
  const ms = Date.parse(toISO) - Date.parse(fromISO);
  if (!Number.isFinite(ms) || ms < 0) return null;
  return ms / 86400000;
}

function buildAlertActivity(alerts: ConfigAnalysis[], resolvedAlerts: ConfigAnalysis[], since?: string): AlertActivity {
  const sinceMs = since ? Date.parse(since) : NaN;
  const inPeriod = (iso?: string) => !Number.isFinite(sinceMs) || (!!iso && Date.parse(iso) >= sinceMs);

  const newCount = alerts.filter((a) => inPeriod(a.firstObserved)).length;
  const resolvedInPeriod = resolvedAlerts.filter((a) => inPeriod(a.lastObserved));
  const closeDays = resolvedInPeriod
    .map((a) => dayDiff(a.firstObserved, a.lastObserved))
    .filter((d): d is number => d !== null);
  const avgCloseDays = closeDays.length > 0
    ? closeDays.reduce((sum, d) => sum + d, 0) / closeDays.length
    : null;

  return { newCount, resolvedCount: resolvedInPeriod.length, avgCloseDays };
}

function formatDays(days: number): string {
  return days < 1 ? '<1d' : `${Math.round(days)}d`;
}

function countSeverity(alerts: ConfigAnalysis[]): SeverityCounts {
  const counts: SeverityCounts = { critical: 0, high: 0, medium: 0, low: 0 };
  for (const a of alerts) {
    const sev = a.severity === 'info' || !a.severity ? 'low' : a.severity;
    if (sev in counts) counts[sev as keyof SeverityCounts]++;
  }
  return counts;
}

function normalizeEntries(data: CatalogReportData): CatalogReportEntry[] {
  if (data.entries && data.entries.length > 0) return data.entries;
  if (!data.configItem?.id) return [];
  return [{
    configItem: data.configItem,
    changeCount: (data.changes || []).length,
    insightCount: (data.analyses || []).length,
    accessCount: (data.access || []).length,
    changes: data.changes || [],
    analyses: data.analyses || [],
    access: data.access || [],
    accessLogs: data.accessLogs || [],
  }];
}

function buildRepoSecurity(entry: CatalogReportEntry, since?: string): RepoSecurity {
  const ci = entry.configItem;
  const analyses = entry.analyses || [];

  const scorecard = analyses.filter((a) => a.source === SCORECARD_SOURCE);
  const alerts = analyses.filter((a) => a.source && a.source in ALERT_SOURCES);
  const otherInsights = analyses.filter(
    (a) => a.source !== SCORECARD_SOURCE && !(a.source && a.source in ALERT_SOURCES),
  );

  const checks: ScorecardCheck[] = scorecard
    .map((a) => ({
      name: a.analyzer,
      risk: (a.severity || 'info') as ConfigSeverity,
      passing: a.status === 'resolved',
      reason: a.summary || a.message || '',
      score: findProperty(a.properties, 'Score')?.value,
      docURL: propertyURL(a.properties, 'Documentation'),
    }))
    .sort((a, b) => {
      if (a.passing !== b.passing) return a.passing ? 1 : -1;
      return SEVERITY_ORDER.indexOf(a.risk) - SEVERITY_ORDER.indexOf(b.risk);
    });

  const openAlerts = alerts.filter(isOpen);
  const resolvedAlerts = alerts.filter((a) => !isOpen(a));

  const alertsByKind: Record<AlertKind, ConfigAnalysis[]> = {
    dependabot: [],
    'code-scanning': [],
    'secret-scanning': [],
  };
  for (const a of openAlerts) alertsByKind[ALERT_SOURCES[a.source!]].push(a);

  const org = ci.tags?.owner || ci.name?.split('/')[0];
  const repo = ci.tags?.repo || ci.name?.split('/')[1];

  const observed = analyses.map((a) => a.lastObserved).filter(Boolean) as string[];

  const scoreText = findProperty(ci.properties, 'OpenSSF Score')?.text;
  const scorecardScore = scoreText !== undefined && !Number.isNaN(parseFloat(scoreText))
    ? parseFloat(scoreText)
    : null;

  return {
    configItem: ci,
    org,
    repo,
    githubUrl: org && repo ? `https://github.com/${org}/${repo}` : undefined,
    checks,
    scorecardScore,
    scorecardBadgeURL: findProperty(ci.properties, 'OpenSSF Badge')?.text,
    openAlerts,
    resolvedAlerts,
    activity: buildAlertActivity(alerts, resolvedAlerts, since),
    alertsByKind,
    severity: countSeverity(openAlerts),
    otherInsights,
    lastObserved: observed.sort().pop(),
  };
}

// Renders open alerts as one row per alert with the title linked to the
// GitHub alert. Facet's AlertsTable is not used because it drops the url field.
function OpenAlertsList({ alerts }: { alerts: ConfigAnalysis[] }) {
  const rows = [...alerts].sort((a, b) => {
    const sevDiff = SEVERITY_ORDER.indexOf((a.severity || 'info') as ConfigSeverity)
      - SEVERITY_ORDER.indexOf((b.severity || 'info') as ConfigSeverity);
    if (sevDiff !== 0) return sevDiff;
    return (a.summary || '').localeCompare(b.summary || '');
  });
  return (
    <div className="space-y-1">
      {rows.map((a) => {
        const sev = a.severity === 'info' || !a.severity ? 'low' : a.severity;
        const url = insightSourceURL(a) || a.permalink;
        const title = a.summary || a.message || a.analyzer;
        const isCodeScanning = ALERT_SOURCES[a.source!] === 'code-scanning';
        return (
          <div key={a.id} className="flex items-center gap-2 text-xs min-w-0">
            <Badge variant="custom" size="xs" shape="rounded" label={sev} className={SEVERITY_BADGE[sev] ?? SEVERITY_BADGE.info} />
            <span className="text-gray-700 truncate flex-1 min-w-0">
              {url ? <a href={url} className="text-gray-700 underline">{title}</a> : title}
            </span>
            {isCodeScanning && a.analyzer && (
              <code className="text-gray-500 flex-shrink-0 max-w-[50%] text-[10px] bg-gray-100 px-1 rounded truncate">{a.analyzer}</code>
            )}
          </div>
        );
      })}
    </div>
  );
}

function SeverityChips({ counts }: { counts: SeverityCounts }) {
  return (
    <div className="flex items-center gap-[1mm]">
      {(['critical', 'high', 'medium', 'low'] as const).map((sev) => (
        <Badge
          key={sev}
          variant="custom"
          size="xs"
          shape="rounded"
          label={`${counts[sev]} ${sev}`}
          className={counts[sev] > 0 ? SEVERITY_BADGE[sev] : SEVERITY_BADGE.info}
        />
      ))}
    </div>
  );
}

function ScorecardBadge({ security }: { security: RepoSecurity }) {
  const viewerURL = security.org && security.repo
    ? `https://scorecard.dev/viewer/?uri=github.com/${security.org}/${security.repo}`
    : undefined;
  if (security.scorecardBadgeURL) {
    const badge = <img src={security.scorecardBadgeURL} className="h-[4mm] w-auto" alt="OpenSSF Scorecard" />;
    return viewerURL ? <a href={viewerURL}>{badge}</a> : badge;
  }
  if (!viewerURL) return null;
  return (
    <a href={viewerURL} className="text-xs text-blue-600 underline">
      OpenSSF Scorecard
    </a>
  );
}

function GithubLink({ security }: { security: RepoSecurity }) {
  if (!security.githubUrl) return null;
  return (
    <a href={security.githubUrl} className="inline-flex items-center gap-[1mm] text-xs text-blue-600 underline">
      <IconGithub className="w-[3mm] h-[3mm]" />
      {security.org}/{security.repo}
    </a>
  );
}

function RepoSummaryCard({ security }: { security: RepoSecurity }) {
  const ci = security.configItem;
  return (
    <div className="border border-gray-300 rounded-lg p-[3mm]" style={{ pageBreakInside: 'avoid', breakInside: 'avoid' }}>
      <div className="flex items-center gap-[2mm] mb-[1mm]">
        <Icon name={ci.type || 'github'} size={18} className="shrink-0" />
        <span className="text-sm font-bold text-gray-900 flex-1 leading-none">{ci.name}</span>
        {security.scorecardScore !== null && (
          <ScoreGauge score={security.scorecardScore} size="sm" showMinMax={false} />
        )}
      </div>
      {ci.description && <p className="text-xs text-gray-600 mb-[2mm]">{ci.description}</p>}

      <div className="flex items-center gap-[2mm] flex-wrap mb-[2mm]">
        <GithubLink security={security} />
        <ScorecardBadge security={security} />
      </div>

      <div className="grid grid-cols-2 gap-[2mm] text-xs">
        <div className="bg-gray-50 rounded p-[2mm]">
          <div className="text-gray-600 mb-[1mm]">OpenSSF Score</div>
          {security.scorecardScore !== null
            ? <span className="font-bold text-slate-800">{security.scorecardScore.toFixed(1)} / 10</span>
            : <span className="text-gray-400">No scorecard data</span>}
        </div>
        <div className="bg-gray-50 rounded p-[2mm]">
          <div className="text-gray-600 mb-[1mm]">Open Alerts · {security.openAlerts.length} total</div>
          {security.openAlerts.length > 0
            ? <SeverityChips counts={security.severity} />
            : <span className="text-green-700 font-medium">✓ None</span>}
        </div>
      </div>
    </div>
  );
}

function SecurityChecksTable({ checks }: { checks: ScorecardCheck[] }) {
  if (checks.length === 0) {
    return <p className="text-xs text-gray-400 italic">No security check data available</p>;
  }
  return (
    <table className="w-full text-xs border-collapse">
      <thead>
        <tr className="border-b-2 border-gray-300 text-left text-gray-600">
          <th className="py-[1mm] pr-[2mm] font-semibold">Check</th>
          <th className="py-[1mm] pr-[2mm] font-semibold">Risk</th>
          <th className="py-[1mm] pr-[2mm] font-semibold">Score</th>
          <th className="py-[1mm] pr-[2mm] font-semibold">Status</th>
          <th className="py-[1mm] font-semibold">Details</th>
        </tr>
      </thead>
      <tbody>
        {checks.map((check) => (
          <tr key={check.name} className="border-b border-gray-100" style={{ pageBreakInside: 'avoid', breakInside: 'avoid' }}>
            <td className="py-[1mm] pr-[2mm] font-medium text-slate-800 whitespace-nowrap">
              {check.docURL
                ? <a href={check.docURL} className="text-slate-800 underline">{check.name}</a>
                : check.name}
            </td>
            <td className="py-[1mm] pr-[2mm]">
              <Badge variant="custom" size="xs" shape="rounded" label={check.risk} className={SEVERITY_BADGE[check.risk] ?? SEVERITY_BADGE.info} />
            </td>
            <td className="py-[1mm] pr-[2mm] text-gray-600 whitespace-nowrap">
              {check.score !== undefined ? `${check.score}/10` : '-'}
            </td>
            <td className="py-[1mm] pr-[2mm]">
              <Badge
                variant="custom"
                size="xs"
                shape="rounded"
                label={check.passing ? 'pass' : 'open'}
                className={check.passing ? 'text-green-700 bg-green-50 border-green-200' : 'text-red-700 bg-red-50 border-red-200'}
              />
            </td>
            <td className="py-[1mm] text-gray-600">{check.reason}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function AlertActivityRow({ security, periodLabel }: { security: RepoSecurity; periodLabel?: string }) {
  const { newCount, resolvedCount, avgCloseDays } = security.activity;
  if (newCount === 0 && resolvedCount === 0) return null;
  return (
    <div className="flex items-center gap-[2mm] mb-[2mm] text-xs text-gray-600">
      {periodLabel && <span className="font-semibold text-slate-800">Last {periodLabel}:</span>}
      <Badge
        variant="custom" size="xs" shape="rounded"
        label={`${newCount} new`}
        className={newCount > 0 ? 'text-orange-700 bg-orange-50 border-orange-200' : SEVERITY_BADGE.info}
      />
      <Badge
        variant="custom" size="xs" shape="rounded"
        label={`${resolvedCount} resolved`}
        className={resolvedCount > 0 ? 'text-green-700 bg-green-50 border-green-200' : SEVERITY_BADGE.info}
      />
      {avgCloseDays !== null && <span>avg time to resolve: {formatDays(avgCloseDays)}</span>}
    </div>
  );
}

function ResolvedAlertsTable({ security, periodLabel }: { security: RepoSecurity; periodLabel?: string }) {
  if (security.resolvedAlerts.length === 0) return null;
  const rows = [...security.resolvedAlerts].sort(
    (a, b) => (b.lastObserved || '').localeCompare(a.lastObserved || ''),
  );
  return (
    <div className="mt-[3mm]">
      <h3 className="text-sm font-semibold text-gray-800 mb-[2mm]">
        Resolved Alerts{periodLabel ? ` · last ${periodLabel}` : ''}
      </h3>
      <table className="w-full text-xs border-collapse">
        <thead>
          <tr className="border-b-2 border-gray-300 text-left text-gray-600">
            <th className="py-[1mm] pr-[2mm] font-semibold">Type</th>
            <th className="py-[1mm] pr-[2mm] font-semibold">Severity</th>
            <th className="py-[1mm] pr-[2mm] font-semibold">Alert</th>
            <th className="py-[1mm] pr-[2mm] font-semibold">Resolved</th>
            <th className="py-[1mm] font-semibold">Open for</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((a) => {
            const sev = a.severity === 'info' || !a.severity ? 'low' : a.severity;
            const url = insightSourceURL(a) || a.permalink;
            const title = a.summary || a.message || a.analyzer;
            const openFor = dayDiff(a.firstObserved, a.lastObserved);
            return (
              <tr key={a.id} className="border-b border-gray-100" style={{ pageBreakInside: 'avoid', breakInside: 'avoid' }}>
                <td className="py-[1mm] pr-[2mm] whitespace-nowrap text-gray-600">{ALERT_KIND_LABELS[ALERT_SOURCES[a.source!]] || a.source}</td>
                <td className="py-[1mm] pr-[2mm]">
                  <Badge variant="custom" size="xs" shape="rounded" label={sev} className={SEVERITY_BADGE[sev] ?? SEVERITY_BADGE.info} />
                </td>
                <td className="py-[1mm] pr-[2mm] text-slate-800">
                  {url ? <a href={url} className="text-slate-800 underline">{title}</a> : title}
                </td>
                <td className="py-[1mm] pr-[2mm] whitespace-nowrap text-gray-600">{a.lastObserved ? formatDate(a.lastObserved) : '-'}</td>
                <td className="py-[1mm] whitespace-nowrap text-gray-600">{openFor !== null ? formatDays(openFor) : '-'}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <p className="text-xs text-gray-400 mt-[1mm]">
        Resolution time is approximated from when the alert was last observed open by the catalog scraper.
      </p>
    </div>
  );
}

function VulnerabilityAlerts({ security }: { security: RepoSecurity }) {
  if (security.openAlerts.length === 0) {
    return (
      <div className="border border-green-200 bg-green-50 rounded p-[3mm]">
        <div className="text-sm font-semibold text-green-800 mb-[0.5mm]">✓ No open security alerts</div>
        <div className="text-xs text-green-700">
          {security.configItem.name} currently has no open Dependabot, Code Scanning, or Secret Scanning alerts.
        </div>
      </div>
    );
  }

  return (
    <>
      <div className="grid grid-cols-4 gap-[2mm] mb-[3mm]">
        <SeverityStatCard color="red" value={security.severity.critical} label="Critical" />
        <SeverityStatCard color="orange" value={security.severity.high} label="High" />
        <SeverityStatCard color="yellow" value={security.severity.medium} label="Medium" />
        <SeverityStatCard color="blue" value={security.severity.low} label="Low" />
      </div>

      {(Object.keys(ALERT_KIND_LABELS) as AlertKind[]).map((kind) => {
        const alerts = security.alertsByKind[kind];
        if (alerts.length === 0) return null;
        return (
          <div key={kind} className="mb-[3mm]">
            <div className="flex items-center gap-[1.5mm] mb-[1mm]">
              <span className="text-xs font-semibold text-slate-800">{ALERT_KIND_LABELS[kind]}</span>
              <Badge variant="custom" size="xs" shape="pill" label={String(alerts.length)} color="bg-gray-100" textColor="text-gray-500" borderColor="border-gray-200" />
            </div>
            <OpenAlertsList alerts={alerts} />
          </div>
        );
      })}
    </>
  );
}

function RepoSecuritySection({ security, periodLabel }: { security: RepoSecurity; periodLabel?: string }) {
  const ci = security.configItem;
  return (
    <div className="mb-[6mm]">
      <div className="mb-[3mm] pb-[2mm] border-b-2 border-gray-300" style={{ pageBreakInside: 'avoid', breakInside: 'avoid' }}>
        <div className="flex items-start justify-between mb-[1mm]">
          <div className="flex-1">
            <div className="flex items-center gap-[2mm] mb-[1mm]">
              <Icon name={ci.type || 'github'} size={24} className="shrink-0" />
              <span className="text-xl font-bold text-gray-900 leading-none">{ci.name}</span>
            </div>
            {ci.description && <p className="text-xs text-gray-600">{ci.description}</p>}
          </div>
          {security.scorecardScore !== null && <ScoreGauge score={security.scorecardScore} size="md" label="OpenSSF score" />}
        </div>
        <div className="flex justify-end items-center gap-[2mm] flex-wrap">
          <GithubLink security={security} />
          <span className="text-gray-400">•</span>
          <ScorecardBadge security={security} />
          {security.lastObserved && (
            <>
              <span className="text-gray-400">•</span>
              <span className="text-xs text-gray-600">Last updated: {formatDate(security.lastObserved)}</span>
            </>
          )}
        </div>
      </div>

      <h3 className="text-sm font-semibold text-gray-800 mb-[2mm]">Security Checks</h3>
      <SecurityChecksTable checks={security.checks} />

      <h3 className="text-sm font-semibold text-gray-800 mt-[4mm] mb-[2mm]">Vulnerability Alerts</h3>
      <AlertActivityRow security={security} periodLabel={periodLabel} />
      <VulnerabilityAlerts security={security} />
      <ResolvedAlertsTable security={security} periodLabel={periodLabel} />

      {security.otherInsights.length > 0 && (
        <div className="mt-[4mm]">
          <ConfigInsightsSection analyses={security.otherInsights} />
        </div>
      )}
    </div>
  );
}

function OverviewStats({ repos, periodLabel }: { repos: RepoSecurity[]; periodLabel?: string }) {
  const scored = repos.filter((r) => r.scorecardScore !== null);
  const avgScore = scored.length > 0
    ? scored.reduce((sum, r) => sum + (r.scorecardScore || 0), 0) / scored.length
    : null;
  const totalAlerts = repos.reduce((sum, r) => sum + r.openAlerts.length, 0);
  const critical = repos.reduce((sum, r) => sum + r.severity.critical, 0);
  const high = repos.reduce((sum, r) => sum + r.severity.high, 0);
  const newAlerts = repos.reduce((sum, r) => sum + r.activity.newCount, 0);
  const resolved = repos.reduce((sum, r) => sum + r.activity.resolvedCount, 0);
  const closeDays = repos
    .map((r) => r.activity.avgCloseDays !== null ? { avg: r.activity.avgCloseDays, n: r.activity.resolvedCount } : null)
    .filter((x): x is { avg: number; n: number } => x !== null);
  const totalClosed = closeDays.reduce((sum, x) => sum + x.n, 0);
  const avgClose = totalClosed > 0
    ? closeDays.reduce((sum, x) => sum + x.avg * x.n, 0) / totalClosed
    : null;

  return (
    <div className="grid grid-cols-3 gap-[3mm] mb-[4mm]">
      <div className="border border-gray-200 rounded p-[3mm] bg-blue-50">
        <div className="text-xl font-bold text-blue-700 mb-[0.5mm]">{avgScore !== null ? avgScore.toFixed(1) : '-'}</div>
        <div className="text-xs text-gray-700 font-medium">Average OpenSSF Score</div>
        <div className="text-xs text-gray-600 mt-[0.5mm]">Across {scored.length} scored {scored.length === 1 ? 'repository' : 'repositories'} (0-10)</div>
      </div>
      <div className="border border-gray-200 rounded p-[3mm] bg-red-50">
        <div className="text-xl font-bold text-red-700 mb-[0.5mm]">{totalAlerts}</div>
        <div className="text-xs text-gray-700 font-medium">Total Open Alerts</div>
        <div className="text-xs text-gray-600 mt-[0.5mm]">{critical} critical, {high} high</div>
      </div>
      <div className="border border-gray-200 rounded p-[3mm] bg-purple-50">
        <div className="text-xl font-bold text-purple-700 mb-[0.5mm]">{newAlerts} / {resolved}</div>
        <div className="text-xs text-gray-700 font-medium">New / Resolved Alerts{periodLabel ? ` · last ${periodLabel}` : ''}</div>
        <div className="text-xs text-gray-600 mt-[0.5mm]">
          {avgClose !== null ? `Avg time to resolve: ${formatDays(avgClose)}` : 'No alerts resolved in period'}
        </div>
      </div>
    </div>
  );
}

function OtherConfigSection({ entry }: { entry: CatalogReportEntry }) {
  const ci = entry.configItem;
  return (
    <div className="mb-[4mm]">
      <div className="flex items-center gap-[2mm] mb-[2mm] pb-[1mm] border-b-2 border-blue-200">
        {ci.type && <Icon name={ci.type} size={14} />}
        <span className="text-sm font-bold text-slate-800">{ci.name}</span>
        {ci.type && <span className="text-xs text-gray-500">{ci.type}</span>}
      </div>
      {(entry.analyses || []).length > 0
        ? <ConfigInsightsSection analyses={entry.analyses} />
        : <p className="text-xs text-gray-400 italic">No insights recorded</p>}
    </div>
  );
}

interface SecurityReportProps {
  data: CatalogReportData;
}

export default function SecurityReport({ data }: SecurityReportProps) {
  const entries = normalizeEntries(data);
  const repoEntries = entries.filter((e) => e.configItem?.type === GITHUB_REPO_TYPE);
  const otherEntries = entries.filter((e) => e.configItem?.type !== GITHUB_REPO_TYPE);
  const repos = repoEntries.map((e) => buildRepoSecurity(e, data.from));

  const totalAlerts = repos.reduce((sum, r) => sum + r.openAlerts.length, 0);
  const totalResolved = repos.reduce((sum, r) => sum + r.activity.resolvedCount, 0);
  const scored = repos.filter((r) => r.scorecardScore !== null);
  const avgScore = scored.length > 0
    ? scored.reduce((sum, r) => sum + (r.scorecardScore || 0), 0) / scored.length
    : null;
  const generated = formatDateTime(data.generatedAt ?? new Date().toISOString());

  const periodDays = data.from
    ? Math.round((Date.parse(data.generatedAt ?? new Date().toISOString()) - Date.parse(data.from)) / 86400000)
    : null;
  const periodLabel = periodDays && periodDays > 0 ? `${periodDays}d` : undefined;

  const coverStats: Array<{ label: string; value: string | number }> = [
    { label: 'repositories', value: repos.length },
    { label: 'open alerts', value: totalAlerts },
    { label: periodLabel ? `resolved · ${periodLabel}` : 'resolved alerts', value: totalResolved },
  ];
  if (avgScore !== null) coverStats.push({ label: 'avg OpenSSF score', value: avgScore.toFixed(1) });
  if (otherEntries.length > 0) coverStats.push({ label: 'other components', value: otherEntries.length });

  return (
    <Document pageSize="a4" margins={{ top: 1, bottom: 1, left: 5, right: 5 }}>
      <Header type="first" height={0}>
        <></>
      </Header>
      <Header
        variant="solid"
        className="bg-slate-800"
        height={14}
        logo={<MissionControlLogo className="filter grayscale brightness-[250] contrast-100 mix-blend-screen h-[6mm] w-auto" />}
        subtitle="Security Report"
      />
      <Footer height={8}>
        <div className="px-[5mm] py-[1mm] border-t border-gray-200 text-xs text-gray-400 flex items-center justify-between gap-[4mm]">
          <span>Generated {generated}</span>
          <PageNo />
          {data.publicURL && (
            <a href={data.publicURL} className="text-blue-500" style={{ textDecoration: 'none' }}>{data.publicURL}</a>
          )}
        </div>
      </Footer>

      <Page type="first" margins={{ top: 10, bottom: 10, left: 5, right: 5 }}>
        <div className="flex justify-center pt-[10mm] pb-[6mm]">
          <MissionControlLogo className="h-[20mm] w-auto" />
        </div>
        <CoverPage
          title={data.title || 'Security Report'}
          subtitle="Security Report"
          subjects={repos.slice(0, 8).map((r) => r.configItem)}
          stats={coverStats}
          dateRange={data.from || data.to ? { from: data.from, to: data.to } : undefined}
          generatedAt={data.generatedAt}
        />
      </Page>

      <Page>
        <h2 className="text-xl font-bold text-gray-900 mb-[2mm]">Overview</h2>
        <p className="text-xs text-gray-700 mb-[3mm]">
          This report summarizes the security posture of {repos.length} GitHub{' '}
          {repos.length === 1 ? 'repository' : 'repositories'} tracked in Mission Control. It combines
          OpenSSF Scorecard assessments with GitHub security alerts (Dependabot, Code Scanning and
          Secret Scanning) — both open and recently resolved — as collected by catalog scrapers.
        </p>

        <OverviewStats repos={repos} periodLabel={periodLabel} />

        <blockquote className="info mb-[3mm]">
          <strong>About OpenSSF Scorecards:</strong> The Open Source Security Foundation (OpenSSF)
          Scorecard project automatically assesses projects against security best practices such as code
          review, dependency management, SAST tooling and vulnerability handling. Each check listed in
          this report carries its inherent risk level and a 0-10 score; a check is marked <em>pass</em>{' '}
          only when it fully meets the practice. The gauge shown per repository is the aggregate OpenSSF
          Scorecard score (0-10) as published by the OpenSSF Scorecard API.
        </blockquote>

        <blockquote className="mb-[4mm]">
          <strong>GitHub Security Scanning:</strong> GitHub provides three types of automated scanning:
          <ul className="list-disc list-inside mt-[1mm] ml-[3mm] text-xs">
            <li><strong>Dependabot:</strong> scans dependencies for known CVE vulnerabilities</li>
            <li><strong>Code Scanning (CodeQL):</strong> static analysis for vulnerabilities in source code</li>
            <li><strong>Secret Scanning:</strong> detects exposed credentials committed to repositories</li>
          </ul>
        </blockquote>

        <h2 className="text-xl font-bold text-gray-900 mb-[1mm]">Repository Summary</h2>
        <p className="text-xs text-gray-600 mb-[3mm]">
          Security metrics overview for all tracked repositories.
        </p>
        <div className="grid grid-cols-2 gap-[3mm]">
          {repos.map((r) => <RepoSummaryCard key={r.configItem.id} security={r} />)}
        </div>
      </Page>

      {repos.map((r) => (
        <Page key={r.configItem.id}>
          <RepoSecuritySection security={r} periodLabel={periodLabel} />
        </Page>
      ))}

      {otherEntries.length > 0 && (
        <Page>
          <h2 className="text-xl font-bold text-gray-900 mb-[1mm]">Other Components</h2>
          <p className="text-xs text-gray-600 mb-[3mm]">
            Security-relevant insights for non-repository components included in this report.
          </p>
          {otherEntries.map((entry) => (
            <OtherConfigSection key={entry.configItem?.id} entry={entry} />
          ))}
        </Page>
      )}

      <Page>
        <div className="mt-[4mm] border-t-2 border-gray-300 pt-[4mm]">
          <h2 className="text-xl font-bold text-gray-900 mb-[3mm]">Security Summary</h2>
          <OverviewStats repos={repos} periodLabel={periodLabel} />
          <blockquote className="info">
            <strong>Additional Security Resources:</strong> For questions about security practices or to
            report security issues, please contact{' '}
            <a href="mailto:security@flanksource.com" className="text-blue-600 underline">
              security@flanksource.com
            </a>.
          </blockquote>
          <div className="mt-[3mm] text-xs text-gray-500">
            <p>Report generated: {generated}</p>
            <p>Data sources: OpenSSF Scorecard, GitHub Security API (via Mission Control catalog scrapers)</p>
            <p>All metrics reflect insights recorded as of report generation.</p>
          </div>
        </div>
      </Page>
    </Document>
  );
}
