/* octoscope docs — shared chrome (sidebar + topbar) and interactions.
   The nav is defined once here and injected, so every page stays in sync.
   Each page declares <body data-page="..." data-title="..."> to drive the
   active state and the breadcrumb. */

(function () {
  var NAV = [
    { group: "Guide", items: [
      { id: "start",     label: "Getting started",   href: "index.html" },
      { id: "auth",      label: "Authentication",     href: "auth.html" },
      { id: "tabs",      label: "The tabs",           href: "tabs.html" },
      { id: "drill",     label: "Drill-ins & scan",   href: "drill-ins.html" },
      { id: "live",      label: "Live & reliable",    href: "live.html" },
      { id: "themes",    label: "Themes",             href: "themes.html" },
      { id: "settings",  label: "Configuration",      href: "settings.html" },
      { id: "scripting", label: "Scripting",          href: "scripting.html" }
    ]},
    { group: "Reference", items: [
      { id: "flags", label: "CLI flags",          href: "flags.html" },
      { id: "keys",  label: "Keyboard shortcuts", href: "keybinds.html" }
    ]}
  ];

  var page = document.body.dataset.page || "start";
  var title = document.body.dataset.title || "Docs";

  // ---- sidebar ----
  var sb = document.getElementById("sidebar");
  if (sb) {
    var html = '' +
      '<a class="brand" href="../index.html" title="Back to octoscope.dev">' +
      '<span class="glyph">⌖</span><b>octoscope</b>' +
      '<span class="ver" id="guide-ver">v0.29.0</span></a><nav class="side">';
    NAV.forEach(function (g) {
      html += '<div class="navgroup"><div class="label">' + g.group + '</div>';
      g.items.forEach(function (it) {
        var on = it.id === page ? " active" : "";
        html += '<a class="navlink' + on + '" href="' + it.href + '">' +
                '<span class="dot"></span>' + it.label + '</a>';
      });
      html += '</div>';
    });
    html += '</nav><div class="sidebar-foot">' +
      '<a href="../index.html">← octoscope.dev</a>' +
      '<a href="https://github.com/gfazioli/octoscope">GitHub</a></div>';
    sb.innerHTML = html;
  }

  // ---- version badge ----
  // Same trick as the landing's hero pill: read the latest published
  // release rather than hardcoding a number that goes stale the moment
  // the next tag lands. The inline value above is the fallback for
  // offline / rate-limited loads, so it should still be bumped at
  // release time — it is just no longer the only thing keeping this
  // honest.
  fetch("https://api.github.com/repos/gfazioli/octoscope/releases/latest")
    .then(function (r) { return r.ok ? r.json() : null; })
    .then(function (data) {
      if (!data || !data.tag_name) return;
      var el = document.getElementById("guide-ver");
      if (el) el.textContent = data.tag_name;
    })
    .catch(function () { /* keep fallback version */ });

  // ---- topbar ----
  var tb = document.getElementById("topbar");
  if (tb) {
    tb.innerHTML = '' +
      '<button class="iconbtn menu-btn" id="menuBtn" aria-label="Menu">☰</button>' +
      '<span class="crumbs">Docs / <b>' + title + '</b></span>' +
      '<span class="spacer"></span>' +
      '<button class="iconbtn" id="themeBtn" aria-label="Toggle theme">' +
      '<span id="themeIcon">◑</span> Theme</button>' +
      '<a class="iconbtn" href="https://github.com/gfazioli/octoscope">GitHub ↗</a>';
  }

  // ---- theme toggle ----
  var root = document.documentElement;
  try { var s = localStorage.getItem("octo-docs-theme"); if (s) root.setAttribute("data-theme", s); } catch (e) {}
  function isDark() {
    // The stylesheet's :root is dark unconditionally — it carries no
    // prefers-color-scheme fallback — so anything short of an explicit
    // data-theme="light" renders dark, and the icon has to agree.
    return root.getAttribute("data-theme") !== "light";
  }
  function paint() { var i = document.getElementById("themeIcon"); if (i) i.textContent = isDark() ? "☀" : "☾"; }
  paint();
  document.addEventListener("click", function (e) {
    if (e.target.closest("#themeBtn")) {
      var next = isDark() ? "light" : "dark";
      root.setAttribute("data-theme", next);
      try { localStorage.setItem("octo-docs-theme", next); } catch (e2) {}
      paint();
    }
    if (e.target.closest("#menuBtn")) { sb && sb.classList.toggle("open"); }
    var c = e.target.closest(".copy");
    if (c) {
      var pre = c.parentElement.querySelector("pre");
      if (pre && navigator.clipboard) {
        navigator.clipboard.writeText(pre.innerText).then(function () {
          c.textContent = "copied"; setTimeout(function () { c.textContent = "copy"; }, 1200);
        });
      }
    }
  });
})();
