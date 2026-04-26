let articles = [];
let lastPlan = null;
let referencePath = "";
let workspace = "";
let apiKey = localStorage.getItem("defapiApiKey") || "";
let renderedImages = {};
let renderedTemplate = null;
let templatePrompt = "";
let draggedIndex = null;
let currentStep = 1;
let busyCount = 0;
let isRendering = false;
let renderCallTotal = 0;
let renderCallDone = 0;
const $ = (id) => document.getElementById(id);
function esc(s) {
  return String(s).replace(
    /[&<>"']/g,
    (c) =>
      ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[
        c
      ],
  );
}
function lockButtons(on) {
  document.body.classList.toggle("busy", on);
  document.querySelectorAll("button").forEach((b) => {
    if (on) {
      b.dataset.wasDisabled = b.disabled ? "1" : "0";
      b.disabled = true;
    } else {
      b.disabled = b.dataset.wasDisabled === "1";
      delete b.dataset.wasDisabled;
    }
  });
}
async function withBusy(button, msg, fn) {
  if (busyCount > 0) return;
  busyCount++;
  const old = button ? button.innerHTML : "";
  lockButtons(true);
  if (button)
    button.innerHTML =
      '<span class="spinner"></span>' + esc(msg || "Working...");
  try {
    return await fn();
  } finally {
    if (button) button.innerHTML = old;
    busyCount--;
    lockButtons(false);
  }
}
function requireApiKey() {
  if (!apiKey) {
    $("keyGate").classList.remove("hidden");
    return false;
  }
  $("keyGate").classList.add("hidden");
  return true;
}
function saveApiKey() {
  const v = $("apiKeyInput").value.trim();
  if (!v) {
    $("apiKeyStatus").textContent = "Enter an API key.";
    return;
  }
  apiKey = v;
  localStorage.setItem("defapiApiKey", apiKey);
  $("apiKeyInput").value = "";
  $("keyGate").classList.add("hidden");
}
$("saveApiKey").onclick = saveApiKey;
$("changeApiKey").onclick = () => {
  $("apiKeyInput").value = apiKey;
  $("keyGate").classList.remove("hidden");
};
function updateWorkspaceLabel() {
  const el = $("workspaceLabel");
  if (el)
    el.innerHTML = workspace
      ? "Workspace: " +
        esc(workspace) +
        ' · <a href="/work/' +
        esc(workspace) +
        '/magazine.log" target="_blank">log</a>'
      : "No workspace yet";
}
function showStep(n) {
  updateWorkspaceLabel();
  currentStep = n;
  const pt = $("planToolbar");
  if (pt) pt.classList.toggle("hidden", n !== 3 || !lastPlan);
  const rt = $("renderToolbar");
  if (rt) rt.classList.toggle("hidden", n !== 4 || !lastPlan);
  document
    .querySelectorAll(".step-pill")
    .forEach((el) =>
      el.classList.toggle("active", +el.dataset.stepLabel === n),
    );
  [
    ["wizardStyle", 1],
    ["wizardArticles", 2],
    ["wizardPlan", 3],
    ["wizardPdf", 4],
  ].forEach(([id, step]) => $(id).classList.toggle("active", step === n));
  if (n >= 3 && lastPlan) renderPlan(lastPlan);
}
async function ensureStyle() {
  if (!requireApiKey()) return false;
  if ($("style").value.trim().startsWith("{")) return true;
  $("styleStatus").textContent = "Enhancing style JSON...";
  return await enhanceStyle();
}
async function enhanceStyle() {
  const fd = new FormData();
  fd.append("apiKey", apiKey);
  fd.append("title", $("title").value);
  fd.append("style", $("style").value);
  fd.append("workspace", workspace);
  if ($("reference").files[0]) fd.append("reference", $("reference").files[0]);
  const res = await fetch("/api/enhance-style", { method: "POST", body: fd });
  const data = await res.json();
  if (!res.ok) {
    $("styleStatus").textContent = data.error || "Failed";
    return false;
  }
  $("style").value = data.enhancedStyle;
  referencePath = data.referencePath || referencePath;
  workspace = data.workspace || workspace;
  updateWorkspaceLabel();
  $("styleStatus").textContent = referencePath
    ? "Enhanced with reference image."
    : "Enhanced.";
  return true;
}
function renderArticles() {
  const wrap = $("articles");
  wrap.innerHTML = "";
  articles.forEach((a, i) => {
    const div = document.createElement("div");
    div.className = "article";
    const kind = a.kind || "article";
    div.innerHTML =
      '<div class="row"><div><label>Type</label><select data-i="' +
      i +
      '" data-k="kind"><option value="article" ' +
      (kind === "article" ? "selected" : "") +
      '>Article</option><option value="feature" ' +
      (kind === "feature" ? "selected" : "") +
      '>Feature page</option></select></div><div><label>Pages</label><input type="number" min="1" max="8" value="' +
      esc(a.pages || 1) +
      '" data-i="' +
      i +
      '" data-k="pages"></div></div><label>Title</label><input value="' +
      esc(a.title || "") +
      '" data-i="' +
      i +
      '" data-k="title"><label>' +
      (kind === "feature" ? "Feature description" : "Body") +
      '</label><textarea data-i="' +
      i +
      '" data-k="body">' +
      esc(a.body || "") +
      '</textarea><label>Image URLs, one per line</label><textarea data-i="' +
      i +
      '" data-k="images">' +
      esc((a.images || []).join("\n")) +
      '</textarea><button class="ghost" data-remove="' +
      i +
      '">Remove</button>';
    wrap.appendChild(div);
  });
  wrap.querySelectorAll("input,textarea,select").forEach((el) => {
    const update = (e) => {
      const i = +e.target.dataset.i,
        k = e.target.dataset.k;
      articles[i][k] =
        k === "images"
          ? e.target.value
              .split("\n")
              .map((s) => s.trim())
              .filter(Boolean)
          : k === "pages"
            ? Math.max(1, Math.min(8, parseInt(e.target.value || "1", 10)))
            : e.target.value;
      articles[i].enhanced = false;
      if (k === "kind") renderArticles();
    };
    el.oninput = update;
    el.onchange = update;
  });
  wrap.querySelectorAll("[data-remove]").forEach(
    (el) =>
      (el.onclick = (e) => {
        if (isRendering) return;
        articles.splice(+e.target.dataset.remove, 1);
        renderArticles();
      }),
  );
}
$("toArticles").onclick = (e) =>
  withBusy(e.currentTarget, "Preparing...", async () => {
    if (await ensureStyle()) showStep(2);
  });
$("backStyle").onclick = () => {
  if (!isRendering) showStep(1);
};
$("backArticles").onclick = () => {
  if (!isRendering) showStep(2);
};
$("backPlan").onclick = () => {
  if (!isRendering) showStep(3);
};
$("toPlan").onclick = (e) =>
  withBusy(e.currentTarget, "Generating...", async () => {
    showStep(3);
    await buildPlan();
  });
$("toPdf").onclick = (e) =>
  withBusy(e.currentTarget, "Preparing...", () => startRenderFlow());
$("addArticle").onclick = () => {
  if (isRendering) return;
  articles.push({
    kind: "article",
    title: "",
    body: "",
    images: [],
    pages: 1,
    enhanced: false,
  });
  renderArticles();
};
$("addFeature").onclick = () => {
  if (isRendering) return;
  articles.push({
    kind: "feature",
    title: "",
    body: "Describe this feature page: comments, crossword, quiz, listings, letters, classifieds, TV program, calendar, chart, etc.",
    images: [],
    pages: 1,
    enhanced: false,
  });
  renderArticles();
};
$("clear").onclick = () => {
  if (isRendering) return;
  $("style").value = "";
  $("reference").value = "";
  referencePath = "";
  workspace = "";
  $("styleStatus").textContent = "";
};
$("generateArticles").onclick = (e) =>
  withBusy(e.currentTarget, "Generating...", async () => {
    if (!(await ensureStyle())) return;
    $("generateStatus").textContent =
      "Writing articles for this publication...";
    const res = await fetch("/api/generate-articles", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: $("title").value,
        style: $("style").value,
        count: 4,
        workspace,
        apiKey,
      }),
    });
    const data = await res.json();
    if (!res.ok) {
      $("generateStatus").textContent = data.error || "Failed";
      return;
    }
    workspace = data.workspace || workspace;
    updateWorkspaceLabel();
    articles = articles.concat(
      (data.articles || []).map((a) =>
        Object.assign({ kind: "article", pages: 1 }, a),
      ),
    );
    renderArticles();
    $("generateStatus").textContent =
      "Generated " + (data.articles || []).length + " articles.";
  });
