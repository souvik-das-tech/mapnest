import { useState } from "react";
import { useNavigate } from "react-router";
import { ArrowRight, Boxes, Pencil, Plus } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useDeleteWorkspace, useSaveWorkspace, useStores, useWorkspaces } from "@/api/hooks";
import type { Workspace } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { BulkBar, Checkbox, useSelection, type Selection } from "@/components/ui/bulk-delete";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { CopyButton } from "@/components/ui/copy-button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { EmptyState, ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Input, Textarea } from "@/components/ui/input";
import { Field } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/misc";
import { Pagination, usePagination } from "@/components/ui/pagination";
import { cn } from "@/lib/utils";

const PAGE_SIZES = [25, 50, 100];
const NAME_RE = /^[a-z][a-z0-9_]{0,39}$/;

/**
 * Workspaces group stores and their layers, like GeoServer's. Each has its
 * own WMS/WFS endpoint (/ows/<name>); /ows keeps serving every layer.
 */
export function WorkspacesPage() {
  const workspaces = useWorkspaces();
  const stores = useStores();
  const del = useDeleteWorkspace();
  const [editing, setEditing] = useState<Workspace | "new" | null>(null);
  const list = workspaces.data ?? [];
  // The default workspace can never be deleted, so it is not selectable.
  const selection = useSelection(list.filter((w) => !w.default).map((w) => w.name));
  const paging = usePagination(list, { sizes: PAGE_SIZES });
  const storesIn = (name: string) => (stores.data ?? []).filter((s) => s.workspace === name).map((s) => `store ${s.name}`);

  return (
    <>
      <PageHeader
        title="Workspaces"
        description="Group stores and their layers by project or client. Each workspace has its own WMS/WFS address."
        actions={
          <Button onClick={() => setEditing("new")}>
            <Plus /> New workspace
          </Button>
        }
      />

      {workspaces.isLoading && <Skeleton className="h-48" />}
      {workspaces.isError && <ErrorBox error={workspaces.error} />}
      {list.length === 1 && (
        <EmptyState
          icon={<Boxes />}
          title="Only the default workspace so far"
          hint="Create a workspace per project or client, then add stores to it. Its layers get their own WMS/WFS address."
          action={
            <Button onClick={() => setEditing("new")}>
              <Plus /> New workspace
            </Button>
          }
        />
      )}

      {list.length > 0 && (
        <Card className="mt-4 overflow-hidden">
          <div className="border-b p-3">
            <BulkBar
              selection={selection}
              total={list.length - 1}
              noun="workspace"
              dependents={storesIn}
              onDelete={(name) => del.mutateAsync(name)}
              showSelectAll={false}
            />
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="bg-muted/50 text-left text-xs uppercase tracking-wide text-muted-foreground">
                <tr>
                  <th className="w-10 py-2 pl-4">
                    <Checkbox
                      checked={selection.all}
                      indeterminate={selection.some}
                      onCheckedChange={selection.setAll}
                      disabled={list.length < 2}
                      aria-label="Select all workspaces"
                    />
                  </th>
                  <th className="px-4 py-2 font-medium">Workspace</th>
                  <th className="px-4 py-2 text-right font-medium">Stores</th>
                  <th className="px-4 py-2 text-right font-medium">Layers</th>
                  <th className="px-4 py-2 font-medium">WMS / WFS address</th>
                  <th className="w-28 py-2 pr-4">
                    <span className="sr-only">Actions</span>
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y">
                {paging.items.map((w) => (
                  <WorkspaceRow key={w.name} ws={w} selection={selection} onEdit={() => setEditing(w)} />
                ))}
              </tbody>
            </table>
          </div>
          <Pagination paging={paging} noun="workspace" className="border-t px-4 py-2.5" />
        </Card>
      )}

      <p className="mt-4 text-xs text-muted-foreground">
        Store and layer names stay unique across all workspaces. The shared address <code className="font-mono">/ows</code>{" "}
        keeps serving every layer, so existing apps keep working. Styles and icons are shared by all workspaces.
      </p>

      <WorkspaceDialog value={editing} onClose={() => setEditing(null)} />
    </>
  );
}

