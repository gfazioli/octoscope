/*
 * landing.js — the landing page's motion: the scroll reveals, the
 * screenshot carousel, and the octopus that walks the page with the
 * reader.
 *
 * A classic script at the end of <body>, after the inline scripts: the
 * At-a-glance marquee clones its cards there, and the clones have to
 * exist before the reveals are armed. Nothing here is needed to read the
 * page. Without it every section is where the layout put it, the carousel
 * shows its first shot with that shot's caption, and there is no octopus.
 */
(function () {
    'use strict';

    var root = document.documentElement;
    var motionQuery = window.matchMedia ? window.matchMedia('(prefers-reduced-motion: reduce)') : null;

    function reduced() {
        return !!(motionQuery && motionQuery.matches);
    }

    function onMotionChange(fn) {
        if (!motionQuery) return;
        if (motionQuery.addEventListener) motionQuery.addEventListener('change', fn);
        else if (motionQuery.addListener) motionQuery.addListener(fn);
    }

    function each(list, fn) {
        Array.prototype.forEach.call(list, fn);
    }

    // ------------------------------------------------------------------
    // Scroll reveals. The stylesheet's "Scroll reveals" block has the
    // poses and the three states; this only moves scopes between them.
    // ------------------------------------------------------------------
    (function reveals() {
        // Where nothing can say a scope came into view, nothing is armed,
        // so nothing waits for a signal that will never come.
        if (!('IntersectionObserver' in window)) return;

        // The marquee's cards are items of the marquee's scope. Set here
        // rather than in the markup because the marquee is itself a
        // script's: its clones are copies of the originals. The first few
        // land one after another, the way the row is read; the rest are
        // outside the band by then, and land together.
        var track = document.querySelector('.glance-marquee .glance-track');
        if (track) {
            each(track.children, function (card, i) {
                card.setAttribute('data-reveal', 'squash');
                card.style.setProperty('--reveal-delay', Math.min(i, 5) * 90 + 'ms');
            });
        }

        // A scope is a [data-scope], or an item with no scope above it.
        var scopes = [];
        each(document.querySelectorAll('[data-scope]'), function (el) {
            scopes.push(el);
        });
        each(document.querySelectorAll('[data-reveal]:not([data-scope])'), function (el) {
            if (!el.parentElement || !el.parentElement.closest('[data-scope]')) scopes.push(el);
        });

        // The light a landing card catches (the stylesheet's ring) runs
        // only where somebody can see it land: in a band that clips, on
        // the cards inside the band as it arrives. The stylesheet says
        // why it is worth a measurement.
        function glint(scope) {
            var band = scope.getBoundingClientRect();
            var cards = scope.matches("[data-reveal='squash']") ? [scope] : scope.querySelectorAll("[data-reveal='squash']");
            each(cards, function (card) {
                var box = card.getBoundingClientRect();
                if (box.right > band.left && box.left < band.right) card.setAttribute('data-glint', '');
            });
        }

        function reveal(el) {
            glint(el);
            el.setAttribute('data-revealed', '');
            observer.unobserve(el);
        }

        // Threshold 0, with the bottom margin as the lag: any share above
        // 0 is a height some scope can never reach, and a tall one would
        // stay hidden for good. The lag is in pixels, about 8% of a phone
        // or a laptop, rather than 8% of whatever the window is: a window
        // stretched to the whole page, the way a crawler renders one,
        // made that a band a section fits in, and "Get release updates"
        // never left it (measured at 412px wide).
        var observer = new IntersectionObserver(function (entries) {
            entries.forEach(function (entry) {
                if (entry.isIntersecting) reveal(entry.target);
            });
        }, { threshold: 0, rootMargin: '0px 0px -64px 0px' });

        // The keyboard can get there first. A focused element is scrolled
        // only as far as the viewport's edge, which can stop inside the
        // band the margin leaves out, and a focus ring on something
        // invisible is a dead end: focus reveals every armed scope it
        // lands in, at once.
        document.addEventListener('focusin', function (event) {
            var el = event.target;
            while (el && el.closest) {
                var scope = el.closest('[data-armed]:not([data-revealed])');
                if (!scope) break;
                reveal(scope);
                el = scope.parentElement;
            }
        });

        // Armed only if ENTIRELY off screen now: what the reader can
        // already see never moves.
        var height = window.innerHeight || root.clientHeight;
        scopes.forEach(function (el) {
            var box = el.getBoundingClientRect();
            if (box.bottom <= 0 || box.top >= height) {
                el.setAttribute('data-armed', '');
                observer.observe(el);
            }
        });
    })();

    // ------------------------------------------------------------------
    // The screenshot carousel.
    // ------------------------------------------------------------------
    var carousel = (function () {
        var box = document.querySelector('.theme-carousel');
        var track = box && box.querySelector('.theme-carousel-track');
        var dots = document.querySelectorAll('.theme-carousel-dot');
        var says = document.querySelector('.carousel-says');
        if (!track || !track.children.length || !dots.length) return null;
        var slides = track.children;

        // How long a shot stays up. A timeout keyed on the shot rather
        // than an interval, so every change of shot — the timer's, a
        // dot's, the octopus's — gets its full time.
        var DWELL_MS = 4500;
        // How long a turn waits for a shot that has not arrived before it
        // goes anyway: offline, or a missing file, must not stop the
        // carousel for good.
        var LOAD_WAIT_MS = 8000;
        var index = 0;
        var timer = null;
        // Every change of state moves this on, so a timer or a load that
        // outlived the state that started it can tell, and do nothing.
        var turn = 0;
        // It turns by itself until the reader takes over: the first dot
        // or "Next" they click hands them the carousel for good, which is
        // how the dots have always behaved. Never under Reduce Motion.
        var auto = !reduced();
        // Why it is held still, by reason, so one reason ending does not
        // release another that still holds.
        var holds = {};
        var listeners = [];

        function captionOf(i) {
            var caption = slides[i].querySelector('.theme-carousel-caption');
            return caption ? caption.innerHTML : '';
        }

        function held() {
            for (var reason in holds) {
                if (holds[reason]) return true;
            }
            return false;
        }

        // The shots after the first are lazy in the markup: the ten weigh
        // 5 MB, and all of them loaded with the page. Lazy, the page
        // brings the first and the one or two the browser's own lazy
        // loading finds near it (it measures the distance without the
        // carousel's clip); the rest start loading here, a whole dwell
        // before their turn, and never before the page itself has loaded.
        var pageLoaded = document.readyState === 'complete';

        function imageOf(i) {
            return slides[(i + slides.length) % slides.length].querySelector('img');
        }

        function warm(i) {
            var img = imageOf(i);
            // An engine without lazy loading has fetched them all already.
            if (!pageLoaded || !img || img.loading !== 'lazy') return;
            img.loading = 'eager';
        }

        // The load decides afresh rather than replay what was asked for
        // before it: a carousel scrolled away since then wants nothing.
        window.addEventListener('load', function () {
            pageLoaded = true;
            if (!holds.offscreen) {
                warm(index);
                warm(index + 1);
            }
        });

        // Calls fn once shot i can be shown whole, rather than slide an
        // empty frame in while it downloads.
        function ready(i, fn) {
            var img = imageOf(i);
            if (!img || (img.complete && img.naturalWidth > 0)) {
                fn();
                return;
            }
            warm(i);
            var done = false;
            var limit = null;
            function go() {
                if (done) return;
                done = true;
                clearTimeout(limit);
                img.removeEventListener('load', go);
                img.removeEventListener('error', go);
                fn();
            }
            img.addEventListener('load', go);
            img.addEventListener('error', go);
            limit = setTimeout(go, LOAD_WAIT_MS);
        }

        function schedule() {
            clearTimeout(timer);
            timer = null;
            var mine = ++turn;
            if (auto && !held() && slides.length > 1) {
                var next = (index + 1) % slides.length;
                warm(next);
                timer = setTimeout(function () {
                    ready(next, function () {
                        if (mine === turn) show(next);
                    });
                }, DWELL_MS);
            }
        }

        function show(i) {
            index = ((i % slides.length) + slides.length) % slides.length;
            // A dot can ask for any shot, and "Next" for the one after it.
            warm(index);
            warm(index + 1);
            track.style.transform = 'translateX(-' + index * 100 + '%)';
            each(dots, function (dot, j) {
                dot.classList.toggle('active', j === index);
                if (j === index) dot.setAttribute('aria-current', 'true');
                else dot.removeAttribute('aria-current');
            });
            // The caption is our own markup, copied from the slide.
            if (says) says.innerHTML = captionOf(index);
            listeners.forEach(function (fn) {
                fn(index);
            });
            schedule();
        }

        function take(i) {
            auto = false;
            show(i);
        }

        function hold(reason, on) {
            if (!!holds[reason] === on) return;
            holds[reason] = on;
            schedule();
        }

        each(dots, function (dot, i) {
            dot.addEventListener('click', function () {
                take(i);
            });
            // A pointer on a dot, or the keyboard's focus, is a click on its
            // way: its shot starts loading now. The click itself still moves
            // at once, since holding it until a download ends would leave a
            // dot looking dead on a slow connection.
            dot.addEventListener('pointerenter', function () {
                warm(i);
            });
            dot.addEventListener('focus', function () {
                warm(i);
            });
        });
        // A reader with the pointer on the window is reading it.
        box.addEventListener('mouseenter', function () {
            hold('window', true);
        });
        box.addEventListener('mouseleave', function () {
            hold('window', false);
        });
        onMotionChange(function () {
            if (reduced()) {
                auto = false;
                schedule();
            }
        });
        // Off screen, a turn is one nobody sees and, with each shot loading
        // a dwell ahead of its turn, a download nobody asked for. Held
        // from the start until the observer's first report, so the first
        // turn comes a full dwell after the carousel is seen, not after
        // the page opened somewhere above it.
        if ('IntersectionObserver' in window) {
            holds.offscreen = true;
            new IntersectionObserver(function (entries) {
                hold('offscreen', !entries[entries.length - 1].isIntersecting);
            }).observe(box);
        }
        schedule();

        return {
            index: function () {
                return index;
            },
            caption: captionOf,
            next: function () {
                take(index + 1);
            },
            hold: hold,
            onChange: function (fn) {
                listeners.push(fn);
            },
            activeDot: function () {
                return dots[index];
            }
        };
    })();

    // ------------------------------------------------------------------
    // The octopus: the TUI's launch mascot, one character in four places,
    // the way findergit.app's and netfox.app's mascots are in three (asked
    // for on 2026-10-06: "la mascotte dovrebbe essere visibile subito",
    // then the corner while scrolling, then the sponsorship in the footer):
    //
    //   hero    beside the version link from the first second: it walks
    //           up to "what's new" and says what the release is about;
    //   dots    beside the carousel's dots, saying what each shot is and
    //           turning to the next on a click;
    //   corner  in the window's corner once neither is near, walking while
    //           the page scrolls and standing when it stops; a click on it
    //           gives a tip, one of the page's own "At a glance" cards;
    //   card    on the footer's support card, suggesting a sponsorship.
    //
    // Never two on screen, and the corner whenever no other is: a place in
    // the page keeps its octopus standing once it has arrived, out of sight
    // when the reader scrolls on, so coming back finds it where it was, and
    // the corner's walks out. Where a place in the page has no room for
    // what it says (a phone, a narrow window), the corner says it instead.
    // It arrives on every load; dismissed anywhere, it leaves every place
    // until the next one.
    // ------------------------------------------------------------------
    (function octopus() {
        var artEl = document.getElementById('octopus-art');
        if (!artEl) return;
        var art;
        try {
            art = JSON.parse(artEl.textContent);
        } catch (e) {
            return;
        }

        // Matches oc-walk-in in the stylesheet: the walk to a place in the page.
        var WALK_MS = 2200;
        // Match oc-corner-in and oc-card-in.
        var CORNER_IN_MS = 900;
        var CARD_IN_MS = 420;
        // Matches the fade on the way out.
        var LEAVE_MS = 260;
        // The hop it makes on arriving, before the corner speaks.
        var HOP_MS = 420;
        // After the last scroll event, before its legs stop.
        var STILL_MS = 160;
        // From the script running to the walk to the version link: one beat,
        // so the walk is seen from its start.
        var HERO_DELAY_MS = 400;
        // After the dots come into view, a beat for the eye to land; none
        // when the corner's octopus is there to hand over from, or there
        // would be a beat with no octopus at all.
        var DOTS_DELAY_MS = 700;
        // What the corner opens by itself folds after this: it covers the page.
        var FOLD_MS = 8000;
        // How much of the support card is on screen before it goes there. It
        // leaves once none is, so a card half in view does not send it back
        // and forth.
        var CARD_RATIO = 0.3;
        // And how far below the nav the card's top edge has to be: room for
        // the octopus and its bubble, which stand above it (60px and, on a
        // phone, a bubble beside it up to about 110px). It leaves once the
        // octopus itself would slide under the nav: on a short phone the
        // footer is taller than the window, and at the end of the page the
        // card's top sits about 80px down (measured at 320x800).
        var CARD_ROOM = 112;
        var CARD_KEEP = 60;
        // After the newsletter prompt closes, a beat for its overlay to fade.
        var PROMPT_GONE_MS = 400;
        // How long what the reader asked for stays in the live region.
        var SPOKEN_MS = 1500;
        // A tip is a card whose description fits a bubble.
        var TIP_CHARS = 160;
        // Screen pixels per art pixel, across; twice as many down, as a
        // quadrant block's pixel is in a terminal cell.
        var PX = 3;
        // Beside the version link: the gap to the octopus, the gap from it
        // to its bubble, and the narrowest and widest bubble.
        var GAP = 16;
        var BUBBLE_GAP = 10;
        var MIN_ROOM = 200;
        var MAX_ROOM = 300;
        // What it says on the support card: the footer's own words, so it
        // makes no claim the page does not.
        var SPONSOR_LINE = 'octoscope is free and MIT-licensed. If you find it useful, consider sponsoring the project.';

        // One pose as rows of pixels, composed the way mascotGrid composes
        // it in internal/ui/mascot.go.
        function pose(look, blink, curl) {
            var rows = art.top[String(look)].concat(art.head, curl ? art.tent.curled : art.tent.splayed);
            var grid = rows.map(function (row) {
                return row.split('');
            });
            var top = art.eyeY;
            var low = art.eyeY + 1;
            art.eyeX.forEach(function (x) {
                if (blink) {
                    // The lid is down: the upper half stays body.
                    grid[low][x] = 'o';
                    grid[low][x + 1] = 'o';
                    return;
                }
                [top, low].forEach(function (y) {
                    grid[y][x] = 'o';
                    grid[y][x + 1] = 'o';
                    // Looking sideways leaves only the half on that side lit.
                    if (look < 0) grid[y][x + 1] = '.';
                    if (look > 0) grid[y][x] = '.';
                });
            });
            return grid;
        }

        // Each horizontal run of one colour as one rectangle, two units
        // tall: the viewBox is in art pixels across and half-pixels down.
        function rects(grid) {
            var out = '';
            grid.forEach(function (row, y) {
                var x = 0;
                while (x < row.length) {
                    var c = row[x];
                    if (c !== '#' && c !== 'o') {
                        x += 1;
                        continue;
                    }
                    var w = 1;
                    while (row[x + w] === c) w += 1;
                    out += '<rect class="' + (c === '#' ? 'oc-body' : 'oc-lens') + '" x="' + x + '" y="' + y * 2 +
                        '" width="' + w + '" height="2"/>';
                    x += w;
                }
            });
            return out;
        }

        var width = art.head[0].length;
        var height = art.top['0'].length + art.head.length + art.tent.splayed.length;
        var SPRITE_W = width * PX;
        var SPRITE_H = height * 2 * PX;

        // The drawing, its periscope turned one way (-1 left, 0 up, 1
        // right): three frames, one on at a time — tentacles splayed,
        // curled, and the blink.
        function sprite(look) {
            return '<svg class="oc-sprite" viewBox="0 0 ' + width + ' ' + height * 2 + '" width="' + SPRITE_W +
                '" height="' + SPRITE_H + '" shape-rendering="crispEdges" aria-hidden="true" focusable="false">' +
                '<g class="oc-frame oc-open">' + rects(pose(look, false, false)) + '</g>' +
                '<g class="oc-frame oc-curl">' + rects(pose(look, false, true)) + '</g>' +
                '<g class="oc-frame oc-blink">' + rects(pose(look, true, false)) + '</g>' +
                '</svg>';
        }

        function plainText(html) {
            var scratch = document.createElement('div');
            scratch.innerHTML = html;
            return (scratch.textContent || '').replace(/\s+/g, ' ').trim();
        }

        function covered() {
            return root.hasAttribute('data-newsletter-prompt');
        }

        // Whether any of el is in the window.
        function visible(el) {
            var box = el.getBoundingClientRect();
            return box.bottom > 0 && box.top < (window.innerHeight || root.clientHeight);
        }

        // How much of el is in the window, from 0 to 1.
        function share(el) {
            var box = el.getBoundingClientRect();
            var seen = Math.min(box.bottom, window.innerHeight || root.clientHeight) - Math.max(box.top, 0);
            return box.height > 0 ? Math.max(0, seen) / box.height : 0;
        }

        // What the release is, in its own notes' words when the page has
        // them (the inline script publishes what it fetched for the pill),
        // else just its number. Built as text: the notes are GitHub's.
        var pill = document.getElementById('version-pill');
        function releaseLine(el) {
            var data = window.octoscopeRelease || {};
            var version = data.version || (pill ? pill.textContent.trim() : '');
            var strong = document.createElement('strong');
            el.textContent = '';
            if (data.headline) {
                strong.textContent = 'New in ' + version + ':';
                el.appendChild(strong);
                el.appendChild(document.createTextNode(' ' + data.headline));
            } else {
                strong.textContent = 'octoscope ' + version;
                el.appendChild(strong);
                el.appendChild(document.createTextNode(' is out.'));
            }
        }

        // ---- One place's octopus ----
        // hidden -> walking -> here -> leaving -> hidden. Its timers are its
        // own, so leaving cancels whatever it was still waiting for.
        function station(name, className, delay, inMs) {
            var el = document.createElement('div');
            el.className = 'oc-mascot ' + className;
            el.setAttribute('data-phase', 'hidden');
            var s = { name: name, el: el, phase: 'hidden', delay: delay, inMs: inMs, timers: [], waiting: false, moving: false };
            s.later = function (fn, ms) {
                var id = setTimeout(function () {
                    s.timers.splice(s.timers.indexOf(id), 1);
                    fn();
                }, ms);
                s.timers.push(id);
            };
            s.cancel = function () {
                s.timers.forEach(clearTimeout);
                s.timers = [];
                s.waiting = false;
            };
            s.set = function (phase) {
                s.phase = phase;
                el.setAttribute('data-phase', phase);
                // Leaving, nothing on it can be reached: it is about to go.
                el.inert = phase === 'leaving';
                legs(s);
                if (s.onSet) s.onSet(phase);
            };
            return s;
        }

        // Its legs go while it walks in, and in the corner while the page scrolls.
        function legs(s) {
            if (!reduced() && (s.phase === 'walking' || (s.phase === 'here' && s.moving))) {
                s.el.setAttribute('data-legs', '');
            } else {
                s.el.removeAttribute('data-legs');
            }
        }

        // One hop, with a blink as it comes down; then still. Every effect
        // here ends.
        function hop(s) {
            var svg = s.el.querySelector('.oc-sprite');
            if (reduced() || !svg || !svg.animate) return;
            svg.animate(
                [{ transform: 'none' }, { transform: 'translateY(-8px)', offset: 0.4 }, { transform: 'none' }],
                { duration: HOP_MS, easing: 'ease-out' }
            );
            setTimeout(function () {
                s.el.setAttribute('data-blink', '');
            }, 250);
            setTimeout(function () {
                s.el.removeAttribute('data-blink');
            }, 400);
        }

        // New words fade in.
        function fresh(el) {
            if (reduced() || !el.animate) return;
            el.animate(
                [{ opacity: 0, transform: 'translateY(3px)' }, { opacity: 1, transform: 'none' }],
                { duration: 260, easing: 'ease-out' }
            );
        }

        // Comes to its place: a beat (a place in the page waits for the eye),
        // then in, unless by then the place is no longer its.
        function come(s) {
            if (s.phase !== 'hidden' || s.waiting) return;
            s.waiting = true;
            s.later(function () {
                s.waiting = false;
                if (want() !== s.name || s.phase !== 'hidden') return;
                // A walk under the newsletter prompt's overlay is an
                // entrance nobody sees: it sets out once the prompt has gone.
                if (s.anchor && covered()) return;
                if (s.prepare) s.prepare();
                if (reduced()) {
                    // Settled, not skipped: it arrives standing.
                    s.set('here');
                    s.arrived();
                    return;
                }
                s.set('walking');
                s.later(function () {
                    if (s.phase !== 'walking') return;
                    s.set('here');
                    s.arrived();
                }, s.inMs);
            }, typeof s.delay === 'function' ? s.delay() : s.delay);
        }

        // Leaves its place: mid-walk it steps off at once, standing it fades.
        // The keyboard is handed somewhere first, never dropped to the page.
        function leave(s) {
            if (s.phase === 'hidden' || s.phase === 'leaving') {
                if (s.phase === 'hidden') s.cancel();
                return;
            }
            s.cancel();
            if (s.el.contains(document.activeElement) && s.focusBack) s.focusBack();
            if (s.phase === 'walking' || reduced()) {
                s.set('hidden');
                setTimeout(sync, 0);
                return;
            }
            s.set('leaving');
            s.later(function () {
                s.set('hidden');
                sync();
            }, LEAVE_MS);
        }

        var dismissed = false;
        var places = [];

        // Sent away from one place, it leaves them all for the life of the page.
        function dismissAll() {
            dismissed = true;
            places.forEach(leave);
        }

        var FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), ' +
            'textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

        // The keyboard was on an octopus that is about to go, in the corner
        // or on the card: hand it to a control on screen, without scrolling,
        // so the reader stays where they are. The last one before it on the
        // page that is in view; else any in view; else the last one before
        // it. The corner rides at any height, so the control before it in
        // the markup is usually a screen or more away (Codex, round 1 of
        // netfox.app's #83, where this was first written).
        function handFocusBack(el) {
            var others = Array.prototype.filter.call(document.querySelectorAll(FOCUSABLE), function (other) {
                return !el.contains(other) && other.tabIndex >= 0 && !other.closest('[aria-hidden="true"]') &&
                    other.getClientRects().length > 0;
            });
            var before = others.filter(function (other) {
                return el.compareDocumentPosition(other) & Node.DOCUMENT_POSITION_PRECEDING;
            });
            function inView(other) {
                var box = other.getBoundingClientRect();
                return box.bottom > 0 && box.top < window.innerHeight && box.right > 0 && box.left < window.innerWidth;
            }
            var shown = before.filter(inView);
            var target = shown[shown.length - 1] || others.filter(inView)[0] || before[before.length - 1];
            if (target) target.focus({ preventScroll: true });
        }

        // ---- hero: beside the version link ----
        var heroLink = document.querySelector('header .version-link');
        var hero = heroLink && heroLink.parentElement ? (function () {
            var href = heroLink.getAttribute('href');
            var s = station('hero', 'oc-guide oc-hero', HERO_DELAY_MS, WALK_MS);
            // The octopus is a second way to the same page for the pointer
            // only: the bubble's link is the one the keyboard and a screen
            // reader get.
            s.el.innerHTML =
                '<a class="oc-walker" href="' + href + '" tabindex="-1" aria-hidden="true">' + sprite(-1) + '</a>' +
                '<div class="oc-bubble">' +
                '<a class="oc-say" href="' + href + '"><span class="oc-caption"></span>' +
                '<span class="oc-next">Read the notes &rarr;</span></a>' +
                '<button type="button" class="oc-dismiss" aria-label="Dismiss">&times;</button>' +
                '</div>';
            heroLink.parentElement.appendChild(s.el);
            var say = s.el.querySelector('.oc-say');
            var caption = s.el.querySelector('.oc-caption');

            s.anchor = heroLink;
            // Room right of the octopus for its bubble, short of the window's edge.
            function room() {
                var box = heroLink.getBoundingClientRect();
                return (window.innerWidth || root.clientWidth) - GAP - (box.right + GAP + SPRITE_W + BUBBLE_GAP);
            }
            s.fits = function () {
                return room() >= MIN_ROOM;
            };
            // Feet on the link's bottom edge, a gap right of it.
            s.layout = function () {
                s.el.style.left = heroLink.offsetLeft + heroLink.offsetWidth + GAP + 'px';
                s.el.style.top = heroLink.offsetTop + heroLink.offsetHeight - SPRITE_H + 'px';
                s.el.style.setProperty('--oc-room', Math.max(MIN_ROOM, Math.min(MAX_ROOM, room())) + 'px');
            };
            s.speak = function () {
                releaseLine(caption);
                // Named for what it says and where it goes; the arrow is
                // not worth reading out.
                say.setAttribute('aria-label', caption.textContent + ' Read the notes');
            };
            s.prepare = function () {
                s.layout();
                s.speak();
            };
            s.arrived = function () {
                hop(s);
            };
            s.focusBack = function () {
                heroLink.focus({ preventScroll: true });
            };
            s.el.querySelector('.oc-dismiss').addEventListener('click', dismissAll);
            return s;
        })() : null;

        // ---- dots: beside the carousel's dots ----
        var foot = document.querySelector('.carousel-foot');
        // Where the stylesheet hides it: no room beside the dots for it and a bubble.
        var narrow = window.matchMedia ? window.matchMedia('(max-width: 64em)') : null;
        var dots = carousel && foot ? (function () {
            var s = station('dots', 'oc-guide oc-dots', function () {
                return corner.phase === 'here' || corner.phase === 'leaving' ? 0 : DOTS_DELAY_MS;
            }, WALK_MS);
            s.el.innerHTML =
                '<button type="button" class="oc-walker" aria-label="Show the next screenshot">' + sprite(-1) + '</button>' +
                '<div class="oc-bubble">' +
                '<button type="button" class="oc-say"><span class="oc-caption"></span>' +
                '<span class="oc-next">Next &rarr;</span></button>' +
                '<button type="button" class="oc-dismiss" aria-label="Dismiss">&times;</button>' +
                '</div>';
            foot.appendChild(s.el);
            var say = s.el.querySelector('.oc-say');
            var captionEl = s.el.querySelector('.oc-caption');
            // Why it holds the carousel: the pointer is on it, or the
            // KEYBOARD's focus is in it. Two reasons, so that one ending does
            // not release the other.
            var holding = { pointer: false, focus: false };

            function report() {
                carousel.hold('octopus', holding.pointer || holding.focus);
            }

            function speak(i) {
                var html = carousel.caption(i);
                captionEl.innerHTML = html;
                // Named for what it says AND for what it does: the visible
                // "Next" has to be in the name (WCAG 2.5.3); the arrow is not
                // worth reading out.
                say.setAttribute('aria-label', plainText(html) + ' Next');
                fresh(captionEl);
            }

            s.anchor = foot;
            s.fits = function () {
                return !(narrow && narrow.matches);
            };
            s.onSet = function (phase) {
                // The plain caption steps back as soon as the octopus sets
                // out: it stops where the end of a long caption would be, and
                // walking over the words it is about to say reads as a glitch.
                if (phase === 'walking' || phase === 'here') foot.setAttribute('data-narrated', '');
                else foot.removeAttribute('data-narrated');
                // Leaving or gone, nothing on it can hold the carousel: an
                // element that vanishes under the pointer never reports the
                // pointer leaving, and a hold nobody releases stops the
                // carousel for the rest of the page's life.
                if ((phase === 'leaving' || phase === 'hidden') && (holding.pointer || holding.focus)) {
                    holding.pointer = false;
                    holding.focus = false;
                    report();
                }
            };
            s.arrived = function () {
                speak(carousel.index());
                hop(s);
            };
            // The keyboard was on the × that is about to go: hand the focus
            // to the dot of the shot on screen, the control the octopus stood
            // beside, rather than let it drop to the page.
            s.focusBack = function () {
                var dot = carousel.activeDot();
                if (dot) dot.focus();
            };

            s.el.querySelector('.oc-walker').addEventListener('click', function () {
                carousel.next();
            });
            say.addEventListener('click', function () {
                carousel.next();
            });
            s.el.querySelector('.oc-dismiss').addEventListener('click', dismissAll);

            carousel.onChange(function (i) {
                if (s.phase !== 'here') return;
                speak(i);
                hop(s);
            });

            s.el.addEventListener('mouseenter', function () {
                holding.pointer = true;
                report();
            });
            s.el.addEventListener('mouseleave', function () {
                holding.pointer = false;
                report();
            });

            // Whether the browser draws the focus on el: its own answer to
            // "is this the keyboard?". Chrome also focuses a button on a mouse
            // click, and a click is how the octopus is used most, so a plain
            // focus hold would outlive the pointer and freeze the carousel.
            // An engine without the selector says yes to any focus.
            function shownFocus(el) {
                try {
                    return el.matches(':focus-visible');
                } catch (e) {
                    return true;
                }
            }
            // Every focus decides afresh: a click after a Tab moves the focus
            // without showing it, and lets go of the Tab's hold.
            s.el.addEventListener('focusin', function (event) {
                holding.focus = shownFocus(event.target);
                report();
            });
            s.el.addEventListener('focusout', function (event) {
                if (!s.el.contains(event.relatedTarget)) {
                    holding.focus = false;
                    report();
                }
            });
            // A key pressed after a click can turn the focus ring on with no
            // new focus event, and not every key does; so the browser is asked
            // once it has handled the key, rather than its rule guessed here.
            s.el.addEventListener('keydown', function (event) {
                var target = event.target;
                window.requestAnimationFrame(function () {
                    if (!holding.focus && document.activeElement === target && shownFocus(target)) {
                        holding.focus = true;
                        report();
                    }
                });
            });
            return s;
        })() : null;

        // A place in the page that cannot take the octopus now, but is on
        // screen: the corner says what it would.
        function heroNeedsCorner() {
            return !!hero && !hero.fits() && visible(hero.anchor);
        }
        function dotsNeedCorner() {
            return !!dots && !dots.fits() && visible(dots.anchor);
        }

        // The tips the corner gives: the page's own "At a glance" cards, the
        // originals rather than the marquee's copies, those whose words fit a
        // bubble. So it says nothing the page does not.
        var tips = [];
        each(document.querySelectorAll('.glance-track .feature:not([aria-hidden])'), function (card) {
            var title = card.querySelector('h3');
            var text = card.querySelector('p');
            if (!title || !text) return;
            var words = (text.textContent || '').replace(/\s+/g, ' ').trim();
            if (words.length <= TIP_CHARS) tips.push({ title: title.textContent.trim(), text: words });
        });

        // ---- corner: the window's corner ----
        var corner = (function () {
            var s = station('corner', 'oc-corner', 0, CORNER_IN_MS);
            s.el.innerHTML =
                '<button type="button" class="oc-walker">' + sprite(0) + '</button>' +
                '<div class="oc-announcer" aria-live="polite" aria-atomic="true"></div>' +
                '<div class="oc-bubble" hidden>' +
                '<div class="oc-say"><span class="oc-caption"></span><span class="oc-more"></span></div>' +
                '<button type="button" class="oc-dismiss" aria-label="Dismiss">&times;</button>' +
                '</div>';
            document.body.appendChild(s.el);
            var walker = s.el.querySelector('.oc-walker');
            var announcer = s.el.querySelector('.oc-announcer');
            var bubble = s.el.querySelector('.oc-bubble');
            var caption = s.el.querySelector('.oc-caption');
            var more = s.el.querySelector('.oc-more');
            // closed, or open on what it says: the release ('hero'), the
            // carousel's shot ('carousel'), or a tip the reader asked for.
            var mode = 'closed';
            // Each place's words are said by the corner once, on its own.
            var told = { hero: false, carousel: false };
            // The reader turned the carousel from here: its caption is theirs,
            // and folds when they scroll past it, not on the timer.
            var turned = false;
            // A turn the reader asked for, waiting for its caption to be said.
            var sayTurn = false;
            // Where the reader opened a tip: half a window away, it folds.
            var openedAt = 0;
            var said = -1;
            var spoken = null;
            var still = null;
            // It has hopped on arriving: from now on it may speak on its own.
            var settled = false;

            function name() {
                walker.setAttribute('aria-label', dotsNeedCorner() ? 'Show the next screenshot'
                    : heroNeedsCorner() && !told.hero ? "What's new" : mode === 'tip' ? 'Show another tip' : 'Show a tip');
            }

            // Only what the reader asked for is announced: the bubble opens by
            // itself and moves with the scroll, and a screen reader reading
            // something else should not be interrupted by it.
            function announce(words) {
                announcer.textContent = words;
                clearTimeout(spoken);
                spoken = setTimeout(function () {
                    announcer.textContent = '';
                }, SPOKEN_MS);
            }

            function open(next) {
                var was = mode;
                // The keyboard is in the bubble that is about to fold, or
                // whose buttons are about to change: it goes to the octopus,
                // which stays, rather than drop to the page (Codex, round 1).
                if ((next === 'closed' || next !== was) && bubble.contains(document.activeElement)) {
                    walker.focus({ preventScroll: true });
                }
                mode = next;
                bubble.hidden = next === 'closed';
                if (next !== 'carousel') turned = false;
                if (next !== was) {
                    if (next === 'hero') {
                        more.innerHTML = '<a class="oc-next" href="' + (heroLink ? heroLink.getAttribute('href') : '#') +
                            '">Read the notes &rarr;</a>';
                    } else if (next === 'carousel') {
                        more.innerHTML = '<button type="button" class="oc-next" data-act="turn" aria-label="Next screenshot">Next &rarr;</button>';
                    } else if (next === 'tip') {
                        more.innerHTML = '<button type="button" class="oc-next" data-act="tip" aria-label="Next tip">Next &rarr;</button>';
                    } else {
                        more.innerHTML = '';
                    }
                }
                if (next === 'hero') releaseLine(caption);
                if (next === 'carousel') caption.innerHTML = carousel.caption(carousel.index());
                // One caption on screen: the plain one under the dots steps
                // back while this one says it.
                if (foot) {
                    if (next === 'carousel') foot.setAttribute('data-corner-says', '');
                    else foot.removeAttribute('data-corner-says');
                }
                name();
            }

            function tip() {
                if (!tips.length) {
                    hop(s);
                    return;
                }
                // Asked for: what the places would say is no longer its to open.
                told.hero = told.carousel = true;
                said = (said + 1) % tips.length;
                var t = tips[said];
                if (mode !== 'tip') {
                    openedAt = window.scrollY;
                    open('tip');
                }
                caption.textContent = '';
                var strong = document.createElement('strong');
                strong.textContent = t.title;
                caption.appendChild(strong);
                caption.appendChild(document.createTextNode(' ' + t.text));
                fresh(caption);
                hop(s);
                announce(t.title + ': ' + t.text);
            }

            // The version link's words, asked for: the first click while
            // the hero is on screen and has no room for its own octopus.
            function whatsNew() {
                told.hero = true;
                open('hero');
                fresh(caption);
                hop(s);
                announce(caption.textContent);
            }

            function turn() {
                told.hero = told.carousel = true;
                if (mode !== 'carousel') open('carousel');
                turned = true;
                sayTurn = true;
                carousel.next();
            }

            if (carousel) {
                carousel.onChange(function (i) {
                    if (mode === 'carousel') {
                        caption.innerHTML = carousel.caption(i);
                        fresh(caption);
                        hop(s);
                    }
                    if (sayTurn) {
                        sayTurn = false;
                        announce(plainText(carousel.caption(i)));
                    }
                });
            }

            // What the dots would say, said once, by itself; it folds after a
            // while, since here it covers the page. Not the version link's
            // words: where the corner stands in for it, the page has just
            // loaded, and a bubble opened there sits on the hero's buttons
            // (measured at 390: over Download and Read the docs). Those wait
            // for the reader's first click on the octopus.
            function speakForPlaces() {
                if (!settled || s.phase !== 'here' || mode !== 'closed') return;
                if (!told.carousel && dotsNeedCorner() && carousel) {
                    told.carousel = true;
                    open('carousel');
                    s.later(function () {
                        // Not under the keyboard: a reader tabbing through it
                        // is reading it. It folds when the dots scroll past.
                        if (mode === 'carousel' && !turned && !bubble.contains(document.activeElement)) open('closed');
                    }, FOLD_MS);
                }
            }

            s.arrived = function () {
                hop(s);
                s.later(function () {
                    settled = true;
                    speakForPlaces();
                }, reduced() ? 0 : HOP_MS);
            };
            s.onSet = function (phase) {
                if (phase === 'hidden') {
                    settled = false;
                    s.moving = false;
                    open('closed');
                }
            };
            s.focusBack = function () {
                handFocusBack(s.el);
            };
            // On every look at the page: words whose place is gone fold.
            s.update = function () {
                if (mode === 'hero' && !heroNeedsCorner()) open('closed');
                if (mode === 'carousel' && !(dots && visible(dots.anchor))) open('closed');
                speakForPlaces();
                name();
            };
            s.scrolled = function () {
                if (s.phase === 'here' && !reduced()) {
                    if (!s.moving) {
                        s.moving = true;
                        legs(s);
                    }
                    clearTimeout(still);
                    still = setTimeout(function () {
                        s.moving = false;
                        legs(s);
                    }, STILL_MS);
                }
                if (mode === 'tip' && Math.abs(window.scrollY - openedAt) > window.innerHeight / 2) open('closed');
            };

            // The octopus itself: lands it if it is on its way in, then turns
            // the carousel where it narrates it, or gives a tip.
            walker.addEventListener('click', function () {
                if (s.phase === 'walking') {
                    s.cancel();
                    s.set('here');
                    s.arrived();
                }
                if (s.phase !== 'here') return;
                if (dotsNeedCorner() && carousel) turn();
                else if (heroNeedsCorner() && !told.hero) whatsNew();
                else tip();
            });
            more.addEventListener('click', function (event) {
                var act = event.target.closest('[data-act]');
                if (!act) return;
                if (act.getAttribute('data-act') === 'turn') turn();
                else tip();
            });
            s.el.querySelector('.oc-dismiss').addEventListener('click', dismissAll);
            name();
            return s;
        })();

        // ---- card: the footer's support card ----
        var cardHost = document.getElementById('sponsor');
        var card = cardHost ? (function () {
            var s = station('card', 'oc-card', 0, CARD_IN_MS);
            s.el.innerHTML =
                '<span class="oc-sitter">' + sprite(-1) + '</span>' +
                '<div class="oc-bubble"><p class="oc-line"></p>' +
                '<button type="button" class="oc-dismiss" aria-label="Dismiss">&times;</button></div>';
            s.el.querySelector('.oc-line').textContent = SPONSOR_LINE;
            cardHost.appendChild(s.el);
            s.arrived = function () {
                hop(s);
            };
            s.focusBack = function () {
                handFocusBack(s.el);
            };
            s.el.querySelector('.oc-dismiss').addEventListener('click', dismissAll);
            return s;
        })() : null;

        var inPage = [hero, dots].filter(Boolean);
        places = inPage.concat([corner, card].filter(Boolean));
        var cardSeen = false;

        // Where the octopus belongs now, from what is on screen.
        function want() {
            if (dismissed) return 'none';
            if (cardSeen) return covered() ? 'none' : 'card';
            var i;
            for (i = 0; i < inPage.length; i++) {
                if (inPage[i].fits() && visible(inPage[i].anchor)) return inPage[i].name;
            }
            return covered() ? 'none' : 'corner';
        }

        // Moves the octopus to where it belongs. Out of sight, a place in
        // the page keeps its octopus standing; on screen while another place
        // has it, or out of room, it goes.
        function sync() {
            if (card) {
                var seen = share(cardHost);
                var nav = document.querySelector('.site-nav');
                var top = cardHost.getBoundingClientRect().top - (nav ? nav.getBoundingClientRect().bottom : 0);
                if (seen >= CARD_RATIO && top >= CARD_ROOM) cardSeen = true;
                else if (seen <= 0 || top < CARD_KEEP) cardSeen = false;
            }
            var to = want();
            inPage.forEach(function (s) {
                if (s.name === to) return;
                if (!s.fits() || (s.phase !== 'hidden' && visible(s.anchor))) leave(s);
            });
            // The newsletter prompt opened mid-walk: it steps off, and walks
            // in again from the start once the prompt has gone.
            if (covered()) {
                inPage.forEach(function (s) {
                    if (s.phase === 'walking') leave(s);
                });
            }
            [corner, card].forEach(function (s) {
                if (s && s.name !== to) leave(s);
            });
            places.forEach(function (s) {
                if (s.name === to) come(s);
            });
            if (corner.phase === 'here') corner.update();
        }

        // One look per frame while the page scrolls. Measured, never
        // observed: an IntersectionObserver reports a change in what is
        // visible, and a jump straight past a place (an anchor, a restored
        // scroll) is none (found on findergit.app and netfox.app).
        var looking = false;
        window.addEventListener('scroll', function () {
            corner.scrolled();
            if (looking) return;
            looking = true;
            window.requestAnimationFrame(function () {
                looking = false;
                sync();
            });
        }, { passive: true });

        function relayout() {
            if (hero && hero.phase !== 'hidden') hero.layout();
            sync();
        }
        window.addEventListener('resize', relayout);
        // The web fonts move the version link when they land.
        window.addEventListener('load', relayout);
        if (document.fonts && document.fonts.ready) document.fonts.ready.then(relayout);

        // The release the inline script fetched for the pill: its headline is
        // what the octopus says about it.
        document.addEventListener('octoscope:release', function () {
            if (hero && hero.phase !== 'hidden') hero.speak();
            corner.update();
        });

        // The newsletter prompt: while it is open the corner and the card go,
        // a walk steps off, and one already standing in the page just stays.
        if ('MutationObserver' in window) {
            new MutationObserver(function () {
                if (covered()) sync();
                else setTimeout(sync, PROMPT_GONE_MS);
            }).observe(root, { attributes: true, attributeFilter: ['data-newsletter-prompt'] });
        }

        // Reduce Motion switched on mid-way: land where it was going, now,
        // and stand still.
        onMotionChange(function () {
            if (!reduced()) return;
            places.forEach(function (s) {
                s.moving = false;
                if (s.phase === 'walking') {
                    s.cancel();
                    s.set('here');
                    s.arrived();
                } else {
                    legs(s);
                }
            });
        });

        sync();
    })();
})();
