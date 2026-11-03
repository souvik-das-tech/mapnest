import { useState } from "react";
import { Link } from "react-router";
import { Search } from "lucide-react";
import { useIcons } from "@/api/hooks";
import { AuthImage } from "@/components/ui/auth-image";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { CopyIconButton, sldSnippet } from "@/features/icons/IconsPage";

/**
 * Uploaded icons with copy buttons, opened from the SLD editor. A dialog,
 * not a new tab: the login lives in sessionStorage, which is per tab.
 */
export function IconPathsDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const icons = useIcons();
  const [q, setQ] = useState("");
  const list = (icons.data ?? []).filter((ic) => ic.name.toLowerCase().includes(q.toLowerCase()));
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl">
        <DialogHeader>
          <DialogTitle>Icon paths</DialogTitle>
          <DialogDescription>
            <b>Path</b> copies the file name for <code className="font-mono">xlink:href</code>. <b>SLD</b> copies a whole{" "}
            <code className="font-mono">&lt;Graphic&gt;</code> block to paste inside a <code className="font-mono">&lt;PointSymbolizer&gt;</code>.
          </DialogDescription>
        </DialogHeader>
        {(icons.data?.length ?? 0) > 8 && (
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
            <Input placeholder="Search icons…" value={q} onChange={(e) => setQ(e.target.value)} className="pl-8" autoFocus />
          </div>
        )}
        {icons.isLoading ? (
          <p className="py-8 text-center text-sm text-muted-foreground">Loading icons…</p>
        ) : !icons.data?.length ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            No icons uploaded yet.{" "}
            <Link to="/icons" className="text-primary hover:underline" onClick={() => onOpenChange(false)}>
              Upload icons
            </Link>
            .
          </p>
        ) : (
          <div className="grid max-h-[60vh] grid-cols-2 gap-2 overflow-y-auto sm:grid-cols-3 md:grid-cols-4">
            {list.map((ic) => (
              <div key={ic.name} className="flex flex-col items-center gap-1.5 rounded-md border p-2">
                <div className="flex size-12 items-center justify-center rounded bg-[repeating-conic-gradient(var(--muted)_0_25%,transparent_0_50%)] bg-[length:10px_10px]">
                  <AuthImage src={`/icons/${encodeURIComponent(ic.name)}`} alt={ic.name} className="max-h-10 max-w-10 object-contain" />
                </div>
                <div className="w-full truncate text-center font-mono text-xs" title={ic.name}>
                  {ic.name}
                </div>
                <div className="grid w-full grid-cols-2 gap-1">
                  <CopyIconButton label="Path" title="Copy the path for xlink:href" text={ic.name} />
                  <CopyIconButton label="SLD" title="Copy an SLD Graphic for this icon" text={sldSnippet(ic.name, ic.height)} code />
                </div>
              </div>
            ))}
            {list.length === 0 && <p className="col-span-full py-6 text-center text-sm text-muted-foreground">No icon matches “{q}”.</p>}
          </div>
        )}
        <div className="flex justify-end">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