function WorkspaceRow({ ws, selection, onEdit }: { ws: Workspace; selection: Selection; onEdit: () => void }) {
  const navigate = useNavigate();
  const checked = selection.has(ws.name);
  return (
    <tr className={cn(checked ? "bg-primary/5" : "hover:bg-muted/40")}>
      <td className="py-3 pl-4">
        {!ws.default && <Checkbox checked={checked} onCheckedChange={(on) => selection.toggle(ws.name, on)} aria-label={`Select ${ws.name}`} />}
      </td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <span className="font-medium">{ws.title || ws.name}</span>
          {ws.default && <Badge variant="secondary">default</Badge>}
        </div>
        <div className="font-mono text-xs text-muted-foreground">{ws.name}</div>
        {ws.description && <div className="mt-0.5 max-w-md truncate text-xs text-muted-foreground" title={ws.description}>{ws.description}</div>}
      </td>
      <td className="px-4 py-3 text-right tabular-nums">{ws.stores}</td>
      <td className="px-4 py-3 text-right tabular-nums">{ws.layers}</td>
      <td className="px-4 py-3">
        <div className="flex items-center gap-2">
          <code className="max-w-72 truncate rounded bg-muted px-1.5 py-0.5 font-mono text-xs" title={ws.ows_url}>
            {ws.ows_url}
          </code>
          <CopyButton text={ws.ows_url} />
        </div>
      </td>
      <td className="whitespace-nowrap py-3 pr-4 text-right">
        <Button variant="ghost" size="icon" onClick={onEdit} aria-label={`Edit ${ws.name}`}>
          <Pencil />
        </Button>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => navigate(`/layers?workspace=${encodeURIComponent(ws.name)}`)}
        >
          Open <ArrowRight />
        </Button>
      </td>
    </tr>
  );
}

function WorkspaceDialog({ value, onClose }: { value: Workspace | "new" | null; onClose: () => void }) {
  const isNew = value === "new";
  const existing = value && value !== "new" ? value : null;
  return (
    <Dialog open={!!value} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        {value && <WorkspaceForm key={existing?.name ?? "new"} isNew={isNew} existing={existing} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function WorkspaceForm({ isNew, existing, onClose }: { isNew: boolean; existing: Workspace | null; onClose: () => void }) {
  const save = useSaveWorkspace();
  const [name, setName] = useState(existing?.name ?? "");
  const [title, setTitle] = useState(existing?.title ?? "");
  const [description, setDescription] = useState(existing?.description ?? "");
  const nameOK = NAME_RE.test(name);

  const submit = async () => {
    try {
      const w = await save.mutateAsync({ name, title, description, isNew });
      toast.success(isNew ? `Workspace “${w.name}” created` : "Workspace saved");
      onClose();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        if (nameOK) submit();
      }}
      className="grid gap-4"
    >
      <DialogHeader>
        <DialogTitle>{isNew ? "New workspace" : `Edit ${existing!.name}`}</DialogTitle>
        <DialogDescription>
          {isNew
            ? "A group for one project or client. Its layers get their own WMS/WFS address."
            : "The name is part of the WMS/WFS address, so it cannot change."}
        </DialogDescription>
      </DialogHeader>
      {isNew && (
        <Field
          label="Name"
          htmlFor="ws-name"
          error={name && !nameOK ? "Lowercase letters, digits and _, starting with a letter (max 40)" : undefined}
          hint={nameOK ? `Address: …/ows/${name}` : "e.g. city_survey"}
        >
          <Input id="ws-name" value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="city_survey" autoFocus />
        </Field>
      )}
      <Field label="Title" htmlFor="ws-title" hint="Shown in the UI and as the service title in GetCapabilities">
        <Input id="ws-title" value={title} onChange={(e) => setTitle(e.target.value)} placeholder="City survey 2026" autoFocus={!isNew} />
      </Field>
      <Field label="Description" htmlFor="ws-desc">
        <Textarea id="ws-desc" value={description} onChange={(e) => setDescription(e.target.value)} rows={3} />
      </Field>
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button type="submit" disabled={!nameOK || save.isPending}>
          {isNew ? "Create" : "Save"}
        </Button>
      </DialogFooter>
    </form>
  );
}
