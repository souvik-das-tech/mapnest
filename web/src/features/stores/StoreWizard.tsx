import { createContext, useCallback, useContext, useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { ArrowLeft, CheckCircle2, Cloud, Database, FolderOpen, Loader2, PlugZap, XCircle } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useCreateStore, useTestUnsavedStore, useUpdateStore, useWorkspaces } from "@/api/hooks";
import { workspaceLabel } from "@/app/workspace";
import type { LocalConnection, PostGISConnection, S3Connection, Store, StoreInput, StoreType, TestResult, Workspace } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input, NativeSelect } from "@/components/ui/input";
import { Field } from "@/components/ui/label";
import { Switch } from "@/components/ui/misc";
import { cn } from "@/lib/utils";

/** Workspace a new store is created in, picked next to the store name. */
interface WorkspaceChoice {
  value: string;
  set: (name: string) => void;
  options: Workspace[];
}
const WizardWorkspace = createContext<WorkspaceChoice>({ value: "", set: () => {}, options: [] });

/** Workspace picker beside the store name (new stores only, and only when there is a choice). */
function NameRow({ children, editing }: { children: React.ReactNode; editing?: Store }) {
  const ws = useContext(WizardWorkspace);
  if (editing || ws.options.length < 2) return <>{children}</>;
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <Field label="Workspace" hint="Its layers are served at this workspace's address">
        <NativeSelect value={ws.value} onChange={(e) => ws.set(e.target.value)}>
          {ws.options.map((w) => (
            <option key={w.name} value={w.name}>
              {workspaceLabel(w)}
            </option>
          ))}
        </NativeSelect>
      </Field>
      {children}
    </div>
  );
}

/** Small heading that splits a form into groups. */
function SectionTitle({ children }: { children: React.ReactNode }) {
  return (
    <div className="-mb-1 border-t pt-4 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
      {children}
    </div>
  );
}

const name = z
  .string()
  .regex(/^[A-Za-z][A-Za-z0-9_]{0,62}$/, "Start with a letter; letters, digits and _ only");

const pgSchema = z.object({
  name,
  host: z.string().min(1, "Required"),
  port: z.coerce.number().int().min(1).max(65535),
  database: z.string().min(1, "Required"),
  user: z.string().min(1, "Required"),
  password: z.string().optional(),
  schema: z.string().min(1, "Required"),
  sslmode: z.enum(["disable", "allow", "prefer", "require", "verify-ca", "verify-full"]),
});

const s3Schema = z.object({
  name,
  bucket: z.string().min(1, "Required"),
  region: z.string().min(1, "Required"),
  endpoint: z.string().optional(),
  prefix: z.string().optional(),
  access_key: z.string().min(1, "Required"),
  secret_key: z.string().optional(),
  use_https: z.boolean(),
  virtual_hosting: z.boolean(),
});

const localSchema = z.object({
  name,
  path: z
    .string()
    .refine((p) => !p.split(/[\\/]/).includes(".."), "Must stay inside the raster folder"),
});

type PgForm = z.infer<typeof pgSchema>;
type S3Form = z.infer<typeof s3Schema>;
type LocalForm = z.infer<typeof localSchema>;

/**
 * Create (or edit) a store: choose type → form → test connection (must pass) → save.
 * PLAN.md §11.3 Stores.
 */
export function StoreWizard({
  open,
  onOpenChange,
  editing,
  onSaved,
  defaultWorkspace,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  editing?: Store;
  onSaved?: (s: Store) => void;
  /** Preselected workspace for a new store (the Stores page filter). */
  defaultWorkspace?: string;
}) {
  const [type, setType] = useState<StoreType | null>(editing?.type ?? null);
  const workspaces = useWorkspaces();
  const [workspace, setWorkspace] = useState(defaultWorkspace || "default");

  useEffect(() => {
    if (open) {
      setType(editing?.type ?? null);
      setWorkspace(defaultWorkspace || "default");
    }
  }, [open, editing, defaultWorkspace]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{editing ? `Edit store “${editing.name}”` : "Add a store"}</DialogTitle>
          <DialogDescription>
            {type === null
              ? "Where does your data live?"
              : type === "postgis"
                ? "Connect to your PostGIS database. We only ever read from it."
                : type === "s3"
                  ? "Connect to an S3 or S3-compatible bucket with Cloud Optimized GeoTIFFs."
                  : "Use a folder of GeoTIFFs on the server (the raster folder mounted into MapServer)."}
          </DialogDescription>
        </DialogHeader>

        <WizardWorkspace.Provider value={{ value: workspace, set: setWorkspace, options: workspaces.data ?? [] }}>
        {type === null ? (
          <div className="grid gap-3 sm:grid-cols-3">
            <TypeCard
              icon={<Database />}
              title="PostGIS"
              text="Vector tables: points, lines, polygons."
              onClick={() => setType("postgis")}
            />
            <TypeCard
              icon={<Cloud />}
              title="S3 bucket"
              text="Raster COGs on AWS S3, MinIO, SeaweedFS…"
              onClick={() => setType("s3")}
            />
            <TypeCard
              icon={<FolderOpen />}
              title="Local folder"
              text="GeoTIFFs in a folder on the server."
              onClick={() => setType("local")}
            />
          </div>
        ) : type === "postgis" ? (
          <PostGISForm
            editing={editing}
            onBack={editing ? undefined : () => setType(null)}
            onDone={(s) => {
              onOpenChange(false);
              onSaved?.(s);
            }}
          />
        ) : type === "s3" ? (
          <S3Form
            editing={editing}
            onBack={editing ? undefined : () => setType(null)}
            onDone={(s) => {
              onOpenChange(false);
              onSaved?.(s);
            }}
          />
        ) : (
          <LocalFolderForm
            editing={editing}
            onBack={editing ? undefined : () => setType(null)}
            onDone={(s) => {
              onOpenChange(false);
              onSaved?.(s);
            }}
          />
        )}
        </WizardWorkspace.Provider>
      </DialogContent>
    </Dialog>
  );
}

