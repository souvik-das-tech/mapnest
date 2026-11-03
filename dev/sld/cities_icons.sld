<?xml version="1.0" encoding="UTF-8"?>
<!-- Icon points: big cities get capital.png, the rest city.png. Layer: cities -->
<StyledLayerDescriptor version="1.0.0"
    xmlns="http://www.opengis.net/sld"
    xmlns:ogc="http://www.opengis.net/ogc"
    xmlns:xlink="http://www.w3.org/1999/xlink">
  <NamedLayer>
    <Name>cities</Name>
    <UserStyle>
      <Name>cities_icons</Name>
      <FeatureTypeStyle>
        <Rule>
          <Title>Megacities</Title>
          <ogc:Filter>
            <ogc:PropertyIsGreaterThanOrEqualTo>
              <ogc:PropertyName>population</ogc:PropertyName>
              <ogc:Literal>10000000</ogc:Literal>
            </ogc:PropertyIsGreaterThanOrEqualTo>
          </ogc:Filter>
          <PointSymbolizer>
            <Graphic>
              <ExternalGraphic>
                <OnlineResource xlink:type="simple" xlink:href="http://geoserver.example.com/styles/icons/capital.png"/>
                <Format>image/png</Format>
              </ExternalGraphic>
              <Size>24</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>
        <Rule>
          <Title>Other cities</Title>
          <ElseFilter/>
          <PointSymbolizer>
            <Graphic>
              <ExternalGraphic>
                <OnlineResource xlink:type="simple" xlink:href="city.png"/>
                <Format>image/png</Format>
              </ExternalGraphic>
              <Size>16</Size>
            </Graphic>
          </PointSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>
