package pptx

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// The functions below write the XML parts of a presentation. Each one produces
// exactly the bytes of the text/template file in templates/ that it replaced,
// including the indented blank lines that the templates' {{range}}, {{if}} and
// {{end}} lines left in the output; parts_test.go checks this against the
// templates. Strings go through escapeXML. Integers are written in decimal,
// which never needs escaping.

// contentTypesXML writes [Content_Types].xml, the media type of every part.
func contentTypesXML(c ContentTypesXmlContent) []byte {
	b := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
    <Default
        Extension="jpeg" ContentType="image/jpeg" />
        <Default Extension="png" ContentType="image/png" />
        <Default
        Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml" />
        <Default
        Extension="xml" ContentType="application/xml" />
        <Override PartName="/docProps/app.xml"
        ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml" />
        <Override
        PartName="/docProps/core.xml"
        ContentType="application/vnd.openxmlformats-package.core-properties+xml" />
        <Override
        PartName="/ppt/presProps.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.presProps+xml" />
        <Override
        PartName="/ppt/presentation.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout1.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout10.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout11.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout2.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout3.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout4.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout5.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout6.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout7.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout8.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideLayouts/slideLayout9.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideLayout+xml" />
        <Override
        PartName="/ppt/slideMasters/slideMaster1.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.slideMaster+xml" />
        `)
	for _, name := range c.FileNames {
		b = fmt.Appendf(b, `
        <Override PartName="/ppt/slides/%s.xml" ContentType="application/vnd.openxmlformats-officedocument.presentationml.slide+xml" />
        `, escapeXML(name))
	}
	// The template had a space after its {{end}}.
	return append(b, " \n"+`        <Override PartName="/ppt/tableStyles.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.tableStyles+xml" />
        <Override
        PartName="/ppt/theme/theme1.xml"
        ContentType="application/vnd.openxmlformats-officedocument.theme+xml" />
        <Override
        PartName="/ppt/viewProps.xml"
        ContentType="application/vnd.openxmlformats-officedocument.presentationml.viewProps+xml" />
</Types>
`...)
}

// slideRelsXML writes ppt/slides/_rels/slideN.xml.rels: the slide's layout,
// its image, and the targets of its links.
func slideRelsXML(c RelsSlideXmlContent) []byte {
	b := fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
    <Relationship Id="rId1"
        Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout"
        Target="../slideLayouts/slideLayout7.xml" />
    <Relationship Id="%s"
        Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image"
        Target="../media/%s.png" />
    `, escapeXML(c.RelationshipID), escapeXML(c.FileName))
	for _, link := range c.Links {
		b = append(b, "\n    "...)
		if link.ExternalUrl != "" {
			b = fmt.Appendf(b, `
    <Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External" />
    `, escapeXML(link.RelationshipID), escapeXML(link.ExternalUrl))
		} else {
			b = fmt.Appendf(b, `
    <Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slide%d.xml" />
    `, escapeXML(link.RelationshipID), link.SlideIndex)
		}
		b = append(b, "\n    "...)
	}
	return append(b, "\n</Relationships>\n"...)
}

