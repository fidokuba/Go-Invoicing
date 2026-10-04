import { useState } from "react";
import { useParams } from "react-router-dom";
import { Ban, Send } from "lucide-react";
import { useCancelInvoice, useInvoice, useInvoicePayments, useSendInvoice } from "@/api/queries/invoices";
import { friendlyMessage } from "@/api/errors";
import { canCancelInvoice, canRecordPayment, canSendInvoice } from "@/lib/invoiceLifecycle";
import { hasRole, useAuth } from "@/lib/useAuth";
import { formatMoney } from "@/lib/money";
import { formatDateOnly, formatTimestamp } from "@/lib/date";
import { PageHeader } from "@/components/layout/PageHeader";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Dialog } from "@/components/ui/dialog";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import { InvoiceStatusBadge, type InvoiceStatus } from "@/components/invoice/InvoiceStatusBadge";
import { InvoicePdfActions } from "@/components/invoice/InvoicePdfActions";
import { RecordPaymentDialog } from "@/components/invoice/RecordPaymentDialog";

function SendInvoiceControl({ invoiceId }: { invoiceId: string }) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const sendInvoice = useSendInvoice(invoiceId);

  return (
    <>
      <Button size="sm" onClick={() => setConfirmOpen(true)}>
        <Send className="h-4 w-4" aria-hidden="true" />
        Send invoice
      </Button>
      <Dialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Send this invoice?"
        description="This finalises the invoice: it snapshots your organisation and customer details and locks the invoice's financial content. This does not send an email — Go Invoicing does not deliver invoices for you."
      >
        {sendInvoice.isError && (
          <div className="mb-4">
            <Alert>{friendlyMessage(sendInvoice.error)}</Alert>
          </div>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setConfirmOpen(false)}>
            Cancel
          </Button>
          <Button
            disabled={sendInvoice.isPending}
            onClick={() => sendInvoice.mutate(undefined, { onSuccess: () => setConfirmOpen(false) })}
          >
            {sendInvoice.isPending ? "Sending…" : "Send invoice"}
          </Button>
        </div>
      </Dialog>
    </>
  );
}

function CancelInvoiceControl({ invoiceId, isDraft }: { invoiceId: string; isDraft: boolean }) {
  const [confirmOpen, setConfirmOpen] = useState(false);
  const cancelInvoice = useCancelInvoice(invoiceId);

  return (
    <>
      <Button size="sm" variant="secondary" onClick={() => setConfirmOpen(true)}>
        <Ban className="h-4 w-4" aria-hidden="true" />
        Cancel invoice
      </Button>
      <Dialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title="Cancel this invoice?"
        description={
          isDraft
            ? "The invoice will be marked Cancelled and can no longer be sent. It stays in your records — invoices are never deleted. This can't be undone."
            : "The invoice will be marked Cancelled and can no longer be paid. It stays in your records — invoices are never deleted. If you've already given it to your customer, let them know it's been cancelled. This can't be undone."
        }
      >
        {cancelInvoice.isError && (
          <div className="mb-4">
            <Alert>{friendlyMessage(cancelInvoice.error)}</Alert>
          </div>
        )}
        <div className="flex justify-end gap-2">
          <Button variant="secondary" onClick={() => setConfirmOpen(false)}>
            Keep invoice
          </Button>
          <Button
            variant="danger"
            disabled={cancelInvoice.isPending}
            onClick={() => cancelInvoice.mutate(undefined, { onSuccess: () => setConfirmOpen(false) })}
          >
            {cancelInvoice.isPending ? "Cancelling…" : "Cancel invoice"}
          </Button>
        </div>
      </Dialog>
    </>
  );
}

function PaymentHistoryCard({ invoiceId, currency }: { invoiceId: string; currency: string }) {
  const query = useInvoicePayments(invoiceId);
  return (
    <Card>
      <CardHeader>
        <CardTitle>Payment history</CardTitle>
      </CardHeader>
      <QueryBoundary query={query}>
        {(payments) =>
          payments.length === 0 ? (
            <div className="px-5 py-4 text-sm text-slate-500">No payments recorded yet.</div>
          ) : (
            <TableContainer>
              <THead>
                <Tr>
                  <Th>Date</Th>
                  <Th>Method</Th>
                  <Th>Reference</Th>
                  <Th className="text-right">Amount</Th>
                </Tr>
              </THead>
              <TBody>
                {payments.map((payment) => (
                  <Tr key={payment.id}>
                    <Td>{formatDateOnly(payment.paymentDate)}</Td>
                    <Td>{payment.paymentMethod || "—"}</Td>
                    <Td>{payment.reference || "—"}</Td>
                    <Td className="text-right">{formatMoney(payment.amount, currency)}</Td>
                  </Tr>
                ))}
              </TBody>
            </TableContainer>
          )
        }
      </QueryBoundary>
    </Card>
  );
}

export function InvoiceDetailPage() {
  const { id } = useParams<{ id: string }>();
  const invoiceQuery = useInvoice(id);
  const [paymentDialogOpen, setPaymentDialogOpen] = useState(false);

  return (
    <QueryBoundary query={invoiceQuery}>
      {(invoice) => (
        <InvoiceDetailContent
          invoice={invoice}
          paymentDialogOpen={paymentDialogOpen}
          setPaymentDialogOpen={setPaymentDialogOpen}
        />
      )}
    </QueryBoundary>
  );
}

