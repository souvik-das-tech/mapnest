// Starting points for the SLD editor: plain SLD 1.0, as GeoServer accepts it.

const wrap = (name: string, rules: string) => `<?xml version="1.0" encoding="UTF-8"?>
<StyledLayerDescriptor version="1.0.0"
    xmlns="http://www.opengis.net/sld"
    xmlns:ogc="http://www.opengis.net/ogc"
    xmlns:xlink="http://www.w3.org/1999/xlink">
  <NamedLayer>
    <Name>${name}</Name>
    <UserStyle>
      <Name>${name}</Name>
      <FeatureTypeStyle>
${rules}
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>
`;

export const SLD_TEMPLATES: { id: string; label: string; sld: string }[] = [
  {
    id: "point",
    label: "Point: circle",
    sld: wrap(
      "points",
      `        <Rule>
          <PointSymbolizer>
            <Graphic>
              <Mark>
                <WellKnownName>circle</WellKnownName>
                <Fill><CssParameter name="fill">#2563eb</CssParameter></Fill>
                <Stroke>
                  <CssParameter name="stroke">#ffffff</CssParameter>
                  <CssParameter name="stroke-width">1</CssParameter>
                </Stroke>
              </Mark>
              <Size>10</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>`,
    ),
  },
  {
    id: "icon",
    label: "Point: icon",
    sld: wrap(
      "icon_points",
      `        <Rule>
          <PointSymbolizer>
            <Graphic>
              <ExternalGraphic>
                <!-- Copy the icon file name from the Icons page -->
                <OnlineResource xlink:type="simple" xlink:href="school.png"/>
                <Format>image/png</Format>
              </ExternalGraphic>
              <Size>24</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>`,
    ),
  },
  {
    id: "category",
    label: "Point: icon per value",
    sld: wrap(
      "by_type",
      `        <Rule>
          <Title>School</Title>
          <ogc:Filter>
            <ogc:PropertyIsEqualTo>
              <ogc:PropertyName>type</ogc:PropertyName>
              <ogc:Literal>school</ogc:Literal>
            </ogc:PropertyIsEqualTo>
          </ogc:Filter>
          <PointSymbolizer>
            <Graphic>
              <ExternalGraphic>
                <OnlineResource xlink:type="simple" xlink:href="school.png"/>
                <Format>image/png</Format>
              </ExternalGraphic>
              <Size>24</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>
        <Rule>
          <Title>Large (population &gt;= 100000)</Title>
          <ogc:Filter>
            <ogc:PropertyIsGreaterThanOrEqualTo>
              <ogc:PropertyName>population</ogc:PropertyName>
              <ogc:Literal>100000</ogc:Literal>
            </ogc:PropertyIsGreaterThanOrEqualTo>
          </ogc:Filter>
          <PointSymbolizer>
            <Graphic>
              <Mark>
                <WellKnownName>star</WellKnownName>
                <Fill><CssParameter name="fill">#dc2626</CssParameter></Fill>
              </Mark>
              <Size>18</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>
        <Rule>
          <Title>Everything else</Title>
          <ElseFilter/>
          <PointSymbolizer>
            <Graphic>
              <Mark>
                <WellKnownName>circle</WellKnownName>
                <Fill><CssParameter name="fill">#6b7280</CssParameter></Fill>
              </Mark>
              <Size>8</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>`,
    ),
  },
  {
    id: "line",
    label: "Line: dashed",
    sld: wrap(
      "lines",
      `        <Rule>
          <LineSymbolizer>
            <Stroke>
              <CssParameter name="stroke">#2563eb</CssParameter>
              <CssParameter name="stroke-width">2</CssParameter>
              <!-- dash, gap (px); e.g. "8 4 2 4" for dash-dot -->
              <CssParameter name="stroke-dasharray">8 4</CssParameter>
              <CssParameter name="stroke-linecap">round</CssParameter>
            </Stroke>
          </LineSymbolizer>
        </Rule>`,
    ),
  },
  {
    id: "polygon",
    label: "Polygon: fill + outline + label",
    sld: wrap(
      "areas",
      `        <Rule>
          <PolygonSymbolizer>
            <Fill>
              <CssParameter name="fill">#93c5fd</CssParameter>
              <CssParameter name="fill-opacity">0.6</CssParameter>
            </Fill>
            <Stroke>
              <CssParameter name="stroke">#1d4ed8</CssParameter>
              <CssParameter name="stroke-width">1</CssParameter>
            </Stroke>
          </PolygonSymbolizer>
          <TextSymbolizer>
            <Label><ogc:PropertyName>name</ogc:PropertyName></Label>
            <Font>
              <CssParameter name="font-family">Sans</CssParameter>
              <CssParameter name="font-size">11</CssParameter>
            </Font>
            <Halo>
              <Radius>1.5</Radius>
              <Fill><CssParameter name="fill">#ffffff</CssParameter></Fill>
            </Halo>
            <Fill><CssParameter name="fill">#111827</CssParameter></Fill>
          </TextSymbolizer>
        </Rule>`,
    ),
  },
  {
    id: "raster",
    label: "Raster: colour ramp",
    sld: wrap(
      "raster_ramp",
      `        <Rule>
          <RasterSymbolizer>
            <Opacity>1</Opacity>
            <ColorMap type="ramp">
              <ColorMapEntry color="#2b83ba" quantity="0" label="low"/>
              <ColorMapEntry color="#ffffbf" quantity="50"/>
              <ColorMapEntry color="#d7191c" quantity="100" label="high"/>
            </ColorMap>
          </RasterSymbolizer>
        </Rule>`,
    ),
  },
];
