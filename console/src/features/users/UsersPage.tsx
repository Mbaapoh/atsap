import { useState } from "react";
import { usePermissions } from "../auth/permissions";
import { AddUserDialog } from "./AddUserDialog";
import { GrantRoleDialog, RevokeRoleDialog } from "./RoleDialogs";
import { DisableDialog, ResetPasswordFlow } from "./MembershipDialogs";
import { useToast } from "../../components/Toast";
import { RowActions, type RowAction } from "../../components/RowActions";
import { Alert } from "../../components/Alert";
import { DataTable, type Column } from "../../components/DataTable";
import { EmptyState } from "../../components/EmptyState";
import { PageHeader } from "../../components/PageHeader";
import { Pager } from "../../components/Pager";
import { StatusBadge } from "../../components/StatusBadge";
import { roleLabel } from "../auth/permissions";
import { useMembershipStatus, useUsers, type MembershipStatus, type RoleGrant, type UserRow } from "./adapter";

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
  const { canAdminister } = usePermissions();
  const [adding, setAdding] = useState(false);
  const [granting, setGranting] = useState<UserRow | null>(null);
  const [revoking, setRevoking] = useState<{ user: UserRow; grant: RoleGrant } | null>(null);
  const [disabling, setDisabling] = useState<UserRow | null>(null);
  const [resetting, setResetting] = useState<UserRow | null>(null);
  const { setActive } = useMembershipStatus();
  const toast = useToast();

  async function enable(u: UserRow) {
    try {
      await setActive({ principalId: u.principalId, active: true });
      toast.show(`${u.name || u.email} enabled in this tenant.`);
    } catch {
      toast.show(`${u.name || u.email} could not be enabled. Try again.`, "warning");
    }
  }

  function actionsFor(u: UserRow): RowAction[] {
    return [
      { label: "Grant a role…", onSelect: () => setGranting(u) },
      u.status === "disabled"
        ? { label: "Enable", onSelect: () => void enable(u) }
        : { label: "Disable…", onSelect: () => setDisabling(u), destructive: true },
      { label: "Reset password…", onSelect: () => setResetting(u), destructive: true },
      ...u.roles.map((g) => ({ label: `Revoke ${roleLabel(g.role)}`, onSelect: () => setRevoking({ user: u, grant: g }), destructive: true })),
    ];
  }

  return (
    <section className="page" aria-labelledby="page-title">
      <PageHeader
        title="Users"
        description="People who can sign in to this tenant, and what each may do."
        action={
          canAdminister ? (
            <button type="button" className="button button-primary" onClick={() => setAdding(true)}>
              Add user
            </button>
          ) : undefined
        }
      />
      <AddUserDialog open={adding} onClose={() => setAdding(false)} />
      <GrantRoleDialog user={granting} onClose={() => setGranting(null)} />
      <RevokeRoleDialog target={revoking} onClose={() => setRevoking(null)} />
      <DisableDialog user={disabling} onClose={() => setDisabling(null)} />
      <ResetPasswordFlow user={resetting} onClose={() => setResetting(null)} />
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
            rowActions={canAdminister ? (u) => <RowActions label={u.name || u.email} actions={actionsFor(u)} /> : undefined}
            empty={<EmptyState title="No users yet">People appear here once they are added to this tenant.</EmptyState>}
          />
          <Pager page={page} rowCount={users.data.length} onPage={setPage} label="Users pages" />
        </>
      ) : null}
    </section>
  );
}