function TypeCard({ icon, title, text, onClick }: { icon: React.ReactNode; title: string; text: string; onClick: () => void }) {
  return (
    <button
      onClick={onClick}
      className="group flex cursor-pointer flex-col items-start gap-3 rounded-lg border p-4 text-left transition-all hover:border-primary hover:shadow-md"
    >
      <div className="flex size-10 items-center justify-center rounded-lg bg-accent text-accent-foreground [&_svg]:size-5">
        {icon}
      </div>
      <div>
        <div className="font-semibold">{title}</div>
        <div className="text-sm text-muted-foreground">{text}</div>
      </div>
    </button>
  );
}

/** Shared test-then-save logic for both forms. */
function useTestAndSave(editing: Store | undefined, onDone: (s: Store) => void) {
  const test = useTestUnsavedStore();
  const create = useCreateStore();
  const update = useUpdateStore(editing?.name ?? "");
  const workspace = useContext(WizardWorkspace).value;
  const [result, setResult] = useState<TestResult | null>(null);
  const reset = useCallback(() => setResult(null), []);

  const runTest = async (input: StoreInput) => {
    setResult(null);
    try {
      setResult(await test.mutateAsync(input));
    } catch (e) {
      setResult({ ok: false, latency_ms: 0, error: e instanceof ApiError ? e.full : String(e) });
    }
  };

  const save = async (input: StoreInput) => {
    try {
      const s = editing ? await update.mutateAsync(input) : await create.mutateAsync({ ...input, workspace: workspace || undefined });
      toast.success(editing ? `Store “${s.name}” updated` : `Store “${s.name}” created`);
      onDone(s);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  return {
    result,
    reset,
    runTest,
    save,
    testing: test.isPending,
    saving: create.isPending || update.isPending,
  };
}

function TestStatus({ result, testing }: { result: TestResult | null; testing: boolean }) {
  if (testing)
    return (
      <div className="flex items-center gap-2 rounded-md bg-muted px-3 py-2 text-sm">
        <Loader2 className="size-4 animate-spin" /> Testing connection…
      </div>
    );
  if (!result) return null;
  return (
    <div
      className={cn(
        "flex items-start gap-2 rounded-md px-3 py-2 text-sm",
        result.ok ? "bg-success/10 text-success" : "bg-destructive/10 text-destructive",
      )}
    >
      {result.ok ? <CheckCircle2 className="mt-0.5 size-4 shrink-0" /> : <XCircle className="mt-0.5 size-4 shrink-0" />}
      <div className="min-w-0">
        {result.ok ? (
          <>
            <div className="font-medium">
              Connected <span className="font-normal opacity-80">· {result.latency_ms} ms</span>
            </div>
            {result.info?.postgis_version && (
              <div className="text-xs text-foreground/70" title={result.info.server_version}>
                {/* "16.4 (Debian 16.4-1.pgdg110+2)" → "16.4" */}
                PostgreSQL {result.info.server_version?.split(" ")[0]} · PostGIS {result.info.postgis_version}
              </div>
            )}
          </>
        ) : (
          <span className="break-words">{result.error}</span>
        )}
      </div>
    </div>
  );
}

function FormFooter({
  onBack,
  canSave,
  testing,
  saving,
  onTest,
}: {
  onBack?: () => void;
  canSave: boolean;
  testing: boolean;
  saving: boolean;
  onTest: () => void;
}) {
  return (
    <DialogFooter className="sm:justify-between">
      {onBack ? (
        <Button type="button" variant="ghost" onClick={onBack}>
          <ArrowLeft /> Back
        </Button>
      ) : (
        <span />
      )}
      <div className="flex gap-2">
        <Button type="button" variant="outline" onClick={onTest} disabled={testing}>
          <PlugZap /> Test connection
        </Button>
        <Button type="submit" disabled={!canSave || saving} title={canSave ? "" : "Test the connection first"}>
          {saving && <Loader2 className="animate-spin" />} Save
        </Button>
      </div>
    </DialogFooter>
  );
}

function PostGISForm({ editing, onBack, onDone }: { editing?: Store; onBack?: () => void; onDone: (s: Store) => void }) {
  const c = editing?.connection as PostGISConnection | undefined;
  const form = useForm<PgForm>({
    resolver: zodResolver(pgSchema) as never,
    defaultValues: {
      name: editing?.name ?? "",
      host: c?.host ?? "",
      port: c?.port ?? 5432,
      database: c?.database ?? "",
      user: c?.user ?? "",
      password: "",
      schema: c?.schema ?? "public",
      sslmode: (c?.sslmode as PgForm["sslmode"]) ?? "prefer",
    },
  });
  const { result, reset, runTest, save, testing, saving } = useTestAndSave(editing, onDone);
  const { errors } = form.formState;

  // Any change invalidates the last test.
  useEffect(() => {
    const sub = form.watch(() => reset());
    return () => sub.unsubscribe();
  }, [form, reset]);

  const toInput = (v: PgForm): StoreInput => {
    const { name, password, ...conn } = v;
    // On edit, an empty password means "keep the stored one".
    return { name, type: "postgis", connection: editing && !password ? conn : { ...conn, password: password ?? "" } };
  };

  return (
    <form onSubmit={form.handleSubmit((v) => save(toInput(v)))} className="grid gap-4">
      <NameRow editing={editing}>
        <Field label="Store name" error={errors.name?.message} hint="Letters, digits and _">
          <Input {...form.register("name")} disabled={!!editing} aria-invalid={!!errors.name} placeholder="city_db" />
        </Field>
      </NameRow>
      <SectionTitle>Connection</SectionTitle>
      <div className="grid gap-4 sm:grid-cols-[1fr_7rem]">
        <Field label="Host" error={errors.host?.message}>
          <Input {...form.register("host")} placeholder="db.example.com" aria-invalid={!!errors.host} />
        </Field>
        <Field label="Port" error={errors.port?.message}>
          <Input type="number" {...form.register("port")} />
        </Field>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Database" error={errors.database?.message}>
          <Input {...form.register("database")} placeholder="gisdb" aria-invalid={!!errors.database} />
        </Field>
        <Field label="Schema" error={errors.schema?.message}>
          <Input {...form.register("schema")} />
        </Field>
        <Field label="SSL mode" className="sm:col-span-2">
          <NativeSelect {...form.register("sslmode")}>
            {["disable", "allow", "prefer", "require", "verify-ca", "verify-full"].map((m) => (
              <option key={m}>{m}</option>
            ))}
          </NativeSelect>
        </Field>
      </div>
      <SectionTitle>Login</SectionTitle>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="User" error={errors.user?.message}>
          <Input {...form.register("user")} autoComplete="off" aria-invalid={!!errors.user} placeholder="map_reader" />
        </Field>
        <Field label="Password" hint={editing?.has_secret ? "Leave empty to keep the current one" : undefined}>
          <Input type="password" {...form.register("password")} autoComplete="new-password" />
        </Field>
      </div>
      <p className="-mt-2 text-[11.5px] text-muted-foreground">
        A read-only role is enough: MapNest only reads from your database.
      </p>

      <TestStatus result={result} testing={testing} />
      <FormFooter
        onBack={onBack}
        canSave={!!result?.ok}
        testing={testing}
        saving={saving}
        onTest={form.handleSubmit((v) => runTest(toInput(v)))}
      />
    </form>
  );
}

function S3Form({ editing, onBack, onDone }: { editing?: Store; onBack?: () => void; onDone: (s: Store) => void }) {
  const c = editing?.connection as S3Connection | undefined;
  const form = useForm<S3Form>({
    resolver: zodResolver(s3Schema) as never,
    defaultValues: {
      name: editing?.name ?? "",
      bucket: c?.bucket ?? "",
      region: c?.region ?? "us-east-1",
      endpoint: c?.endpoint ?? "",
      prefix: c?.prefix ?? "",
      access_key: c?.access_key ?? "",
      secret_key: "",
      use_https: c?.use_https ?? true,
      virtual_hosting: c?.virtual_hosting ?? false,
    },
  });
  const { result, reset, runTest, save, testing, saving } = useTestAndSave(editing, onDone);
  const { errors } = form.formState;
  const useHttps = form.watch("use_https");
  const virtualHosting = form.watch("virtual_hosting");

  useEffect(() => {
    const sub = form.watch(() => reset());
    return () => sub.unsubscribe();
  }, [form, reset]);

  const toInput = (v: S3Form): StoreInput => {
    const { name, secret_key, ...conn } = v;
    return { name, type: "s3", connection: editing && !secret_key ? conn : { ...conn, secret_key: secret_key ?? "" } };
  };

  return (
    <form onSubmit={form.handleSubmit((v) => save(toInput(v)))} className="grid gap-4">
      <NameRow editing={editing}>
        <Field label="Store name" error={errors.name?.message} hint="Letters, digits and _">
          <Input {...form.register("name")} disabled={!!editing} aria-invalid={!!errors.name} placeholder="raster_bucket" />
        </Field>
      </NameRow>
      <SectionTitle>Bucket</SectionTitle>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Bucket" error={errors.bucket?.message}>
          <Input {...form.register("bucket")} placeholder="rasters" aria-invalid={!!errors.bucket} />
        </Field>
        <Field label="Region" error={errors.region?.message}>
          <Input {...form.register("region")} />
        </Field>
      </div>
      <Field label="Endpoint" hint="Leave empty for AWS. For MinIO/SeaweedFS: host:port, e.g. s3:8333">
        <Input {...form.register("endpoint")} placeholder="s3.example.com:9000" />
      </Field>
      <Field label="Prefix" hint="Optional folder inside the bucket">
        <Input {...form.register("prefix")} placeholder="cogs/" />
      </Field>
      <SectionTitle>Login</SectionTitle>
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label="Access key" error={errors.access_key?.message}>
          <Input {...form.register("access_key")} autoComplete="off" aria-invalid={!!errors.access_key} />
        </Field>
        <Field label="Secret key" hint={editing?.has_secret ? "Leave empty to keep the current one" : undefined}>
          <Input type="password" {...form.register("secret_key")} autoComplete="new-password" />
        </Field>
      </div>
      <div className="flex flex-wrap gap-4">
        <label className="flex items-center gap-2 text-sm">
          <Switch checked={useHttps} onCheckedChange={(v) => form.setValue("use_https", v, { shouldDirty: true })} />
          Use HTTPS
        </label>
        <label className="flex items-center gap-2 text-sm">
          <Switch
            checked={virtualHosting}
            onCheckedChange={(v) => form.setValue("virtual_hosting", v, { shouldDirty: true })}
          />
          Virtual-hosted URLs (AWS style)
        </label>
      </div>

      <TestStatus result={result} testing={testing} />
      <FormFooter
        onBack={onBack}
        canSave={!!result?.ok}
        testing={testing}
        saving={saving}
        onTest={form.handleSubmit((v) => runTest(toInput(v)))}
      />
    </form>
  );
}

function LocalFolderForm({ editing, onBack, onDone }: { editing?: Store; onBack?: () => void; onDone: (s: Store) => void }) {
  const c = editing?.connection as LocalConnection | undefined;
  const form = useForm<LocalForm>({
    resolver: zodResolver(localSchema) as never,
    defaultValues: { name: editing?.name ?? "", path: c?.path ?? "" },
  });
  const { result, reset, runTest, save, testing, saving } = useTestAndSave(editing, onDone);
  const { errors } = form.formState;

  useEffect(() => {
    const sub = form.watch(() => reset());
    return () => sub.unsubscribe();
  }, [form, reset]);

  const toInput = ({ name, path }: LocalForm): StoreInput => ({ name, type: "local", connection: { path } });

  return (
    <form onSubmit={form.handleSubmit((v) => save(toInput(v)))} className="grid gap-4">
      <NameRow editing={editing}>
        <Field label="Store name" error={errors.name?.message} hint="Letters, digits and _">
          <Input {...form.register("name")} disabled={!!editing} aria-invalid={!!errors.name} placeholder="drone_rasters" />
        </Field>
      </NameRow>
      <Field
        label="Folder"
        error={errors.path?.message}
        hint="Subfolder of the server's raster folder (RASTER_DIR in server/.env, default server/rasters). Leave empty for all of it."
      >
        <Input {...form.register("path")} placeholder="drone/2026" aria-invalid={!!errors.path} />
      </Field>

      <TestStatus result={result} testing={testing} />
      <FormFooter
        onBack={onBack}
        canSave={!!result?.ok}
        testing={testing}
        saving={saving}
        onTest={form.handleSubmit((v) => runTest(toInput(v)))}
      />
    </form>
  );
}