// slideXML writes ppt/slides/slideN.xml: the board's image, the navigation
// header when the presentation has one, and a transparent rectangle over each
// link.
func slideXML(c SlideXmlContent) []byte {
	description := escapeXML(c.Description)
	b := fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
    xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
    xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
    <p:cSld>
        <p:spTree>
            <p:nvGrpSpPr>
                <p:cNvPr id="1" name="" />
                <p:cNvGrpSpPr />
                <p:nvPr />
            </p:nvGrpSpPr>
            <p:grpSpPr>
                <a:xfrm>
                    <a:off x="0" y="0" />
                    <a:ext cx="0" cy="0" />
                    <a:chOff x="0" y="0" />
                    <a:chExt cx="0" cy="0" />
                </a:xfrm>
            </p:grpSpPr>
            <p:pic>
                <p:nvPicPr>
                    <p:cNvPr id="2" name="%s" descr="%s" />
                    <p:cNvPicPr>
                        <a:picLocks noChangeAspect="1" />
                    </p:cNvPicPr>
                    <p:nvPr />
                </p:nvPicPr>
                <p:blipFill>
                    <a:blip r:embed="%s" />
                    <a:stretch>
                        <a:fillRect />
                    </a:stretch>
                </p:blipFill>
                <p:spPr>
                    <a:xfrm>
                        <a:off x="%d" y="%d" />
                        <a:ext cx="%d" cy="%d" />
                    </a:xfrm>
                    <a:prstGeom prst="rect">
                        <a:avLst />
                    </a:prstGeom>
                </p:spPr>
            </p:pic>
            `, description, description, escapeXML(c.ImageID), c.ImageLeft, c.ImageTop, c.ImageWidth, c.ImageHeight)
	if c.HeaderHeight > 0 {
		b = fmt.Appendf(b, `
            <p:sp>
                <p:nvSpPr>
                    <p:cNvPr id="95" name="%s" />
                    <p:cNvSpPr txBox="1" />
                    <p:nvPr />
                </p:nvSpPr>
                <p:spPr>
                    <a:xfrm>
                        <a:off x="4001" y="6239" />
                        <a:ext cx="9135998" cy="%d" />
                    </a:xfrm>
                    <a:prstGeom prst="rect">
                        <a:avLst />
                    </a:prstGeom>
                    <a:ln w="12700">
                        <a:miter lim="400000" />
                    </a:ln>
                    <a:extLst>
                        <a:ext uri="{C572A759-6A51-4108-AA02-DFA0A04FC94B}">
                            <ma14:wrappingTextBoxFlag
                                xmlns:ma14="http://schemas.microsoft.com/office/mac/drawingml/2011/main"
                                xmlns="" val="1" />
                        </a:ext>
                    </a:extLst>
                </p:spPr>
                <p:txBody>
                    <a:bodyPr lIns="45719" rIns="45719">
                        <a:spAutoFit />
                    </a:bodyPr>
                    <a:lstStyle>
                        <a:lvl1pPr>
                            <a:defRPr sz="2400" />
                        </a:lvl1pPr>
                    </a:lstStyle>
                    <a:p>
                        `, description, c.HeaderHeight)
		for _, prefix := range c.TitlePrefix {
			b = fmt.Appendf(b, `
                        <a:r>
                            <a:rPr>
                                <a:hlinkClick r:id="%s" invalidUrl=""
                                    action="ppaction://hlinksldjump" tgtFrame="" tooltip=""
                                    history="1" highlightClick="0" endSnd="0" />
                            </a:rPr>
                            <a:t>%s</a:t>
                        </a:r>
                        <a:r><a:t>  /  </a:t></a:r>
                        `, escapeXML(prefix.RelationshipID), escapeXML(prefix.Name))
		}
		b = fmt.Appendf(b, `
                        <a:r>
                            <a:rPr b="1" />
                            <a:t>%s</a:t>
                        </a:r>
                    </a:p>
                </p:txBody>
            </p:sp>
            `, escapeXML(c.Title))
	}
	b = append(b, "\n            "...)
	for _, link := range c.Links {
		name := escapeXML(link.Name)
		b = fmt.Appendf(b, `
            <p:sp>
                <p:nvSpPr>
                    <p:cNvPr id="%d" name="%s">
                        <a:hlinkClick r:id="%s" action="%s" tooltip="%s" history="1" invalidUrl=""
                            tgtFrame="" highlightClick="0" endSnd="0" />
                    </p:cNvPr>
                    <p:cNvSpPr />
                    <p:nvPr />
                </p:nvSpPr>
                <p:spPr>
                    <a:xfrm>
                        <a:off x="%d" y="%d" />
                        <a:ext cx="%d" cy="%d" />
                    </a:xfrm>
                    <a:prstGeom prst="rect">
                        <a:avLst />
                    </a:prstGeom>
                    <a:solidFill>
                        <a:srgbClr val="FFFFFF">
                            <a:alpha val="0" />
                        </a:srgbClr>
                    </a:solidFill>
                    <a:ln w="12700">
                        <a:miter lim="400000" />
                    </a:ln>
                </p:spPr>
            </p:sp>
            `, link.ID, name, escapeXML(link.RelationshipID), escapeXML(link.Action), name,
			link.Left, link.Top, link.Width, link.Height)
	}
	return append(b, `
        </p:spTree>
    </p:cSld>
    <p:clrMapOvr>
        <a:masterClrMapping />
    </p:clrMapOvr>
</p:sld>
`...)
}

// presentationRelsXML writes ppt/_rels/presentation.xml.rels: the
// presentation's properties, theme, master and slides.
func presentationRelsXML(c RelsPresentationXmlContent) []byte {
	b := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId3"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/presProps"
    Target="presProps.xml" />
<Relationship Id="rId4"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/viewProps"
    Target="viewProps.xml" />
<Relationship Id="rId5"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme"
    Target="theme/theme1.xml" />
<Relationship Id="rId6"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/tableStyles"
    Target="tableStyles.xml" />
<Relationship Id="rId1"
    Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideMaster"
    Target="slideMasters/slideMaster1.xml" />
    `)
	for _, slide := range c.Slides {
		b = fmt.Appendf(b, `
    <Relationship Id="%s" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/%s.xml" />
    `, escapeXML(slide.RelationshipID), escapeXML(slide.FileName))
	}
	return append(b, "\n</Relationships>\n"...)
}

