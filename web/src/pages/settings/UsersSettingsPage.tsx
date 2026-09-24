import { useState, type FormEvent } from "react";
import { Plus } from "lucide-react";
import { useUsers, useCreateUser } from "@/api/queries/users";
import { useAuth, hasRole, type UserRole } from "@/lib/useAuth";
import { friendlyMessage } from "@/api/errors";
import { formatTimestampDate } from "@/lib/date";
import { Button } from "@/components/ui/button";
import { Input, Label, Select, FieldError } from "@/components/ui/input";
import { PasswordInput } from "@/components/ui/password-input";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { ForbiddenPage } from "@/pages/ForbiddenPage";

const MIN_PASSWORD_LENGTH = 12;

function CreateUserDialog({
  open,
  onOpenChange,
  assignableRoles,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  assignableRoles: UserRole[];
}) {
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [role, setRole] = useState<UserRole>(assignableRoles[assignableRoles.length - 1] ?? "user");
  const [passwordError, setPasswordError] = useState<string | null>(null);
  const createUser = useCreateUser();

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (createUser.isPending) return;
    if (password.length < MIN_PASSWORD_LENGTH) {
      setPasswordError(`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`);
      return;
    }
    setPasswordError(null);
    createUser.mutate({ name, email, password, role }, { onSuccess: () => onOpenChange(false) });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange} title="Add a user">
      {createUser.isError && (
        <div className="mb-4">
          <Alert>{friendlyMessage(createUser.error)}</Alert>
        </div>
      )}
      <form onSubmit={handleSubmit} noValidate className="space-y-4">
        <div>
          <Label htmlFor="new-user-name">Name</Label>
          <Input id="new-user-name" required value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div>
          <Label htmlFor="new-user-email">Email</Label>
          <Input
            id="new-user-email"
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div>
          <Label htmlFor="new-user-password">Password</Label>
          <PasswordInput
            id="new-user-password"
            required
            minLength={MIN_PASSWORD_LENGTH}
            aria-invalid={passwordError !== null}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          <FieldError>{passwordError}</FieldError>
        </div>
        {assignableRoles.length > 1 && (
          <div>
            <Label htmlFor="new-user-role">Role</Label>
            <Select id="new-user-role" value={role} onChange={(e) => setRole(e.target.value as UserRole)}>
              {assignableRoles.map((r) => (
                <option key={r} value={r}>
                  {r.charAt(0).toUpperCase() + r.slice(1)}
                </option>
              ))}
            </Select>
          </div>
        )}
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" disabled={createUser.isPending}>
            {createUser.isPending ? "Adding…" : "Add user"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function UsersTable() {
  const query = useUsers({ limit: 100 });
  return (
    <QueryBoundary query={query}>
      {(page) => (
        <TableContainer>
          <THead>
            <Tr>
              <Th>Name</Th>
              <Th>Email</Th>
              <Th>Role</Th>
              <Th>Status</Th>
              <Th>Joined</Th>
            </Tr>
          </THead>
          <TBody>
            {page.items.map((u) => (
              <Tr key={u.id}>
                <Td className="font-medium text-slate-900">{u.name}</Td>
                <Td>{u.email}</Td>
                <Td className="capitalize">{u.role}</Td>
                <Td>
                  <Badge tone={u.isActive ? "green" : "slate"}>{u.isActive ? "Active" : "Inactive"}</Badge>
                </Td>
                <Td>{formatTimestampDate(u.createdAt)}</Td>
              </Tr>
            ))}
          </TBody>
        </TableContainer>
      )}
    </QueryBoundary>
  );
}

// Role assignment rule mirrors the backend exactly (see
// api/openapi.yaml's POST /users description): an admin may assign any
// role; a manager may only create a plain "user" — never admin or
// manager. This is UX guidance only, hiding an option the backend would
// reject anyway (403) — the backend re-enforces this independently.
function assignableRolesFor(role: UserRole | undefined): UserRole[] {
  if (role === "admin") return ["admin", "manager", "user"];
  if (role === "manager") return ["user"];
  return [];
}

export function UsersSettingsPage() {
  const { user } = useAuth();
  const [dialogOpen, setDialogOpen] = useState(false);

  if (!hasRole(user, "admin", "manager")) {
    return <ForbiddenPage />;
  }

  const assignableRoles = assignableRolesFor(user?.role as UserRole | undefined);

  return (
    <div>
      <div className="mb-4 flex justify-end">
        <Button size="sm" onClick={() => setDialogOpen(true)}>
          <Plus className="h-4 w-4" aria-hidden="true" />
          Add user
        </Button>
      </div>
      <UsersTable />
      <CreateUserDialog open={dialogOpen} onOpenChange={setDialogOpen} assignableRoles={assignableRoles} />
    </div>
  );
}
