<?xml version="1.0" encoding="UTF-8"?>
<!-- Filtered marks with labels: size classes by population, state filter. Layer: cities -->
<StyledLayerDescriptor version="1.0.0"
    xmlns="http://www.opengis.net/sld"
    xmlns:ogc="http://www.opengis.net/ogc">
  <NamedLayer>
    <Name>cities</Name>
    <UserStyle>
      <Name>cities_filtered</Name>
      <FeatureTypeStyle>
        <Rule>
          <Title>Maharashtra</Title>
          <ogc:Filter>
            <ogc:PropertyIsEqualTo>
              <ogc:PropertyName>state</ogc:PropertyName>
              <ogc:Literal>Maharashtra</ogc:Literal>
            </ogc:PropertyIsEqualTo>
          </ogc:Filter>
          <PointSymbolizer>
            <Graphic>
              <Mark>
                <WellKnownName>star</WellKnownName>
                <Fill><CssParameter name="fill">#F97316</CssParameter></Fill>
                <Stroke><CssParameter name="stroke">#7C2D12</CssParameter></Stroke>
              </Mark>
              <Size>16</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>
        <Rule>
          <Title>10 – 20 million</Title>
          <ogc:Filter>
            <ogc:PropertyIsBetween>
              <ogc:PropertyName>population</ogc:PropertyName>
              <ogc:LowerBoundary><ogc:Literal>10000000</ogc:Literal></ogc:LowerBoundary>
              <ogc:UpperBoundary><ogc:Literal>20000000</ogc:Literal></ogc:UpperBoundary>
            </ogc:PropertyIsBetween>
          </ogc:Filter>
          <PointSymbolizer>
            <Graphic>
              <Mark>
                <WellKnownName>circle</WellKnownName>
                <Fill>
                  <CssParameter name="fill">#2563EB</CssParameter>
                  <CssParameter name="fill-opacity">0.8</CssParameter>
                </Fill>
                <Stroke>
                  <CssParameter name="stroke">#FFFFFF</CssParameter>
                  <CssParameter name="stroke-width">1.5</CssParameter>
                </Stroke>
              </Mark>
              <Size>14</Size>
            </Graphic>
          </PointSymbolizer>
          <TextSymbolizer>
            <Label><ogc:PropertyName>name</ogc:PropertyName></Label>
            <Font>
              <CssParameter name="font-family">Arial</CssParameter>
              <CssParameter name="font-size">11</CssParameter>
              <CssParameter name="font-weight">bold</CssParameter>
            </Font>
            <LabelPlacement>
              <PointPlacement>
                <AnchorPoint><AnchorPointX>0</AnchorPointX><AnchorPointY>0.5</AnchorPointY></AnchorPoint>
                <Displacement><DisplacementX>8</DisplacementX><DisplacementY>0</DisplacementY></Displacement>
              </PointPlacement>
            </LabelPlacement>
            <Halo>
              <Radius>2</Radius>
              <Fill><CssParameter name="fill">#FFFFFF</CssParameter></Fill>
            </Halo>
            <Fill><CssParameter name="fill">#111827</CssParameter></Fill>
          </TextSymbolizer>
        </Rule>
        <Rule>
          <Title>Names starting with K or L</Title>
          <ogc:Filter>
            <ogc:Or>
              <ogc:PropertyIsLike wildCard="*" singleChar="." escape="!">
                <ogc:PropertyName>name</ogc:PropertyName>
                <ogc:Literal>K*</ogc:Literal>
              </ogc:PropertyIsLike>
              <ogc:PropertyIsLike wildCard="*" singleChar="." escape="!">
                <ogc:PropertyName>name</ogc:PropertyName>
                <ogc:Literal>L*</ogc:Literal>
              </ogc:PropertyIsLike>
            </ogc:Or>
          </ogc:Filter>
          <PointSymbolizer>
            <Graphic>
              <Mark>
                <WellKnownName>triangle</WellKnownName>
                <Fill><CssParameter name="fill">#16A34A</CssParameter></Fill>
              </Mark>
              <Size>12</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>
        <Rule>
          <Title>Other</Title>
          <ElseFilter/>
          <PointSymbolizer>
            <Graphic>
              <Mark>
                <WellKnownName>circle</WellKnownName>
                <Fill><CssParameter name="fill">#94A3B8</CssParameter></Fill>
              </Mark>
              <Size>8</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>
