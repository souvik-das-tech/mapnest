import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import CodeMirror from "@uiw/react-codemirror";
import { xml } from "@codemirror/lang-xml";
import { AlertTriangle, ArrowLeft, FileUp, Image, Loader2, Save } from "lucide-react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { previewStyle, useIcons, useLayers, useSaveStyle, useStyleBody, useStyleMeta, validateStyle } from "@/api/hooks";
import type { StyleFormat, StyleValidation } from "@/api/types";
import { useTheme } from "@/app/theme";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ErrorBox, PageHeader } from "@/components/ui/empty-state";
import { Input, NativeSelect } from "@/components/ui/input";
import { Field } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/misc";
import { styleNameFromFile } from "./StylesPage";
import { SLD_TEMPLATES } from "./sldTemplates";
import { IconPathsDialog } from "./IconPathsDialog";
import { GeoServerNotes } from "./GeoServerNotes";

/**
 * SLD code editor: paste, drop or write an SLD, see it rendered on a layer
 * (debounced POST /layers/{name}/preview) with converter warnings inline.
 * Icons are referenced by file name; copy them from the Icons page.
 */
export function StyleEditor() {
  const { name: routeName } = useParams();
  const isNew = !routeName;
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const { theme } = useTheme();
  const layers = useLayers();
  const icons = useIcons();
  const meta = useStyleMeta(routeName ?? "", !isNew);
  const body = useStyleBody(routeName ?? "", !isNew);
  const save = useSaveStyle();

  const [name, setName] = useState("");
  const [text, setText] = useState("");
  // Styles made earlier in the old visual editor are JSON; they stay JSON.
  const [format, setFormat] = useState<StyleFormat>("sld");
  const [loaded, setLoaded] = useState(isNew);
  const [previewLayer, setPreviewLayer] = useState(params.get("layer") ?? "");
  const [validation, setValidation] = useState<StyleValidation | null>(null);
  const [error, setError] = useState<ApiError | null>(null);
  const [img, setImg] = useState<string | null>(null);
  const [rendering, setRendering] = useState(false);
  const [dragging, setDragging] = useState(false);
  const fileInput = useRef<HTMLInputElement>(null);
  const [iconsOpen, setIconsOpen] = useState(false);

  // Load an existing style once.
  useEffect(() => {
    if (isNew || loaded || !meta.data || body.data === undefined) return;
    setName(meta.data.name);
    setFormat(meta.data.format);
    setText(body.data);
    setLoaded(true);
  }, [isNew, loaded, meta.data, body.data]);

  const target = validation?.target ?? meta.data?.target ?? "vector";
  const candidates = useMemo(
    () => (layers.data ?? []).filter((l) => l.kind === target && l.enabled),
    [layers.data, target],
  );
  useEffect(() => {
    if (!candidates.some((l) => l.name === previewLayer)) setPreviewLayer(candidates[0]?.name ?? "");
  }, [candidates, previewLayer]);
  const layer = candidates.find((l) => l.name === previewLayer);

  // Debounced validate + preview.
  useEffect(() => {
    if (!loaded || !text.trim()) {
      setValidation(null);
      setError(null);
      return;
    }
    const ctrl = new AbortController();
    const t = setTimeout(async () => {
      try {
        const v = await validateStyle(format, text, layer?.name, name || "preview");
        if (ctrl.signal.aborted) return;
        setValidation(v);
        setError(null);
        if (layer && v.target === layer.kind) {
          setRendering(true);
          const url = await previewStyle(layer.name, format, text, ctrl.signal);
          setImg((old) => {
            if (old) URL.revokeObjectURL(old);
            return url;
          });
        }
      } catch (e) {
        if (!ctrl.signal.aborted) setError(e instanceof ApiError ? e : new ApiError(0, String(e)));
      } finally {
        if (!ctrl.signal.aborted) setRendering(false);
      }
    }, 600);
    return () => {
      clearTimeout(t);
      ctrl.abort();
    };
  }, [loaded, text, format, layer, name]);

  const loadFile = async (file: File) => {
    setText(await file.text());
    setFormat(file.name.toLowerCase().endsWith(".json") ? "json" : "sld");
    if (isNew && !name) setName(styleNameFromFile(file.name));
    toast.success(`Loaded ${file.name}`);
  };

  const insertTemplate = (id: string) => {
    const tpl = SLD_TEMPLATES.find((t) => t.id === id);
    if (!tpl) return;
    if (text.trim() && !confirm("Replace the current SLD with this example?")) return;
    // Examples reference school.png; use an icon that is actually uploaded.
    const icon = icons.data?.[0]?.name;
    setText(icon ? tpl.sld.replaceAll("school.png", icon) : tpl.sld);
    setFormat("sld");
  };

  const nameValid = /^[A-Za-z][A-Za-z0-9_]{0,62}$/.test(name);

  const doSave = async () => {
    try {
      const st = await save.mutateAsync({ name, format, body: text, isNew });
      toast.success(isNew ? `Style “${name}” created` : "Style saved");
      if (isNew) navigate(`/styles/${encodeURIComponent(name)}`, { replace: true });
      if (st.warnings.length) toast.warning(`${st.warnings.length} part(s) could not be converted; see warnings`);
    } catch (e) {
      toast.error(e instanceof ApiError ? e.full : String(e));
    }
  };

  if (!isNew && (meta.isLoading || body.isLoading)) return <Skeleton className="h-96" />;
  if (!isNew && meta.isError) return <ErrorBox error={meta.error} />;

  return (
    <div
      onDragOver={(e) => {
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={(e) => e.currentTarget === e.target && setDragging(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        const f = e.dataTransfer.files[0];
        if (f) loadFile(f);
      }}
      className="relative"
    >
      {dragging && (
        <div className="pointer-events-none absolute inset-0 z-20 flex items-center justify-center rounded-xl border-2 border-dashed border-primary bg-primary/5 font-medium text-primary">
          Drop an .sld file
        </div>
      )}
      <Button variant="ghost" size="sm" asChild className="mb-2 -ml-2">
        <Link to="/styles">
          <ArrowLeft /> Styles
        </Link>
      </Button>
      <PageHeader
        title={isNew ? "New style" : name}
        description={
          meta.data?.used_by.length
            ? `Used by ${meta.data.used_by.join(", ")}`
            : "Paste or write an SLD. The preview updates as you type."
        }
        actions={
          <>
            <Button variant="outline" onClick={() => fileInput.current?.click()}>
              <FileUp /> Import .sld
            </Button>
            <Button onClick={doSave} disabled={!nameValid || !text.trim() || save.isPending || !!error}>
              {save.isPending ? <Loader2 className="animate-spin" /> : <Save />} Save
            </Button>
          </>
        }
      />
      <input
        ref={fileInput}
        type="file"
        accept=".sld,.xml,.json"
        hidden
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) loadFile(f);
          e.target.value = "";
        }}
      />

      <div className="grid gap-4 xl:grid-cols-2">
        {/* Left: SLD code */}
        <div className="grid content-start gap-4">
          {isNew && (
            <Field label="Style name" error={name && !nameValid ? "Start with a letter; letters, digits and _" : undefined}>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="roads_main" />
            </Field>
          )}
          <div className="flex flex-wrap items-center justify-between gap-2">
            <span className="text-sm font-medium">{format === "json" ? "Style (JSON)" : "SLD"}</span>
            <div className="flex items-center gap-2">
              <NativeSelect
                value=""
                onChange={(e) => insertTemplate(e.target.value)}
                className="h-8 w-auto"
                aria-label="Insert an example SLD"
              >
                <option value="">Insert example…</option>
                {SLD_TEMPLATES.map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.label}
                  </option>
                ))}
              </NativeSelect>
              <Button variant="outline" size="sm" onClick={() => setIconsOpen(true)}>
                <Image /> Icon paths
              </Button>
            </div>
          </div>
          <Card className="overflow-hidden">
            <CodeMirror
              value={text}
              height="600px"
              theme={theme}
              extensions={format === "sld" ? [xml()] : []}
              onChange={setText}
              placeholder="Paste a GeoServer SLD here, drop an .sld file, or pick an example above…"
              basicSetup={{ foldGutter: true, highlightActiveLine: true }}
            />
          </Card>
          <GeoServerNotes defaultOpen={isNew} />
        </div>

        {/* Right: preview + warnings */}
        <div className="grid content-start gap-4 xl:sticky xl:top-4">
          <Card className="overflow-hidden">
            <CardHeader className="flex-row items-center justify-between gap-3 border-b py-3">
              <CardTitle className="text-sm">Live preview</CardTitle>
              <NativeSelect
                className="h-8 max-w-56"
                value={previewLayer}
                onChange={(e) => setPreviewLayer(e.target.value)}
                disabled={!candidates.length}
              >
                {candidates.length === 0 && <option value="">No {target} layers</option>}
                {candidates.map((l) => (
                  <option key={l.name} value={l.name}>
                    {l.title || l.name}
                  </option>
                ))}
              </NativeSelect>
            </CardHeader>
            <div className="relative flex min-h-80 items-center justify-center bg-[repeating-conic-gradient(var(--muted)_0_25%,transparent_0_50%)] bg-[length:16px_16px]">
              {img && text.trim() ? (
                <img src={img} alt="Style preview" className="max-h-[480px] w-full object-contain" />
              ) : (
                <span className="p-8 text-center text-sm text-muted-foreground">
                  {!text.trim()
                    ? "Paste or write an SLD to see it here."
                    : candidates.length
                      ? "Rendering…"
                      : `Publish a ${target} layer to preview this style.`}
                </span>
              )}
              {rendering && (
                <span className="absolute right-3 top-3 flex items-center gap-1 rounded-full bg-card px-2 py-1 text-xs shadow">
                  <Loader2 className="size-3 animate-spin" /> updating
                </span>
              )}
            </div>
          </Card>

          {error && <ErrorBox error={error} />}
          {!!validation?.warnings.length && (
            <Card>
              <CardContent className="grid gap-2 p-4">
                <div className="flex items-center gap-2 text-sm font-medium">
                  <AlertTriangle className="size-4 text-warning" /> {validation.warnings.length} warning
                  {validation.warnings.length > 1 ? "s" : ""}: not converted
                </div>
                <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
                  {validation.warnings.map((w, i) => (
                    <li key={i}>{w}</li>
                  ))}
                </ul>
              </CardContent>
            </Card>
          )}
          {validation && (
            <details className="rounded-lg border bg-card">
              <summary className="cursor-pointer px-4 py-3 text-sm font-medium">Generated MapServer classes</summary>
              <pre className="max-h-80 overflow-auto border-t p-4 font-mono text-xs">{validation.compiled}</pre>
            </details>
          )}
        </div>
      </div>
      <IconPathsDialog open={iconsOpen} onOpenChange={setIconsOpen} />
    </div>
  );
}
