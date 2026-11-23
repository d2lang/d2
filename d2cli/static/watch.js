"use strict";
window.addEventListener("DOMContentLoaded", () => {
  init(1000);
});

function init(reconnectDelay) {
  const d2ErrDiv = window.document.querySelector("#d2-err");
  const d2SVG = window.document.querySelector("#d2-svg-container");

  const devMode = document.body.dataset.d2DevMode === "true";
  const ws = new WebSocket(`ws://${window.location.host}/watch`);
  let previousScale;
  let ratio;
  ws.onopen = () => {
    reconnectDelay = 1000;
    console.info("watch websocket opened");
  };
  ws.onmessage = (ev) => {
    const msg = JSON.parse(ev.data);
    if (devMode) {
      console.debug("watch websocket received data", ev);
    } else {
      console.debug("watch websocket received data");
    }
    if (msg.svg) {
      // we can't just set `d2SVG.innerHTML = msg.svg` need to parse this as xml not html
      const parsedXML = new DOMParser().parseFromString(msg.svg, "text/xml");
      d2SVG.replaceChildren(parsedXML.documentElement);
      changeFavicon("/static/favicon.ico");
      const svgEl = d2SVG.querySelector(".d2-svg");
      // just use inner SVG in watch mode
      svgEl.parentElement.replaceWith(svgEl);
      const width = Number(svgEl.getAttribute("width"));
      const height = Number(svgEl.getAttribute("height"));
      const scale = msg.scale ?? null;
      if (scale !== previousScale) {
        previousScale = scale;
        // Automatic sizing fits inside the body's 8px margin on each side.
        ratio = scale ?? Math.min(1, Math.max(1, window.innerWidth - 16) / width, Math.max(1, window.innerHeight - 16) / height);
      }
      svgEl.setAttribute("width", Math.ceil(width * ratio));
      svgEl.setAttribute("height", Math.ceil(height * ratio));

      d2ErrDiv.style.display = "none";
    }
    if (msg.err) {
      d2ErrDiv.innerText = msg.err;
      d2ErrDiv.style.display = "block";
      changeFavicon("/static/favicon-err.ico");
      d2ErrDiv.scrollIntoView();
    }
  };
  ws.onerror = (ev) => {
    console.error("watch websocket connection error", ev);
  };
  ws.onclose = (ev) => {
    console.error(`watch websocket closed with code`, ev.code, `and reason`, ev.reason);
    console.info(`reconnecting in ${reconnectDelay / 1000} seconds`);
    setTimeout(() => {
      if (reconnectDelay < 16000) {
        reconnectDelay *= 2;
      }
      init(reconnectDelay);
    }, reconnectDelay);
  };
}

const changeFavicon = function (iconURL) {
  const faviconLink = document.getElementById("favicon");
  faviconLink.href = iconURL;
};
