# Styling

MapNest accepts two style formats:

- **SLD 1.0 / SE 1.1**: bring the files you already use in GeoServer.
- **JSON**: a short format for writing styles by hand or generating them in code.

Both are converted **once, at upload**, into MapServer `CLASS` blocks. Map
requests never parse a style, so styling has no per-request cost. The original
file is stored and returned byte for byte by `GET /styles/{name}`.

Why not MapServer's built-in per-request SLD? It parses the SLD on every request
and can make the server fetch URLs given by a client. MapNest disables that
(`wms_sld_enabled false`) and converts styles ahead of time instead, which is
faster, safer, and lets it tell you exactly what was not supported.

## Several styles per layer

Each style becomes a MapServer class group. A layer has a **default** style and
any number of **alternates**:

```bash
curl -sH "$H" -H 'Content-Type: application/json' -X PUT $API/layers/roads/styles \
  -d '{"default": "roads_by_class", "alternates": ["roads_simple", "roads_night"]}'
```

Clients choose with the standard WMS parameter `STYLES=roads_night`.
`GetLegendGraphic` works per style.

## SLD support

| SLD 1.0 / SE 1.1 | Becomes in MapServer |
|---|---|
| `Rule` | `CLASS` |
| `MinScaleDenominator` / `MaxScaleDenominator` | `MINSCALEDENOM` / `MAXSCALEDENOM` |
| `ogc:Filter`: `PropertyIsEqualTo`, `NotEqualTo`, `LessThan(OrEqualTo)`, `GreaterThan(OrEqualTo)`, `PropertyIsBetween`, `PropertyIsLike`, `PropertyIsNull`, `And`, `Or`, `Not` | `EXPRESSION (…)` |
| `ElseFilter` | last `CLASS` without an expression |
| `PointSymbolizer` + `Mark` (`circle`, `square`, `triangle`, `star`, `cross`, `x`) | `STYLE` with built-in symbols |
| `PointSymbolizer` + `ExternalGraphic` | pixmap symbol from an uploaded [icon](#icons) |
| `Size`, `Rotation`, `Opacity` | `SIZE`, `ANGLE`, `OPACITY` |
| `LineSymbolizer`: stroke, width, opacity, dash array, line cap, line join | `STYLE` with `COLOR`, `WIDTH`, `OPACITY`, `PATTERN`, `LINECAP`, `LINEJOIN` |
| `PolygonSymbolizer`: fill, fill-opacity, stroke | `STYLE` with `COLOR`, `OPACITY`, `OUTLINECOLOR`, `WIDTH` |
| `TextSymbolizer`: label, font family/size/weight, halo, fill, point offset, line placement | `LABEL` with `TEXT`, `FONT`, `SIZE`, `OUTLINECOLOR`, `COLOR`, `OFFSET`, `ANGLE FOLLOW` |
| `RasterSymbolizer` + `ColorMap type="ramp"` | `COLORRANGE` / `DATARANGE` classes |
| `ColorMap type="intervals"` / `"values"` | one class per entry on `[pixel]` |
| `RasterSymbolizer` `Opacity` | layer `OPACITY` |

Anything else (GeoServer vendor options, rendering transformations, …) is **not
silently dropped**: the upload succeeds and lists it in `warnings`, or fails with
`422` if the style would render nothing. Check before saving with
`POST /styles/validate?layer=<layer>`, and see the generated classes with
`GET /styles/{name}/compiled`.

Fonts: labels use the DejaVu fonts shipped in the image; SLD font families are
mapped to the closest one.

## JSON styles

### Vector

```json
{
  "target": "vector",
  "rules": [
    { "name": "Megacities", "filter": "population >= 10000000",
      "icon": "capital.png", "size": 24,
      "label": "name", "font_size": 11, "halo_color": "#ffffff" },
    { "name": "State capitals", "filter": "is_capital = 'Y'",
      "mark": "star", "fill": "#f97316", "stroke": "#7c2d12", "size": 14 },
    { "name": "Other", "else": true, "mark": "circle", "fill": "#3366cc", "size": 8 }
  ]
}
```

Rules are evaluated in order; the first match wins. A rule has either a `filter`
or `"else": true`.

**Filters** use a small CQL subset:

```
type = 'school' AND pop >= 1000
name LIKE 'Ko%' OR NOT (kind = 'x')
height BETWEEN 10 AND 20
code IS NULL      code IS NOT NULL
```

(`IN` is not supported; use `OR`.)

**Rule fields**

| Group | Fields |
|---|---|
| Rule | `name`, `filter`, `else`, `min_scale`, `max_scale`, `symbol` (`point` / `line` / `polygon`, inferred when empty) |
| Points | `icon` (uploaded PNG) or `mark` (`circle`, `square`, `triangle`, `star`, `cross`, `x`), `size` or `size_attr` (from a numeric column), `rotation` or `rotation_attr`, `opacity`, `offset_x`, `offset_y` |
| Lines | `stroke`, `stroke_width`, `stroke_opacity`, `dash` (e.g. `[6, 3]`), `line_cap` (`butt`/`round`/`square`), `line_join` (`round`/`miter`/`bevel`), `offset`, `casing_color` + `casing_width` (road-style outline), `marker`, `marker_size`, `marker_spacing`, `marker_color` (repeated marks, e.g. direction arrows) |
| Polygons | `fill`, `fill_opacity`, `stroke`, `stroke_width`, `fill_pattern` (`solid`, `hatch`, `cross`, `dots`), `pattern_spacing`, `pattern_angle`, `pattern_width`, `pattern_background` |
| Labels | `label` (a column, or text with `[column]` bindings), `font_family`, `font_size`, `font_weight`, `font_style`, `label_color`, `halo_color`, `halo_radius`, `label_position` (`auto`, `center`, `n`, `ne`, `e`, `se`, `s`, `sw`, `w`, `nw`), `label_offset_x`, `label_offset_y`, `label_angle`, `label_follow_line`, `label_allow_overlap`, `label_repeat`, `label_priority` |

Colours are `#rrggbb` or `#rgb`.

### Raster

```json
{
  "target": "raster",
  "opacity": 0.8,
  "colormap": {
    "type": "ramp",
    "entries": [
      { "quantity": 0,    "color": "#2b83ba", "label": "0 m" },
      { "quantity": 500,  "color": "#ffffbf" },
      { "quantity": 1500, "color": "#d7191c", "label": "1500 m" }
    ]
  }
}
```

`type` is `ramp` (smooth gradient), `intervals` (classes up to each quantity) or
`values` (exact pixel values, e.g. land-cover codes). Each entry may have an
`opacity`.

## Classified styles from the data

Build graduated or categorised styles from real values:

```bash
# most frequent values of a text column
curl -sH "$H" "$API/layers/landuse/attributes/class?limit=20"
# min, max and 5 quantile classes of a numeric column
curl -sH "$H" "$API/layers/districts/attributes/population?classes=5"
```

Turn the breaks into rules, `POST` the style, and preview it without saving:

```bash
curl -sH "$H" -H 'Content-Type: application/json' -X POST \
  "$API/layers/districts/preview?width=800" --data-binary @districts.json -o preview.png
```

The Admin UI's style editor does the same with a live map.

## Icons

Upload PNGs (≤ 2 MB, ≤ 512×512 px) with `POST /icons`, then reference them:

- SLD: `<ExternalGraphic><OnlineResource xlink:href="school.png"/>…` (also
  `icons/school.png` or a URL ending in `/icons/school.png`, so GeoServer SLDs
  work unchanged)
- JSON: `"icon": "school.png"`

An SLD that references an icon you have not uploaded is refused with `422`
naming the missing file. Sample SLDs and icons are in [`dev/`](../dev).
