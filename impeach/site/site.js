/* Impeach landing page behaviour.
 *
 * Vanilla, no dependencies, no network. Everything here is an enhancement:
 * the page is complete and readable with this file absent or failing, and the
 * verdict is already landed in CSS so the most important word never depends
 * on script.
 *
 * prefers-reduced-motion is a kill switch. Under it nothing animates and every
 * scene is left in its final state.
 */
(function () {
  "use strict";

  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var dark = window.matchMedia("(prefers-color-scheme: dark)").matches;

  /* ------------------------------------------------------------- helpers */

  function onceInView(el, fn) {
    if (!el) return;
    if (reduce || !("IntersectionObserver" in window)) { fn(); return; }
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) { io.disconnect(); fn(); }
      });
    }, { threshold: 0.35 });
    io.observe(el);
  }

  /* ---------------------------------------------------------- hero scene */

  var hero = document.getElementById("hero");
  var canvas = document.getElementById("table");
  var verdict = document.getElementById("verdict");
  var recordEl = document.getElementById("record");
  if (!hero || !canvas) return;

  // The record lines and which of them contradict the testimony, read from
  // the DOM so the shader and the accessible copy cannot disagree.
  var lines = [].slice.call(recordEl ? recordEl.querySelectorAll("li") : []);
  var stamped = false;

  function landVerdict(originX, originY) {
    if (stamped) return;
    stamped = true;
    if (verdict) {
      verdict.classList.remove("staged");
      verdict.classList.add("landed");
    }
    scene && scene.stamp(originX, originY);
  }

  /* The record texture. Drawn once with Canvas2D at 2x, then sampled by the
     shader. Not redrawn per frame. */
  function buildRecordTexture(w, h) {
    var c = document.createElement("canvas");
    var scale = 2;
    c.width = Math.max(2, Math.floor(w * scale));
    c.height = Math.max(2, Math.floor(h * scale));
    var g = c.getContext("2d");
    if (!g) return { canvas: c, rects: [] };

    g.scale(scale, scale);
    g.clearRect(0, 0, w, h);
    g.textBaseline = "top";
    g.fillStyle = "#000";

    var rects = [];
    if (!lines.length) return { canvas: c, rects: rects };

    // Lay the record where the DOM record sits, offset slightly, so the ink
    // reads as a second hand writing underneath rather than a duplicate.
    var heroBox = hero.getBoundingClientRect();
    var size = Math.max(12, Math.min(15, w / 78));
    g.font = "400 " + size + "px 'JetBrains Mono', ui-monospace, monospace";

    lines.forEach(function (li) {
      var b = li.getBoundingClientRect();
      var x = b.left - heroBox.left + 8;
      var y = b.top - heroBox.top + 6;
      var text = li.textContent.replace(/\s+/g, " ").trim();
      g.fillText(text, x, y);

      if (li.classList.contains("bleeds")) {
        var m = g.measureText(text);
        // Top-down UV, the same space as the record texture and as the
        // shader's texUV lookup. Everything here stays top-down; only the
        // uniform upload converts to GL's bottom-up frame.
        rects.push([
          Math.max(0, (x - 6) / w),
          Math.max(0, (y - 4) / h),
          Math.min(1, (x + m.width + 6) / w),
          Math.min(1, (y + size + 6) / h)
        ]);
      }
    });
    return { canvas: c, rects: rects };
  }

  /* --------------------------------------------------------------- WebGL */

  function initGL() {
    var gl = canvas.getContext("webgl2", { antialias: false, alpha: false });
    if (!gl) return null;

    var VERT = "#version 300 es\n" +
      "in vec2 p; void main(){ gl_Position = vec4(p, 0.0, 1.0); }";

    function compile(type, src) {
      var s = gl.createShader(type);
      gl.shaderSource(s, src);
      gl.compileShader(s);
      if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
        // A shader that will not compile is a fallback for the visitor, but
        // it must not be silent for whoever is working on it. Failing quietly
        // is how the page shipped its fallback once already.
        if (window.console && console.warn) {
          console.warn("hero shader failed to compile:\n" + gl.getShaderInfoLog(s));
        }
        return null;
      }
      return s;
    }

    var frag = window.__HERO_FRAG__;
    if (!frag) return null;

    var vs = compile(gl.VERTEX_SHADER, VERT);
    var fs = compile(gl.FRAGMENT_SHADER, frag);
    if (!vs || !fs) return null;

    var prog = gl.createProgram();
    gl.attachShader(prog, vs);
    gl.attachShader(prog, fs);
    gl.linkProgram(prog);
    if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
      if (window.console && console.warn) {
        console.warn("hero program failed to link:\n" + gl.getProgramInfoLog(prog));
      }
      return null;
    }
    gl.useProgram(prog);

    var buf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, buf);
    gl.bufferData(gl.ARRAY_BUFFER,
      new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
    var loc = gl.getAttribLocation(prog, "p");
    gl.enableVertexAttribArray(loc);
    gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0);

    var u = {};
    ["uResolution", "uTime", "uLight", "uLightRadius", "uStamp",
     "uStampOrigin", "uBleed", "uBleedCount", "uDark", "uRecordTex"
    ].forEach(function (n) { u[n] = gl.getUniformLocation(prog, n); });

    var tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);

    return { gl: gl, u: u, tex: tex };
  }

  /* ---------------------------------------------------------- the scene */

  var scene = null;

  function makeScene(ctx) {
    var gl = ctx.gl, u = ctx.u;
    var dpr = Math.min(window.devicePixelRatio || 1, 1.5);
    var W = 0, H = 0;
    var bleed = [];
    var light = { x: 0, y: 0 };      // eased
    var target = { x: 0, y: 0 };     // raw input
    var radius = 200;
    var stampT = 0, stampAt = 0, stampOrigin = [0, 0];
    var running = false, visible = true, inView = true;
    var t0 = performance.now();
    var interacted = false;

    function resize() {
      var r = canvas.getBoundingClientRect();
      W = Math.max(1, Math.floor(r.width * dpr));
      H = Math.max(1, Math.floor(r.height * dpr));
      canvas.width = W;
      canvas.height = H;
      gl.viewport(0, 0, W, H);
      radius = Math.min(r.width, r.height) * 0.22 * dpr;

      var built = buildRecordTexture(r.width, r.height);
      bleed = built.rects;
      gl.bindTexture(gl.TEXTURE_2D, ctx.tex);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, built.canvas);

      // Start the lamp over the centroid of the bleed, so the point of the
      // page is not hidden before the visitor moves.
      if (!interacted) {
        var c = centroid();
        target.x = light.x = c[0] * W;
        target.y = light.y = c[1] * H;
      }
    }

    function centroid() {
      if (!bleed.length) return [0.5, 0.5];
      var sx = 0, sy = 0;
      bleed.forEach(function (r) { sx += (r[0] + r[2]) / 2; sy += (r[1] + r[3]) / 2; });
      return [sx / bleed.length, sy / bleed.length];
    }

    function overBleed() {
      if (!bleed.length) return false;
      var ux = light.x / W, uy = light.y / H;
      return bleed.some(function (r) {
        return ux > r[0] - 0.04 && ux < r[2] + 0.04 && uy > r[1] - 0.06 && uy < r[3] + 0.06;
      });
    }

    function draw(now) {
      var t = (now - t0) / 1000;

      // A spring, so the lamp lags the cursor by roughly 120ms and feels like
      // a held object rather than a pointer.
      if (reduce) {
        light.x = target.x; light.y = target.y;
      } else {
        light.x += (target.x - light.x) * 0.14;
        light.y += (target.y - light.y) * 0.14;
      }

      if (!stamped && !reduce && overBleed()) landVerdict(light.x / dpr, light.y / dpr);

      if (stampAt > 0) {
        stampT = Math.min(1, (now - stampAt) / 600);
      }

      gl.uniform2f(u.uResolution, W, H);
      gl.uniform1f(u.uTime, reduce ? 0 : t);
      // gl_FragCoord.y counts from the bottom, so the lamp is converted here
      // and nowhere else. Getting this wrong mirrors the light vertically,
      // which is subtle enough to look like a shader bug rather than a
      // coordinate one.
      gl.uniform2f(u.uLight, light.x, H - light.y);
      gl.uniform1f(u.uLightRadius, radius);
      gl.uniform1f(u.uStamp, stampT);
      gl.uniform2f(u.uStampOrigin, stampOrigin[0], H - stampOrigin[1]);
      gl.uniform1i(u.uBleedCount, Math.min(4, bleed.length));
      if (bleed.length) {
        var flat = new Float32Array(16);
        for (var i = 0; i < Math.min(4, bleed.length); i++) {
          flat[i * 4 + 0] = bleed[i][0];
          flat[i * 4 + 1] = bleed[i][1];
          flat[i * 4 + 2] = bleed[i][2];
          flat[i * 4 + 3] = bleed[i][3];
        }
        gl.uniform4fv(u.uBleed, flat);
      }
      gl.uniform1f(u.uDark, dark ? 1 : 0);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, ctx.tex);
      gl.uniform1i(u.uRecordTex, 0);

      gl.drawArrays(gl.TRIANGLES, 0, 3);

      // Under reduced motion one frame is the whole animation.
      if (reduce) { running = false; return; }
      if (running && visible && inView) requestAnimationFrame(draw);
      else running = false;
    }

    function start() {
      if (running) return;
      running = true;
      requestAnimationFrame(draw);
    }

    return {
      resize: resize,
      start: start,
      setVisible: function (v) { visible = v; if (v) start(); },
      setInView: function (v) { inView = v; if (v) start(); },
      point: function (x, y, isInteraction) {
        if (isInteraction) interacted = true;
        target.x = x * dpr;
        target.y = y * dpr;
        start();
      },
      stamp: function (x, y) {
        stampOrigin = [x * dpr, y * dpr];
        stampAt = performance.now();
        start();
      },
      hasBleed: function () { return bleed.length > 0; },
      interacted: function () { return interacted; }
    };
  }

  /* ------------------------------------------------------------ fallback */

  function initFallback() {
    hero.classList.add("no-webgl");
    // The lamp becomes a CSS mask positioned by two custom properties.
    var box = function () { return hero.getBoundingClientRect(); };
    function move(x, y) {
      var r = box();
      hero.style.setProperty("--lx", ((x - r.left) / r.width * 100) + "%");
      hero.style.setProperty("--ly", ((y - r.top) / r.height * 100) + "%");
      if (!stamped) {
        var bl = hero.querySelector(".record li.bleeds");
        if (bl) {
          var b = bl.getBoundingClientRect();
          if (y > b.top - 30 && y < b.bottom + 30) landVerdict(0, 0);
        }
      }
    }
    if (reduce) {
      var bl = hero.querySelector(".record li.bleeds");
      if (bl) {
        var r = box(), b = bl.getBoundingClientRect();
        hero.style.setProperty("--lx", ((b.left + b.width / 2 - r.left) / r.width * 100) + "%");
        hero.style.setProperty("--ly", ((b.top + b.height / 2 - r.top) / r.height * 100) + "%");
      }
      landVerdict(0, 0);
      return;
    }
    hero.style.setProperty("--lx", "50%");
    hero.style.setProperty("--ly", "58%");
    window.addEventListener("pointermove", function (e) { move(e.clientX, e.clientY); }, { passive: true });
    window.setTimeout(function () { landVerdict(0, 0); }, 6000);
  }

  /* ------------------------------------------------------ section rules */

  // Staged only when script is running, so a scriptless page simply has its
  // rules drawn.
  [].slice.call(document.querySelectorAll(".ruled")).forEach(function (h) {
    if (reduce) { h.classList.add("drawn"); return; }
    h.classList.add("stage");
    onceInView(h, function () { h.classList.add("drawn"); });
  });

  /* ------------------------------------------------------ context ledger */

  (function ledger() {
    var section = document.getElementById("ledger-section");
    var toolLog = document.querySelector('.chan[data-channel="tool-log"]');
    var tally = document.getElementById("tally");
    if (!section || !toolLog || !tally) return;

    var state = toolLog.querySelector("[data-state]");

    function settle() {
      toolLog.classList.add("redacted");
      if (state) state.textContent = "redacted";
      tally.textContent = "1 corroborated";
    }

    if (reduce) { settle(); return; }

    onceInView(section, function () {
      // The bars start present. After a beat the tool log is redacted, and
      // the count falls with it, because a redacted channel can no longer
      // corroborate anything.
      window.setTimeout(function () {
        toolLog.classList.add("sweeping");

        // The word and the hatch land with the ink, not before it, so the
        // final state is never visible ahead of the sweep that causes it.
        window.setTimeout(function () {
          if (state) state.textContent = "redacted";
          toolLog.classList.add("redacted");
        }, 460);

        var from = 3, to = 1, start = performance.now(), dur = 500;
        (function tick(now) {
          var t = Math.min(1, ((now || performance.now()) - start) / dur);
          var n = Math.round(from + (to - from) * t);
          tally.textContent = n + " corroborated";
          if (t < 1) requestAnimationFrame(tick);
        })(start);
      }, 800);
    });
  })();

  /* ------------------------------------------------------------ terminal */

  (function terminal() {
    var pre = document.getElementById("term");
    var cmd = document.getElementById("term-cmd");
    if (!pre || !cmd) return;

    var rows = [].slice.call(pre.querySelectorAll(".term-line.pending"));

    function settle() {
      rows.forEach(function (r) { r.classList.remove("pending"); });
    }
    if (reduce) { settle(); return; }

    var full = cmd.textContent;

    onceInView(pre, function () {
      // The command is typed by revealing its own text, so the line is never
      // absent from the document and stays selectable throughout.
      cmd.textContent = "";
      var caret = document.createElement("span");
      caret.className = "caret";
      caret.textContent = " ";
      cmd.appendChild(caret);

      var i = 0;
      var typer = window.setInterval(function () {
        i++;
        cmd.textContent = full.slice(0, i);
        if (i < full.length) {
          cmd.appendChild(caret);
        } else {
          window.clearInterval(typer);
          // Then the table arrives a row at a time.
          rows.forEach(function (r, n) {
            window.setTimeout(function () { r.classList.remove("pending"); }, 140 * (n + 1));
          });
        }
      }, 18);
    });
  })();

  /* ------------------------------------------------------------- numbers */

  (function figures() {
    var wrap = document.getElementById("figures");
    if (!wrap) return;
    var nums = [].slice.call(wrap.querySelectorAll(".fig-n"));

    if (reduce) return; // The final values are already in the markup.

    onceInView(wrap, function () {
      var start = performance.now(), dur = 900;
      (function tick(now) {
        var t = Math.min(1, ((now || performance.now()) - start) / dur);
        // Ease out, so the count decelerates into its final value.
        var e = 1 - Math.pow(1 - t, 3);
        nums.forEach(function (el) {
          var to = parseInt(el.getAttribute("data-to"), 10) || 0;
          el.textContent = String(Math.round(to * e));
        });
        if (t < 1) requestAnimationFrame(tick);
      })(start);
    });
  })();

  /* ---------------------------------------------------------------- boot */

  function boot(fragSource) {
    // The #version directive must be the very first thing in the source. An
    // inline script block starts with a newline after the opening tag, which
    // pushes it to line 2 and makes the whole shader compile as GLSL ES 1.00.
    // Every other error that produces is downstream of this one.
    window.__HERO_FRAG__ = String(fragSource).replace(/^\s+/, "");
    var ctx = initGL();
    if (!ctx) { initFallback(); return; }

    // Marks the shader as the owner of the record layer. Set before the
    // first measure so the texture is built from the final layout.
    hero.classList.add("webgl");

    scene = makeScene(ctx);
    scene.resize();
    scene.start();

    if (reduce) {
      // Final state, once, no loop and no drift.
      landVerdict(0, 0);
      scene.resize();
      scene.start();
      return;
    }

    var hasPointer = window.matchMedia("(pointer: fine)").matches;

    window.addEventListener("pointermove", function (e) {
      var r = canvas.getBoundingClientRect();
      scene.point(e.clientX - r.left, e.clientY - r.top, true);
    }, { passive: true });

    window.addEventListener("touchmove", function (e) {
      if (!e.touches.length) return;
      var r = canvas.getBoundingClientRect();
      scene.point(e.touches[0].clientX - r.left, e.touches[0].clientY - r.top, true);
    }, { passive: true });

    // Touch and pointerless devices get a slow drift across the bleed, so the
    // contradiction is discoverable without a cursor. Desktop does not drift
    // once the visitor has taken over.
    if (!hasPointer) {
      var t = 0;
      (function drift() {
        if (scene.interacted()) return;
        t += 0.006;
        var r = canvas.getBoundingClientRect();
        var cx = r.width * (0.5 + 0.22 * Math.sin(t));
        var cy = r.height * (0.56 + 0.12 * Math.sin(t * 1.7));
        scene.point(cx, cy, false);
        requestAnimationFrame(drift);
      })();
    }

    // Never land later than six seconds, so a visitor who does not move the
    // light still sees the verdict.
    window.setTimeout(function () { landVerdict(canvas.clientWidth * 0.5, canvas.clientHeight * 0.6); }, 6000);

    var rt;
    window.addEventListener("resize", function () {
      clearTimeout(rt);
      rt = setTimeout(function () { scene.resize(); scene.start(); }, 200);
    });

    document.addEventListener("visibilitychange", function () {
      scene.setVisible(!document.hidden);
    });

    if ("IntersectionObserver" in window) {
      new IntersectionObserver(function (es) {
        es.forEach(function (e) { scene.setInView(e.isIntersecting); });
      }, { threshold: 0 }).observe(canvas);
    }
  }

  // Stage the verdict only once we know script is running and motion is
  // wanted. Until then CSS has it landed.
  if (!reduce && verdict) verdict.classList.add("staged");

  // The shader lives in its own file so it stays readable. Fetch fails under
  // file:// in some browsers, so fall back to the inline copy when present.
  var inline = document.getElementById("hero-frag");
  if (inline && inline.textContent.trim()) {
    boot(inline.textContent);
  } else {
    fetch("hero.frag.glsl").then(function (r) { return r.text(); })
      .then(boot)
      .catch(function () { initFallback(); });
  }
})();
