import { useRef, useState } from "react";
import { Archive, Download, FileCode2, GitCompare, History, Loader2, RotateCcw, Upload, Wrench } from "lucide-react";
import { toast } from "sonner";
import { useQueryClient } from "@tanstack/react-query";
import { api, ApiError, request } from "@/api/client";
import { useMapfile, useRebuild, useRollback, useVersions } from "@/api/hooks";
import { diffLines, withContext } from "@/lib/diff";
import { cn } from "@/lib/utils";
import type { MapfileVersion } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm";
import { ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Skeleton, Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/misc";
import { timeAgo } from "@/lib/utils";

/** Generated mapfile (read-only), version history, rollback, rebuild. */
export function SystemPage() {
  const mapfile = useMapfile();
  const versions = useVersions();
  const rollback = useRollback();
  const rebuild = useRebuild();
  const [viewing, setViewing] = useState<MapfileVersion | null>(null);
  const [compare, setCompare] = useState(false);
  const [busy, setBusy] = useState<"backup" | "restore" | null>(null);
  const restoreInput = useRef<HTMLInputElement>(null);
  const qc = useQueryClient();

  const downloadBackup = async () => {
    setBusy("backup");
    try {
      const blob = await request<Blob>("GET", "/system/backup");
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = `msapi-backup-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.tar.gz`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(a.href), 1000);
      toast.success("Backup downloaded. Keep MSAPI_SECRET_KEY with it (not inside it).");
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    } finally {
      setBusy(null);
    }
  };

  const restore = async (file: File) => {
    setBusy("restore");
    try {
      const res = await api.post<{ ok: boolean; warning?: string }>("/system/restore", file, {
        headers: { "Content-Type": "application/gzip" },
      });
      toast.success("Configuration restored and mapfile rebuilt");
      if (res.warning) toast.warning(res.warning);
      qc.invalidateQueries();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    } finally {
      setBusy(null);
    }
  };

  const doRebuild = async () => {
    try {
      await rebuild.mutateAsync();
      toast.success("Mapfile rebuilt from the database");
      versions.refetch();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  const doRollback = async (id: number) => {
    try {
      await rollback.mutateAsync(id);
      toast.success(`Version ${id} is live again`);
      versions.refetch();
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  const view = async (id: number) => {
    try {
      setViewing(await api.get<MapfileVersion>(`/mapfile/versions/${id}`));
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return (
    <>
      <PageHeader
        title="System"
        description="The generated mapfile and its history. The database is the source of truth; this file is rebuilt on every change."
        actions={
          <>
            <Button variant="outline" onClick={downloadBackup} disabled={busy !== null}>
              {busy === "backup" ? <Loader2 className="animate-spin" /> : <Download />} Backup
            </Button>
            <ConfirmDialog
              title="Restore from a backup?"
              description="All stores, layers, styles and icons are replaced by the backup's content, and the mapfile is rebuilt. Take a backup first if unsure."
              confirmLabel="Choose file…"
              onConfirm={() => restoreInput.current?.click()}
              trigger={
                <Button variant="outline" disabled={busy !== null}>
                  {busy === "restore" ? <Loader2 className="animate-spin" /> : <Upload />} Restore
                </Button>
              }
            />
            <Button variant="outline" onClick={doRebuild} disabled={rebuild.isPending}>
              {rebuild.isPending ? <Loader2 className="animate-spin" /> : <Wrench />} Rebuild now
            </Button>
            <input
              ref={restoreInput}
              type="file"
              accept=".tar.gz,.tgz,application/gzip"
              hidden
              onChange={(e) => {
                const f = e.target.files?.[0];
                if (f) restore(f);
                e.target.value = "";
              }}
            />
          </>
        }
      />

      <Tabs defaultValue="mapfile">
        <TabsList>
          <TabsTrigger value="mapfile">
            <FileCode2 /> Current mapfile
          </TabsTrigger>
          <TabsTrigger value="versions">
            <History /> Versions
          </TabsTrigger>
        </TabsList>

        <TabsContent value="mapfile">
          <Card>
            <CardContent className="p-0">
              {mapfile.isLoading && <Skeleton className="m-5 h-96" />}
              {mapfile.isError && (
                <div className="p-4">
                  <ErrorBox error={mapfile.error} />
                </div>
              )}
              {mapfile.data && <MapfileCode text={mapfile.data} />}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="versions">
          <div className="grid gap-4 lg:grid-cols-5">
            <Card className="lg:col-span-2">
              <CardHeader>
                <CardTitle>History</CardTitle>
                <CardDescription>The last 50 generated mapfiles.</CardDescription>
              </CardHeader>
              <CardContent>
                {versions.isLoading && <Skeleton className="h-40" />}
                <ul className="divide-y">
                  {versions.data?.map((v, i) => (
                    <li key={v.id} className="flex items-center gap-2 py-2">
                      <button onClick={() => view(v.id)} className="min-w-0 flex-1 cursor-pointer text-left">
                        <div className="flex items-center gap-2 text-sm font-medium">
                          #{v.id} {i === 0 && <Badge variant="success">latest</Badge>}
                        </div>
                        <div className="truncate text-xs text-muted-foreground">
                          {v.reason} · {timeAgo(v.created_at)}
                        </div>
                      </button>
                      {i > 0 && (
                        <ConfirmDialog
                          title={`Roll back to version ${v.id}?`}
                          description="This version goes live immediately. The database is not changed, so the next edit or rebuild regenerates the mapfile from the database again. Use it as an emergency switch."
                          confirmLabel="Roll back"
                          onConfirm={() => doRollback(v.id)}
                          pending={rollback.isPending}
                          trigger={
                            <Button size="sm" variant="ghost">
                              <RotateCcw /> Roll back
                            </Button>
                          }
                        />
                      )}
                    </li>
                  ))}
                </ul>
              </CardContent>
            </Card>
            <Card className="overflow-hidden lg:col-span-3">
              {viewing ? (
                <>
                  <div className="flex flex-wrap items-center justify-between gap-2 border-b px-5 py-3 text-sm">
                    <span className="font-medium">
                      Version #{viewing.id} · {viewing.reason}
                    </span>
                    <Button size="sm" variant={compare ? "default" : "outline"} onClick={() => setCompare(!compare)}>
                      <GitCompare /> {compare ? "Show file" : "Diff with current"}
                    </Button>
                  </div>
                  {compare ? (
                    <DiffView from={viewing.content ?? ""} to={mapfile.data ?? ""} />
                  ) : (
                    <MapfileCode text={viewing.content ?? ""} />
                  )}
                </>
              ) : (
                <div className="flex h-64 items-center justify-center text-sm text-muted-foreground">
                  Pick a version to see its content.
                </div>
              )}
            </Card>
          </div>
        </TabsContent>
      </Tabs>
    </>
  );
}

/** Mapfile text with light keyword/string/comment highlighting. */
function MapfileCode({ text }: { text: string }) {
  const keywords =
    /^(\s*)(MAP|END|LAYER|CLASS|STYLE|LABEL|WEB|METADATA|PROJECTION|OUTPUTFORMAT|LEGEND|SYMBOL|NAME|TYPE|STATUS|DATA|CONNECTION|CONNECTIONTYPE|PROCESSING|EXTENT|CONFIG|COLOR|OUTLINECOLOR|WIDTH|SIZE|CLASSGROUP|GROUP|EXPRESSION|TEMPLATE|SYMBOLSET|IMAGETYPE|IMAGECOLOR|UNITS|MAXSIZE|DRIVER|MIMETYPE|IMAGEMODE|EXTENSION|TRANSPARENT|FORMATOPTION|KEYSIZE|OPACITY|MINSCALEDENOM|MAXSCALEDENOM)\b/;
  return (
    <pre className="max-h-[70vh] overflow-auto p-4 font-mono text-xs leading-relaxed">
      {text.split("\n").map((line, i) => {
        if (line.trim().startsWith("#"))
          return (
            <div key={i} className="text-muted-foreground italic">
              {line || " "}
            </div>
          );
        const m = line.match(keywords);
        const rest = m ? line.slice(m[0].length) : line;
        return (
          <div key={i}>
            {m && (
              <>
                {m[1]}
                <span className="font-semibold text-primary">{m[2]}</span>
              </>
            )}
            {rest.split(/("(?:[^"\\]|\\.)*")/).map((part, j) =>
              part.startsWith('"') ? (
                <span key={j} className="text-success">
                  {part}
                </span>
              ) : (
                <span key={j}>{part}</span>
              ),
            )}
            {!line && " "}
          </div>
        );
      })}
    </pre>
  );
}

/** Old version (red, −) against the current mapfile (green, +). */
function DiffView({ from, to }: { from: string; to: string }) {
  const lines = withContext(diffLines(from, to));
  if (!lines.some((l) => l.kind === "add" || l.kind === "del"))
    return (
      <div className="flex h-40 items-center justify-center gap-2 text-sm text-muted-foreground">
        <Archive className="size-4" /> Identical to the current mapfile.
      </div>
    );
  return (
    <pre className="max-h-[70vh] overflow-auto py-3 font-mono text-xs leading-relaxed">
      {lines.map((l, i) =>
        l.kind === "skip" ? (
          <div key={i} className="bg-muted/50 px-5 py-0.5 text-muted-foreground">
            ⋯ {l.count} unchanged line{l.count > 1 ? "s" : ""}
          </div>
        ) : (
          <div
            key={i}
            className={cn(
              "px-5",
              l.kind === "add" && "bg-success/15 text-success",
              l.kind === "del" && "bg-destructive/10 text-destructive",
            )}
          >
            {l.kind === "add" ? "+ " : l.kind === "del" ? "− " : "  "}
            {l.text || " "}
          </div>
        ),
      )}
    </pre>
  );
}
