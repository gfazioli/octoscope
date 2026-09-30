/*
 * landing.js — the landing page's motion: the scroll reveals, the
 * screenshot carousel, and the octopus that narrates it.
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
    // The octopus: the TUI's launch mascot, walking in along the dots to
    // say what the carousel is showing. It arrives on every load; once
    // dismissed it stays away until the next one.
    // ------------------------------------------------------------------
    (function octopus() {
        var foot = document.querySelector('.carousel-foot');
        var artEl = document.getElementById('octopus-art');
        if (!carousel || !foot || !artEl) return;
        var art;
        try {
            art = JSON.parse(artEl.textContent);
        } catch (e) {
            return;
        }

        // Where the stylesheet hides it: no room for it and a bubble.
        var narrow = window.matchMedia ? window.matchMedia('(max-width: 64em)') : null;
        // After the dots come into view, a beat for the eye to land.
        var DELAY_MS = 700;
        // Matches oc-walk-in in the stylesheet.
        var WALK_MS = 2200;
        // Matches the fade on .oc-guide.
        var LEAVE_MS = 260;
        // After the newsletter prompt closes, a beat for its overlay to fade.
        var PROMPT_GONE_MS = 400;
        // Screen pixels per art pixel, across; twice as many down, as a
        // quadrant block's pixel is in a terminal cell.
        var PX = 3;
        // Where it looks: at the window, which is up and to its left.
        var LOOK = -1;

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
        var sprite =
            '<svg class="oc-sprite" viewBox="0 0 ' + width + ' ' + height * 2 + '" width="' + width * PX +
            '" height="' + height * 2 * PX + '" shape-rendering="crispEdges" aria-hidden="true" focusable="false">' +
            '<g class="oc-frame oc-open">' + rects(pose(LOOK, false, false)) + '</g>' +
            '<g class="oc-frame oc-curl">' + rects(pose(LOOK, false, true)) + '</g>' +
            '<g class="oc-frame oc-blink">' + rects(pose(LOOK, true, false)) + '</g>' +
            '</svg>';

        var guide = document.createElement('div');
        guide.className = 'oc-guide';
        guide.setAttribute('data-phase', 'hidden');
        guide.innerHTML =
            '<button type="button" class="oc-walker" aria-label="Show the next screenshot">' + sprite + '</button>' +
            '<div class="oc-bubble">' +
            '<button type="button" class="oc-say"><span class="oc-caption"></span>' +
            '<span class="oc-next">Next &rarr;</span></button>' +
            '<button type="button" class="oc-dismiss" aria-label="Dismiss">&times;</button>' +
            '</div>';
        foot.appendChild(guide);
        var walker = guide.querySelector('.oc-walker');
        var svg = guide.querySelector('.oc-sprite');
        var say = guide.querySelector('.oc-say');
        var captionEl = guide.querySelector('.oc-caption');
        var dismissButton = guide.querySelector('.oc-dismiss');

        // hidden -> walking -> pointing -> leaving -> hidden
        var phase = 'hidden';
        // For the life of the page: a reload brings it back.
        var dismissed = false;
        // The dots have been in view.
        var arrived = false;
        var walkEnd = null;
        // Why it holds the carousel: the pointer is on it, or the
        // KEYBOARD's focus is in it. Two reasons, so that one ending does
        // not release the other.
        var holding = { pointer: false, focus: false };

        function report() {
            carousel.hold('octopus', holding.pointer || holding.focus);
        }

        function covered() {
            return root.hasAttribute('data-newsletter-prompt');
        }

        function move(next) {
            phase = next;
            guide.setAttribute('data-phase', next);
            // The plain caption steps back as soon as the octopus sets
            // out: it stops where the end of a long caption would be, and
            // walking over the words it is about to say reads as a glitch.
            if (next === 'walking' || next === 'pointing') foot.setAttribute('data-narrated', '');
            else foot.removeAttribute('data-narrated');
            if (next === 'pointing') {
                speak(carousel.index());
                hop();
            }
            // Leaving or gone, nothing on it can hold the carousel: an
            // element that vanishes under the pointer never reports the
            // pointer leaving, and a hold nobody releases stops the
            // carousel for the rest of the page's life.
            if ((next === 'leaving' || next === 'hidden') && (holding.pointer || holding.focus)) {
                holding.pointer = false;
                holding.focus = false;
                report();
            }
        }

        function plainText(html) {
            var scratch = document.createElement('div');
            scratch.innerHTML = html;
            return (scratch.textContent || '').replace(/\s+/g, ' ').trim();
        }

        function speak(i) {
            var html = carousel.caption(i);
            captionEl.innerHTML = html;
            // Named for what it says AND for what it does: the visible
            // "Next" has to be in the name (WCAG 2.5.3); the arrow is not
            // worth reading out.
            say.setAttribute('aria-label', plainText(html) + ' Next');
            if (!reduced() && captionEl.animate) {
                captionEl.animate(
                    [{ opacity: 0, transform: 'translateY(3px)' }, { opacity: 1, transform: 'none' }],
                    { duration: 260, easing: 'ease-out' }
                );
            }
        }

        // One hop on arrival and on every new shot, with a blink as it
        // comes down; then still. Every effect here ends.
        function hop() {
            if (reduced() || !svg.animate) return;
            svg.animate(
                [{ transform: 'none' }, { transform: 'translateY(-8px)', offset: 0.4 }, { transform: 'none' }],
                { duration: 420, easing: 'ease-out' }
            );
            setTimeout(function () {
                guide.setAttribute('data-blink', '');
            }, 250);
            setTimeout(function () {
                guide.removeAttribute('data-blink');
            }, 400);
        }

        function walkIn() {
            if (dismissed || covered() || phase !== 'hidden') return;
            // A walk nobody can see: it sets out once there is room.
            if (narrow && narrow.matches) return;
            if (reduced()) {
                // Settled, not skipped: it arrives standing.
                move('pointing');
                return;
            }
            move('walking');
            walkEnd = setTimeout(function () {
                if (phase === 'walking') move('pointing');
            }, WALK_MS);
        }

        function dismiss() {
            dismissed = true;
            // The keyboard was on the × that is about to go: hand the focus
            // to the dot of the shot on screen, the control the octopus
            // stood beside, rather than let it drop to the page.
            if (guide.contains(document.activeElement)) {
                var dot = carousel.activeDot();
                if (dot) dot.focus();
            }
            move('leaving');
            setTimeout(function () {
                move('hidden');
            }, LEAVE_MS);
        }

        walker.addEventListener('click', function () {
            carousel.next();
        });
        say.addEventListener('click', function () {
            carousel.next();
        });
        dismissButton.addEventListener('click', dismiss);

        carousel.onChange(function (i) {
            if (phase !== 'pointing') return;
            speak(i);
            hop();
        });

        guide.addEventListener('mouseenter', function () {
            holding.pointer = true;
            report();
        });
        guide.addEventListener('mouseleave', function () {
            holding.pointer = false;
            report();
        });

        // Whether the browser draws the focus on el: its own answer to "is
        // this the keyboard?". Chrome also focuses a button on a mouse
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
        guide.addEventListener('focusin', function (event) {
            holding.focus = shownFocus(event.target);
            report();
        });
        guide.addEventListener('focusout', function (event) {
            if (!guide.contains(event.relatedTarget)) {
                holding.focus = false;
                report();
            }
        });
        // A key pressed after a click can turn the focus ring on with no
        // new focus event, and not every key does; so the browser is asked
        // once it has handled the key, rather than its rule guessed here.
        guide.addEventListener('keydown', function (event) {
            var target = event.target;
            window.requestAnimationFrame(function () {
                if (!holding.focus && document.activeElement === target && shownFocus(target)) {
                    holding.focus = true;
                    report();
                }
            });
        });

        // The newsletter prompt opens on about the scroll that brings the
        // dots into view, and a walk played behind its overlay is one
        // nobody sees. So while it is open the octopus does not set out;
        // if it opens mid-walk the octopus steps off, and walks in again
        // from the start once the prompt has gone. One already standing
        // just stays.
        if ('MutationObserver' in window) {
            new MutationObserver(function () {
                if (covered()) {
                    if (phase === 'walking') {
                        clearTimeout(walkEnd);
                        move('hidden');
                    }
                } else if (arrived) {
                    setTimeout(walkIn, PROMPT_GONE_MS);
                }
            }).observe(root, { attributes: true, attributeFilter: ['data-newsletter-prompt'] });
        }

        // The same for a window narrowed past the breakpoint: hiding the
        // octopus cancels its walk, and showing it again would restart
        // the walk under a timer still counting the first one — it would
        // jump to the end halfway. So it steps off mid-walk, lets go of
        // the carousel (nothing hidden can hold it), and walks in from the
        // start once there is room again.
        if (narrow) {
            var onWidth = function () {
                if (narrow.matches) {
                    if (phase === 'walking') {
                        clearTimeout(walkEnd);
                        move('hidden');
                    } else if (holding.pointer || holding.focus) {
                        holding.pointer = false;
                        holding.focus = false;
                        report();
                    }
                } else if (arrived) {
                    walkIn();
                }
            };
            if (narrow.addEventListener) narrow.addEventListener('change', onWidth);
            else if (narrow.addListener) narrow.addListener(onWidth);
        }

        // Reduce Motion switched on mid-walk: land where the walk was going, now.
        onMotionChange(function () {
            if (reduced() && phase === 'walking') {
                clearTimeout(walkEnd);
                move('pointing');
            }
        });

        function arrive() {
            if (arrived) return;
            arrived = true;
            setTimeout(walkIn, DELAY_MS);
        }
        // Where nothing can say the dots came into view, it comes after the delay.
        if (!('IntersectionObserver' in window)) {
            arrive();
            return;
        }
        var seen = new IntersectionObserver(function (entries) {
            if (entries.some(function (entry) { return entry.isIntersecting; })) {
                seen.disconnect();
                arrive();
            }
        }, { rootMargin: '0px 0px -10% 0px' });
        seen.observe(foot);
    })();
})();
