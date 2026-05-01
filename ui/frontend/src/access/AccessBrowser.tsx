import { useState, type ReactNode } from "react";
import {
  Badge,
  DataTable,
  DetailEmptyState,
  FilterBar,
  KeyValueList,
  MatrixTable,
  Section,
  TabButton,
  type DataTableColumn,
  type KeyValueListItem,
  type MatrixTableRow,
} from "@flanksource/clicky-ui";
import { ResourceIcon } from "@flanksource/icons/icon";
import { ConfigIcon } from "../ConfigIcon";
import {
  useAccessGroupDetail,
  useAccessGroups,
  useAccessUserDetail,
  useAccessUsers,
} from "../api/hooks";
import type {
  ConfigAccessSummary,
  ExternalGroup,
  ExternalUser,
  ExternalUserGroupMembership,
} from "../api/types";
import { TagList } from "../config-detail/TagList";
import { formatDate, isIndirectAccess, stringifyValue, timeAgo } from "../config-detail/utils";
import { DetailPageLayout, EntityHeader, HeaderPill, PageBreadcrumbs } from "../layout/DetailPageLayout";

export type AccessBrowserMode = "users" | "groups";

export type AccessBrowserProps = {
  mode: AccessBrowserMode;
  id?: string;
};

export function AccessBrowser({ mode, id }: AccessBrowserProps) {
  if (id) {
    return mode === "users" ? <AccessUserDetail id={id} /> : <AccessGroupDetail id={id} />;
  }
  return mode === "users" ? <AccessUsersList /> : <AccessGroupsList />;
}

function AccessUsersList() {
  const query = useAccessUsers();
  return (
    <AccessShell
      title="Access Users"
      subtitle="External identities with catalog access"
      icon="lucide:user"
      headerMeta={query.data ? <HeaderPill icon="lucide:hash" label={`${query.data.length} users`} /> : undefined}
    >
      <QueryState query={query} emptyIcon="lucide:user" emptyLabel="No access users" />
      {query.data && query.data.length > 0 && (
        <DataTable
          data={query.data as unknown as Record<string, unknown>[]}
          columns={userColumns}
          getRowId={(row) => String(row.id)}
          getRowHref={(row) => `/ui/access/users/${encodeURIComponent(String(row.id))}`}
          defaultSort={{ key: "name", dir: "asc" }}
          autoFilter
        />
      )}
    </AccessShell>
  );
}

function AccessGroupsList() {
  const query = useAccessGroups();
  return (
    <AccessShell
      title="Access Groups"
      subtitle="External groups and their catalog grants"
      icon="lucide:users"
      headerMeta={query.data ? <HeaderPill icon="lucide:hash" label={`${query.data.length} groups`} /> : undefined}
    >
      <QueryState query={query} emptyIcon="lucide:users" emptyLabel="No access groups" />
      {query.data && query.data.length > 0 && (
        <DataTable
          data={query.data as unknown as Record<string, unknown>[]}
          columns={groupColumns}
          getRowId={(row) => String(row.id)}
          getRowHref={(row) => `/ui/access/groups/${encodeURIComponent(String(row.id))}`}
          defaultSort={{ key: "name", dir: "asc" }}
          autoFilter
        />
      )}
    </AccessShell>
  );
}

