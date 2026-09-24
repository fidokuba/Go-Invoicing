import { useRef, useState, type FormEvent } from "react";
import { useCreatePayment } from "@/api/queries/invoices";
import { friendlyMessage } from "@/api/errors";
import { isIdempotencyKeyReused, isUncertainFailure, newIdempotencyKey } from "@/lib/idempotencyKey";
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
  // The Idempotency-Key for the logical payment attempt currently in
  // progress (Milestone 13 Part 1), or null when there is none. Created on
  // the first submit; kept across resubmits while the outcome is
  // uncertain, so a retry can never record the payment twice; cleared once
  // the outcome is known, so the next submission is a new payment. Lives
  // as long as this component (which, like the form fields, survives the
  // dialog being closed and reopened) — deliberately not persisted
  // anywhere, so a page reload starts afresh.
  const idempotencyKey = useRef<string | null>(null);

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

    idempotencyKey.current ??= newIdempotencyKey();

    createPayment.mutate(
      {
        body: {
          amount: amountMinor,
          paymentMethod: paymentMethod || undefined,
          paymentDate,
          reference: reference || undefined,
        },
        idempotencyKey: idempotencyKey.current,
      },
      {
        onSuccess: () => {
          // Recorded (or replayed): this attempt is over.
          idempotencyKey.current = null;
          onOpenChange(false);
        },
        onError: (err) => {
          // A network failure or 5xx may or may not have recorded the
          // payment: keep the key so resubmitting is a safe retry. Any
          // other outcome is definitive — a plain 4xx recorded nothing,
          // and a reused key means this attempt can't proceed — so the
          // next submission is a new attempt with a new key.
          if (isIdempotencyKeyReused(err) || !isUncertainFailure(err)) {
            idempotencyKey.current = null;
          }
        },
      },
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