// presentationXML writes ppt/presentation.xml: the slide list, the slide size
// and the default text style.
func presentationXML(c PresentationXmlContent) []byte {
	b := []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<p:presentation xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
    xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
    xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" saveSubsetFonts="1"
    autoCompressPictures="0">
    <p:sldMasterIdLst>
        <p:sldMasterId id="2147483648" r:id="rId1" />
    </p:sldMasterIdLst>
    <p:sldIdLst>
        `)
	for _, slide := range c.Slides {
		b = fmt.Appendf(b, `
        <p:sldId id="%d" r:id="%s" />
        `, slide.ID, escapeXML(slide.RelationshipID))
	}
	return fmt.Appendf(b, `
    </p:sldIdLst>
    <p:sldSz cx="%d" cy="%d" type="screen16x9" />
    <p:notesSz cx="6858000" cy="9144000" />
    <p:defaultTextStyle>
        <a:defPPr>
            <a:defRPr lang="en-US" />
        </a:defPPr>
        <a:lvl1pPr marL="0" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl1pPr>
        <a:lvl2pPr marL="457200" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl2pPr>
        <a:lvl3pPr marL="914400" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl3pPr>
        <a:lvl4pPr marL="1371600" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl4pPr>
        <a:lvl5pPr marL="1828800" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl5pPr>
        <a:lvl6pPr marL="2286000" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl6pPr>
        <a:lvl7pPr marL="2743200" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl7pPr>
        <a:lvl8pPr marL="3200400" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl8pPr>
        <a:lvl9pPr marL="3657600" algn="l" defTabSz="457200" rtl="0" eaLnBrk="1" latinLnBrk="0"
            hangingPunct="1">
            <a:defRPr sz="1800" kern="1200">
                <a:solidFill>
                    <a:schemeClr val="tx1" />
                </a:solidFill>
                <a:latin typeface="+mn-lt" />
                <a:ea typeface="+mn-ea" />
                <a:cs typeface="+mn-cs" />
            </a:defRPr>
        </a:lvl9pPr>
    </p:defaultTextStyle>
    <p:extLst>
        <p:ext uri="{EFAFB233-063F-42B5-8137-9DF3F51BA10A}">
            <p15:sldGuideLst xmlns:p15="http://schemas.microsoft.com/office/powerpoint/2012/main">
                <p15:guide id="1" orient="horz" pos="2160">
                    <p15:clr>
                        <a:srgbClr val="A4A3A4" />
                    </p15:clr>
                </p15:guide>
                <p15:guide id="2" pos="2880">
                    <p15:clr>
                        <a:srgbClr val="A4A3A4" />
                    </p15:clr>
                </p15:guide>
            </p15:sldGuideLst>
        </p:ext>
    </p:extLst>
</p:presentation>
`, c.SlideWidth, c.SlideHeight)
}

// coreXML writes docProps/core.xml, the document's title, subject and author.
func coreXML(c CoreXmlContent) []byte {
	return fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties
    xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties"
    xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/"
    xmlns:dcmitype="http://purl.org/dc/dcmitype/"
    xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
    <dc:title>%s</dc:title>
    <dc:subject>%s</dc:subject>
    <dc:creator>%s</dc:creator>
    <cp:keywords />
    <dc:description>%s</dc:description>
    <cp:lastModifiedBy>%s</cp:lastModifiedBy>
    <cp:revision>1</cp:revision>
    <cp:category />
</cp:coreProperties>
`, escapeXML(c.Title), escapeXML(c.Subject), escapeXML(c.Creator), escapeXML(c.Description),
		escapeXML(c.LastModifiedBy))
}