function AccessUserDetail({ id }: { id: string }) {
  const query = useAccessUserDetail(id);
  const user = query.data?.user;
  const groups = query.data?.groups ?? [];
  const access = query.data?.access ?? [];
  const [activeTab, setActiveTab] = useState<"groups" | "permissions">("groups");

  return (
    <AccessShell
      title={user?.name ?? "Access User"}
      subtitle={user?.email ?? id}
      icon={identityIcon(user?.id ?? id, user?.user_type)}
      backHref="/ui/access/users"
      backLabel="Access Users"
      headerMeta={user ? <UserHeaderMeta user={user} groupCount={groups.length} accessCount={access.length} /> : undefined}
    >
      <QueryState query={query} emptyIcon="lucide:user" emptyLabel="Access user not found" />
      {user && (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-[22rem_minmax(0,1fr)]">
          <div className="flex min-w-0 flex-col gap-4">
            <Section title="Details" icon="lucide:id-card" defaultOpen>
              <KeyValueList items={userDetailItems(user)} />
            </Section>
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <AccessDetailTabs
              active={activeTab}
              membershipKey="groups"
              membershipLabel="Groups"
              membershipIcon="lucide:users"
              membershipCount={groups.length}
              permissionsCount={access.length}
              onChange={setActiveTab}
            />
            {activeTab === "groups" && <MembershipGroupsTable rows={groups} />}
            {activeTab === "permissions" && <PermissionsMatrix rows={access} emptyLabel="No permissions" />}
          </div>
        </div>
      )}
    </AccessShell>
  );
}

function AccessGroupDetail({ id }: { id: string }) {
  const query = useAccessGroupDetail(id);
  const group = query.data?.group;
  const members = query.data?.members ?? [];
  const access = query.data?.access ?? [];
  const [activeTab, setActiveTab] = useState<"users" | "permissions">("users");

  return (
    <AccessShell
      title={group?.name ?? "Access Group"}
      subtitle={id}
      icon={groupTypeIcon(group?.group_type)}
      backHref="/ui/access/groups"
      backLabel="Access Groups"
      headerMeta={group ? <GroupHeaderMeta group={group} memberCount={members.length} accessCount={access.length} /> : undefined}
    >
      <QueryState query={query} emptyIcon="lucide:users" emptyLabel="Access group not found" />
      {group && (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-[22rem_minmax(0,1fr)]">
          <div className="flex min-w-0 flex-col gap-4">
            <Section title="Details" icon="lucide:id-card" defaultOpen>
              <KeyValueList items={groupDetailItems(group)} />
            </Section>
          </div>
          <div className="flex min-w-0 flex-col gap-4">
            <AccessDetailTabs
              active={activeTab}
              membershipKey="users"
              membershipLabel="Users"
              membershipIcon="lucide:user-round-check"
              membershipCount={members.length}
              permissionsCount={access.length}
              onChange={setActiveTab}
            />
            {activeTab === "users" && <MembershipUsersTable rows={members} />}
            {activeTab === "permissions" && <PermissionsMatrix rows={access} emptyLabel="No permissions" />}
          </div>
        </div>
      )}
    </AccessShell>
  );
}

function AccessShell({
  title,
  subtitle,
  icon,
  backHref,
  backLabel,
  headerMeta,
  children,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  icon: string;
  backHref?: string;
  backLabel?: string;
  headerMeta?: ReactNode;
  children: ReactNode;
}) {
  return (
    <DetailPageLayout
      breadcrumbs={backHref ? (
        <PageBreadcrumbs
          items={[
            { label: backLabel ?? "Access", href: backHref, icon: "lucide:chevron-left" },
            { label: title, title: typeof title === "string" ? title : undefined, className: "max-w-[24rem]" },
          ]}
        />
      ) : undefined}
      header={
        <EntityHeader
          variant="card"
          titleSize="lg"
          icon={icon}
          title={title}
          description={subtitle ? <span className="block truncate">{subtitle}</span> : undefined}
          meta={headerMeta}
        />
      }
      main={children}
    />
  );
}

function UserHeaderMeta({
  user,
  groupCount,
  accessCount,
}: {
  user: ExternalUser;
  groupCount: number;
  accessCount: number;
}) {
  return (
    <>
      {user.account_id && <HeaderPill icon="lucide:building-2" label={user.account_id} mono />}
      <CountBadge icon="lucide:users" count={groupCount} singular="group" plural="groups" />
      <CountBadge icon="lucide:shield-check" count={accessCount} singular="permission" plural="permissions" />
      {user.aliases?.length ? <TagList values={user.aliases} maxVisible={2} className="max-w-xl" /> : null}
      <HeaderDatePill label="Created" value={user.created_at} />
      <HeaderDatePill label="Updated" value={user.updated_at} />
      <HeaderPill icon="lucide:fingerprint" label={user.id} mono />
    </>
  );
}

