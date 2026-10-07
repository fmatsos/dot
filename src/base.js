/* Generic behaviours, each switched on by markup; no design. Inlined first by build.py.
   [data-motion-toggle]   the Animations button (aria-pressed); html[data-motion] = on|off
   img[data-src]          decorative sprites: loaded after `load`, only while motion is on
   [data-copy]            a button copying the text of its previous sibling (or of data-copy="#id");
                          data-copy-shell drops "$ " prompts and "# comments"; fires "site:copied"
   [data-part]            parts of a page: the one under the middle of the screen gets .active,
                          links to it in [data-parts-nav] get aria-current="true"; html.snap when
                          every part fits the screen (with html[data-snap]); --hh = header height
   [data-rail]            a scroll rail: its [data-rail-thumb] follows the scroll and can be dragged
   [data-hop="sel"]       a sprite that jumps onto the elements matching sel inside its parent,
                          measured from their positions (any layout), while its part is active
   html.ready             set once the above is wired: "not shown yet" styles may apply */
(() => {
  const html = document.documentElement, on = () => html.dataset.motion === "on";
  const status = document.getElementById("status");
  const say = msg => { if (!status) return; status.textContent = msg; setTimeout(() => { status.textContent = ""; }, 4000); };
  const listeners = [];   // run when motion is switched
  const onMotion = f => listeners.push(f);

  // decorative sprites
  let loaded = false;
  const sprites = () => {
    if (loaded || !on() || document.readyState !== "complete") return;
    loaded = true;
    document.querySelectorAll("img[data-src]").forEach(i => {
      if (i.dataset.srcset) i.srcset = i.dataset.srcset;
      i.src = i.dataset.src;
    });
  };
  addEventListener("load", sprites);
  onMotion(sprites);

  // RGAA 13.8: every looping animation can be stopped, and the choice sticks
  document.querySelectorAll("[data-motion-toggle]").forEach(b => {
    b.hidden = false;
    b.setAttribute("aria-pressed", String(on()));
    b.addEventListener("click", () => {
      html.dataset.motion = on() ? "off" : "on";
      document.querySelectorAll("[data-motion-toggle]").forEach(x => x.setAttribute("aria-pressed", String(on())));
      try { localStorage.setItem("site-motion", html.dataset.motion); } catch (e) {}
      listeners.forEach(f => f());
    });
  });

  // copy buttons
  document.querySelectorAll("[data-copy]").forEach(b => b.addEventListener("click", () => {
    const src = b.dataset.copy ? document.querySelector(b.dataset.copy) : b.previousElementSibling;
    let t = src.innerText;
    if ("copyShell" in b.dataset) t = t.split("\n").map(l => l.replace(/^\$ /, "").replace(/\s+# .*$/, "")).join("\n");
    navigator.clipboard.writeText(t.trim()).then(() => {
      say(b.dataset.copied || "Copied to the clipboard.");
      b.dispatchEvent(new CustomEvent("site:copied", { bubbles: true, detail: { motion: on() } }));
    }, () => say("Copy failed: select the text and copy it by hand."));
  }));

  // parts of a page
  const parts = [...document.querySelectorAll("[data-part]")];
  const header = document.querySelector("[data-header]");
  const navLinks = [...document.querySelectorAll("[data-parts-nav] a[href^='#']")];
  const max = () => html.scrollHeight - innerHeight;
  let cur;
  const hops = [];
  const current = () => {
    if (!parts.length) return;
    const mid = innerHeight / 2;
    const p = scrollY >= max() - 2 ? parts.at(-1) : parts.find(x => { const r = x.getBoundingClientRect(); return r.top <= mid && r.bottom > mid; });
    if (!p || p === cur) return;
    cur = p;
    parts.forEach(x => x.classList.toggle("active", x === p));
    navLinks.forEach(a => a.getAttribute("href") === "#" + p.id ? a.setAttribute("aria-current", "true") : a.removeAttribute("aria-current"));
    hops.forEach(h => h.start());
  };
  const fit = () => {
    const hh = header ? header.offsetHeight : 0;
    html.style.setProperty("--hh", hh + "px");
    if ("snap" in html.dataset) html.classList.toggle("snap", parts.every(s => s.offsetHeight <= innerHeight - hh + 1));
  };
  if (header) new ResizeObserver(fit).observe(header);
  fit();

  // the scroll rail
  const rail = document.querySelector("[data-rail]"), thumb = rail && rail.querySelector("[data-rail-thumb]");
  const place = () => { if (thumb) thumb.style.transform = `translateY(${(scrollY / Math.max(1, max())) * (rail.clientHeight - thumb.offsetHeight)}px)`; };
  if (thumb) {
    const toY = e => { const r = rail.getBoundingClientRect(); return Math.min(1, Math.max(0, (e.clientY - r.top - thumb.offsetHeight / 2) / (r.height - thumb.offsetHeight))) * max(); };
    thumb.addEventListener("pointerdown", e => {
      e.preventDefault(); thumb.setPointerCapture(e.pointerId); html.classList.add("dragging");
      const move = ev => scrollTo({ top: toY(ev), behavior: "instant" });
      const up = () => { html.classList.remove("dragging"); thumb.removeEventListener("pointermove", move); thumb.removeEventListener("pointerup", up); };
      thumb.addEventListener("pointermove", move); thumb.addEventListener("pointerup", up);
    });
    rail.addEventListener("click", e => { if (!e.target.closest("a, [data-rail-thumb]")) scrollTo({ top: toY(e) }); });
    addEventListener("keydown", e => { if (e.key === "Escape") rail.classList.add("quiet"); });   // WCAG 1.4.13
    ["pointerleave", "focusout"].forEach(ev => rail.addEventListener(ev, () => rail.classList.remove("quiet")));
  }

  // sprites that jump from element to element
  document.querySelectorAll("[data-hop]").forEach(sprite => {
    const box = sprite.parentElement, part = sprite.closest("[data-part]");
    const targets = () => [...box.querySelectorAll(sprite.dataset.hop)];
    let at = null, timer;
    const spot = el => {
      const r = box.getBoundingClientRect(), k = el.getBoundingClientRect();
      const hh = parseFloat(getComputedStyle(html).getPropertyValue("--hh")) || 0;
      const clearTop = sprite.classList.contains("global-hopper") ? hh + 8 - r.top : 0;
      return [k.left - r.left + (k.width - sprite.offsetWidth) / 2, Math.max(clearTop, k.top - r.top - sprite.offsetHeight + 8)];
    };
    const stand = () => { if (at) { const [x, y] = spot(at); sprite.style.transform = `translate(${x}px, ${y}px)`; } };
    const start = () => {
      clearTimeout(timer);
      const hideForFiles = sprite.classList.contains("global-hopper") && cur?.id === "files";
      sprite.hidden = hideForFiles;
      if (hideForFiles) {
        sprite.getAnimations().forEach(a => a.cancel());
        sprite.style.removeProperty("transform");
        return;
      }
      if (!on() || (part && cur !== part)) return;
      const all = targets();
      if (!all.length) return;
      const firstRow = all[0].getBoundingClientRect().bottom;
      const seen = all.filter(el => {   // local hops stay on their first row; the site-wide flyer follows visible stops
        const k = el.getBoundingClientRect();
        return k.left >= 0 && k.right <= innerWidth && (box === document.body || box.id === "main" ? k.top < innerHeight && k.bottom > 0 : k.top < firstRow);
      });
      if (!seen.length) { timer = setTimeout(start, 800); return; }
      const next = seen.find(el => all.indexOf(el) > all.indexOf(at)) || seen[0];
      const from = at ? spot(at) : spot(next), to = spot(next);
      const flip = to[0] < from[0] ? " scaleX(-1)" : "";
      const r = box.getBoundingClientRect(), hh = parseFloat(getComputedStyle(html).getPropertyValue("--hh")) || 0;
      const clearTop = sprite.classList.contains("global-hopper") ? hh + 8 - r.top : 0;
      const peak = Math.max(clearTop, Math.min(from[1], to[1]) - 90);
      const jump = sprite.animate([
        { transform: `translate(${from[0]}px, ${from[1]}px)${flip}`, easing: "ease-out" },
        { transform: `translate(${(from[0] + to[0]) / 2}px, ${peak}px)${flip}`, easing: "ease-in" },
        { transform: `translate(${to[0]}px, ${to[1]}px)${flip}` }], { duration: 700, fill: "forwards" });
      jump.onfinish = () => {
        at = next; stand(); jump.cancel();
        next.animate([{ translate: "0 0" }, { translate: "0 12px", scale: "1.02 .96" }, { translate: "0 0" }], { duration: 400, easing: "ease-out" });
        next.dispatchEvent(new CustomEvent("site:landed", { bubbles: true }));
        timer = setTimeout(start, 1100);
      };
    };
    hops.push({ start });
    onMotion(start);
    addEventListener("resize", () => { sprite.getAnimations().forEach(a => a.cancel()); stand(); });
    box.querySelectorAll("*").forEach(el => { if (el.scrollWidth > el.clientWidth) el.addEventListener("scroll", stand, { passive: true }); });
    if (!part) start();
  });

  addEventListener("scroll", () => { current(); requestAnimationFrame(place); }, { passive: true });
  ["load", "hashchange", "resize"].forEach(e => addEventListener(e, () => { fit(); current(); place(); }));
  current(); place();
  html.classList.add("ready");
})();