$("importRSS").onclick = (e) =>
  withBusy(e.currentTarget, "Importing...", async () => {
    if (!(await ensureStyle())) return;
    $("rssStatus").textContent =
      "Fetching articles, extracting pages and rewriting...";
    const res = await fetch("/api/import-rss", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        url: $("rss").value,
        limit: 10,
        style: $("style").value,
        workspace,
        apiKey,
      }),
    });
    const data = await res.json();
    if (!res.ok) {
      $("rssStatus").textContent = data.error || "Failed";
      return;
    }
    workspace = data.workspace || workspace;
    updateWorkspaceLabel();
    articles = articles.concat(
      (data.articles || []).map((a) =>
        Object.assign({ kind: "article", pages: 1 }, a),
      ),
    );
    renderArticles();
    $("rssStatus").textContent =
      "Imported " + (data.articles || []).length + " rewritten articles.";
  });
async function buildPlan() {
  if (!(await ensureStyle())) return;
  const payload = {
    title: $("title").value,
    magazineType: "",
    style: $("style").value,
    pageCount: +$("pageCount").value,
    articles,
    workspace,
    apiKey,
  };
  const out = $("output");
  const textgenCalls = estimatePlanTextgenCalls();
  out.innerHTML = progressHTML(
    "Preparing " + textgenCalls + " textgen call(s)...",
    8,
    "planProgress",
  );
  renderedImages = {};
  const progressTimer = startEstimatedCallProgress(
    "planProgress",
    textgenCalls,
    "Textgen",
    10,
    88,
  );
  let res;
  try {
    res = await fetch("/api/build", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
  } finally {
    clearInterval(progressTimer);
  }
  setProgress("planProgress", 90, "Building page order...");
  const data = await res.json();
  if (!res.ok) {
    out.innerHTML =
      '<div class="kit">' + esc(data.error || "Failed") + "</div>";
    return;
  }
  lastPlan = data;
  workspace = data.workspace || workspace;
  updateWorkspaceLabel();
  lastPlan.reference = referencePath;
  templatePrompt = templatePrompt || defaultTemplatePrompt(data.style || {});
  articles = (
    data.articles && data.articles.length ? data.articles : articles
  ).map((a) => Object.assign({ kind: "article", pages: 1 }, a));
  renderArticles();
  setProgress("planProgress", 100, "Plan ready.");
  renderPlan(data);
}
$("build").onclick = (e) =>
  withBusy(e.currentTarget, "Generating...", async () => {
    await buildPlan();
    if (lastPlan) showStep(3);
  });
$("download").onclick = () => {
  if (!lastPlan || isRendering) return;
  const blob = new Blob([JSON.stringify(lastPlan, null, 2)], {
    type: "application/json",
  });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = "magazine-plan.json";
  a.click();
  URL.revokeObjectURL(a.href);
};
$("render").onclick = (e) =>
  withBusy(e.currentTarget, "Preparing...", () => startRenderFlow());
$("renderSide").onclick = (e) =>
  withBusy(e.currentTarget, "Preparing...", () => startRenderFlow());
$("downloadPdfJson").onclick = () => $("download").click();
function progressHTML(label, pct, id) {
  id = id || "planProgress";
  return (
    '<div class="progress-row"><div class="status" id="' +
    id +
    'Text">' +
    esc(label) +
    '</div><div class="progress"><div class="progress-bar" id="' +
    id +
    '" style="width:' +
    pct +
    '%"></div></div></div>'
  );
}
function setProgress(id, pct, label) {
  const bar = $(id);
  if (bar) bar.style.width = Math.max(0, Math.min(100, pct)) + "%";
  const text =
    $(id + "Text") ||
    $(id === "renderProgress" ? "renderProgressText" : "planProgressText");
  if (text && label) text.textContent = label;
}
function estimatePlanTextgenCalls() {
  return 1 + articles.filter((a) => !a.enhanced).length;
}
function startEstimatedCallProgress(id, total, label, startPct, endPct) {
  total = Math.max(1, total);
  let tick = 0;
  setProgress(id, startPct, label + " call 1 of " + total + "...");
  return setInterval(() => {
    tick++;
    const estimatedCall = Math.min(total, Math.floor(tick / 8) + 1);
    const pct =
      startPct +
      Math.min(0.95, tick / Math.max(8, total * 8)) * (endPct - startPct);
    setProgress(
      id,
      Math.round(pct),
      label + " call " + estimatedCall + " of " + total + "...",
    );
  }, 1000);
}
function setRenderProgress(label) {
  const next = Math.min(renderCallTotal, renderCallDone + 1);
  const pct = Math.round((renderCallDone / Math.max(1, renderCallTotal)) * 100);
  setProgress(
    "renderProgress",
    pct,
    label + " (external call " + next + " of " + renderCallTotal + ")...",
  );
}
function completeRenderCall(label) {
  renderCallDone = Math.min(renderCallTotal, renderCallDone + 1);
  const pct = Math.round((renderCallDone / Math.max(1, renderCallTotal)) * 100);
  setProgress(
    "renderProgress",
    pct,
    label + " (" + renderCallDone + " of " + renderCallTotal + " complete)",
  );
}
function defaultTemplatePrompt(style) {
  const styleText =
    style && style.template
      ? style.template
      : "blank content-page production dummy with header, folio, grid and image boxes";
  return (
    "Create one completely text-free magazine page layout template.\nFORMAT: Portrait magazine page, aspect ratio 1240:1754, full page visible edge to edge, no crop.\nTEMPLATE: " +
    styleText +
    "\nShow only generic layout geometry: paper texture, margins, column rhythm, blank header band, blank footer band, empty image rectangles, pale rule lines and subtle placeholder blocks. Absolutely no readable text, no letters, no numbers, no masthead, no captions, no labels, no arrows, no registration marks, no technical print marks, no UI and no moodboard."
  );
}
function renderPlan(data) {
  const kit =
    typeof data.creativeKit === "string"
      ? data.creativeKit
      : JSON.stringify(data.creativeKit, null, 2);
  const target =
    currentStep === 4 && $("renderOutput") ? $("renderOutput") : $("output");
  if (!target) return;
  target.innerHTML =
    '<div class="kit"><strong>Style JSON</strong>\n' +
    esc(JSON.stringify(data.style || {}, null, 2)) +
    "\n\n<strong>Creative kit</strong>\n" +
    esc(kit) +
    "</div>" +
    unplannedNoticeHTML() +
    '<div id="pageGrid" class="grid">' +
    pageGridHTML(data.pages || []) +
    "</div>";
  wirePromptEditors();
  wireSwapEditors();
  wireTemplateActions();
  wireDrag();
}
function pageGridHTML(pages) {
  const cover = pages[0] ? pageHTML(pages[0], 0) : "";
  let html = '<div class="top-pair">' + cover + templateCardHTML() + "</div>";
  const rest = pages.slice(1);
  for (let i = 0; i < rest.length; i += 2) {
    html +=
      '<div class="spread">' +
      pageHTML(rest[i], i + 1) +
      (rest[i + 1]
        ? pageHTML(rest[i + 1], i + 2)
        : '<article class="page fixed"><span class="kind">blank</span><h3>Inside cover</h3><div class="preview"></div></article>') +
      "</div>";
  }
  return html;
}
function pageHTML(p, i) {
  const fixed = i === 0 || i === lastPlan.pages.length - 1;
  const item = renderedImages[p.number] || null;
  const img = item ? item.image : "";
  const canDrag = !fixed && !isRendering;
  const swap = swapHTML(p, i, fixed);
  return (
    '<article class="page ' +
    (fixed ? "fixed" : "") +
    '" data-i="' +
    i +
    '" draggable="' +
    canDrag +
    '"><span class="kind">' +
    esc(p.kind) +
    "</span><h3>" +
    p.number +
    ". " +
    esc(p.title) +
    '</h3><div class="page-status" id="status-' +
    p.number +
    '">' +
    (img
      ? "Rendered"
      : fixed
        ? "Fixed page"
        : isRendering
          ? "Render locked"
          : "Drag to reorder") +
    "</div>" +
    swap +
    (img
      ? '<img class="preview" src="' + esc(img) + '">'
      : '<div class="preview"></div>') +
    '<label>Image prompt</label><textarea class="prompt" data-prompt-i="' +
    i +
    '">' +
    esc(p.prompt) +
    "</textarea></article>"
  );
}
function swapHTML(p, i, fixed) {
  if (isRendering || fixed || !p.article) return "";
  const opts = unplannedArticleEntries();
  if (!opts.length) return "";
  return (
    '<label>Swap with unplaced</label><select data-swap-i="' +
    i +
    '"><option value="">Keep this page</option>' +
    opts
      .map(
        (item) =>
          '<option value="' +
          item.index +
          '">' +
          esc(
            (item.article.kind === "feature" ? "Feature: " : "Article: ") +
              (item.article.title || "Untitled"),
          ) +
          "</option>",
      )
      .join("") +
    "</select>"
  );
}
function wireSwapEditors() {
  document.querySelectorAll("[data-swap-i]").forEach(
    (el) =>
      (el.onchange = (e) => {
        const i = +e.target.dataset.swapI;
        const articleIndex = parseInt(e.target.value, 10);
        if (!Number.isFinite(articleIndex) || !lastPlan || !lastPlan.pages[i])
          return;
        const a = articles[articleIndex];
        if (!a) return;
        const page = lastPlan.pages[i];
        page.article = Object.assign({ kind: "article", pages: 1 }, a);
        page.kind = page.article.kind || "article";
        page.title = page.article.title || "Untitled";
        page.images = page.article.images || [];
        page.prompt = buildArticlePromptClient(page, page.article);
        delete renderedImages[page.number];
        renderPlan(lastPlan);
      }),
  );
}
function buildArticlePromptClient(page, a) {
  const style = (lastPlan && lastPlan.style) || {};
  const kind = a.kind || "article";
  const styleText = [
    style.core,
    style.content,
    kind === "feature" ? style.feature : style.short,
    style.typography,
    style.color,
    style.print,
    style.avoid ? "Avoid: " + style.avoid : "",
  ]
    .filter(Boolean)
    .join(" ");
  return (
    "Create a " +
    kind +
    " page for " +
    JSON.stringify($("title").value || "Untitled Magazine") +
    ".\nFORMAT: Portrait magazine page, aspect ratio 1240:1754, full page visible edge to edge, no crop.\nSTYLE: " +
    styleText +
    "\nTITLE: " +
    (a.title || "Untitled") +
    "\nBRIEF/BODY: " +
    (a.body || "") +
    "\nLayout with headline, deck, byline/source if available, columns, image slots, captions, pull quote/sidebar where useful."
  );
}
function templateCardHTML() {
  const ready = renderedTemplate && renderedTemplate.publicUrl;
  const img =
    renderedTemplate && renderedTemplate.image
      ? '<img class="preview" src="' + esc(renderedTemplate.image) + '">'
      : '<div class="preview"></div>';
  const status = ready
    ? "Review before rendering content pages"
    : "Template placeholder, edit prompt before rendering";
  return (
    '<article class="page fixed"><span class="kind">template</span><h3>Template</h3><div class="page-status">' +
    status +
    "</div>" +
    img +
    '<label>Template prompt</label><textarea class="prompt" id="templatePrompt">' +
    esc(
      templatePrompt ||
        defaultTemplatePrompt((lastPlan && lastPlan.style) || {}),
    ) +
    '</textarea><div class="template-actions"><button id="templateUse" class="secondary" ' +
    (ready ? "" : "disabled") +
    '>✓ Use</button><button id="templateRefresh" class="ghost" ' +
    (lastPlan ? "" : "disabled") +
    ">↻ Refresh</button></div></article>"
  );
}
function getTemplatePrompt() {
  const prompt = $("templatePrompt");
  return prompt
    ? prompt.value
    : templatePrompt ||
        defaultTemplatePrompt((lastPlan && lastPlan.style) || {});
}
function wireTemplateActions() {
  const prompt = $("templatePrompt");
  if (prompt)
    prompt.oninput = (e) => {
      templatePrompt = e.target.value;
    };
  const use = $("templateUse");
  if (use)
    use.onclick = (e) => {
      e.preventDefault();
      templatePrompt = getTemplatePrompt();
      if (renderedTemplate && renderedTemplate.publicUrl)
        withBusy(e.currentTarget, "Rendering...", () =>
          renderRemainingPages(renderedTemplate.publicUrl),
        );
    };
  const refresh = $("templateRefresh");
  if (refresh)
    refresh.onclick = (e) => {
      e.preventDefault();
      templatePrompt = getTemplatePrompt();
      renderedTemplate = null;
      renderPlan(lastPlan);
      withBusy(e.currentTarget, "Regenerating...", () =>
        renderTemplateForReview(),
      );
    };
}
function wirePromptEditors() {
  document.querySelectorAll("[data-prompt-i]").forEach(
    (el) =>
      (el.oninput = (e) => {
        const i = +e.target.dataset.promptI;
        if (lastPlan && lastPlan.pages[i])
          lastPlan.pages[i].prompt = e.target.value;
      }),
  );
}
function showUnplannedNotice(data) {
  const out = $("output");
  if (out) {
    const note = unplannedNoticeHTML();
    if (note) out.insertAdjacentHTML("afterbegin", note);
  }
}
function unplannedNoticeHTML() {
  const missing = unplannedArticles();
  if (!missing.length) return "";
  return (
    '<div class="kit"><strong>Not placed</strong> ' +
    missing.length +
    " article/feature item(s) did not fit in the selected page count. Use the swap dropdown on a planned article page, increase pages, shorten page counts, or remove items.</div>"
  );
}
function unplannedArticles() {
  return unplannedArticleEntries().map((item) => item.article);
}
function unplannedArticleEntries() {
  if (!lastPlan) return [];
  const planned = new Set(
    (lastPlan.pages || [])
      .filter((p) => p.article)
      .map((p) => articleKey(p.article)),
  );
  return (articles || [])
    .map((article, index) => ({ article, index }))
    .filter((item) => !planned.has(articleKey(item.article)));
}
function articleKey(a) {
  return [
    a.kind || "article",
    a.title || "",
    a.body || "",
    a.source || "",
  ].join("\u001f");
}
function uniquePlannedArticles(pages) {
  const seen = new Set();
  const out = [];
  (pages || []).forEach((p) => {
    if (!p.article) return;
    const key = [
      p.article.kind || "article",
      p.article.title || "",
      p.article.body || "",
      p.article.source || "",
    ].join("\u001f");
    if (seen.has(key)) return;
    seen.add(key);
    out.push(Object.assign({ kind: "article", pages: 1 }, p.article));
  });
  return out;
}
function wireDrag() {
  if (isRendering) return;
  document.querySelectorAll(".page[draggable=true]").forEach((card) => {
    card.ondragstart = (e) => {
      draggedIndex = +card.dataset.i;
      card.classList.add("dragging");
    };
    card.ondragend = (e) => card.classList.remove("dragging");
    card.ondragover = (e) => e.preventDefault();
    card.ondrop = (e) => {
      e.preventDefault();
      const target = +card.dataset.i;
      if (
        draggedIndex === null ||
        target === 0 ||
        target === lastPlan.pages.length - 1 ||
        isRendering
      )
        return;
      movePage(draggedIndex, target);
    };
  });
}
function movePage(from, to) {
  const pages = lastPlan.pages;
  if (
    isRendering ||
    from === 0 ||
    from === pages.length - 1 ||
    to === 0 ||
    to === pages.length - 1
  )
    return;
  const [p] = pages.splice(from, 1);
  pages.splice(to, 0, p);
  renumberPages();
  renderPlan(lastPlan);
}
function renumberPages() {
  lastPlan.pages.forEach((p, i) => {
    p.number = i + 1;
  });
}
async function startRenderFlow() {
  if (!lastPlan) {
    $("renderStatus").textContent = "Generate a plan first.";
    return;
  }
  isRendering = true;
  showStep(4);
  renderedImages = {};
  renderedTemplate = null;
  $("templateReview").innerHTML = "";
  renderCallTotal = 2 + Math.max(0, lastPlan.pages.length - 1) + 1;
  renderCallDone = 0;
  $("renderStatus").innerHTML = progressHTML(
    "Starting imagegen call 1 of " + renderCallTotal + "...",
    1,
    "renderProgress",
  );
  renderPlan(lastPlan);
  try {
    setRenderProgress("Rendering cover");
    let cover = await renderPage(lastPlan.pages[0], "");
    renderedImages[1] = cover;
    completeRenderCall("Cover rendered");
    setRenderProgress("Rendering template");
    renderPlan(lastPlan);
    await renderTemplateForReview();
  } catch (e) {
    $("renderStatus").textContent = e.message || "Render failed";
    isRendering = false;
    renderPlan(lastPlan);
  }
}
async function renderTemplateForReview() {
  setRenderProgress("Rendering shared content template");
  const template = await renderTemplate();
  completeRenderCall("Template rendered");
  renderedTemplate = template;
  const templateRef = template.publicUrl || "";
  if (!templateRef) {
    $("renderStatus").textContent =
      "Template rendered, but imagegen did not return a public Image URL. Check workspace log.";
    isRendering = false;
    renderPlan(lastPlan);
    return;
  }
  setRenderProgress("Review the template card beside the cover.");
  $("templateReview").innerHTML =
    '<p class="muted">Use ✓ or refresh ↻ on the template card.</p>';
  renderPlan(lastPlan);
}
async function renderRemainingPages(templateRef) {
  try {
    $("templateReview").innerHTML = "";
    renderedTemplate = null;
    const middle = lastPlan.pages.slice(1);
    setRenderProgress("Rendering content pages");
    for (let i = 0; i < middle.length; i += 3) {
      await Promise.all(
        middle.slice(i, i + 3).map(async (p) => {
          setStatus(p.number, "Imagegen call queued...");
          const img = await renderPage(p, templateRef);
          renderedImages[p.number] = img;
          completeRenderCall("Rendered page " + p.number);
          renderPlan(lastPlan);
        }),
      );
      setRenderProgress("Rendering content pages");
    }
    const ordered = lastPlan.pages
      .map((p) => renderedImages[p.number] && renderedImages[p.number].image)
      .filter(Boolean);
    setRenderProgress("Writing PDF");
    const res = await fetch("/api/write-pdf", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: $("title").value,
        images: ordered,
        workspace,
      }),
    });
    const data = await res.json();
    if (!res.ok) {
      $("renderStatus").textContent = data.error || "PDF failed";
      return;
    }
    completeRenderCall("PDF ready");
    setProgress(
      "renderProgress",
      100,
      "Done. Download should start automatically.",
    );
    $("renderStatus").insertAdjacentHTML(
      "beforeend",
      '<div class="status"><a href="' +
        esc(data.pdf) +
        '" target="_blank">Open PDF</a></div>',
    );
    const a = document.createElement("a");
    a.href = data.pdf;
    a.download = "";
    document.body.appendChild(a);
    a.click();
    a.remove();
  } finally {
    isRendering = false;
    renderPlan(lastPlan);
  }
}
async function renderTemplate() {
  templatePrompt = getTemplatePrompt();
  const res = await fetch("/api/render-template", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      style: lastPlan.style,
      reference: referencePath,
      workspace,
      apiKey,
      prompt: templatePrompt,
    }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || "template failed");
  return { image: data.image, publicUrl: data.publicUrl || "" };
}
async function renderPage(page, styleReference) {
  setStatus(page.number, "Rendering...");
  const ref = page.number === 1 ? referencePath : "";
  const renderPage = Object.assign({}, page, {
    prompt: finalRenderPrompt(page),
  });
  const res = await fetch("/api/render-page", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      page: renderPage,
      styleReference,
      reference: ref,
      workspace,
      apiKey,
    }),
  });
  const data = await res.json();
  if (!res.ok) {
    setStatus(page.number, data.error || "Failed");
    throw new Error(data.error || "render failed");
  }
  setStatus(page.number, "Rendered");
  return { image: data.image, publicUrl: data.publicUrl || "" };
}
function finalRenderPrompt(page) {
  const side = page.number % 2 === 0 ? "left-hand page" : "right-hand page";
  const folioSide = page.number % 2 === 0 ? "left" : "right";
  let extra =
    "\n\nRENDER POSITION: This is page " +
    page.number +
    ", a " +
    side +
    ". Put page number " +
    page.number +
    " on the " +
    folioSide +
    " side in the footer.";
  if (page.kind === "cover") {
    extra +=
      "\nCOVER CONTENTS: Use cover lines for the actual final page order: " +
      coverLinePages() +
      ". These page numbers are final after reordering.";
  }
  return String(page.prompt || "") + extra;
}
function coverLinePages() {
  if (!lastPlan || !lastPlan.pages) return "inside stories";
  return (
    lastPlan.pages
      .filter((p) => p.number > 1 && p.kind !== "back-page")
      .filter(
        (p) => p.title && p.title !== "Advert" && p.title !== "Departments",
      )
      .slice(0, 7)
      .map((p) => "p" + p.number + " " + p.title)
      .join("; ") || "inside stories"
  );
}
function setStatus(n, msg) {
  const el = $("status-" + n);
  if (el) el.textContent = msg;
}
renderArticles();
updateWorkspaceLabel();
showStep(1);
requireApiKey();