function GroupHeaderMeta({
  group,
  memberCount,
  accessCount,
}: {
  group: ExternalGroup;
  memberCount: number;
  accessCount: number;
}) {
  return (
    <>
      {group.account_id && <HeaderPill icon="lucide:building-2" label={group.account_id} mono />}
      <CountBadge icon="lucide:user-round-check" count={memberCount} singular="member" plural="members" />
      <CountBadge icon="lucide:shield-check" count={accessCount} singular="permission" plural="permissions" />
      {group.aliases?.length ? <TagList values={group.aliases} maxVisible={2} className="max-w-xl" /> : null}
      <HeaderDatePill label="Created" value={group.created_at} />
      <HeaderDatePill label="Updated" value={group.updated_at} />
      <HeaderPill icon="lucide:fingerprint" label={group.id} mono />
    </>
  );
}

function CountBadge({
  icon,
  count,
  singular,
  plural,
}: {
  icon: string;
  count: number;
  singular: string;
  plural: string;
}) {
  if (count <= 0) return null;
  return (
    <Badge size="xs" icon={icon}>
      {count} {count === 1 ? singular : plural}
    </Badge>
  );
}

function HeaderDatePill({ label, value }: { label: string; value?: string | null }) {
  if (!value) return null;
  return (
    <span title={formatDate(value)}>
      <HeaderPill icon="lucide:clock-3" label={`${label}: ${timeAgo(value)}`} />
    </span>
  );
}

type AccessDetailTab = "groups" | "users" | "permissions";

function AccessDetailTabs({
  active,
  membershipKey,
  membershipLabel,
  membershipIcon,
  membershipCount,
  permissionsCount,
  onChange,
}: {
  active: AccessDetailTab;
  membershipKey: "groups" | "users";
  membershipLabel: "Groups" | "Users";
  membershipIcon: string;
  membershipCount: number;
  permissionsCount: number;
  onChange: (tab: any) => void;
}) {
  return (
    <div className="border-b border-border pb-2">
      <div className="flex flex-wrap items-center gap-2" role="tablist">
        <TabButton
          active={active === membershipKey}
          onClick={() => onChange(membershipKey)}
          label={membershipLabel}
          icon={membershipIcon}
          count={membershipCount}
        />
        <TabButton
          active={active === "permissions"}
          onClick={() => onChange("permissions")}
          label="Permissions"
          icon="lucide:shield-check"
          count={permissionsCount}
        />
      </div>
    </div>
  );
}

function QueryState({
  query,
  emptyIcon,
  emptyLabel,
}: {
  query: { isLoading: boolean; error: unknown; data?: unknown };
  emptyIcon: string;
  emptyLabel: string;
}) {
  if (query.isLoading) {
    return <div className="text-sm text-muted-foreground">Loading...</div>;
  }
  if (query.error) {
    return (
      <div className="rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm text-destructive">
        {query.error instanceof Error ? query.error.message : String(query.error)}
      </div>
    );
  }
  if (Array.isArray(query.data) && query.data.length === 0) {
    return <DetailEmptyState icon={emptyIcon} label={emptyLabel} />;
  }
  if (query.data && typeof query.data === "object" && "user" in query.data && !(query.data as { user?: unknown }).user) {
    return <DetailEmptyState icon={emptyIcon} label={emptyLabel} />;
  }
  if (query.data && typeof query.data === "object" && "group" in query.data && !(query.data as { group?: unknown }).group) {
    return <DetailEmptyState icon={emptyIcon} label={emptyLabel} />;
  }
  return null;
}

