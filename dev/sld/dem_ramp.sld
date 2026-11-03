<?xml version="1.0" encoding="UTF-8"?>
<!-- DEM colour ramp (Kolkata: about 0–60 m). -9999 / below 0 transparent. Layer: dem -->
<StyledLayerDescriptor version="1.0.0"
    xmlns="http://www.opengis.net/sld"
    xmlns:ogc="http://www.opengis.net/ogc">
  <NamedLayer>
    <Name>dem</Name>
    <UserStyle>
      <Name>dem_ramp</Name>
      <FeatureTypeStyle>
        <Rule>
          <RasterSymbolizer>
            <Opacity>0.85</Opacity>
            <ColorMap type="ramp">
              <ColorMapEntry color="#000000" quantity="-9999" opacity="0" label="no data"/>
              <ColorMapEntry color="#2B83BA" quantity="0" label="0 m"/>
              <ColorMapEntry color="#ABDDA4" quantity="10" label="10 m"/>
              <ColorMapEntry color="#FFFFBF" quantity="20" label="20 m"/>
              <ColorMapEntry color="#FDAE61" quantity="35" label="35 m"/>
              <ColorMapEntry color="#D7191C" quantity="60" label="60 m"/>
            </ColorMap>
          </RasterSymbolizer>
        </Rule>
      </FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>
