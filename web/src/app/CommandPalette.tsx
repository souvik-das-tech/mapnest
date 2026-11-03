import { Command } from "cmdk";
import { useNavigate } from "react-router";
import { Boxes, Database, Layers, LayoutDashboard, Map as MapIcon, Plus, Settings2 } from "lucide-react";
import { useLayers, useStores } from "@/api/hooks";
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";

/** Ctrl+K: jump to any layer, store or page (PLAN.md §11.1). */
export function CommandPalette({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const navigate = useNavigate();
  const layers = useLayers();
  const stores = useStores();

  const go = (to: string) => {
    onOpenChange(false);
    navigate(to);
  };

  const item =
    "flex cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-sm aria-selected:bg-accent aria-selected:text-accent-foreground [&_svg]:size-4 [&_svg]:text-muted-foreground";
  const group = "px-1 py-1 [&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:py-1.5 [&_[cmdk-group-heading]]:text-xs [&_[cmdk-group-heading]]:text-muted-foreground";

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="top-[20%] max-w-xl translate-y-0 gap-0 overflow-hidden p-0">
        <DialogTitle className="sr-only">Search</DialogTitle>
        <Command loop>
          <Command.Input
            autoFocus
            placeholder="Type a layer, store or page…"
            className="h-12 w-full border-b bg-transparent px-4 text-sm outline-none placeholder:text-muted-foreground"
          />
          <Command.List className="max-h-80 overflow-y-auto p-1">
            <Command.Empty className="py-8 text-center text-sm text-muted-foreground">No results.</Command.Empty>
            <Command.Group heading="Pages" className={group}>
              <Command.Item className={item} onSelect={() => go("/")}>
                <LayoutDashboard /> Dashboard
              </Command.Item>
              <Command.Item className={item} onSelect={() => go("/layers/new")}>
                <Plus /> Publish a layer
              </Command.Item>
              <Command.Item className={item} onSelect={() => go("/stores?new=1")}>
                <Plus /> Add a store
              </Command.Item>
              <Command.Item className={item} onSelect={() => go("/workspaces")}>
                <Boxes /> Workspaces
              </Command.Item>
              <Command.Item className={item} onSelect={() => go("/map")}>
                <MapIcon /> Map viewer
              </Command.Item>
              <Command.Item className={item} onSelect={() => go("/system")}>
                <Settings2 /> System
              </Command.Item>
            </Command.Group>
            {!!layers.data?.length && (
              <Command.Group heading="Layers" className={group}>
                {layers.data.map((l) => (
                  <Command.Item
                    key={l.name}
                    value={`layer ${l.name} ${l.title}`}
                    className={item}
                    onSelect={() => go(`/layers/${encodeURIComponent(l.name)}`)}
                  >
                    <Layers /> {l.title || l.name}
                    <span className="ml-auto text-xs text-muted-foreground">{l.name}</span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}
            {!!stores.data?.length && (
              <Command.Group heading="Stores" className={group}>
                {stores.data.map((s) => (
                  <Command.Item
                    key={s.name}
                    value={`store ${s.name}`}
                    className={item}
                    onSelect={() => go(`/stores/${encodeURIComponent(s.name)}`)}
                  >
                    <Database /> {s.name}
                    <span className="ml-auto text-xs text-muted-foreground">{s.type}</span>
                  </Command.Item>
                ))}
              </Command.Group>
            )}
          </Command.List>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
