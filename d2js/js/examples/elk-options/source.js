// Both comparison panes use the same theme, graph and renderer settings.
export function diagramSource(example, value, diagram = example.source, target = example.target) {
  const setting = example.target ? "" : `    data: {${example.option}: ${JSON.stringify(value)}}\n`;
  const constraint = example.target ? `\n${target}.${example.option}: ${value}\n` : "";
  return `vars: {
  d2-config: {
    layout-engine: elk
    theme-overrides: {
      B1: "#2c574d"
      B6: "#e9f2ec"
      N1: "#183b35"
    }
${setting}  }
}

style.fill: transparent
*.style.border-radius: 6

${diagram}${constraint}`;
}
