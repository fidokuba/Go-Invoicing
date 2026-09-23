import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";
import { useCreateCustomer } from "@/api/queries/customers";
import { friendlyMessage } from "@/api/errors";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";

export function CustomerNewPage() {
  const [name, setName] = useState("");
  const [companyName, setCompanyName] = useState("");
  const [email, setEmail] = useState("");
  const [phone, setPhone] = useState("");
  const [taxId, setTaxId] = useState("");
  const navigate = useNavigate();
  const createCustomer = useCreateCustomer();

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (createCustomer.isPending) return;

    createCustomer.mutate(
      {
        name,
        companyName: companyName || undefined,
        email: email || undefined,
        phone: phone || undefined,
        taxId: taxId || undefined,
      },
      { onSuccess: (customer) => navigate(`/customers/${customer.id}`) },
    );
  }

  return (
    <div className="max-w-xl">
      <PageHeader title="New customer" />
      <Card>
        <CardContent>
          {createCustomer.isError && (
            <div className="mb-4">
              <Alert>{friendlyMessage(createCustomer.error)}</Alert>
            </div>
          )}
          <form onSubmit={handleSubmit} noValidate className="space-y-4">
            <div>
              <Label htmlFor="name">Name</Label>
              <Input id="name" required value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="companyName">Company name</Label>
              <Input id="companyName" value={companyName} onChange={(e) => setCompanyName(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="email">Email</Label>
              <Input id="email" type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="phone">Phone</Label>
              <Input id="phone" type="tel" value={phone} onChange={(e) => setPhone(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="taxId">Tax ID</Label>
              <Input id="taxId" value={taxId} onChange={(e) => setTaxId(e.target.value)} />
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button type="button" variant="secondary" onClick={() => navigate(-1)}>
                Cancel
              </Button>
              <Button type="submit" disabled={createCustomer.isPending}>
                {createCustomer.isPending ? "Creating…" : "Create customer"}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
