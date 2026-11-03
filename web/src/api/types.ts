// Mirrors server/msapi/internal/api response shapes.

export type StoreType = "postgis" | "s3" | "local";

export interface PostGISConnection {
  host: string;
  port: number;
  database: string;
  user: string;
  schema: string;
  sslmode?: string;
  password?: string; // write-only
}

export interface S3Connection {
  bucket: string;
  region: string;
  endpoint?: string;
  prefix?: string;
  access_key: string;
  use_https: boolean;
  virtual_hosting: boolean;
  secret_key?: string; // write-only
}

// Folder inside the server's raster directory (MSAPI_RASTER_DIR); "" = all of it.
export interface LocalConnection {
  path: string;
}

export interface Workspace {
  name: string;
  title: string;
  description: string;
  stores: number;
  layers: number;
  ows_url: string; // WMS + WFS endpoint of this workspace
  default: boolean;
  created_at: string;
  updated_at: string;
}

export interface Store {
  name: string;
  type: StoreType;
  workspace: string;
  connection: PostGISConnection | S3Connection | LocalConnection;
  has_secret: boolean;
  created_at: string;
  updated_at: string;
}

export interface StoreInput {
  name: string;
  type: StoreType;
  workspace?: string;
  connection: Partial<PostGISConnection> | Partial<S3Connection> | Partial<LocalConnection>;
}

export interface TestResult {
  ok: boolean;
  latency_ms: number;
  error?: string;
  info?: { server_version?: string; postgis_version?: string };
}

export interface PgTable {
  schema: string;
  table: string;
  geom_column: string;
  geom_type: string;
  srid: number;
  estimated_rows: number;
  can_select: boolean;
}

export interface S3Object {
  path: string;
  size: number;
  last_modified: string;
}

export type StoreResources =
  | { type: "postgis"; tables: PgTable[] }
  | { type: "s3" | "local"; objects: S3Object[]; truncated: boolean };

export interface Extent {
  minx: number;
  miny: number;
  maxx: number;
  maxy: number;
}

export interface VectorSource {
  schema: string;
  table: string;
  geom_column: string;
  pk: string;
  geom_type: string;
  layer_type: "POINT" | "LINE" | "POLYGON";
  srid: number;
  /** srid was chosen by the user; column_srid is the column's own (0 = none). */
  declared_srid?: boolean;
  column_srid?: number;
}

export interface RasterSource {
  path: string;
  bands: number;
  dtype: string;
  nodata?: number;
  srid?: number;
  wkt?: string;
  declared_srid?: boolean; // srid set by the user; file_srid is the file's own
  file_srid?: number;
  /** Not Cloud Optimized: MapNest serves a fast copy it makes itself. */
  not_cog?: boolean;
  cache_status?: "building" | "ready" | "failed";
  cache_file?: string;
  cache_bytes?: number;
  cache_error?: string;
  min?: number;
  max?: number;
}

export interface Layer {
  name: string;
  title: string;
  abstract: string;
  store: string;
  workspace: string;
  kind: "vector" | "raster";
  source: VectorSource | RasterSource;
  extent: Extent | null;
  enabled: boolean;
  wfs_enabled: boolean;
  min_scale: number | null;
  max_scale: number | null;
  wms_url: string;
  ows_url: string; // the workspace's WMS/WFS endpoint
  styles: string[]; // assigned, default first; empty = built-in default
  warnings?: string[];
  created_at: string;
  updated_at: string;
}

export interface LayerInput {
  name: string;
  title?: string;
  abstract?: string;
  store: string;
  schema?: string;
  table?: string;
  geom_column?: string;
  pk?: string;
  layer_type?: string;
  srid?: number; // declares the CRS of a geometry column without SRID
  path?: string;
  enabled?: boolean;
  wfs_enabled?: boolean;
}

