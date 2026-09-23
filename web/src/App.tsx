import { Suspense, lazy } from "react";
import { Navigate, Route, Routes } from "react-router-dom";
import { ProtectedRoute } from "@/components/ProtectedRoute";
import { AppShell } from "@/components/layout/AppShell";
import { LoginPage } from "@/pages/auth/LoginPage";
import { RegisterPage } from "@/pages/auth/RegisterPage";
import { PageLoading } from "@/components/ui/spinner";

// Everything behind the authenticated shell is code-split (Milestone 12
// section 44): a first-time visitor only ever needs LoginPage/
// RegisterPage (kept as ordinary eager imports, above) before they've
// authenticated at all, so the rest of the application — the bulk of
// this bundle — loads on demand once they're inside the shell, not
// upfront.
const DashboardPage = lazy(() => import("@/pages/DashboardPage").then((m) => ({ default: m.DashboardPage })));
const CustomersListPage = lazy(() =>
  import("@/pages/customers/CustomersListPage").then((m) => ({ default: m.CustomersListPage })),
);
const CustomerNewPage = lazy(() =>
  import("@/pages/customers/CustomerNewPage").then((m) => ({ default: m.CustomerNewPage })),
);
const CustomerDetailPage = lazy(() =>
  import("@/pages/customers/CustomerDetailPage").then((m) => ({ default: m.CustomerDetailPage })),
);
const ProductsListPage = lazy(() =>
  import("@/pages/products/ProductsListPage").then((m) => ({ default: m.ProductsListPage })),
);
const ProductNewPage = lazy(() =>
  import("@/pages/products/ProductNewPage").then((m) => ({ default: m.ProductNewPage })),
);
const ProductDetailPage = lazy(() =>
  import("@/pages/products/ProductDetailPage").then((m) => ({ default: m.ProductDetailPage })),
);
const InvoicesListPage = lazy(() =>
  import("@/pages/invoices/InvoicesListPage").then((m) => ({ default: m.InvoicesListPage })),
);
const InvoiceNewPage = lazy(() =>
  import("@/pages/invoices/InvoiceNewPage").then((m) => ({ default: m.InvoiceNewPage })),
);
const InvoiceDetailPage = lazy(() =>
  import("@/pages/invoices/InvoiceDetailPage").then((m) => ({ default: m.InvoiceDetailPage })),
);
const SettingsLayout = lazy(() =>
  import("@/pages/settings/SettingsLayout").then((m) => ({ default: m.SettingsLayout })),
);
const OrganisationSettingsPage = lazy(() =>
  import("@/pages/settings/OrganisationSettingsPage").then((m) => ({ default: m.OrganisationSettingsPage })),
);
const BillingAddressSettingsPage = lazy(() =>
  import("@/pages/settings/BillingAddressSettingsPage").then((m) => ({ default: m.BillingAddressSettingsPage })),
);
const InvoiceSettingsPage = lazy(() =>
  import("@/pages/settings/InvoiceSettingsPage").then((m) => ({ default: m.InvoiceSettingsPage })),
);
const UsersSettingsPage = lazy(() =>
  import("@/pages/settings/UsersSettingsPage").then((m) => ({ default: m.UsersSettingsPage })),
);
const NotFoundPage = lazy(() => import("@/pages/NotFoundPage").then((m) => ({ default: m.NotFoundPage })));

export function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />

      <Route element={<ProtectedRoute />}>
        <Route element={<AppShell />}>
          <Route
            path="*"
            element={
              <Suspense fallback={<PageLoading />}>
                <Routes>
                  <Route path="/" element={<Navigate to="/dashboard" replace />} />
                  <Route path="dashboard" element={<DashboardPage />} />

                  <Route path="customers" element={<CustomersListPage />} />
                  <Route path="customers/new" element={<CustomerNewPage />} />
                  <Route path="customers/:id" element={<CustomerDetailPage />} />

                  <Route path="products" element={<ProductsListPage />} />
                  <Route path="products/new" element={<ProductNewPage />} />
                  <Route path="products/:id" element={<ProductDetailPage />} />

                  <Route path="invoices" element={<InvoicesListPage />} />
                  <Route path="invoices/new" element={<InvoiceNewPage />} />
                  <Route path="invoices/:id" element={<InvoiceDetailPage />} />

                  <Route path="settings" element={<SettingsLayout />}>
                    <Route index element={<Navigate to="/settings/organisation" replace />} />
                    <Route path="organisation" element={<OrganisationSettingsPage />} />
                    <Route path="billing-address" element={<BillingAddressSettingsPage />} />
                    <Route path="invoice" element={<InvoiceSettingsPage />} />
                    <Route path="users" element={<UsersSettingsPage />} />
                  </Route>

                  <Route path="*" element={<NotFoundPage />} />
                </Routes>
              </Suspense>
            }
          />
        </Route>
      </Route>
    </Routes>
  );
}
