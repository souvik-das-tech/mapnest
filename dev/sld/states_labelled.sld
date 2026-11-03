<?xml version="1.0" encoding="UTF-8"?>
<!-- SE 1.1 (se: namespace, SvgParameter): polygons coloured by region, with
     labels and one GeoServer vendor option (reported as a warning). Layer: states -->
<StyledLayerDescriptor version="1.1.0"
    xmlns="http://www.opengis.net/sld"
    xmlns:se="http://www.opengis.net/se"
    xmlns:ogc="http://www.opengis.net/ogc">
  <NamedLayer>
    <se:Name>states</se:Name>
    <UserStyle>
      <se:Name>states_labelled</se:Name>
      <se:FeatureTypeStyle>
        <se:Rule>
          <se:Name>east</se:Name>
          <se:Description><se:Title>East</se:Title></se:Description>
          <ogc:Filter>
            <ogc:PropertyIsEqualTo>
              <ogc:PropertyName>region</ogc:PropertyName>
              <ogc:Literal>east</ogc:Literal>
            </ogc:PropertyIsEqualTo>
          </ogc:Filter>
          <se:PolygonSymbolizer>
            <se:Fill>
              <se:SvgParameter name="fill">#FDE68A</se:SvgParameter>
              <se:SvgParameter name="fill-opacity">0.7</se:SvgParameter>
            </se:Fill>
            <se:Stroke>
              <se:SvgParameter name="stroke">#B45309</se:SvgParameter>
              <se:SvgParameter name="stroke-width">1</se:SvgParameter>
            </se:Stroke>
          </se:PolygonSymbolizer>
          <se:TextSymbolizer>
            <se:Label><ogc:PropertyName>name</ogc:PropertyName></se:Label>
            <se:Font>
              <se:SvgParameter name="font-family">DejaVu Sans</se:SvgParameter>
              <se:SvgParameter name="font-size">12</se:SvgParameter>
            </se:Font>
            <se:LabelPlacement>
              <se:PointPlacement>
                <se:AnchorPoint><se:AnchorPointX>0.5</se:AnchorPointX><se:AnchorPointY>0.5</se:AnchorPointY></se:AnchorPoint>
              </se:PointPlacement>
            </se:LabelPlacement>
            <se:Halo>
              <se:Radius>1.5</se:Radius>
              <se:Fill><se:SvgParameter name="fill">#FFFFFF</se:SvgParameter></se:Fill>
            </se:Halo>
            <se:Fill><se:SvgParameter name="fill">#78350F</se:SvgParameter></se:Fill>
            <se:VendorOption name="maxDisplacement">40</se:VendorOption>
          </se:TextSymbolizer>
        </se:Rule>
        <se:Rule>
          <se:Name>other</se:Name>
          <se:Description><se:Title>Other regions</se:Title></se:Description>
          <se:ElseFilter/>
          <se:PolygonSymbolizer>
            <se:Fill><se:SvgParameter name="fill">#E5E7EB</se:SvgParameter></se:Fill>
            <se:Stroke>
              <se:SvgParameter name="stroke">#6B7280</se:SvgParameter>
              <se:SvgParameter name="stroke-width">0.8</se:SvgParameter>
              <se:SvgParameter name="stroke-dasharray">4 2</se:SvgParameter>
            </se:Stroke>
          </se:PolygonSymbolizer>
          <se:TextSymbolizer>
            <se:Label><ogc:PropertyName>name</ogc:PropertyName></se:Label>
            <se:Font>
              <se:SvgParameter name="font-family">Serif</se:SvgParameter>
              <se:SvgParameter name="font-size">10</se:SvgParameter>
              <se:SvgParameter name="font-style">italic</se:SvgParameter>
            </se:Font>
            <se:Fill><se:SvgParameter name="fill">#374151</se:SvgParameter></se:Fill>
          </se:TextSymbolizer>
        </se:Rule>
      </se:FeatureTypeStyle>
    </UserStyle>
  </NamedLayer>
</StyledLayerDescriptor>
