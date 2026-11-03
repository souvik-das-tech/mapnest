<?xml version="1.0" encoding="UTF-8"?>
<!-- Dashed lines with a scale range and a casing. Layer: rivers -->
<StyledLayerDescriptor version="1.0.0"
    xmlns="http://www.opengis.net/sld"
    xmlns:ogc="http://www.opengis.net/ogc">
  <NamedLayer>
    <Name>rivers</Name>
    <UserStyle>
      <Name>rivers_dashed</Name>
      <FeatureTypeStyle>
        <Rule>
          <Title>Long rivers</Title>
          <ogc:Filter>
            <ogc:PropertyIsGreaterThan>
              <ogc:PropertyName>length_km</ogc:PropertyName>
              <ogc:Literal>1400</ogc:Literal>
            </ogc:PropertyIsGreaterThan>
          </ogc:Filter>
          <LineSymbolizer>
            <Stroke>
              <CssParameter name="stroke">#BFDBFE</CssParameter>
              <CssParameter name="stroke-width">6</CssParameter>
              <CssParameter name="stroke-linecap">round</CssParameter>
            </Stroke>
          </LineSymbolizer>
          <LineSymbolizer>
            <Stroke>
              <CssParameter name="stroke">#1D4ED8</CssParameter>
              <CssParameter name="stroke-width">2.5</CssParameter>
              <CssParameter name="stroke-linecap">round</CssParameter>
              <CssParameter name="stroke-linejoin">round</CssParameter>
            </Stroke>
          </LineSymbolizer>
        </Rule>
        <Rule>
          <Title>Other rivers</Title>
          <ElseFilter/>
          <MaxScaleDenominator>20000000</MaxScaleDenominator>
          <LineSymbolizer>
            <Stroke>
              <CssParameter name="stroke">#3B82F6</CssParameter>
              <CssParameter name="stroke-width">1.5</CssParameter>
              <CssParameter name="stroke-dasharray">6 3</CssParameter>
              <CssParameter name="stroke-opacity">0.8</CssParameter>
            </Stroke>
          </LineSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>
