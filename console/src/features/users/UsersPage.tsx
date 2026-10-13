import { useState } from "react";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { PageHeader } from "../../components/PageHeader";
import { Pager } from "../../components/Pager";
import { StatusBadge } from "../../components/StatusBadge";
import { roleLabel } from "../auth/permissions";
import { useUsers, type MembershipStatus, type UserRow } from "./adapter";

const statusBadge: Record<MembershipStatus, { label: string; tone: "success" | "neutral" | "warning" }> = {
  active: { label: "Active", tone: "success" },
  disabled: { label: "Disabled", tone: "neutral" },
  unknown: { label: "Unknown", tone: "warning" },
};

export const userColumns: Column<UserRow>[] = [
  { key: "name", header: "Name", render: (u) => u.name || "—" },
  { key: "email", header: "Email", render: (u) => u.email },
  { key: "status", header: "Status", render: (u) => <StatusBadge {...statusBadge[u.status]} /> },
  {
    key: "roles",
    header: "Roles",
    render: (u) => (u.roles.length ? u.roles.map((r) => roleLabel(r.role)).join(", ") : "No role"),
  },
];

// UsersPage lists the people who are members of this tenant, with their
// status and roles (CONSOLE-DESIGN "Users"). No extension column, agent
// controls or per-person history in Phase A (D-65).
export function UsersPage() {
  const [page, setPage] = useState(0);
  const users = useUsers(page);

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader title="Users" description="People who can sign in to this tenant, and what each may do." />
      {users.isPending ? <p role="status">Loading users…</p> : null}
      {users.isError ? (
        <Alert tone="danger" title="Users could not be loaded">
          <p>{users.error.kind === "forbidden" ? "Your role cannot see this tenant's users." : users.error.message}</p>
        </Alert>
      ) : null}
      {users.data ? (
        <>
          <DataTable
            caption="Users of this tenant"
            captionHidden
            columns={userColumns}
            rows={users.data}
            rowKey={(u) => u.principalId}
            empty={<EmptyState title="No users yet">People appear here once they are added to this tenant.</EmptyState>}
          />
          <Pager page={page} rowCount={users.data.length} onPage={setPage} label="Users pages" />
        </>
      ) : null}
    </section>
  );
}
