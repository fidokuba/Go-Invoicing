import { Link } from "react-router-dom";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent } from "@/components/ui/card";

// PLACEHOLDER: every section below is placeholder text only and must be
// replaced with reviewed legal wording before this page is relied upon.
const sections = [
  "About Go Invoicing",
  "Account Registration",
  "Use of the Service",
  "User Responsibilities",
  "Invoices and Financial Information",
  "Fees and Subscriptions",
  "Data and Content",
  "Service Availability",
  "Intellectual Property",
  "Limitation of Liability",
  "Account Suspension and Termination",
  "Changes to These Terms",
  "Governing Law",
  "Contact Us",
];

/** Public Terms & Conditions page — reachable without signing in. */
export function TermsPage() {
  return (
    <div className="min-h-screen bg-slate-50 px-4 py-10">
      <div className="mx-auto max-w-3xl">
        <Link to="/" className="mb-6 inline-block text-sm text-brand-600 hover:underline">
          ← Go Invoicing
        </Link>
        <PageHeader title="Terms & Conditions" description="Last updated: [PLACEHOLDER — date]" />
        <Card>
          <CardContent>
            <div className="space-y-6">
              {sections.map((heading, index) => (
                <section key={heading}>
                  <h2 className="text-base font-semibold text-slate-900">
                    {index + 1}. {heading}
                  </h2>
                  <p className="mt-1 text-sm text-slate-600">
                    [PLACEHOLDER] The {heading.toLowerCase()} terms for Go Invoicing will be set out here. This text is
                    not final and must be replaced before publication.
                  </p>
                </section>
              ))}
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