const userColumns: DataTableColumn<Record<string, unknown>>[] = [
  {
    key: "name",
    label: "User",
    grow: true,
    render: (value, row) => {
      const type = stringifyValue(row.user_type);
      return (
        <PrincipalCell
          icon={identityIcon(String(row.id ?? ""), type)}
          iconTitle={type !== "-" ? type : undefined}
          name={stringifyValue(value)}
          subtitle={stringifyValue(row.email)}
        />
      );
    },
    filterValue: (value, row) => [stringifyValue(value), stringifyValue(row.email), stringifyValue(row.user_type)],
  },
  { key: "account_id", label: "Account", shrink: true, render: (value) => <span className="font-mono text-xs">{stringifyValue(value)}</span> },
  {
    key: "aliases",
    label: "Aliases",
    shrink: true,
    cellClassName: "w-80 max-w-80",
    render: (value) => <TagList values={Array.isArray(value) ? value.map(String) : []} maxVisible={2} />,
    filterValue: (value) => Array.isArray(value) ? value : [],
  },
  { key: "created_at", label: "Created", shrink: true, render: (value) => timeAgo(String(value || "")), sortValue: (value) => new Date(String(value)).getTime() },
];

const groupColumns: DataTableColumn<Record<string, unknown>>[] = [
  {
    key: "name",
    label: "Group",
    grow: true,
    render: (value, row) => {
      const type = stringifyValue(row.group_type);
      return (
        <PrincipalCell
          icon={groupTypeIcon(type)}
          iconTitle={type !== "-" ? type : undefined}
          name={stringifyValue(value)}
        />
      );
    },
    filterValue: (value, row) => [stringifyValue(value), stringifyValue(row.group_type)],
  },
  { key: "account_id", label: "Account", shrink: true, render: (value) => <span className="font-mono text-xs">{stringifyValue(value)}</span> },
  { key: "members_count", label: "Members", shrink: true, render: (value) => numberCountBadge(value), sortValue: numberSortValue },
  { key: "permissions_count", label: "Permissions", shrink: true, render: (value) => numberCountBadge(value), sortValue: numberSortValue },
  {
    key: "aliases",
    label: "Aliases",
    shrink: true,
    cellClassName: "w-80 max-w-80",
    render: (value) => <TagList values={Array.isArray(value) ? value.map(String) : []} maxVisible={2} />,
    filterValue: (value) => Array.isArray(value) ? value : [],
  },
  { key: "created_at", label: "Created", shrink: true, render: (value) => timeAgo(String(value || "")), sortValue: (value) => new Date(String(value)).getTime() },
];

function numberCountBadge(value: unknown) {
  const count = Number(value ?? 0);
  if (!Number.isFinite(count) || count <= 0) return null;
  return <Badge size="xs">{count}</Badge>;
}

function numberSortValue(value: unknown) {
  const count = Number(value ?? 0);
  return Number.isFinite(count) ? count : 0;
}

function MembershipGroupsTable({ rows }: { rows: ExternalUserGroupMembership[] }) {
  const [showRemoved, setShowRemoved] = useState(false);
  const [search, setSearch] = useState("");
  if (rows.length === 0) return <DetailEmptyState label="No group memberships" />;
  const activeRows = rows.filter((row) => !row.deleted_at);
  const visibleRows = (showRemoved ? rows : activeRows).filter((row) =>
    membershipGroupSearchText(row).includes(search.trim().toLowerCase()),
  );

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <FilterBar
        search={{
          value: search,
          onChange: setSearch,
          placeholder: "Search all columns...",
          ariaLabel: "Search all columns",
        }}
      >
        <MembershipStatusFilter showRemoved={showRemoved} onChange={setShowRemoved} />
      </FilterBar>
      {visibleRows.length === 0 ? (
        <DetailEmptyState label={showRemoved ? "No matching group memberships" : "No active group memberships"} />
      ) : (
        <DataTable
          data={visibleRows as unknown as Record<string, unknown>[]}
          columns={membershipGroupColumns}
          getRowId={(row, index) => `${row.external_group_id}-${index}`}
          getRowHref={(row) => `/ui/access/groups/${encodeURIComponent(String(row.external_group_id))}`}
          showGlobalFilter={false}
        />
      )}
    </div>
  );
}

