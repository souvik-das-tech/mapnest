import { useSearchParams } from "react-router";
import { useWorkspaces } from "@/api/hooks";
import { NativeSelect } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/**
 * Per-page workspace filter kept in the URL (?workspace=), so links, reloads
 * and back/forward keep it. "" = all workspaces.
 */
export function useWorkspaceParam(): [string, (name: string) => void] {
  const [params, setParams] = useSearchParams();
  const value = params.get("workspace") ?? "";
  const set = (name: string) =>
    setParams(
      (p) => {
        if (name) p.set("workspace", name);
        else p.delete("workspace");
        p.delete("page"); // the filtered list starts on page 1
        return p;
      },
      { replace: true },
    );
  return [value, set];
}

/** Items in the workspace, or all of them for "". */
export function inWorkspace<T extends { workspace: string }>(items: T[] | undefined, workspace: string): T[] {
  return (items ?? []).filter((x) => !workspace || x.workspace === workspace);
}

export const workspaceLabel = (w: { name: string; title: string }) =>
  w.title && w.title !== w.name ? `${w.title} (${w.name})` : w.name;

/** Workspace dropdown for page toolbars. Hidden while only "default" exists. */
export function WorkspaceFilter({
  value,
  onChange,
  className,
}: {
  value: string;
  onChange: (name: string) => void;
  className?: string;
}) {
  const workspaces = useWorkspaces();
  if ((workspaces.data?.length ?? 0) < 2 && !value) return null;
  return (
    <NativeSelect
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={cn("w-auto min-w-40", className)}
      aria-label="Filter by workspace"
    >
      <option value="">All workspaces</option>
      {workspaces.data?.map((w) => (
        <option key={w.name} value={w.name}>
          {workspaceLabel(w)}
        </option>
      ))}
    </NativeSelect>
  );
}
