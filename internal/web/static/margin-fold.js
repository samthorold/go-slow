// Folds and unfolds a page's margin: the button marked data-margin-fold
// toggles the data-margin attribute ("open" or "folded") of the region that
// holds it, and the stylesheet hides the margin notes of a folded region.
// The state lives in that attribute alone, so nothing is remembered across
// pages: each page renders its own default. The button's aria-expanded is
// kept in step with the region, set from it whenever content loads
// (htmx:load, on the first page and on every swap).
(function () {
  function regionOf(button) {
    return button.closest("[data-margin]");
  }

  function sync(button) {
    var region = regionOf(button);
    if (region) button.setAttribute("aria-expanded", String(region.getAttribute("data-margin") === "open"));
  }

  document.addEventListener("htmx:load", function (event) {
    var root = event.target;
    if (root.matches && root.matches("[data-margin-fold]")) sync(root);
    if (root.querySelectorAll) root.querySelectorAll("[data-margin-fold]").forEach(sync);
  });

  // One listener on document, which no swap replaces: a button swapped in
  // later folds too. A button activates on click, Enter and Space alike.
  document.addEventListener("click", function (event) {
    var button = event.target.closest && event.target.closest("[data-margin-fold]");
    if (!button) return;
    var region = regionOf(button);
    if (!region) return;
    region.setAttribute("data-margin", region.getAttribute("data-margin") === "open" ? "folded" : "open");
    sync(button);
  });
})();
