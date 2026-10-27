// Shows a request that failed without swapping in the layout's
// #request-errors region, and clears it on the next request that succeeds.
// The listeners are on document, which no swap replaces; the region is looked
// up on each event, since a swap of body re-renders it.
(function () {
  function show(text) {
    var region = document.getElementById("request-errors");
    if (region) region.textContent = text;
  }

  function tried(detail) {
    var verb = ((detail.requestConfig && detail.requestConfig.verb) || "get").toUpperCase();
    var path = (detail.pathInfo && detail.pathInfo.requestPath) || "";
    return { verb: verb, path: path, what: verb + " " + path };
  }

  document.addEventListener("htmx:responseError", function (event) {
    var t = tried(event.detail);
    var xhr = event.detail.xhr;
    var status = (xhr.status + " " + xhr.statusText).trim();
    show(t.what + " failed: " + status + "." + (t.verb === "GET" ? "" : " Nothing was kept."));
  });

  document.addEventListener("htmx:sendError", function (event) {
    show(tried(event.detail).what + " failed: go-slow did not answer. Is it running?");
  });

  document.addEventListener("htmx:afterRequest", function (event) {
    if (event.detail.successful === true) show("");
  });
})();
