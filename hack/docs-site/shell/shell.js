(function () {
  var KEY = "color-mode";
  var mq = window.matchMedia("(prefers-color-scheme: dark)");
  function stored() { try { var m = localStorage.getItem(KEY); return m === "light" || m === "dark" || m === "system" ? m : "system"; } catch (e) { return "system"; } }
  function apply(m) {
    document.documentElement.classList.toggle("dark", m === "dark" || (m === "system" && mq.matches));
    document.querySelectorAll("[data-mode-label]").forEach(function (el) { el.textContent = m.charAt(0).toUpperCase() + m.slice(1); });
    document.querySelectorAll("[data-mode-icon]").forEach(function (el) { el.hidden = el.getAttribute("data-mode-icon") !== m; });
  }
  mq.addEventListener("change", function () { if (stored() === "system") { apply("system"); } });
  apply(stored());

  var menu = document.querySelector(".mode-menu"), trig = document.querySelector(".mode-trigger");
  if (trig) {
    trig.addEventListener("click", function (e) { e.stopPropagation(); menu.hidden = !menu.hidden; trig.setAttribute("aria-expanded", String(!menu.hidden)); });
    menu.querySelectorAll("button").forEach(function (b) {
      b.addEventListener("click", function () { var m = b.getAttribute("data-set"); try { localStorage.setItem(KEY, m); } catch (e) {} apply(m); menu.hidden = true; });
    });
    document.addEventListener("click", function () { menu.hidden = true; });
  }

  var sheet = document.querySelector(".sheet"), overlay = document.querySelector(".overlay");
  function openSheet(open) { if (!sheet) { return; } sheet.hidden = !open; overlay.hidden = !open; }
  document.querySelectorAll("[data-open-sheet]").forEach(function (b) { b.addEventListener("click", function () { openSheet(true); }); });
  document.querySelectorAll("[data-close-sheet]").forEach(function (b) { b.addEventListener("click", function () { openSheet(false); }); });
  if (location.hash === "#menu") { openSheet(true); }

  var bar = document.querySelector(".searchbar");
  document.querySelectorAll("[data-open-search]").forEach(function (b) {
    b.addEventListener("click", function () { bar.classList.toggle("open"); if (bar.classList.contains("open")) { bar.querySelector("input").focus(); } });
  });

  function esc(t) { return t.replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; }); }
  document.querySelectorAll(".search").forEach(function (box) {
    var input = box.querySelector("input"), list = box.querySelector(".results");
    function show() {
      var q = input.value.trim().toLowerCase();
      if (!q) { list.hidden = true; return; }
      var terms = q.split(/\s+/), hits = window.docsIndex.filter(function (p) {
        var hay = (p[0] + " " + p[2]).toLowerCase();
        return terms.every(function (t) { return hay.indexOf(t) >= 0; });
      }).slice(0, 10);
      var out = [], last = "";
      hits.forEach(function (p, i) {
        if (p[1] !== last) { out.push('<li class="grp">' + esc(p[1]) + "</li>"); last = p[1]; }
        out.push('<li><a href="' + esc(p[3]) + '"' + (i === 0 ? ' class="sel"' : "") + ">" + esc(p[0]) + "<small>" + esc(p[2]) + "</small></a></li>");
      });
      list.innerHTML = out.length ? out.join("") : '<li class="none">No page matches.</li>';
      list.hidden = false;
    }
    input.addEventListener("input", show);
    input.addEventListener("keydown", function (e) { if (e.key === "Escape") { list.hidden = true; input.blur(); } });
    var q = new URLSearchParams(location.search).get("q");
    if (q && box.offsetParent) { input.value = q; show(); }
  });
  document.addEventListener("keydown", function (e) {
    var t = document.activeElement.tagName;
    if (e.key === "/" && t !== "INPUT" && t !== "TEXTAREA") { var i = document.querySelector(".only-desk .search input"); if (i && i.offsetParent) { e.preventDefault(); i.focus(); } }
  });

  var lists = {};
  function option(e, cur) {
    return '<option value="' + esc(e.name) + '" data-prefix="' + esc(e.prefix) + '" data-home="' + esc(e.home) + '"' + (e.name === cur ? " selected" : "") + ">" + esc(e.label) + "</option>";
  }
  document.querySelectorAll(".vpick select").forEach(function (sel) {
    var prefix = sel.getAttribute("data-prefix"), home = sel.getAttribute("data-home"), cur = sel.value, url = sel.getAttribute("data-versions");
    lists[url] = lists[url] || fetch(url, { cache: "no-cache" }).then(function (r) { return r.ok ? r.json() : null; });
    lists[url].then(function (v) {
      if (!v || !v.versions || !v.versions.length) { return; }
      var html = v.versions.map(function (e) { return option(e, cur); }).join("");
      if (!v.versions.some(function (e) { return e.name === cur; })) { html = sel.options[sel.selectedIndex].outerHTML + html; }
      sel.innerHTML = html;
    }).catch(function () {});
    sel.addEventListener("change", function () {
      var o = sel.options[sel.selectedIndex], to = o.getAttribute("data-home"), p = location.pathname;
      var rel = p === home ? "" : p.indexOf(prefix) === 0 ? p.slice(prefix.length) : "";
      if (!rel) { location.href = to; return; }
      var page = o.getAttribute("data-prefix") + rel;
      fetch(page, { method: "HEAD" }).then(function (r) { location.href = r.ok ? page + location.hash : to; }, function () { location.href = to; });
    });
  });

  var links = Array.prototype.slice.call(document.querySelectorAll(".onpage a"));
  if (links.length) {
    var heads = links.map(function (a) { return document.getElementById(decodeURIComponent(a.getAttribute("href").slice(1))); });
    function spy() {
      var cur = 0;
      heads.forEach(function (h, i) { if (h && h.getBoundingClientRect().top < 120) { cur = i; } });
      links.forEach(function (a, i) { a.classList.toggle("on", i === cur); });
    }
    document.addEventListener("scroll", spy, { passive: true });
    spy();
  }
})();
