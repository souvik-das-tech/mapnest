import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, fetchHealth } from "./client";
import type {
  Health,
  Icon,
  InspectResult,
  LayerStyles,
  StyleFormat,
  StyleInfo,
  StyleValidation,
  Layer,
  LayerInput,
  LayerUpdate,
  MapfileVersion,
  Store,
  StoreInput,
  Workspace,
  StoreResources,
  TestResult,
} from "./types";

export const qk = {
  health: ["health"] as const,
  stores: ["stores"] as const,
  store: (n: string) => ["stores", n] as const,
  resources: (n: string) => ["stores", n, "resources"] as const,
  layers: ["layers"] as const,
  layer: (n: string) => ["layers", n] as const,
  mapfile: ["mapfile"] as const,
  versions: ["mapfile", "versions"] as const,
  capabilities: ["wms-capabilities"] as const,
  styles: ["styles"] as const,
  style: (n: string) => ["styles", n] as const,
  layerStyles: (n: string) => ["layers", n, "styles"] as const,
  icons: ["icons"] as const,
  workspaces: ["workspaces"] as const,
};

// --- workspaces ---

export const useWorkspaces = () => useQuery({ queryKey: qk.workspaces, queryFn: () => api.get<Workspace[]>("/workspaces") });

export function useSaveWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ isNew, ...w }: { name: string; title: string; description: string; isNew: boolean }) =>
      isNew ? api.post<Workspace>("/workspaces", w) : api.put<Workspace>(`/workspaces/${encodeURIComponent(w.name)}`, w),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.workspaces }),
  });
}

export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.del(`/workspaces/${encodeURIComponent(name)}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.workspaces }),
  });
}

/** Moves a store, and with it its layers, to another workspace. */
export function useMoveStore() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ store, workspace }: { store: string; workspace: string }) =>
      api.put<Store>(`/stores/${encodeURIComponent(store)}/workspace`, { workspace }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.stores });
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.capabilities });
    },
  });
}

// --- health ---

export const useHealth = (withStores = false) =>
  useQuery({
    queryKey: [...qk.health, withStores],
    queryFn: () => fetchHealth<Health>(withStores),
    refetchInterval: 30_000,
    retry: false,
  });

// --- stores ---

export const useStores = () => useQuery({ queryKey: qk.stores, queryFn: () => api.get<Store[]>("/stores") });

export const useStore = (name: string) =>
  useQuery({ queryKey: qk.store(name), queryFn: () => api.get<Store>(`/stores/${encodeURIComponent(name)}`) });

export const useStoreResources = (name: string, enabled = true) =>
  useQuery({
    queryKey: qk.resources(name),
    queryFn: () => api.get<StoreResources>(`/stores/${encodeURIComponent(name)}/resources`),
    enabled,
    retry: false,
  });

export function useCreateStore() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (s: StoreInput) => api.post<Store>("/stores", s),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.stores });
      qc.invalidateQueries({ queryKey: qk.workspaces });
    },
  });
}

export function useUpdateStore(name: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (s: StoreInput) => api.put<Store>(`/stores/${encodeURIComponent(name)}`, s),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.stores }),
  });
}

export function useDeleteStore() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.del(`/stores/${encodeURIComponent(name)}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.stores });
      qc.invalidateQueries({ queryKey: qk.workspaces });
    },
  });
}

export const useTestUnsavedStore = () =>
  useMutation({ mutationFn: (s: StoreInput) => api.post<TestResult>("/stores/test", s) });

export const useTestStore = () =>
  useMutation({ mutationFn: (name: string) => api.post<TestResult>(`/stores/${encodeURIComponent(name)}/test`) });

// --- layers ---

export const useLayers = () => useQuery({ queryKey: qk.layers, queryFn: () => api.get<Layer[]>("/layers") });

export const useLayer = (name: string) =>
  useQuery({
    queryKey: qk.layer(name),
    queryFn: () => api.get<Layer>(`/layers/${encodeURIComponent(name)}`),
    // While a raster's fast copy is being made, check back until it is ready.
    refetchInterval: (q) =>
      (q.state.data?.source as { cache_status?: string } | undefined)?.cache_status === "building" ? 5000 : false,
  });

export function useRetryRasterCache(name: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post(`/layers/${encodeURIComponent(name)}/cache`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.layer(name) }),
  });
}

export function useCreateLayer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (l: LayerInput) => api.post<Layer>("/layers", l),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.mapfile });
      qc.invalidateQueries({ queryKey: qk.capabilities });
    },
  });
}

export const useInspectLayer = () =>
  useMutation({ mutationFn: (l: LayerInput) => api.post<InspectResult>("/layers/inspect", l) });

export function useUpdateLayer(name: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (u: LayerUpdate) => api.put<Layer>(`/layers/${encodeURIComponent(name)}`, u),
    onSuccess: (l) => {
      qc.setQueryData(qk.layer(name), l);
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.mapfile });
      qc.invalidateQueries({ queryKey: qk.capabilities });
    },
  });
}

export function useRefreshLayer(name: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<Layer>(`/layers/${encodeURIComponent(name)}/refresh`),
    onSuccess: (l) => {
      qc.setQueryData(qk.layer(name), l);
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.capabilities });
    },
  });
}

