// The page is complete without this script. Every evidence panel is present in
// the markup and open by default, so a reader with scripts disabled sees all
// the evidence. All this does is collapse the panels and add filtering.
(function () {
  "use strict";

  var rows = Array.prototype.slice.call(document.querySelectorAll("details.row"));
  if (!rows.length) return;

  // Collapse on load. Without JS they stay open, which is the point.
  rows.forEach(function (r) { r.open = false; });

  var controls = document.getElementById("controls");
  if (controls) controls.hidden = false;

  function setAll(open) {
    rows.forEach(function (r) {
      if (r.hidden) return;
      r.open = open;
    });
  }

  var expand = document.getElementById("expand-all");
  var collapse = document.getElementById("collapse-all");
  if (expand) expand.addEventListener("click", function () { setAll(true); });
  if (collapse) collapse.addEventListener("click", function () { setAll(false); });

  // Filters are additive toggles. With none pressed, everything shows.
  var filters = Array.prototype.slice.call(document.querySelectorAll("button[data-verdict]"));

  function active() {
    return filters
      .filter(function (b) { return b.getAttribute("aria-pressed") === "true"; })
      .map(function (b) { return b.getAttribute("data-verdict"); });
  }

  function apply() {
    var on = active();
    rows.forEach(function (r) {
      r.hidden = on.length > 0 && on.indexOf(r.getAttribute("data-verdict")) === -1;
    });
    var reset = document.getElementById("show-all");
    if (reset) reset.hidden = on.length === 0;
  }

  filters.forEach(function (b) {
    b.addEventListener("click", function () {
      var pressed = b.getAttribute("aria-pressed") === "true";
      b.setAttribute("aria-pressed", pressed ? "false" : "true");
      apply();
    });
  });

  var reset = document.getElementById("show-all");
  if (reset) {
    reset.addEventListener("click", function () {
      filters.forEach(function (b) { b.setAttribute("aria-pressed", "false"); });
      apply();
    });
  }

  apply();
})();