function MembershipStatusFilter({
  showRemoved,
  onChange,
}: {
  showRemoved: boolean;
  onChange: (showRemoved: boolean) => void;
}) {
  return (
    <div className="inline-flex h-8 shrink-0 rounded-md border border-input bg-muted/30 p-0.5 text-xs">
      <button
        type="button"
        onClick={() => onChange(false)}
        className={[
          "rounded px-2 transition-colors",
          !showRemoved ? "bg-background font-medium text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
        ].join(" ")}
      >
        Active
      </button>
      <button
        type="button"
        onClick={() => onChange(true)}
        className={[
          "rounded px-2 transition-colors",
          showRemoved ? "bg-background font-medium text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
        ].join(" ")}
      >
        All
      </button>
    </div>
  );
}

function membershipGroupSearchText(row: ExternalUserGroupMembership) {
  const group = row.external_groups;
  return [
    group?.name,
    group?.group_type,
    group?.account_id,
    row.external_group_id,
    row.created_at,
    row.deleted_at,
    row.deleted_at ? "removed" : "active",
  ]
    .filter(Boolean)
    .join(" ")
    .toLowerCase();
}

function MembershipUsersTable({ rows }: { rows: ExternalUserGroupMembership[] }) {
  if (rows.length === 0) return <DetailEmptyState label="No members" />;
  return (
    <DataTable
      data={rows as unknown as Record<string, unknown>[]}
      columns={membershipUserColumns}
      getRowId={(row, index) => `${row.external_user_id}-${index}`}
      getRowHref={(row) => `/ui/access/users/${encodeURIComponent(String(row.external_user_id))}`}
      autoFilter
    />
  );
}

const membershipGroupColumns: DataTableColumn<Record<string, unknown>>[] = [
  {
    key: "external_groups",
    label: "Group",
    grow: true,
    render: (value) => {
      const group = value as ExternalGroup | null;
      return (
        <PrincipalCell
          icon={groupTypeIcon(group?.group_type)}
          iconTitle={group?.group_type ?? undefined}
          name={group?.name ?? "-"}
        />
      );
    },
    filterValue: (value) => {
      const group = value as ExternalGroup | null;
      return [group?.name ?? "", group?.group_type ?? ""];
    },
  },
  { key: "created_at", label: "Added", shrink: true, render: (value) => formatDate(String(value || "")), sortValue: (value) => new Date(String(value)).getTime() },
  { key: "deleted_at", label: "Status", shrink: true, render: (value) => membershipStatus(value) },
];

const membershipUserColumns: DataTableColumn<Record<string, unknown>>[] = [
  {
    key: "external_users",
    label: "User",
    grow: true,
    render: (value) => {
      const user = value as ExternalUser | null;
      return (
        <PrincipalCell
          icon={identityIcon(user?.id ?? "", user?.user_type ?? "")}
          iconTitle={user?.user_type ?? undefined}
          name={user?.name ?? "-"}
          subtitle={user?.email ?? undefined}
        />
      );
    },
    filterValue: (value) => {
      const user = value as ExternalUser | null;
      return [user?.name ?? "", user?.email ?? "", user?.user_type ?? ""];
    },
  },
  { key: "created_at", label: "Added", shrink: true, render: (value) => formatDate(String(value || "")), sortValue: (value) => new Date(String(value)).getTime() },
  { key: "deleted_at", label: "Status", shrink: true, render: (value) => membershipStatus(value) },
];

function PermissionsMatrix({ rows, emptyLabel }: { rows: ConfigAccessSummary[]; emptyLabel: string }) {
  if (rows.length === 0) return <DetailEmptyState label={emptyLabel} />;
  const groups = groupPermissionsByResourceType(rows);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      {groups.map((group) => (
        <Section
          key={group.type}
          title={<span className="inline-flex min-w-0 items-center gap-2"><ConfigIcon primary={group.type} className="h-4 max-w-4 shrink-0" />{group.type}</span>}
          defaultOpen
          summary={`${group.resources.length} resources`}
        >
          <MatrixTable
            columns={group.roles.map(permissionHeader)}
            rows={permissionMatrixRows(group)}
            corner={<span className="text-xs text-muted-foreground">Resource</span>}
            emptyMessage="No permissions"
            angledHeaders
            density="compact"
            columnWidth={48}
            headerHeight={120}
            rowLabelClassName="min-w-72 max-w-[32rem]"
          />
        </Section>
      ))}
      <PermissionsMatrixLegend />
    </div>
  );
}