export function useDeleteLayer() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.del(`/layers/${encodeURIComponent(name)}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.stores });
      qc.invalidateQueries({ queryKey: qk.mapfile });
      qc.invalidateQueries({ queryKey: qk.capabilities });
    },
  });
}

// --- mapfile ---

export const useMapfile = () => useQuery({ queryKey: qk.mapfile, queryFn: () => api.get<string>("/mapfile") });

export const useVersions = () =>
  useQuery({ queryKey: qk.versions, queryFn: () => api.get<MapfileVersion[]>("/mapfile/versions") });

export function useRollback() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api.post(`/mapfile/rollback/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.mapfile }),
  });
}

export function useRebuild() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post("/mapfile/rebuild"),
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.mapfile }),
  });
}

// --- styles ---

const styleContentType = (f: StyleFormat) => (f === "json" ? "application/json" : "application/vnd.ogc.sld+xml");

export const useStyles = () => useQuery({ queryKey: qk.styles, queryFn: () => api.get<StyleInfo[]>("/styles") });

export const useStyleMeta = (name: string, enabled = true) =>
  useQuery({
    queryKey: [...qk.style(name), "meta"],
    queryFn: () => api.get<StyleInfo>(`/styles/${encodeURIComponent(name)}?meta=1`),
    enabled,
  });

/** Original SLD/JSON body, as uploaded. */
export const useStyleBody = (name: string, enabled = true) =>
  useQuery({
    queryKey: [...qk.style(name), "body"],
    queryFn: async () => {
      const b = await api.get<unknown>(`/styles/${encodeURIComponent(name)}`);
      return typeof b === "string" ? b : JSON.stringify(b, null, 2);
    },
    enabled,
  });

export function useSaveStyle() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ name, format, body, isNew }: { name: string; format: StyleFormat; body: string; isNew: boolean }) => {
      const init = { headers: { "Content-Type": styleContentType(format) } };
      return isNew
        ? api.post<StyleInfo>(`/styles?name=${encodeURIComponent(name)}`, body, init)
        : api.put<StyleInfo>(`/styles/${encodeURIComponent(name)}`, body, init);
    },
    onSuccess: (_d, v) => {
      qc.invalidateQueries({ queryKey: qk.styles });
      qc.invalidateQueries({ queryKey: qk.style(v.name) });
      qc.invalidateQueries({ queryKey: qk.capabilities });
      qc.invalidateQueries({ queryKey: qk.mapfile });
    },
  });
}

export function useDeleteStyle() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.del(`/styles/${encodeURIComponent(name)}`),
    // Layers using it fall back to another style, so they change too.
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.styles });
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.mapfile });
      qc.invalidateQueries({ queryKey: qk.capabilities });
    },
  });
}

export const validateStyle = (format: StyleFormat, body: string, layer?: string, name?: string) => {
  const q = new URLSearchParams();
  if (layer) q.set("layer", layer);
  if (name) q.set("name", name);
  return api.post<StyleValidation>(`/styles/validate?${q}`, body, {
    headers: { "Content-Type": styleContentType(format) },
  });
};

/** PNG of a layer rendered with an unsaved style (object URL). */
export async function previewStyle(layer: string, format: StyleFormat, body: string, signal?: AbortSignal) {
  const blob = await api.post<Blob>(`/layers/${encodeURIComponent(layer)}/preview?width=640`, body, {
    headers: { "Content-Type": styleContentType(format) },
    signal,
  });
  return URL.createObjectURL(blob);
}

export const useLayerStyles = (layer: string) =>
  useQuery({
    queryKey: qk.layerStyles(layer),
    queryFn: () => api.get<LayerStyles>(`/layers/${encodeURIComponent(layer)}/styles`),
  });

export function useSetLayerStyles(layer: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (s: LayerStyles) => api.put<LayerStyles>(`/layers/${encodeURIComponent(layer)}/styles`, s),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.layerStyles(layer) });
      qc.invalidateQueries({ queryKey: qk.layer(layer) });
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.styles });
      qc.invalidateQueries({ queryKey: qk.capabilities });
      qc.invalidateQueries({ queryKey: qk.mapfile });
    },
  });
}

// --- icons ---

export const useIcons = () => useQuery({ queryKey: qk.icons, queryFn: () => api.get<Icon[]>("/icons") });

export function useUploadIcon() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ file, name }: { file: File; name?: string }) => {
      const fd = new FormData();
      fd.append("file", file);
      if (name) fd.append("name", name);
      return api.post<Icon>("/icons", fd);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: qk.icons }),
  });
}

export function useDeleteIcon() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (name: string) => api.del(`/icons/${encodeURIComponent(name)}`),
    // Layers using it fall back to another style, so they change too.
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: qk.icons });
      qc.invalidateQueries({ queryKey: qk.styles });
      qc.invalidateQueries({ queryKey: qk.layers });
      qc.invalidateQueries({ queryKey: qk.workspaces });
      qc.invalidateQueries({ queryKey: qk.mapfile });
      qc.invalidateQueries({ queryKey: qk.capabilities });
    },
  });
}
