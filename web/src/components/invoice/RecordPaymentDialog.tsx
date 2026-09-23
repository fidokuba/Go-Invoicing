import { useState, type FormEvent } from "react";
import { useCreatePayment } from "@/api/queries/invoices";
import { friendlyMessage } from "@/api/errors";
import { formatMoney, minorToInputString, parseMoneyInput } from "@/lib/money";
import { todayDateOnly } from "@/lib/date";
import { Dialog } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input, Label, FieldError } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";

export function RecordPaymentDialog({
  open,
  onOpenChange,
  invoiceId,
  currency,
  amountOutstanding,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  invoiceId: string;
  currency: string;
  amountOutstanding: number;
}) {
  const [amount, setAmount] = useState(() => minorToInputString(amountOutstanding, currency));
  const [paymentMethod, setPaymentMethod] = useState("");
  const [paymentDate, setPaymentDate] = useState(todayDateOnly());
  const [reference, setReference] = useState("");
  const [amountError, setAmountError] = useState<string | null>(null);
  const createPayment = useCreatePayment(invoiceId);

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (createPayment.isPending) return;

    const amountMinor = parseMoneyInput(amount, currency);
    if (amountMinor === null || amountMinor <= 0) {
      setAmountError("Enter a valid amount greater than zero.");
      return;
    }
    // Client-side convenience check only — the backend independently
    // enforces this (409) as the authority on the invoice's current
    // outstanding balance (see api/openapi.yaml's Payments tag).
    if (amountMinor > amountOutstanding) {
      setAmountError(`Amount cannot exceed the outstanding balance of ${formatMoney(amountOutstanding, currency)}.`);
      return;
    }
    setAmountError(null);

    createPayment.mutate(
      {
        amount: amountMinor,
        paymentMethod: paymentMethod || undefined,
        paymentDate,
        reference: reference || undefined,
      },
      { onSuccess: () => onOpenChange(false) },
    );
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Record a payment"
      description={`Outstanding balance: ${formatMoney(amountOutstanding, currency)}`}
    >
      {createPayment.isError && (
        <div className="mb-4">
          <Alert>{friendlyMessage(createPayment.error)}</Alert>
        </div>
      )}
      <form onSubmit={handleSubmit} noValidate className="space-y-4">
        <div>
          <Label htmlFor="payment-amount">Amount ({currency})</Label>
          <Input
            id="payment-amount"
            inputMode="decimal"
            value={amount}
            aria-invalid={amountError !== null}
            onChange={(e) => setAmount(e.target.value)}
          />
          <FieldError>{amountError}</FieldError>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <Label htmlFor="payment-method">Method</Label>
            <Input
              id="payment-method"
              placeholder="bank_transfer, card, cash…"
              value={paymentMethod}
              onChange={(e) => setPaymentMethod(e.target.value)}
            />
          </div>
          <div>
            <Label htmlFor="payment-date">Date</Label>
            <Input id="payment-date" type="date" value={paymentDate} onChange={(e) => setPaymentDate(e.target.value)} />
          </div>
        </div>
        <div>
          <Label htmlFor="payment-reference">Reference (optional)</Label>
          <Input id="payment-reference" value={reference} onChange={(e) => setReference(e.target.value)} />
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <Button type="button" variant="secondary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button type="submit" disabled={createPayment.isPending}>
            {createPayment.isPending ? "Recording…" : "Record payment"}
          </Button>
        </div>
      </form>
    </Dialog>
  );
}