type PermissionResourceGroup = {
  type: string;
  roles: string[];
  resources: PermissionResource[];
};

type PermissionResource = {
  id: string;
  name: string;
  type: string;
  roles: Map<string, ConfigAccessSummary>;
};

function groupPermissionsByResourceType(rows: ConfigAccessSummary[]): PermissionResourceGroup[] {
  const groupMap = new Map<string, { roles: Set<string>; resources: Map<string, PermissionResource> }>();

  for (const row of rows) {
    const type = row.config_type || "Unknown";
    const role = row.role || "unknown";
    const id = row.config_id || row.config_name || `${type}:${role}`;
    const group = groupMap.get(type) ?? { roles: new Set<string>(), resources: new Map<string, PermissionResource>() };
    group.roles.add(role);

    const resource = group.resources.get(id) ?? {
      id,
      name: row.config_name || id,
      type,
      roles: new Map<string, ConfigAccessSummary>(),
    };
    resource.roles.set(role, mergePermissionRows(resource.roles.get(role), row));
    group.resources.set(id, resource);
    groupMap.set(type, group);
  }

  return Array.from(groupMap.entries())
    .map(([type, group]) => ({
      type,
      roles: Array.from(group.roles).sort((a, b) => a.localeCompare(b)),
      resources: Array.from(group.resources.values()).sort((a, b) => a.name.localeCompare(b.name)),
    }))
    .sort((a, b) => a.type.localeCompare(b.type));
}

function permissionMatrixRows(group: PermissionResourceGroup): MatrixTableRow[] {
  return group.resources.map((resource) => ({
    key: resource.id,
    label: (
      <a href={configHref(resource.id)} className="flex min-w-0 items-center gap-2 hover:underline">
        <ConfigIcon primary={resource.type} className="h-4 max-w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0">
          <div className="truncate">{resource.name}</div>
          {resource.id && resource.id !== resource.name && (
            <div className="truncate font-mono text-[11px] font-normal text-muted-foreground">{resource.id}</div>
          )}
        </div>
      </a>
    ),
    cells: group.roles.map((role) => {
      const permission = resource.roles.get(role);
      if (!permission) return null;
      return (
        <a href={configHref(resource.id)} className="inline-flex w-full justify-center" title={permissionTooltip(permission)}>
          <PermissionDot indirect={isIndirectAccess(permission)} />
        </a>
      );
    }),
  }));
}

function mergePermissionRows(existing: ConfigAccessSummary | undefined, next: ConfigAccessSummary) {
  if (!existing) return next;
  if (isIndirectAccess(existing) && !isIndirectAccess(next)) return next;
  if (!existing.last_reviewed_at && next.last_reviewed_at) return next;
  if (!existing.last_signed_in_at && next.last_signed_in_at) return next;
  if (!existing.created_at && next.created_at) return next;
  return existing;
}

function configHref(id: string) {
  return `/ui/item/${encodeURIComponent(id)}`;
}

function permissionHeader(label: string) {
  return <span title={label}>{label}</span>;
}

function permissionTooltip(row: ConfigAccessSummary) {
  const accessType = isIndirectAccess(row) ? "Group / indirect" : "Direct";
  const groupName = row.group_name || row.external_group_id;
  const parts = [
    row.role ? `Role: ${row.role}` : undefined,
    accessType,
    groupName ? `Group: ${groupName}` : undefined,
    row.config_name ? `Resource: ${row.config_name}` : undefined,
    row.last_reviewed_at ? `Reviewed: ${formatDate(row.last_reviewed_at)}` : undefined,
    row.last_signed_in_at ? `Last sign in: ${formatDate(row.last_signed_in_at)}` : undefined,
  ];
  return parts.filter(Boolean).join("\n");
}