export interface LayerUpdate {
  title?: string;
  abstract?: string;
  enabled?: boolean;
  wfs_enabled?: boolean;
  min_scale?: number | null;
  max_scale?: number | null;
  srid?: number; // EPSG override; 0 = the source's own
}

export interface HealthCheck {
  ok: boolean;
  latency_ms: number;
  error?: string;
  detail?: string;
}

export interface Health {
  status: "ok" | "degraded";
  version?: string;
  checks: Record<string, HealthCheck>;
  stores?: Record<string, HealthCheck>;
}

export interface MapfileVersion {
  id: number;
  created_at: string;
  sha256: string;
  reason: string;
  content?: string;
}

export interface InspectResult {
  kind: "vector" | "raster";
  source: VectorSource | RasterSource;
  extent: Extent | null;
  warnings?: string[];
}

export type StyleFormat = "sld" | "json";

export interface StyleInfo {
  name: string;
  format: StyleFormat;
  target: "vector" | "raster";
  warnings: string[];
  used_by: string[];
  created_at: string;
  updated_at: string;
}

export interface StyleValidation {
  format: StyleFormat;
  target: "vector" | "raster";
  warnings: string[];
  compiled: string;
  used_icons: string[];
}

export interface LayerStyles {
  default: string;
  alternates: string[];
}

export interface Icon {
  name: string;
  mime: string;
  width: number;
  height: number;
  sha256: string;
  symbol: string;
  used_by: string[];
  created_at: string;
}

/** JSON style shortcut (PLAN.md §6.5); mirrors internal/sld/jsonstyle.go. */
export interface JsonRule {
  name?: string;
  filter?: string;
  else?: boolean;
  min_scale?: number;
  max_scale?: number;
  symbol?: "point" | "line" | "polygon";
  icon?: string;
  mark?: string;
  size?: number;
  size_attr?: string;
  rotation?: number;
  rotation_attr?: string;
  opacity?: number;
  offset_x?: number;
  offset_y?: number;
  fill?: string;
  fill_opacity?: number;
  stroke?: string;
  stroke_width?: number;
  stroke_opacity?: number;
  dash?: number[];
  line_cap?: "butt" | "round" | "square";
  line_join?: "round" | "miter" | "bevel";
  offset?: number;
  casing_color?: string;
  casing_width?: number;
  marker?: string;
  marker_size?: number;
  marker_spacing?: number;
  marker_color?: string;
  fill_pattern?: "solid" | "hatch" | "cross" | "dots";
  pattern_spacing?: number;
  pattern_angle?: number;
  pattern_width?: number;
  pattern_background?: string;
  label?: string;
  font_family?: string;
  font_size?: number;
  font_weight?: string;
  font_style?: "normal" | "italic";
  label_color?: string;
  halo_color?: string;
  halo_radius?: number;
  label_position?: LabelPosition;
  label_offset_x?: number;
  label_offset_y?: number;
  label_angle?: number;
  label_follow_line?: boolean;
  label_allow_overlap?: boolean;
  label_repeat?: number;
  label_priority?: number;
}

export type LabelPosition = "auto" | "center" | "n" | "ne" | "e" | "se" | "s" | "sw" | "w" | "nw";

export interface SRIDCandidate {
  srid: number;
  name: string;
  area: string; // area of use
  lon: number; // where the middle of the data would be with this CRS
  lat: number;
  used_in_database: boolean;
}

export interface LayerAttribute {
  name: string;
  type: string;
  numeric: boolean;
}

export interface AttributeStats extends LayerAttribute {
  count: number;
  distinct: number;
  values: { value: string | null; count: number }[];
  min?: number;
  max?: number;
  quantiles?: number[];
}

export interface JsonColorEntry {
  quantity: number;
  color: string;
  opacity?: number;
  label?: string;
}

export interface JsonStyle {
  target: "vector" | "raster";
  rules?: JsonRule[];
  colormap?: { type: "ramp" | "intervals" | "values"; entries: JsonColorEntry[] };
  opacity?: number;
}
