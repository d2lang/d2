import { D2 } from "../../dist/browser/index.js";

const d2 = new D2();

export async function renderDiagram(source) {
  const { diagram, renderOptions } = await d2.compile(source, { layout: "elk", pad: 10 });
  return d2.render(diagram, renderOptions);
}
