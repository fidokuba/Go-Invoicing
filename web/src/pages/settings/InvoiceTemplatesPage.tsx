import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { Plus } from "lucide-react";
import {
  useTemplates,
  useDeleteTemplate,
  useSetDefaultTemplate,
} from "@/api/queries/templates";
import { friendlyMessage } from "@/api/errors";
import { formatTimestampDate } from "@/lib/date";
import { Button } from "@/components/ui/button";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { QueryBoundary } from "@/components/ui/query-boundary";
import { TableContainer, THead, TBody, Tr, Th, Td } from "@/components/ui/table";
import type { components } from "@/api/schema";

type Template = components["schemas"]["TemplateResponse"];

function DeleteTemplateDialog({
  template,
  open,
  onOpenChange,
}: {
  template: Template | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const deleteTemplate = useDeleteTemplate();

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title="Are you sure you want to delete the layout? - this is irreversible."
    >
      {deleteTemplate.isError && (
        <div className="mb-4">
          <Alert>{friendlyMessage(deleteTemplate.error)}</Alert>
        </div>
      )}
      <div className="flex justify-end gap-2">
        <Button variant="secondary" onClick={() => onOpenChange(false)}>
          Cancel
        </Button>
        <Button
          variant="danger"
          disabled={deleteTemplate.isPending || !template}
          onClick={() => {
            if (!template) return;
            deleteTemplate.mutate(template.id, { onSuccess: () => onOpenChange(false) });
          }}
        >
          {deleteTemplate.isPending ? "Deleting…" : "Delete"}
        </Button>
      </div>
    </Dialog>
  );
}

function TemplatesTable() {
  const query = useTemplates();
  const navigate = useNavigate();
  const setDefaultTemplate = useSetDefaultTemplate();
  const [templateToDelete, setTemplateToDelete] = useState<Template | null>(null);

  return (
    <QueryBoundary query={query}>
      {(templates) => (
        <>
          {setDefaultTemplate.isError && (
            <div className="mb-4">
              <Alert>{friendlyMessage(setDefaultTemplate.error)}</Alert>
            </div>
          )}
          <TableContainer>
            <THead>
              <Tr>
                <Th>Name</Th>
                <Th>Status</Th>
                <Th>Last updated</Th>
                <Th />
              </Tr>
            </THead>
            <TBody>
              {templates.map((t) => (
                <Tr key={t.id}>
                  <Td className="font-medium text-slate-900">{t.name}</Td>
                  <Td>
                    <div className="flex gap-1.5">
                      {t.isDefault && <Badge tone="green">In use</Badge>}
                      {t.isSystem && <Badge tone="slate">System default</Badge>}
                    </div>
                  </Td>
                  <Td>{formatTimestampDate(t.updatedAt)}</Td>
                  <Td>
                    <div className="flex justify-end gap-2">
                      {!t.isDefault && (
                        <Button
                          size="sm"
                          variant="secondary"
                          disabled={setDefaultTemplate.isPending}
                          onClick={() => setDefaultTemplate.mutate(t.id)}
                        >
                          Use This Layout
                        </Button>
                      )}
                      <Button
                        size="sm"
                        variant="secondary"
                        onClick={() => navigate(`/settings/invoice-templates/${t.id}`)}
                      >
                        {t.isSystem ? "View" : "Edit"}
                      </Button>
                      {!t.isSystem && (
                        <Button size="sm" variant="danger" onClick={() => setTemplateToDelete(t)}>
                          Delete
                        </Button>
                      )}
                    </div>
                  </Td>
                </Tr>
              ))}
            </TBody>
          </TableContainer>
          <DeleteTemplateDialog
            template={templateToDelete}
            open={templateToDelete !== null}
            onOpenChange={(open) => !open && setTemplateToDelete(null)}
          />
        </>
      )}
    </QueryBoundary>
  );
}

export function InvoiceTemplatesPage() {
  const navigate = useNavigate();

  return (
    <div>
      <div className="mb-4 flex justify-end">
        <Button size="sm" onClick={() => navigate("/settings/invoice-templates/new")}>
          <Plus className="h-4 w-4" aria-hidden="true" />
          New template
        </Button>
      </div>
      <TemplatesTable />
    </div>
  );
}
