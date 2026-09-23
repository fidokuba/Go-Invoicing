import { useEffect, useRef, useState } from "react";
import { Download, Eye, Loader2 } from "lucide-react";
import { fetchInvoicePdf } from "@/api/queries/invoices";
import { friendlyMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";

/**
 * View/Download actions for an invoice's server-generated PDF
 * (Milestone 12 section 16). The bearer token can't be attached to a
 * plain `<a href="...">` or `window.open(url)` (there is no way to add
 * an Authorization header to a browser-initiated navigation), so this
 * fetches the PDF as a Blob through the same authenticated API client
 * every other request uses, then hands the browser a local
 * `blob:` object URL to view or download instead.
 *
 * Object URL lifecycle: each action creates a fresh object URL, revokes
 * whatever this component created previously, and revokes on unmount —
 * so a URL is never leaked, but a "View" tab that's still open keeps
 * working (the previous URL is only revoked once a *new* one is
 * requested or the component itself unmounts, not immediately after
 * opening).
 */
export function InvoicePdfActions({ invoiceId, invoiceNumber }: { invoiceId: string; invoiceNumber: string }) {
  const [loading, setLoading] = useState<"view" | "download" | null>(null);
  const [error, setError] = useState<string | null>(null);
  const lastObjectUrl = useRef<string | null>(null);

  useEffect(() => {
    return () => {
      if (lastObjectUrl.current) URL.revokeObjectURL(lastObjectUrl.current);
    };
  }, []);

  async function withPdf(mode: "view" | "download") {
    setLoading(mode);
    setError(null);
    try {
      const blob = await fetchInvoicePdf(invoiceId);
      if (lastObjectUrl.current) URL.revokeObjectURL(lastObjectUrl.current);
      const url = URL.createObjectURL(blob);
      lastObjectUrl.current = url;

      if (mode === "view") {
        window.open(url, "_blank", "noopener,noreferrer");
      } else {
        const link = document.createElement("a");
        link.href = url;
        link.download = `${invoiceNumber}.pdf`;
        document.body.appendChild(link);
        link.click();
        link.remove();
      }
    } catch (err) {
      setError(friendlyMessage(err));
    } finally {
      setLoading(null);
    }
  }

  return (
    <div>
      <div className="flex gap-2">
        <Button variant="secondary" size="sm" disabled={loading !== null} onClick={() => withPdf("view")}>
          {loading === "view" ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" /> : <Eye className="h-4 w-4" aria-hidden="true" />}
          View PDF
        </Button>
        <Button variant="secondary" size="sm" disabled={loading !== null} onClick={() => withPdf("download")}>
          {loading === "download" ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" /> : <Download className="h-4 w-4" aria-hidden="true" />}
          Download
        </Button>
      </div>
      {error && (
        <p role="alert" className="mt-2 text-sm text-red-600">
          {error}
        </p>
      )}
    </div>
  );
}
