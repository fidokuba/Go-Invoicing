import { NavLink, Outlet } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { cn } from "@/lib/cn";
import { useAuth, hasRole } from "@/lib/useAuth";

const TABS = [
  { to: "/settings/organisation", label: "Organisation" },
  { to: "/settings/billing-address", label: "Billing Address" },
  { to: "/settings/invoice", label: "Invoice Settings" },
  { to: "/settings/users", label: "Users", roles: ["admin", "manager"] as const },
];

export function SettingsLayout() {
  const { user } = useAuth();
  // Milestone 12 section 21: don't show a nav destination the current
  // role clearly can't use — UsersSettingsPage still enforces this
  // itself independently (via ForbiddenPage) for a direct/typed URL.
  const visibleTabs = TABS.filter((tab) => !tab.roles || hasRole(user, ...tab.roles));

  return (
    <div>
      <PageHeader title="Settings" />
      <div className="mb-6 border-b border-slate-200">
        <nav aria-label="Settings sections" className="-mb-px flex gap-6">
          {visibleTabs.map((tab) => (
            <NavLink
              key={tab.to}
              to={tab.to}
              className={({ isActive }) =>
                cn(
                  "border-b-2 px-1 pb-3 text-sm font-medium",
                  isActive
                    ? "border-brand-600 text-brand-700"
                    : "border-transparent text-slate-500 hover:text-slate-700",
                )
              }
            >
              {tab.label}
            </NavLink>
          ))}
        </nav>
      </div>
      <Outlet />
    </div>
  );
}