// appXML writes docProps/app.xml: the application, the slide count and the
// slide titles.
func appXML(c AppXmlContent) []byte {
	b := fmt.Appendf(nil, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"
    xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">
    <TotalTime>1</TotalTime>
    <Words>0</Words>
    <Application>D2</Application>
    <PresentationFormat>On-screen Show (16:9)</PresentationFormat>
    <Paragraphs>0</Paragraphs>
    <Slides>%d</Slides>
    <Notes>0</Notes>
    <HiddenSlides>0</HiddenSlides>
    <MMClips>0</MMClips>
    <ScaleCrop>false</ScaleCrop>
    <HeadingPairs>
        <vt:vector size="6" baseType="variant">
            <vt:variant>
                <vt:lpstr>Fonts</vt:lpstr>
            </vt:variant>
            <vt:variant>
                <vt:i4>2</vt:i4>
            </vt:variant>
            <vt:variant>
                <vt:lpstr>Theme</vt:lpstr>
            </vt:variant>
            <vt:variant>
                <vt:i4>1</vt:i4>
            </vt:variant>
            <vt:variant>
                <vt:lpstr>Slide Titles</vt:lpstr>
            </vt:variant>
            <vt:variant>
                <vt:i4>%d</vt:i4>
            </vt:variant>
        </vt:vector>
    </HeadingPairs>
    <TitlesOfParts>
        <vt:vector size="%d" baseType="lpstr">
            <vt:lpstr>Arial</vt:lpstr>
            <vt:lpstr>Calibri</vt:lpstr>
            <vt:lpstr>Office Theme</vt:lpstr>
            `, c.SlideCount, c.SlideCount, c.TitlesOfPartsCount)
	for _, title := range c.Titles {
		b = fmt.Appendf(b, `
            <vt:lpstr>%s</vt:lpstr>
            `, escapeXML(title))
	}
	return fmt.Appendf(b, `
        </vt:vector>
    </TitlesOfParts>
    <LinksUpToDate>false</LinksUpToDate>
    <SharedDoc>false</SharedDoc>
    <HyperlinkBase></HyperlinkBase>
    <HyperlinksChanged>false</HyperlinksChanged>
    <AppVersion>%s</AppVersion>
</Properties>
`, escapeXML(c.D2Version))
}

// escapeXML escapes s for XML character data and attribute values.
// xml.EscapeText also replaces invalid UTF-8 and characters outside XML's
// character range with U+FFFD.
func escapeXML(s string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(s)) // a strings.Builder never fails
	return escaped.String()
}
