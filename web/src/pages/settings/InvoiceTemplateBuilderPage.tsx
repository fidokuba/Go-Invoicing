import { useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ArrowLeft } from "lucide-react";
import { Puck, type Data } from "@puckeditor/core";
import "@puckeditor/core/puck.css";
import { templateBuilderConfig, emptyTemplateDefinition } from "@/components/templates/templateBuilderConfig";
import {
  useVersionedTemplate,
  useCreateTemplate,
  useUpdateTemplate,
  useSetDefaultTemplate,
  useDeleteTemplate,
} from "@/api/queries/templates";
import { friendlyMessage } from "@/api/errors";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Dialog } from "@/components/ui/dialog";
import { QueryBoundary } from "@/components/ui/query-boundary";

// The template builder is a full-height page, not a Settings tab's
// content — see App.tsx's own comment on why this route is a sibling of
// SettingsLayout rather than nested under it. Puck supplies its own
// "Publish" action (top right, inside its own chrome) — this page
// deliberately doesn't fight that with a duplicate custom "Save" button;
// the helper text next to the name field says so, so "Publish" reads as
// "Save" rather than something more permanent-sounding.

function BuilderHeader({
  onBack,
  children,
}: {
  onBack: () => void;
  children: React.ReactNode;
}) {
  return (
    <header className="flex flex-wrap items-center gap-3 border-b border-slate-200 bg-white px-4 py-3">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft className="h-4 w-4" aria-hidden="true" />
        Back
      </Button>
      {children}
    </header>
  );
}

function NewTemplateBuilder() {
  const navigate = useNavigate();
  const [name, setName] = useState("New Template");
  const createTemplate = useCreateTemplate();
  const back = () => navigate("/settings/invoice-templates");

  return (
    <div className="flex h-[calc(100vh-1px)] flex-col">
      <BuilderHeader onBack={back}>
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          aria-label="Template name"
          className="max-w-xs"
        />
        <span className="text-xs text-slate-500">Click Publish (in the editor) to save</span>
        {createTemplate.isError && (
          <Alert>{friendlyMessage(createTemplate.error)}</Alert>
        )}
      </BuilderHeader>
      <div className="min-h-0 flex-1">
        <Puck
          config={templateBuilderConfig}
          data={emptyTemplateDefinition}
          onPublish={(data) => {
            createTemplate.mutate(
              { name: name.trim() || "Untitled Template", definition: data as unknown as Record<string, never> },
              { onSuccess: (created) => navigate(`/settings/invoice-templates/${created.id}`, { replace: true }) },
            );
          }}
        />
      </div>
    </div>
  );
}

function ExistingTemplateBuilder({ id }: { id: string }) {
  const navigate = useNavigate();
  const query = useVersionedTemplate(id);
  const updateTemplate = useUpdateTemplate(id);
  const setDefaultTemplate = useSetDefaultTemplate();
  const deleteTemplate = useDeleteTemplate();
  const [name, setName] = useState<string | null>(null);
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false);
  const back = () => navigate("/settings/invoice-templates");

  return (
    <QueryBoundary query={query}>
      {(versioned) => {
        const template = versioned.data;
        const currentName = name ?? template.name;
        // A regression fix: each of these mutations starts in React
        // Query's idle state with .error === null, never undefined — so
        // `a ?? b ?? c` here always evaluated to null, and the stale
        // `error !== undefined` check below was true for that null on
        // every single render, not just after a real failure. The
        // result was the generic error banner appearing unconditionally
        // on every visit to any existing template's edit page, success
        // or not (see InvoiceTemplatesPage.tsx's sibling list page,
        // which already used the correct `.isError` boolean throughout
        // — this file was the one place that didn't).
        const error = updateTemplate.isError
          ? updateTemplate.error
          : setDefaultTemplate.isError
            ? setDefaultTemplate.error
            : deleteTemplate.isError
              ? deleteTemplate.error
              : undefined;

        return (
          <div className="flex h-[calc(100vh-1px)] flex-col">
            <BuilderHeader onBack={back}>
              <Input
                value={currentName}
                onChange={(e) => setName(e.target.value)}
                disabled={template.isSystem}
                aria-label="Template name"
                className="max-w-xs"
              />
              {template.isSystem ? (
                <span className="text-xs text-slate-500">
                  The system default layout can't be renamed, edited, or deleted.
                </span>
              ) : (
                <span className="text-xs text-slate-500">Click Publish (in the editor) to save</span>
              )}
              {template.isDefault ? (
                <Badge tone="green">Currently in use</Badge>
              ) : (
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={setDefaultTemplate.isPending}
                  onClick={() => setDefaultTemplate.mutate(id)}
                >
                  {setDefaultTemplate.isPending ? "Setting…" : "Use This Layout"}
                </Button>
              )}
              {!template.isSystem && (
                <Button size="sm" variant="danger" onClick={() => setDeleteConfirmOpen(true)}>
                  Delete
                </Button>
              )}
              {error !== undefined && <Alert>{friendlyMessage(error)}</Alert>}
            </BuilderHeader>
            <div className="min-h-0 flex-1">
              {template.isSystem ? (
                // Classic's stored "definition" is a placeholder marker
                // ({"system":"classic"}), not a real Puck document — it
                // is drawn by its own hand-written HTML template
                // (renderer/src/classicTemplate.js), never interpreted
                // by Puck at all, so there is nothing real to show here.
                // Showing the editor anyway produced an empty, dead-end
                // canvas that looked like a crash, especially once paired
                // with the (now-fixed) false error banner above — this
                // explains the gap and points at the one actual way to
                // customize an invoice's layout instead.
                <div className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center">
                  <p className="max-w-md text-sm text-slate-600">
                    Classic is the system's built-in layout and isn't a design you can open and edit
                    here. To customize your invoice's appearance, create a new layout — Classic stays
                    available as a fallback for as long as nothing else is set as default.
                  </p>
                  <Button onClick={() => navigate("/settings/invoice-templates/new")}>
                    Create a new layout
                  </Button>
                </div>
              ) : (
                <Puck
                  key={`${template.id}-${template.updatedAt}`}
                  config={templateBuilderConfig}
                  data={template.definition as unknown as Data}
                  onPublish={(data) => {
                    updateTemplate.mutate({
                      body: { name: currentName.trim() || template.name, definition: data as unknown as Record<string, never> },
                      etag: versioned.etag,
                    });
                  }}
                />
              )}
            </div>
            <Dialog
              open={deleteConfirmOpen}
              onOpenChange={setDeleteConfirmOpen}
              title="Are you sure you want to delete the layout? - this is irreversible."
            >
              {deleteTemplate.isError && (
                <div className="mb-4">
                  <Alert>{friendlyMessage(deleteTemplate.error)}</Alert>
                </div>
              )}
              <div className="flex justify-end gap-2">
                <Button variant="secondary" onClick={() => setDeleteConfirmOpen(false)}>
                  Cancel
                </Button>
                <Button
                  variant="danger"
                  disabled={deleteTemplate.isPending}
                  onClick={() => deleteTemplate.mutate(id, { onSuccess: back })}
                >
                  {deleteTemplate.isPending ? "Deleting…" : "Delete"}
                </Button>
              </div>
            </Dialog>
          </div>
        );
      }}
    </QueryBoundary>
  );
}

export function InvoiceTemplateBuilderPage() {
  const { id } = useParams();

  if (!id || id === "new") {
    return <NewTemplateBuilder />;
  }
  return <ExistingTemplateBuilder id={id} />;
}