function PermissionDot({ indirect = false }: { indirect?: boolean }) {
  return (
    <span className="inline-flex h-5 w-full items-center justify-center">
      <span
        className={
          indirect
            ? "h-3 w-3 rounded-full border-2 border-violet-600 bg-transparent"
            : "h-3 w-3 rounded-full bg-blue-600"
        }
      />
    </span>
  );
}

function PermissionsMatrixLegend() {
  return (
    <div className="flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
      <span className="font-medium text-foreground">Legend:</span>
      <span className="inline-flex items-center gap-1"><PermissionDot /> Direct</span>
      <span className="inline-flex items-center gap-1"><PermissionDot indirect /> Group / indirect</span>
    </div>
  );
}

function userDetailItems(user: ExternalUser): KeyValueListItem[] {
  return [
    { key: "id", label: "ID", value: <span className="font-mono text-xs">{user.id}</span> },
    { key: "name", label: "Name", value: user.name },
    { key: "email", label: "Email", value: user.email ?? "-", hidden: !user.email },
    { key: "account", label: "Account", value: user.account_id ?? "-", hidden: !user.account_id },
    { key: "aliases", label: "Aliases", value: <TagList values={user.aliases} maxVisible={3} />, hidden: !user.aliases?.length },
  ];
}

function groupDetailItems(group: ExternalGroup): KeyValueListItem[] {
  return [
    { key: "id", label: "ID", value: <span className="font-mono text-xs">{group.id}</span> },
    { key: "name", label: "Name", value: group.name },
    { key: "account", label: "Account", value: group.account_id ?? "-", hidden: !group.account_id },
    { key: "aliases", label: "Aliases", value: <TagList values={group.aliases} maxVisible={3} />, hidden: !group.aliases?.length },
  ];
}

function PrincipalCell({
  icon,
  iconTitle,
  name,
  subtitle,
}: {
  icon: string;
  iconTitle?: string;
  name: ReactNode;
  subtitle?: ReactNode;
}) {
  return (
    <div className="flex min-w-0 items-center gap-2">
      <span title={iconTitle} aria-label={iconTitle}>
        <ResourceIcon
          primary={icon}
          className="h-4 max-w-4 shrink-0 text-muted-foreground"
          alt={iconTitle ?? ""}
        />
      </span>
      <div className="min-w-0">
        <div className="truncate font-medium">{name}</div>
        {subtitle && subtitle !== "-" && <div className="truncate text-xs text-muted-foreground">{subtitle}</div>}
      </div>
    </div>
  );
}

function membershipStatus(value: unknown) {
  if (!value) return <Badge tone="success" size="xs">Active</Badge>;
  const removedAt = String(value);
  return (
    <span className="inline-flex items-center gap-2" title={formatDate(removedAt)}>
      <Badge tone="danger" size="xs">Removed</Badge>
      <span className="whitespace-nowrap text-xs text-muted-foreground">{formatDate(removedAt)}</span>
    </span>
  );
}

function identityIcon(id: string, type?: string | null) {
  const value = `${id} ${type ?? ""}`.toLowerCase();
  if (value.includes("aws") && value.includes("iam")) return "aws-iam-user";
  if (value.includes("azure")) return "azure-user";
  if (value.includes("k8s") || value.includes("kubernetes")) return "k8s-user";
  if (/service[-_\s]?account|svc[-_]|service[-_]/i.test(value)) return "k8s-serviceaccount";
  if (/bot|automation|pipeline/i.test(value)) return "bot";
  return "user";
}

function groupTypeIcon(type?: string | null) {
  const value = (type ?? "").toLowerCase();
  if (value.includes("aws") && value.includes("security")) return "aws-security-group";
  if (value.includes("azure")) return "azure-group";
  if (value.includes("k8s") || value.includes("kubernetes")) return "k8s-group";
  if (/team|department|org|organization/.test(value)) return "teams";
  if (/role|permission|rbac|policy|admin/.test(value)) return "shield-check";
  return "group";
}