function InvoiceDetailContent({
  invoice,
  paymentDialogOpen,
  setPaymentDialogOpen,
}: {
  invoice: NonNullable<ReturnType<typeof useInvoice>["data"]>;
  paymentDialogOpen: boolean;
  setPaymentDialogOpen: (open: boolean) => void;
}) {
  const status = invoice.status as InvoiceStatus;
  const { user } = useAuth();
  const showCancel = canCancelInvoice(status, invoice.amountPaid) && hasRole(user, "admin", "manager");

  return (
    <div>
      <PageHeader
        title={invoice.invoiceNumber}
        description={invoice.customerName || undefined}
        actions={
          <div className="flex items-center gap-2">
            <InvoiceStatusBadge status={status} />
            {canSendInvoice(status) && <SendInvoiceControl invoiceId={invoice.id} />}
            {canRecordPayment(status) && (
              <Button size="sm" variant="secondary" onClick={() => setPaymentDialogOpen(true)}>
                Record payment
              </Button>
            )}
            {showCancel && <CancelInvoiceControl invoiceId={invoice.id} isDraft={status === "draft"} />}
          </div>
        }
      />

      {status === "cancelled" && (
        <div className="mb-6">
          <Alert tone="info" title="This invoice has been cancelled.">
            It is kept for your records but can no longer be sent or paid, and nothing is outstanding on it.
          </Alert>
        </div>
      )}

      {status === "paid" && (
        <div className="mb-6">
          <Alert tone="info" title="This invoice is fully paid.">
            No further payment can be recorded against it.
          </Alert>
        </div>
      )}

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="space-y-6 lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Lines</CardTitle>
            </CardHeader>
            <TableContainer>
              <THead>
                <Tr>
                  <Th>Description</Th>
                  <Th className="text-right">Qty</Th>
                  <Th className="text-right">Unit price</Th>
                  {invoice.vatRegistered && <Th className="text-right">VAT</Th>}
                  <Th className="text-right">Total</Th>
                </Tr>
              </THead>
              <TBody>
                {invoice.lines.map((line) => (
                  <Tr key={line.id}>
                    <Td>{line.description}</Td>
                    <Td className="text-right">{line.quantity}</Td>
                    <Td className="text-right">{formatMoney(line.unitPrice, invoice.currency)}</Td>
                    {invoice.vatRegistered && (
                      <Td className="text-right">
                        {line.vatRate}% ({formatMoney(line.vatAmount, invoice.currency)})
                      </Td>
                    )}
                    <Td className="text-right">{formatMoney(line.total, invoice.currency)}</Td>
                  </Tr>
                ))}
              </TBody>
            </TableContainer>
            <div className="flex justify-end border-t border-slate-200 px-5 py-4">
              <dl className="w-56 space-y-1.5 text-sm">
                {invoice.vatRegistered && (
                  <>
                    <div className="flex justify-between">
                      <dt className="text-slate-500">Subtotal</dt>
                      <dd>{formatMoney(invoice.subtotal, invoice.currency)}</dd>
                    </div>
                    <div className="flex justify-between">
                      <dt className="text-slate-500">VAT</dt>
                      <dd>{formatMoney(invoice.vatTotal, invoice.currency)}</dd>
                    </div>
                  </>
                )}
                <div className="flex justify-between text-base font-semibold">
                  <dt>Total</dt>
                  <dd>{formatMoney(invoice.total, invoice.currency)}</dd>
                </div>
                <div className="flex justify-between border-t border-slate-100 pt-1.5">
                  <dt className="text-slate-500">Paid</dt>
                  <dd>{formatMoney(invoice.amountPaid, invoice.currency)}</dd>
                </div>
                <div className="flex justify-between font-medium">
                  <dt>Outstanding</dt>
                  <dd>{formatMoney(invoice.amountOutstanding, invoice.currency)}</dd>
                </div>
              </dl>
            </div>
          </Card>

          {invoice.notes && (
            <Card>
              <CardHeader>
                <CardTitle>Notes</CardTitle>
              </CardHeader>
              <CardContent>
                <p className="whitespace-pre-wrap text-sm text-slate-700">{invoice.notes}</p>
              </CardContent>
            </Card>
          )}

          <PaymentHistoryCard invoiceId={invoice.id} currency={invoice.currency} />
        </div>

        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Details</CardTitle>
            </CardHeader>
            <CardContent>
              <dl className="space-y-2 text-sm">
                <div className="flex justify-between gap-4">
                  <dt className="text-slate-500">Issue date</dt>
                  <dd>{formatDateOnly(invoice.issueDate)}</dd>
                </div>
                <div className="flex justify-between gap-4">
                  <dt className="text-slate-500">Due date</dt>
                  <dd>{formatDateOnly(invoice.dueDate)}</dd>
                </div>
                {invoice.sentAt && (
                  <div className="flex justify-between gap-4">
                    <dt className="text-slate-500">Sent</dt>
                    <dd>{formatTimestamp(invoice.sentAt)}</dd>
                  </div>
                )}
                {invoice.cancelledAt && (
                  <div className="flex justify-between gap-4">
                    <dt className="text-slate-500">Cancelled</dt>
                    <dd>{formatTimestamp(invoice.cancelledAt)}</dd>
                  </div>
                )}
              </dl>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>PDF</CardTitle>
            </CardHeader>
            <CardContent>
              <InvoicePdfActions invoiceId={invoice.id} invoiceNumber={invoice.invoiceNumber} />
            </CardContent>
          </Card>
        </div>
      </div>

      {canRecordPayment(status) && (
        <RecordPaymentDialog
          open={paymentDialogOpen}
          onOpenChange={setPaymentDialogOpen}
          invoiceId={invoice.id}
          currency={invoice.currency}
          amountOutstanding={invoice.amountOutstanding}
        />
      )}
    </div>
  );
}
