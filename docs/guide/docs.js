/* octoscope docs — shared chrome (sidebar + topbar) and interactions.
   The nav is defined once here and injected, so every page stays in sync.
   Each page declares <body data-page="..." data-title="..."> to drive the
   active state and the breadcrumb. */

(function () {
  var NAV = [
    { group: "Guide", items: [
      { id: "start",     label: "Getting started",   href: "index.html" },
      { id: "auth",      label: "Authentication",     href: "auth.html" },
      // anchors: in-page sub-navigation, rendered only while this page is
      // the active one. The tabs page is by far the longest in the guide —
      // eight tabs under one sidebar entry meant a reader after the Inbox
      // scrolled past six others to reach it. Kept as anchors rather than
      // split into pages: the pager chain is linear and hand-maintained,
      // and it has already been broken once by an inserted page.
      { id: "tabs",      label: "The tabs",           href: "tabs.html",
        anchors: [
          { href: "#overview",    label: "1 Overview" },
          { href: "#repos",       label: "2 Repos" },
          { href: "#prs-issues",  label: "3 PRs · 4 Issues" },
          { href: "#activity",    label: "5 Activity" },
          { href: "#gists",       label: "6 Gists" },
          { href: "#inbox",       label: "7 Inbox" },
          { href: "#whats-new",   label: "8 What's new" }
        ] },
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
        var isActive = it.id === page;
        var on = isActive ? " active" : "";
        html += '<a class="navlink' + on + '" href="' + it.href + '">' +
                '<span class="dot"></span>' + it.label + '</a>';
        // Sub-navigation only for the page you are on. Showing every
        // page's anchors would trade one long page for a long sidebar.
        if (isActive && it.anchors) {
          html += '<div class="subnav">';
          it.anchors.forEach(function (a) {
            html += '<a class="sublink" href="' + a.href + '">' + a.label + '</a>';
          });
          html += '</div>';
        }
      });
      html += '</div>';
    });
    html += '</nav><div class="sidebar-foot">' +
      '<a href="../index.html">← octoscope.dev</a>' +
      '<a href="https://github.com/gfazioli/octoscope">GitHub</a></div>';
    sb.innerHTML = html;
  }

  // ---- sub-navigation: mark the section you are actually reading ----
  //
  // Without this the `.here` style would be a rule nothing ever applies, and
  // an in-page nav that cannot tell you where you are is just a list.
  //
  // Observes the headings rather than listening to scroll: nothing to
  // throttle, and it stays correct when a click jumps straight to an anchor.
  // The band is pushed down by the sticky topbar, or the heading sitting
  // under it would read as off-screen while it is the one being read.
  (function () {
    var links = [].slice.call(document.querySelectorAll(".sublink"));
    if (!links.length || !("IntersectionObserver" in window)) return;

    var targets = links
      .map(function (a) { return document.getElementById(a.getAttribute("href").slice(1)); })
      .filter(Boolean);
    if (!targets.length) return;

    var topbar = parseInt(getComputedStyle(document.documentElement)
      .getPropertyValue("--topbar"), 10) || 56;
    var band = topbar + 40;

    var passed = {};
    function paint() {
      // The last heading whose top has crossed the band is the one being
      // read; anything below it is still ahead of the reader.
      var current = null;
      targets.forEach(function (t) { if (passed[t.id]) current = t.id; });
      links.forEach(function (a) {
        a.classList.toggle("here", a.getAttribute("href") === "#" + current);
      });
    }

    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        passed[e.target.id] = e.boundingClientRect.top < band;
      });
      paint();
    }, { threshold: 0, rootMargin: "-" + band + "px 0px 0px 0px" });

    targets.forEach(function (t) { io.observe(t); });
  })();

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
