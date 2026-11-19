import catalog from "./examples.js";
import { diagramSource } from "./source.js";
import { renderDiagram } from "./runtime.js";

const $ = (id) => document.getElementById(id);
const cases = new Map(catalog.map((c) => [c.id, c]));
const states = new Map();
const labels = ["Baseline", "Comparison"];
const groups = [...new Set(catalog.map((c) => c.group))];
const panes = [];
const liveCache = new Map();
let current,
  state,
  results = [],
  imageURLs = [],
  serial = 0;
let modalURL;
const dialog = $("dialog"),
  body = $("dialog-body");
function el(tag, cl, text) {
  const e = document.createElement(tag);
  if (cl) e.className = cl;
  if (text !== undefined) e.textContent = text;
  return e;
}
function link(text, href) {
  const a = el("a", "", text);
  a.href = href;
  a.target = "_blank";
  a.rel = "noopener";
  return a;
}
function button(text, fn) {
  const b = el("button", "", text);
  b.type = "button";
  b.addEventListener("click", fn);
  return b;
}
function download(blob, name) {
  const url = URL.createObjectURL(blob),
    a = el("a");
  a.href = url;
  a.download = name;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
}
async function copyText(text, b) {
  try {
    await navigator.clipboard.writeText(text);
    b.textContent = "Copied";
  } catch {
    const selection = getSelection();
    const pre = body.querySelector("pre");
    if (pre && selection) {
      const range = document.createRange();
      range.selectNodeContents(pre);
      selection.removeAllRanges();
      selection.addRange(range);
    }
    b.textContent = "Source selected";
  }
}
const svgBlob = (text) => new Blob([text], { type: "image/svg+xml;charset=utf-8" });
function valueLabel(c, i) {
  if (c.labels) return c.labels[i];
  const val = c.values[i];
  if (typeof val === "boolean") return val ? "On" : "Off";
  if (typeof val === "number") return val + " px";
  return String(val);
}
function newState(c) {
  return {
    source: c.source,
    draft: c.source,
    target: c.target,
    draftTarget: c.target,
    chosen: [0, 1],
    custom: [String(c.values[0]), String(c.values[1])],
  };
}
function navList(container, query, mobile = false) {
  const term = query.trim().toLowerCase();
  const frag = document.createDocumentFragment();
  let count = 0;
  for (const group of groups) {
    const matches = catalog.filter(
      (c) =>
        c.group === group &&
        [c.title, c.key, c.group, c.option].join(" ").toLowerCase().includes(term)
    );
    if (!matches.length) continue;
    const section = el("div", "navgroup");
    section.append(el("h3", "", group));
    for (const c of matches) {
      const a = el("a", "navitem", c.title);
      a.href = "#" + c.id;
      if (c.id === current.id) a.setAttribute("aria-current", "page");
      if (mobile) a.addEventListener("click", () => dialog.close());
      section.append(a);
      count++;
    }
    frag.append(section);
  }
  if (!count) frag.append(el("p", "empty-nav", "No matching options."));
  container.replaceChildren(frag);
}
function showDialog(title, wide = false) {
  $("dialog-title").textContent = title;
  body.replaceChildren();
  dialog.classList.toggle("wide", wide);
  if (!dialog.open) dialog.showModal();
}
$("dialog-close").onclick = () => dialog.close();
dialog.addEventListener("click", (event) => {
  if (event.target !== dialog) return;
  const r = dialog.getBoundingClientRect();
  if (
    event.clientX < r.left ||
    event.clientX > r.right ||
    event.clientY < r.top ||
    event.clientY > r.bottom
  )
    dialog.close();
});
dialog.addEventListener("close", () => {
  if (modalURL) URL.revokeObjectURL(modalURL);
  modalURL = null;
  body.replaceChildren();
});
$("browse").onclick = () => {
  showDialog("Browse ELK options");
  const box = el("label", "search modal-search"),
    inp = el("input"),
    nav = el("nav", "modal-nav");
  inp.type = "search";
  inp.placeholder = "Find an option";
  inp.setAttribute("aria-label", "Find a layout option");
  box.append(inp);
  body.append(box, nav);
  navList(nav, "", true);
  inp.oninput = () => navList(nav, inp.value, true);
  inp.focus();
};
$("filter").addEventListener("input", () => navList($("menu"), $("filter").value));
$("filter").addEventListener("keydown", (event) => {
  if (event.key === "ArrowDown" || event.key === "Enter") {
    const a = $("menu").querySelector("a");
    if (a) {
      event.preventDefault();
      if (event.key === "Enter") a.click();
      else a.focus();
    }
  }
});
function fullSource(c, s, index) {
  if (c.target && !s.target.trim())
    throw Error("Enter the D2 ID of the constrained shape.");
  const pick = s.chosen[index];
  let val = c.values[pick];
  if (pick === "custom") {
    const entered = s.custom[index].trim();
    if (!entered) throw Error("Enter a custom option value.");
    if (typeof c.values[0] === "number") {
      val = Number(entered);
      if (!Number.isFinite(val) || val < 0)
        throw Error("Enter a finite, nonnegative number.");
    } else val = entered;
  }
  return diagramSource(c, val, s.source, s.target);
}
async function live(source) {
  if (liveCache.has(source)) return liveCache.get(source);
  const result = { svg: await renderDiagram(source) };
  if (liveCache.size >= 30) liveCache.delete(liveCache.keys().next().value);
  liveCache.set(source, result);
  return result;
}
function geometry(svg) {
  const doc = new DOMParser().parseFromString(svg, "image/svg+xml");
  if (doc.documentElement.localName !== "svg")
    throw Error("D2 returned an unreadable SVG.");
  const box = doc.documentElement.getAttribute("viewBox").split(/\s+/).map(Number);
  const nodes = new Map(),
    edges = [];
  for (const g of doc.querySelectorAll("g[class]")) {
    let name;
    try {
      name = atob(g.getAttribute("class"));
    } catch {
      continue;
    }
    const rect = g.querySelector(":scope > g.shape > rect");
    const path = g.querySelector(":scope > path.connection");
    if (rect)
      nodes.set(
        name,
        Object.fromEntries(
          ["x", "y", "width", "height"].map((k) => [k, Number(rect.getAttribute(k))])
        )
      );
    if (path) edges.push({ name, d: path.getAttribute("d") });
  }
  const signature = [
    ...doc.querySelectorAll("path,rect,text,circle,ellipse,polygon,line,polyline"),
  ].map((n) => [
    n.localName,
    ...[
      "x",
      "y",
      "width",
      "height",
      "d",
      "cx",
      "cy",
      "r",
      "rx",
      "ry",
      "points",
      "transform",
    ].map((k) => n.getAttribute(k)),
  ]);
  return { w: box[2], h: box[3], nodes, edges, signature: JSON.stringify(signature) };
}
function measure(g) {
  const fmt = (x) => Number(x.toFixed(1)) + " px";
  if (current.id === "merge-edges") {
    const exits = g.edges
      .filter((e) => e.name.startsWith("(Gateway -&gt; "))
      .map((e) => e.d.match(/^M\s+([\d.+-]+)\s+([\d.+-]+)/)?.[0])
      .filter(Boolean);
    if (exits.length) {
      const n = new Set(exits).size;
      return n + (n === 1 ? " exit point" : " exit points") + " at Gateway";
    }
  }
  if (current.id === "node-spacing") {
    const peers = ["Billing", "Events", "Orders"].map((id) => g.nodes.get(id));
    if (peers.every(Boolean)) {
      peers.sort((a, b) => a.y - b.y);
      return (
        fmt(
          Math.min(...peers.slice(1).map((p, i) => p.y - peers[i].y - peers[i].height))
        ) + " peer gap"
      );
    }
  }
  if (current.id === "layer-node-spacing") {
    const a = g.nodes.get("Read"),
      b = g.nodes.get("Build");
    if (a && b && b.x >= a.x + a.width) return fmt(b.x - a.x - a.width) + " after Read";
  }
  if (current.id === "wrapping" && g.nodes.has("Get") && g.nodes.has("Ship")) {
    const rows = new Set(
      [...g.nodes.values()].map((n) => Math.round(n.y + n.height / 2))
    );
    return rows.size + (rows.size === 1 ? " row" : " rows") + " of stages";
  }
  const n = g.edges.length;
  return n + (n === 1 ? " connection" : " connections");
}
function resize() {
  const good = results.map((r, i) => [r, i]).filter(([r]) => r?.geo && !r.error);
  if (!good.length) return;
  let scale = 1.25;
  for (const [r, i] of good) {
    const slot = panes[i].querySelector(".diagram-slot");
    scale = Math.min(scale, slot.clientWidth / r.geo.w, slot.clientHeight / r.geo.h);
  }
  for (const [r, i] of good) {
    const image = panes[i].querySelector(".diagram-slot img");
    if (image) {
      image.style.width = scale * r.geo.w + "px";
      image.style.height = scale * r.geo.h + "px";
    }
  }
}
function showResult(i, result) {
  const pane = panes[i],
    slot = pane.querySelector(".diagram-slot");
  if (imageURLs[i]) URL.revokeObjectURL(imageURLs[i]);
  imageURLs[i] = null;
  slot.replaceChildren();
  if (result.svg && !result.geo) {
    try {
      result.geo = geometry(result.svg);
    } catch (e) {
      result = { error: e.message };
    }
  }
  results[i] = result;
  const ok = Boolean(result.svg && result.geo);
  for (const sel of [".save-action", ".expand-action"])
    pane.querySelector(sel).disabled = !ok;
  pane.querySelector(".input-action").disabled = !result.source;
  if (ok) {
    const image = el("img");
    image.alt = current.title + " — " + labels[i].toLowerCase() + " D2 rendering";
    imageURLs[i] = URL.createObjectURL(svgBlob(result.svg));
    image.src = imageURLs[i];
    slot.append(image);
    pane.querySelector(".metric").textContent = measure(result.geo);
    pane.querySelector(".bounds").textContent =
      result.geo.w + " × " + result.geo.h + " px";
  } else {
    const box = el("div", "notice-box");
    box.append(
      el("strong", "", result.loading ? "Rendering with D2…" : "Render unavailable")
    );
    if (result.error) box.append(el("code", "", result.error));
    slot.append(box);
    pane.querySelector(".metric").textContent = result.loading
      ? "Compiling"
      : "No SVG produced";
    pane.querySelector(".bounds").textContent = "";
  }
}
function status(message) {
  const dirty = state.draft !== state.source || state.draftTarget !== state.target;
  $("render-status").textContent = dirty ? "Edits ready to render" : message;
}
async function renderBoth() {
  const ticket = ++serial,
    c = current,
    s = state;
  const edited = s.source !== c.source || s.target !== c.target;
  $("fixture").textContent = edited ? "Edited diagram" : c.example;
  $("reading-title").textContent = edited
    ? "In the original example"
    : "What to look for";
  const plans = s.chosen.map((_, i) => {
    try {
      const source = fullSource(c, s, i);
      if (liveCache.has(source)) return { now: liveCache.get(source), source };
      return { source };
    } catch (e) {
      return { now: { error: e.message } };
    }
  });
  const needsCompiler = plans.some((p) => !p.now);
  $("same").hidden = true;
  if (needsCompiler) status("Rendering with D2…");
  for (const [i, p] of plans.entries())
    showResult(i, { ...(p.now || { loading: true }), source: p.source });
  resize();
  await Promise.all(
    plans.map(async (plan, i) => {
      if (plan.now) return;
      let out;
      try {
        out = await live(plan.source);
      } catch (e) {
        out = { error: e.message };
      }
      if (ticket !== serial) return;
      showResult(i, { ...out, source: plan.source });
      resize();
    })
  );
  if (ticket !== serial) return;
  const identical =
    results.every((r) => r.geo) && results[0].geo.signature === results[1].geo.signature;
  $("same").hidden = !identical;
  if (identical)
    $("same").textContent = "Both settings produced identical geometry for this diagram.";
  const failed = results.some((r) => r.error);
  status(failed ? "A render diagnostic is shown above." : "Rendered with D2.");
  updateAvailability();
}
function updateAvailability() {
  const c = current;
  const value =
    state.chosen[1] === "custom" ? state.custom[1] : c.values[state.chosen[1]];
  $("availability").textContent = c.target
    ? "Set this property directly on the shape you want to place."
    : "Use this flag on the command line, or set the same key in vars.d2-config.data. The value in the diagram takes precedence.";
  $("cli").hidden = false;
  $("cli").textContent = c.target
    ? `${state.target}.${c.option}: ${value}`
    : `d2 --layout=elk --${c.option}='${String(value).replaceAll(
        "'",
        "'\\''"
      )}' diagram.d2 diagram.svg`;
}
function selectCase() {
  const id = location.hash.slice(1);
  current = cases.get(id) || catalog[0];
  if (!states.has(current.id)) states.set(current.id, newState(current));
  state = states.get(current.id);
  document.title = "D2 Layout Lab · " + current.title;
  $("total").textContent = cases.size;
  $("case-title").textContent = current.title;
  $("description").textContent = current.description;
  $("option-key").textContent = current.key;
  $("group-name").textContent = current.group;
  $("fixture").textContent = current.example;
  $("insight").textContent = current.insight;
  $("option-doc").href = current.docs;
  $("target-box").hidden = !current.target;
  $("target").value = state.draftTarget || "";
  $("editor").value = state.draft;
  $("editor").scrollTop = 0;
  lineNumbers();
  panes.forEach((pane, i) => {
    const select = pane.querySelector("select"),
      input = pane.querySelector(".custom");
    select.replaceChildren(
      ...current.values.map((_, n) => new Option(valueLabel(current, n), String(n)))
    );
    if (typeof current.values[0] === "number" || current.id === "container-padding")
      select.add(new Option("Custom…", "custom"));
    select.value = String(state.chosen[i]);
    input.hidden = select.value !== "custom";
    input.value = state.custom[i];
    input.type = typeof current.values[0] === "number" ? "number" : "text";
    input.min = "0";
    input.step = "any";
  });
  navList($("menu"), $("filter").value);
  updateAvailability();
  renderBoth();
}
function showInput(i) {
  const text = results[i].source;
  showDialog(labels[i] + " D2 input");
  const pre = el("pre", "source-full", text),
    bar = el("div", "dialog-buttons");
  const copy = button("Copy full D2", () => copyText(text, copy));
  bar.append(
    copy,
    button("Download .d2", () =>
      download(
        new Blob([text], { type: "text/plain" }),
        current.id + "-" + labels[i].toLowerCase() + ".d2"
      )
    )
  );
  body.append(pre, bar);
}
function expand(i) {
  const r = results[i],
    img = el("img"),
    viewer = el("div", "expanded-stage"),
    actions = el("div", "dialog-buttons");
  showDialog(current.title + " · " + labels[i], true);
  modalURL = URL.createObjectURL(svgBlob(r.svg));
  img.src = modalURL;
  img.alt = current.title + " full D2 diagram";
  img.style.width = r.geo.w + "px";
  img.style.height = r.geo.h + "px";
  img.style.objectFit = "contain";
  const zoom = button("Actual size", () => {
    const actual = viewer.classList.toggle("actual");
    zoom.textContent = actual ? "Fit diagram" : "Actual size";
  });
  viewer.append(img);
  actions.append(
    zoom,
    button("Download SVG", () => saveSvg(i))
  );
  body.append(viewer, actions);
}
function saveSvg(i) {
  download(svgBlob(results[i].svg), current.id + "-" + labels[i].toLowerCase() + ".svg");
}
for (let i = 0; i < 2; i++) {
  const pane = $("pane").content.firstElementChild.cloneNode(true),
    label = pane.querySelector("label"),
    select = pane.querySelector("select"),
    input = pane.querySelector(".custom");
  select.id = "value-" + i;
  label.htmlFor = select.id;
  label.textContent = labels[i];
  select.setAttribute("aria-label", labels[i] + " option value");
  input.setAttribute("aria-label", labels[i] + " custom value");
  select.onchange = () => {
    const previous = state.chosen[i];
    state.chosen[i] = select.value === "custom" ? "custom" : Number(select.value);
    if (state.chosen[i] === "custom" && previous !== "custom")
      state.custom[i] = String(current.values[previous]);
    input.value = state.custom[i];
    input.hidden = state.chosen[i] !== "custom";
    if (!input.hidden) {
      input.focus();
      input.select();
    }
    renderBoth();
  };
  input.onchange = () => {
    state.custom[i] = input.value;
    renderBoth();
  };
  input.onkeydown = (e) => {
    if (e.key === "Enter") input.blur();
  };
  pane.querySelector(".input-action").onclick = () => showInput(i);
  pane.querySelector(".save-action").onclick = () => saveSvg(i);
  pane.querySelector(".expand-action").onclick = () => expand(i);
  $("panes").append(pane);
  panes.push(pane);
}
function lineNumbers() {
  const input = $("editor");
  $("gutter").textContent = input.value
    .split("\n")
    .map((_, i) => i + 1)
    .join("\n");
  $("gutter").scrollTop = input.scrollTop;
}
$("editor").oninput = () => {
  state.draft = $("editor").value;
  lineNumbers();
  status("Rendered with D2.");
};
$("editor").onscroll = () => {
  $("gutter").scrollTop = $("editor").scrollTop;
};
$("target").oninput = () => {
  state.draftTarget = $("target").value;
  status("Shape ID updated.");
};
function applyEdits() {
  state.source = state.draft;
  state.target = state.draftTarget;
  renderBoth();
}
$("render").onclick = applyEdits;
$("reset").onclick = () => {
  state.draft = current.source;
  state.source = current.source;
  state.target = current.target;
  state.draftTarget = current.target;
  $("editor").value = state.draft;
  $("target").value = state.target || "";
  lineNumbers();
  renderBoth();
};
document.addEventListener("keydown", (e) => {
  if (
    e.key === "Enter" &&
    (e.metaKey || e.ctrlKey) &&
    document.activeElement === $("editor")
  ) {
    e.preventDefault();
    applyEdits();
  }
  if (
    e.key === "/" &&
    !dialog.open &&
    !["INPUT", "TEXTAREA", "SELECT"].includes(document.activeElement.tagName)
  ) {
    e.preventDefault();
    if ($("filter").getClientRects().length) $("filter").focus();
    else $("browse").click();
  }
});
$("build").onclick = () => {
  showDialog("About this D2 build");
  const prose = el("div", "prose"),
    method = el("p"),
    upstream = el("p");
  method.textContent =
    "Both panes use the same D2 source, theme and scale. SVG dimensions include 10 px of padding on each side. Use the code button below either diagram to download its full input.";
  upstream.append(
    "D2 uses the ",
    link("native Go port of ELK", "https://github.com/d2lang/elk-go"),
    " for both the CLI and browser compiler. ",
    link(
      "ELK Layered documentation",
      "https://eclipse.dev/elk/reference/algorithms/org-eclipse-elk-layered.html"
    )
  );
  prose.append(method, upstream);
  body.append(prose);
};
window.addEventListener("hashchange", () => {
  if (dialog.open) dialog.close();
  selectCase();
  window.scrollTo(0, 0);
});
new ResizeObserver(resize).observe($("panes"));
selectCase();
