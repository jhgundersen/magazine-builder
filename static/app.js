let articles = [];
let lastPlan = null;
let referencePath = "";
let workspace = workspaceFromURL();
let apiKey = localStorage.getItem("defapiApiKey") || "";
let textModel = localStorage.getItem("defapiTextModel") || "claude";
let imageModel = localStorage.getItem("defapiImageModel") || "gpt2";
let renderedImages = {};
let pagePool = [];
let draggedIndex = null;
let currentStep = 1;
let busyCount = 0;
let isRendering = false;
let renderCallTotal = 0;
let renderCallDone = 0;
let originalStylePrompt = "";
const $ = (id) => document.getElementById(id);
function workspaceFromURL() {
  return new URLSearchParams(window.location.search).get("workspace") || "";
}
function updateURLWorkspace(ws) {
  const url = new URL(window.location.href);
  if (ws) {
    url.searchParams.set("workspace", ws);
  } else {
    url.searchParams.delete("workspace");
  }
  history.pushState({ workspace: ws }, "", url.toString());
}
async function saveState(key, value) {
  if (!workspace) return;
  await fetch("/api/workspace-state", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ workspace, key, value: JSON.stringify(value) }),
  }).catch(() => {});
}
let saveArticlesTimer = null;
function scheduleSaveArticles() {
  if (!workspace) return;
  clearTimeout(saveArticlesTimer);
  saveArticlesTimer = setTimeout(() => saveState("articles", articles), 1000);
}
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
function initModelSelectors() {
  if ($("textModel")) $("textModel").value = textModel;
  if ($("imageModel")) $("imageModel").value = imageModel;
  if ($("textModel"))
    $("textModel").onchange = (e) => {
      textModel = e.target.value || "claude";
      localStorage.setItem("defapiTextModel", textModel);
    };
  if ($("imageModel"))
    $("imageModel").onchange = (e) => {
      imageModel = e.target.value || "gpt2";
      localStorage.setItem("defapiImageModel", imageModel);
    };
}
function updateWorkspaceLabel() {
  updateURLWorkspace(workspace);
  const el = $("workspaceLabel");
  if (el)
    el.innerHTML = workspace
      ? "Workspace: " +
        esc(workspace) +
        ' · <a href="/work/' +
        esc(workspace) +
        '/magazine.log" target="_blank">log</a>'
      : "No workspace yet";
  const inp = $("workspaceInput");
  if (inp && workspace && !inp.value) inp.placeholder = workspace;
}
function showStep(n, skipSave) {
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
  if (!skipSave) saveState("step", n);
}
async function ensureStyle() {
  if (!requireApiKey()) return false;
  referencePath = $("reference").value.trim();
  if ($("style").value.trim().startsWith("{")) return true;
  $("styleStatus").textContent = "Enhancing style JSON...";
  return await enhanceStyle();
}
async function enhanceStyle() {
  originalStylePrompt = $("style").value;
  const fd = new FormData();
  fd.append("apiKey", apiKey);
  fd.append("title", $("title").value);
  fd.append("style", $("style").value);
  fd.append("workspace", workspace);
  fd.append("reference", $("reference").value.trim());
  fd.append("textModel", textModel);
  const res = await fetch("/api/enhance-style", { method: "POST", body: fd });
  const init = await res.json();
  if (!res.ok) {
    $("styleStatus").textContent = init.error || "Failed";
    return false;
  }
  workspace = init.workspace || workspace;
  updateWorkspaceLabel();
  $("styleStatus").textContent = "Enhancing style...";
  return new Promise((resolve) => {
    startTaskPolling(workspace, init.taskId, null, (t) => {
      if (t.status === "failed") {
        $("styleStatus").textContent = t.error || "Style enhancement failed";
        resolve(false);
        return;
      }
      try {
        const data = JSON.parse(t.outputJson);
        $("style").value = data.enhancedStyle;
        referencePath = data.referencePath || referencePath;
        workspace = data.workspace || workspace;
        updateWorkspaceLabel();
        saveState("style", {
          prompt: originalStylePrompt,
          enhancedJson: data.enhancedStyle,
          referencePath,
        });
        $("styleStatus").textContent = referencePath
          ? "Enhanced with reference image."
          : "Enhanced.";
        resolve(true);
      } catch (_) {
        $("styleStatus").textContent = "Could not parse style result.";
        resolve(false);
      }
    });
  });
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
      '>Feature page</option></select></div><div><label>Pages</label><input type="number" min="1" max="4" value="' +
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
            ? Math.max(1, Math.min(4, parseInt(e.target.value || "1", 10)))
            : e.target.value;
      articles[i].enhanced = false;
      if (k === "kind") renderArticles();
    };
    el.oninput = (e) => { update(e); scheduleSaveArticles(); };
    el.onchange = (e) => { update(e); scheduleSaveArticles(); };
  });
  wrap.querySelectorAll("[data-remove]").forEach(
    (el) =>
      (el.onclick = (e) => {
        if (isRendering) return;
        articles.splice(+e.target.dataset.remove, 1);
        renderArticles();
        scheduleSaveArticles();
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
$("exportArticles").onclick = () => {
  if (isRendering) return;
  const payload = {
    articles: normalizeArticlesForImport(articles),
  };
  const blob = new Blob([JSON.stringify(payload, null, 2)], {
    type: "application/json",
  });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = "magazine-articles.json";
  a.click();
  URL.revokeObjectURL(a.href);
};
$("importArticles").onclick = () => {
  if (isRendering) return;
  $("articleFile").click();
};
$("articleFile").onchange = (e) => {
  const file = e.target.files && e.target.files[0];
  if (!file || isRendering) return;
  const reader = new FileReader();
  $("generateStatus").textContent = "Reading article JSON...";
  reader.onload = () => {
    try {
      const imported = normalizeArticleImport(
        JSON.parse(String(reader.result || "")),
      );
      articles = imported;
      renderArticles();
      $("generateStatus").textContent =
        "Imported " + articles.length + " article(s).";
    } catch (err) {
      $("generateStatus").textContent =
        err.message || "Could not load article JSON.";
    } finally {
      e.target.value = "";
    }
  };
  reader.onerror = () => {
    $("generateStatus").textContent = "Could not read file.";
    e.target.value = "";
  };
  reader.readAsText(file);
};
$("clear").onclick = () => {
  if (isRendering) return;
  $("style").value = "";
  $("reference").value = "";
  referencePath = "";
  workspace = "";
  $("styleStatus").textContent = "";
};
$("planFile").onchange = (e) => {
  const file = e.target.files && e.target.files[0];
  if (!file || isRendering) return;
  const reader = new FileReader();
  $("importStatus").textContent = "Reading plan JSON...";
  reader.onload = () => {
    try {
      importPlanJSON(JSON.parse(String(reader.result || "")));
      $("importStatus").textContent = "Plan loaded.";
      showStep(3);
    } catch (err) {
      $("importStatus").textContent = err.message || "Could not load JSON.";
    } finally {
      e.target.value = "";
    }
  };
  reader.onerror = () => {
    $("importStatus").textContent = "Could not read file.";
    e.target.value = "";
  };
  reader.readAsText(file);
};
$("generateArticles").onclick = (e) =>
  withBusy(e.currentTarget, "Generating...", async () => {
    if (!(await ensureStyle())) return;
    $("generateStatus").textContent = "Submitting article generation task...";
    const res = await fetch("/api/generate-articles", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: $("title").value,
        style: $("style").value,
        count: 4,
        workspace,
        apiKey,
        textModel,
      }),
    });
    const init = await res.json();
    if (!res.ok) {
      $("generateStatus").textContent = init.error || "Failed";
      return;
    }
    workspace = init.workspace || workspace;
    updateWorkspaceLabel();
    $("generateStatus").textContent = "Generating articles...";
    await new Promise((resolve) => {
      startTaskPolling(workspace, init.taskId, null, (t) => {
        if (t.status === "failed") {
          $("generateStatus").textContent = t.error || "Failed";
        } else {
          try {
            const data = JSON.parse(t.outputJson);
            workspace = data.workspace || workspace;
            updateWorkspaceLabel();
            articles = articles.concat(
              (data.articles || []).map((a) =>
                Object.assign({ kind: "article", pages: 1 }, a),
              ),
            );
            renderArticles();
            scheduleSaveArticles();
            $("generateStatus").textContent =
              "Generated " + (data.articles || []).length + " articles.";
          } catch (_) {
            $("generateStatus").textContent = "Could not parse result.";
          }
        }
        resolve();
      });
    });
  });
$("importRSS").onclick = (e) =>
  withBusy(e.currentTarget, "Importing...", async () => {
    if (!(await ensureStyle())) return;
    const start = parseInt($("rssGroup").value || "0", 10) + 1;
    const end = start + 9;
    $("rssStatus").textContent =
      "Submitting import task for feed items " + start + "-" + end + "...";
    const res = await fetch("/api/import-rss", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        url: $("rss").value,
        limit: 10,
        offset: parseInt($("rssGroup").value || "0", 10),
        style: $("style").value,
        workspace,
        apiKey,
        textModel,
      }),
    });
    const init = await res.json();
    if (!res.ok) {
      $("rssStatus").textContent = init.error || "Failed";
      return;
    }
    workspace = init.workspace || workspace;
    updateWorkspaceLabel();
    $("rssStatus").textContent = "Importing and rewriting articles...";
    await new Promise((resolve) => {
      startTaskPolling(workspace, init.taskId, null, (t) => {
        if (t.status === "failed") {
          $("rssStatus").textContent = t.error || "Failed";
        } else {
          try {
            const data = JSON.parse(t.outputJson);
            workspace = data.workspace || workspace;
            updateWorkspaceLabel();
            articles = articles.concat(
              (data.articles || []).map((a) =>
                Object.assign({ kind: "article", pages: 1 }, a),
              ),
            );
            renderArticles();
            scheduleSaveArticles();
            $("rssStatus").textContent =
              "Imported " + (data.articles || []).length + " rewritten articles.";
          } catch (_) {
            $("rssStatus").textContent = "Could not parse result.";
          }
        }
        resolve();
      });
    });
  });
function applyBuildResult(data) {
  lastPlan = data;
  workspace = data.workspace || workspace;
  updateWorkspaceLabel();
  lastPlan.reference = referencePath;
  pagePool = buildPagePool(data.pages || []);
  articles = (
    data.articles && data.articles.length ? data.articles : articles
  ).map((a) => Object.assign({ kind: "article", pages: 1 }, a));
  renderArticles();
  saveState("plan", lastPlan);
}
async function buildPlan() {
  if (!(await ensureStyle())) return;
  ensureClientWorkspace();
  const payload = {
    title: $("title").value,
    magazineType: "",
    style: $("style").value,
    stylePrompt: originalStylePrompt || $("style").value,
    pageCount: +$("pageCount").value,
    articles,
    workspace,
    apiKey,
    textModel,
    imageModel,
  };
  const out = $("output");
  out.innerHTML = progressHTML("Submitting plan task...", 5, "planProgress");
  renderedImages = {};
  const res = await fetch("/api/build", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });
  const init = await res.json();
  if (!res.ok) {
    out.innerHTML =
      '<div class="kit">' + esc(init.error || "Failed") + "</div>";
    return;
  }
  workspace = init.workspace || workspace;
  updateWorkspaceLabel();
  await new Promise((resolve) => {
    startTaskPolling(workspace, init.taskId, "planProgress", (t) => {
      if (t.status === "failed") {
        out.innerHTML =
          '<div class="kit">' + esc(t.error || "Build failed") + "</div>";
      } else {
        try {
          const data = JSON.parse(t.outputJson);
          applyBuildResult(data);
          setProgress("planProgress", 100, "Plan ready.");
          renderPlan(data);
        } catch (_) {
          out.innerHTML =
            '<div class="kit">Could not parse plan result.</div>';
        }
      }
      resolve();
    });
  });
}
function ensureClientWorkspace() {
  if (workspace) return;
  workspace =
    typeof crypto !== "undefined" && crypto.randomUUID
      ? crypto.randomUUID()
      : "browser-" + Date.now().toString(36);
  updateWorkspaceLabel();
}
$("build").onclick = (e) =>
  withBusy(e.currentTarget, "Generating...", async () => {
    await buildPlan();
    if (lastPlan) showStep(3);
  });
$("download").onclick = () => {
  if (!lastPlan || isRendering) return;
  const blob = new Blob([JSON.stringify(exportPlanJSON(), null, 2)], {
    type: "application/json",
  });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = "magazine-plan.json";
  a.click();
  URL.revokeObjectURL(a.href);
};
function exportPlanJSON() {
  return Object.assign({}, lastPlan, {
    title: $("title").value,
    pageCount: lastPlan && lastPlan.pages ? lastPlan.pages.length : 0,
    reference: referencePath,
    workspace,
    textModel,
    imageModel,
    pagePool,
  });
}
function importPlanJSON(raw) {
  const data = normalizeImportedPlan(raw);
  lastPlan = data;
  workspace = data.workspace || "";
  textModel = data.textModel || textModel;
  imageModel = data.imageModel || imageModel;
  initModelSelectors();
  referencePath = data.reference || data.referencePath || "";
  renderedImages = {};
  isRendering = false;
  if (data.title || (data.style && data.style.name)) {
    $("title").value = data.title || data.style.name;
  }
  if (data.style) {
    $("style").value = JSON.stringify(data.style, null, 2);
  }
  if (data.pages && data.pages.length) {
    $("pageCount").value = String(data.pages.length);
  }
  $("reference").value = referencePath;
  articles = (
    data.articles && data.articles.length
      ? data.articles
      : uniquePlannedArticles(data.pages)
  ).map((a) => Object.assign({ kind: "article", pages: 1 }, a));
  pagePool = buildPagePool(data.pagePool || data.pages || []);
  renumberPages();
  renderArticles();
  updateWorkspaceLabel();
}
function normalizeImportedPlan(raw) {
  if (!raw || typeof raw !== "object") {
    throw new Error("The JSON file does not contain a plan.");
  }
  const data = raw.plan && typeof raw.plan === "object" ? raw.plan : raw;
  if (!Array.isArray(data.pages) || data.pages.length < 2) {
    throw new Error("The JSON file is missing planned pages.");
  }
  data.pages = data.pages.map((p, i) =>
    Object.assign(
      {
        number: i + 1,
        kind:
          i === 0
            ? "cover"
            : i === data.pages.length - 1
              ? "back-page"
              : "article",
        title: "Untitled",
        prompt: "",
      },
      p,
    ),
  );
  data.style = data.style || {};
  data.creativeKit = data.creativeKit || {};
  data.brandAssets = Array.isArray(data.brandAssets) ? data.brandAssets : [];
  data.articles = Array.isArray(data.articles) ? data.articles : [];
  data.issue = normalizeIssue(data.issue);
  return data;
}
function normalizeArticleImport(raw) {
  const list = Array.isArray(raw)
    ? raw
    : raw && Array.isArray(raw.articles)
      ? raw.articles
      : null;
  if (!list) {
    throw new Error("The JSON file is missing an articles array.");
  }
  return normalizeArticlesForImport(list);
}
function normalizeArticlesForImport(list) {
  return (Array.isArray(list) ? list : [])
    .map((a) =>
      Object.assign(
        {
          kind: "article",
          title: "",
          body: "",
          images: [],
          pages: 1,
          enhanced: false,
        },
        a || {},
      ),
    )
    .map((a) => {
      a.kind = a.kind === "feature" ? "feature" : "article";
      a.title = String(a.title || "");
      a.body = String(a.body || "");
      a.images = Array.isArray(a.images)
        ? a.images
            .map(String)
            .map((s) => s.trim())
            .filter(Boolean)
        : [];
      a.pages = Math.max(1, Math.min(4, parseInt(a.pages || "1", 10)));
      a.enhanced = Boolean(a.enhanced);
      if (a.source) a.source = String(a.source);
      return a;
    });
}
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
function estimatePlanDefapiTextCalls() {
  return 3 + articles.filter((a) => !a.enhanced).length;
}
function startTaskPolling(workspaceId, taskId, progressId, onDone) {
  let stopped = false;
  async function poll() {
    if (stopped || !workspaceId || !taskId) return;
    try {
      const res = await fetch(
        "/api/task?workspace=" +
          encodeURIComponent(workspaceId) +
          "&id=" +
          encodeURIComponent(taskId),
      );
      if (!res.ok) return;
      const t = await res.json();
      if (progressId && t.progressTotal > 0) {
        const pct = Math.round(
          (t.progressDone / Math.max(1, t.progressTotal)) * 100,
        );
        const label =
          t.progressMsg +
          " (" +
          t.progressDone +
          " of " +
          t.progressTotal +
          " complete)";
        setProgress(progressId, pct, label);
      }
      if (t.status === "done" || t.status === "failed") {
        stopped = true;
        clearInterval(timer);
        if (onDone) onDone(t);
      }
    } catch (_) {
      // keep polling quietly on network errors
    }
  }
  poll();
  const timer = setInterval(poll, 800);
  return {
    stop() {
      stopped = true;
      clearInterval(timer);
    },
  };
}
function setRenderProgress(label) {
  const next = Math.min(renderCallTotal, renderCallDone + 1);
  const pct = Math.round((renderCallDone / Math.max(1, renderCallTotal)) * 100);
  setProgress(
    "renderProgress",
    pct,
    label + " (render step " + next + " of " + renderCallTotal + ")...",
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
function renderPlan(data) {
  const kit =
    typeof data.creativeKit === "string"
      ? data.creativeKit
      : JSON.stringify(data.creativeKit, null, 2);
  const target =
    currentStep === 4 && $("renderOutput") ? $("renderOutput") : $("output");
  if (!target) return;
  target.innerHTML =
    '<div class="kit"><label>Style JSON</label><textarea class="prompt style-json" data-style-json>' +
    esc(JSON.stringify(data.style || {}, null, 2)) +
    '</textarea><div class="status" id="styleJsonStatus">Edit style JSON to update later prompts.</div><label>Creative kit JSON</label><textarea class="prompt style-json" data-kit-json>' +
    esc(kit) +
    '</textarea><div class="status" id="kitJsonStatus">Edit creative kit JSON to update later page prompts.</div>' +
    brandAssetsHTML(data.brandAssets || []) +
    "</div>" +
    unplannedNoticeHTML() +
    '<div id="pageGrid" class="grid">' +
    pageGridHTML(data.pages || []) +
    "</div>";
  wirePromptEditors();
  wireStyleEditor();
  wireKitEditor();
  wireSwapEditors();
  wireDrag();
}
function brandAssetsHTML(assets) {
  assets = Array.isArray(assets) ? assets : [];
  if (!assets.length) {
    return '<div class="brand-assets empty"><label>Brand assets</label><div class="status">No generated brand sheet for this plan.</div></div>';
  }
  return (
    '<div class="brand-assets"><label>Brand assets</label><div class="brand-asset-row">' +
    assets
      .map(
        (asset) =>
          '<figure class="brand-asset"><img src="' +
          esc(asset.image || asset.publicUrl || "") +
          '" alt=""><figcaption>' +
          esc(asset.label || asset.kind || "Brand reference") +
          (asset.publicUrl
            ? "<span>Used as one render reference image.</span>"
            : "<span>Preview only: no public URL returned.</span>") +
          "</figcaption></figure>",
      )
      .join("") +
    "</div></div>"
  );
}
function pageGridHTML(pages) {
  const cover = pages[0] ? pageHTML(pages[0], 0) : "";
  let html = '<div class="top-pair single">' + cover + "</div>";
  const rest = pages.slice(1);
  for (let i = 0; i < rest.length; i += 2) {
    const second = rest[i + 1] ? pageHTML(rest[i + 1], i + 2) : "";
    html +=
      '<div class="spread ' +
      (second ? "" : "single") +
      '">' +
      pageHTML(rest[i], i + 1) +
      second +
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
  if (isRendering || fixed) return "";
  const opts = unplannedPageEntries();
  if (!opts.length) return "";
  return (
    '<label>Swap with unplaced</label><select data-swap-i="' +
    i +
    '"><option value="">Keep this page</option>' +
    opts
      .map(
        (item) =>
          '<option value="' +
          item.key +
          '">' +
          esc(pageOptionLabel(item.page)) +
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
        const key = e.target.value;
        if (!key || !lastPlan || !lastPlan.pages[i]) return;
        const entry = unplannedPageEntries().find((item) => item.key === key);
        if (!entry) return;
        const number = lastPlan.pages[i].number;
        lastPlan.pages[i] = clonePageForSlot(entry.page, number);
        delete renderedImages[number];
        renderPlan(lastPlan);
      }),
  );
}
function buildPagePool(pages) {
  const seen = new Set();
  const pool = [];
  (pages || []).forEach((page, i) => {
    if (i === 0 || i === (pages || []).length - 1) return;
    if (page.article) return;
    const p = clonePageForSlot(page, page.number || i + 1);
    const key = pagePoolKey(p);
    if (seen.has(key)) return;
    seen.add(key);
    pool.push(p);
  });
  (articles || []).forEach((article) => {
    const p = {
      number: 0,
      kind: article.kind || "article",
      title: article.title || "Untitled",
      article: Object.assign({ kind: "article", pages: 1 }, article),
      images: article.images || [],
      prompt: "",
    };
    p.prompt = buildArticlePromptClient(p, p.article);
    const key = pagePoolKey(p);
    if (!seen.has(key)) {
      seen.add(key);
      pool.push(p);
    }
  });
  return pool;
}
function clonePageForSlot(page, number) {
  const cloned = JSON.parse(JSON.stringify(page || {}));
  cloned.number = number;
  if (cloned.article) {
    cloned.images = cloned.article.images || cloned.images || [];
  }
  return cloned;
}
function pagePoolKey(page) {
  const article = page.article || {};
  if (page.article) {
    return [
      "article",
      article.kind || page.kind || "article",
      article.title || page.title || "",
      article.source || "",
      article.body || "",
    ].join("\u001f");
  }
  return [page.kind || "article", page.title || "", page.prompt || ""].join(
    "\u001f",
  );
}
function plannedPageKeys() {
  return new Set(
    (lastPlan && lastPlan.pages ? lastPlan.pages : [])
      .slice(1, -1)
      .map((page) => pagePoolKey(page)),
  );
}
function unplannedPageEntries() {
  const planned = plannedPageKeys();
  return (pagePool || [])
    .map((page) => ({ page, key: pagePoolKey(page) }))
    .filter((item) => !planned.has(item.key));
}
function pageOptionLabel(page) {
  const prefix =
    page.kind === "advert"
      ? "Advert"
      : page.kind === "feature"
        ? "Feature"
        : page.kind === "filler"
          ? "Department"
          : "Article";
  return prefix + ": " + (page.title || "Untitled");
}
function buildArticlePromptClient(page, a) {
  const style = (lastPlan && lastPlan.style) || {};
  const kind = a.kind || "article";
  return JSON.stringify({
    task: "Create a print magazine content page.",
    metadata: {
      publication: $("title").value || "Untitled Magazine",
      page_role: kind,
      language: style.language || "English",
      format:
        "Portrait magazine page, aspect ratio 1240:1754, full page visible edge to edge, no crop.",
      tone: style.tone || "editorial",
      issue: issueContext(),
    },
    style: {
      visual_brief: visualStyleBrief(kind),
    },
    content: {
      title: a.title || "Untitled",
      brief_body: a.body || "",
    },
    layout: {
      required_elements:
        "headline, deck, byline/source if available, columns, image slots, article-specific image text, pull quote/sidebar where useful",
    },
    constraints: ["full page visible", "no crop"],
  });
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
function wireStyleEditor() {
  document.querySelectorAll("[data-style-json]").forEach((el) => {
    el.oninput = (e) => {
      if (!lastPlan) return;
      try {
        const nextStyle = JSON.parse(e.target.value || "{}");
        lastPlan.style = nextStyle;
        $("style").value = JSON.stringify(nextStyle, null, 2);
        pagePool = buildPagePool(lastPlan.pages || []);
        const status = $("styleJsonStatus");
        if (status) status.textContent = "Style JSON applied.";
      } catch (err) {
        const status = $("styleJsonStatus");
        if (status)
          status.textContent =
            "Invalid JSON: " + (err.message || "check syntax");
      }
    };
  });
}
function wireKitEditor() {
  document.querySelectorAll("[data-kit-json]").forEach((el) => {
    el.oninput = (e) => {
      if (!lastPlan) return;
      try {
        lastPlan.creativeKit = JSON.parse(e.target.value || "{}");
        const status = $("kitJsonStatus");
        if (status) status.textContent = "Creative kit JSON applied.";
      } catch (err) {
        const status = $("kitJsonStatus");
        if (status)
          status.textContent =
            "Invalid JSON: " + (err.message || "check syntax");
      }
    };
  });
}
function showUnplannedNotice(data) {
  const out = $("output");
  if (out) {
    const note = unplannedNoticeHTML();
    if (note) out.insertAdjacentHTML("afterbegin", note);
  }
}
function unplannedNoticeHTML() {
  const missing = unplannedPageEntries();
  if (!missing.length) return "";
  return (
    '<div class="kit"><strong>Not placed</strong> ' +
    missing.length +
    " page option(s) are available, including unused articles, adverts, or department pages. Use the swap dropdown on any middle page to place them.</div>"
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
  saveState("plan", lastPlan);
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
  renderCallTotal = lastPlan.pages.length + 2;
  renderCallDone = 0;
  $("renderStatus").innerHTML = progressHTML(
    "Starting render step 1 of " + renderCallTotal + "...",
    1,
    "renderProgress",
  );
  renderPlan(lastPlan);
  try {
    setRenderProgress("Planning cover lines");
    lastPlan.coverPlan = await generateCoverPlan();
    completeRenderCall("Cover plan ready");
    await renderRemainingPages();
  } catch (e) {
    $("renderStatus").textContent = e.message || "Render failed";
    isRendering = false;
    renderPlan(lastPlan);
  }
}
async function generateCoverPlan() {
  const res = await fetch("/api/cover-plan", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      title: $("title").value,
      style: lastPlan.style || {},
      pages: lastPlan.pages || [],
      issue: issueContext(),
      workspace,
      apiKey,
      textModel,
    }),
  });
  const init = await res.json();
  if (!res.ok) throw new Error(init.error || "cover plan task start failed");
  workspace = init.workspace || workspace;
  updateWorkspaceLabel();
  return new Promise((resolve, reject) => {
    startTaskPolling(workspace, init.taskId, null, (t) => {
      if (t.status === "failed") {
        reject(new Error(t.error || "cover plan failed"));
        return;
      }
      try {
        const data = JSON.parse(t.outputJson);
        resolve(data.coverPlan || null);
      } catch (e) {
        reject(new Error("could not parse cover plan result"));
      }
    });
  });
}
async function renderRemainingPages() {
  try {
    const pages = lastPlan.pages.slice();
    setRenderProgress("Rendering pages");
    let index = 0;
    async function worker() {
      while (index < pages.length) {
        const p = pages[index++];
        setStatus(p.number, "Defapi image call queued...");
        const img = await renderPage(p, "");
        renderedImages[p.number] = img;
        completeRenderCall("Rendered page " + p.number);
        renderPlan(lastPlan);
      }
    }
    await Promise.all([worker(), worker(), worker()]);
    const ordered = lastPlan.pages
      .map((p) => renderedImages[p.number] && renderedImages[p.number].image)
      .filter(Boolean);
    if (ordered.length !== lastPlan.pages.length) {
      throw new Error(
        "Missing rendered pages: expected " +
          lastPlan.pages.length +
          ", got " +
          ordered.length,
      );
    }
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
async function renderPage(page, styleReference) {
  setStatus(page.number, "Queuing render...");
  const ref = page.number === 1 ? referencePath : "";
  const renderPageData = Object.assign({}, page, {
    prompt: finalRenderPrompt(page),
  });
  const res = await fetch("/api/render-page", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      page: renderPageData,
      style: lastPlan.style || {},
      issue: issueContext(),
      brandAssets: brandAssetsForRender(page),
      styleReference,
      reference: ref,
      workspace,
      apiKey,
      textModel,
      imageModel,
    }),
  });
  const init = await res.json();
  if (!res.ok) {
    setStatus(page.number, init.error || "Failed");
    throw new Error(init.error || "render task start failed");
  }
  workspace = init.workspace || workspace;
  setStatus(page.number, "Rendering...");
  return new Promise((resolve, reject) => {
    startTaskPolling(workspace, init.taskId, null, (t) => {
      if (t.status === "failed") {
        setStatus(page.number, t.error || "Render failed");
        reject(new Error(t.error || "render failed"));
        return;
      }
      try {
        const data = JSON.parse(t.outputJson);
        setStatus(page.number, "Rendered");
        resolve({ image: data.image, publicUrl: data.publicUrl || "" });
      } catch (e) {
        setStatus(page.number, "Parse error");
        reject(new Error("could not parse render result"));
      }
    });
  });
}
function finalRenderPrompt(page) {
  const side = page.number % 2 === 0 ? "left-hand page" : "right-hand page";
  const folioSide = page.number % 2 === 0 ? "left" : "right";
  const renderPosition = {
    page_number: page.number,
    side,
    language: ((lastPlan && lastPlan.style) || {}).language || "English",
    issue: issueContext(),
    footer_folio:
      "Put page number " +
      page.number +
      " on the " +
      folioSide +
      " side in the footer.",
  };
  if (page.kind === "cover") {
    renderPosition.cover_plan = lastPlan.coverPlan || {
      lines: coverLinePages(),
    };
  }
  try {
    const prompt = JSON.parse(String(page.prompt || "{}"));
    delete prompt.source_prompt;
    prompt.metadata = Object.assign({}, prompt.metadata || {}, {
      language: ((lastPlan && lastPlan.style) || {}).language || "English",
      tone: ((lastPlan && lastPlan.style) || {}).tone || "editorial",
      issue: issueContext(),
    });
    prompt.style = {
      visual_brief: visualStyleBrief(page.kind || "content"),
      creative_kit: creativeKitForPage(page.kind || "content"),
    };
    const brand = brandAssetPrompt(page);
    if (brand) prompt.style.brand_assets = brand;
    if (prompt.layout) {
      delete prompt.layout.continuity;
    }
    if (prompt.content && prompt.content.brief_body) {
      prompt.content.brief_body = compactClient(
        prompt.content.brief_body,
        1300,
      );
    }
    prompt.render_position = renderPosition;
    return fitPromptJSON(prompt);
  } catch (_) {
    const prompt = cleanPromptFromPage(page);
    prompt.render_position = renderPosition;
    return fitPromptJSON(prompt);
  }
}
function brandAssetsForRender(page) {
  if (!lastPlan || page.kind === "advert") return [];
  return (lastPlan.brandAssets || []).filter((asset) => asset.publicUrl);
}
function brandAssetPrompt(page) {
  if (!lastPlan || page.kind === "advert") return "";
  if (!brandAssetsForRender(page).length) return "";
  const use = brandAssetUseForPage(page);
  return (
    "A supplied brand asset board contains masthead, wordmark, issue seal, divider and folio marks. For this page, use at most one element from the board: " +
    use.element +
    ". Purpose: " +
    use.purpose +
    ". Do not use the other board elements on this page. Do not reproduce the whole board, its background, spacing, labels, or any non-brand explanatory text."
  );
}
function brandAssetUseForPage(page) {
  const kind = page.kind || "article";
  if (kind === "cover") {
    return {
      element: "the large masthead only",
      purpose: "main cover identity",
    };
  }
  if (kind === "back-page") {
    return {
      element: "the issue seal or small folio mark only",
      purpose: "small closing-page furniture",
    };
  }
  if (kind === "feature") {
    return {
      element: "the small horizontal wordmark only",
      purpose: "running header identity",
    };
  }
  if (kind === "filler") {
    return {
      element: "the divider or rule motif only",
      purpose: "department-page structure",
    };
  }
  return {
    element:
      page.number % 2 === 0
        ? "the small folio mark only"
        : "the small horizontal wordmark only",
    purpose: "subtle recurring page furniture",
  };
}
function fitPromptJSON(prompt) {
  let out = JSON.stringify(prompt);
  if (out.length <= 3800) return out;
  if (prompt.content && prompt.content.brief_body) {
    prompt.content.brief_body = compactClient(prompt.content.brief_body, 900);
  }
  out = JSON.stringify(prompt);
  if (out.length <= 3800) return out;
  if (prompt.style && prompt.style.visual_brief) {
    prompt.style.visual_brief = compactClient(prompt.style.visual_brief, 850);
  }
  out = JSON.stringify(prompt);
  if (out.length <= 3800) return out;
  if (prompt.style && prompt.style.creative_kit) {
    delete prompt.style.creative_kit;
  }
  return JSON.stringify(prompt);
}
function cleanPromptFromPage(page) {
  const style = (lastPlan && lastPlan.style) || {};
  const article = page.article || {};
  const body = article.body || page.body || "";
  return {
    task:
      page.kind === "cover"
        ? "Create the magazine cover."
        : page.kind === "advert"
          ? "Create a fictional advert page for this publication."
          : "Create a print magazine content page.",
    metadata: {
      publication: $("title").value || "Untitled Magazine",
      page_role: page.kind || "article",
      language: style.language || "English",
      tone: style.tone || "editorial",
      issue: issueContext(),
      format:
        "Portrait magazine page, aspect ratio 1240:1754, full page visible edge to edge, no crop.",
    },
    style: {
      visual_brief: visualStyleBrief(page.kind || "content"),
      creative_kit: creativeKitForPage(page.kind || "content"),
      brand_assets: brandAssetPrompt(page) || undefined,
    },
    content: {
      title: page.title || article.title || "Untitled",
      brief_body: compactClient(body, 1500),
      modules: pageModules(page),
      image_text_notes:
        "If images or illustrations are used, write short image text from this article. Do not use generic reusable image text. Write only the image text itself, without a label prefix.",
    },
    layout: {
      required_elements:
        "headline, deck, byline/source if available, readable columns, image or comic illustration slots, article-specific image text, pull quote/sidebar where useful",
    },
    constraints: [
      "full page visible",
      "no crop",
      style.avoid ? "avoid " + style.avoid : "",
    ].filter(Boolean),
  };
}
function compactClient(s, max) {
  s = String(s || "")
    .replace(/\s+/g, " ")
    .trim();
  return s.length > max ? s.slice(0, max).trim() + "..." : s;
}
function issueContext() {
  if (lastPlan && lastPlan.issue) {
    lastPlan.issue = normalizeIssue(lastPlan.issue);
    return lastPlan.issue;
  }
  const issue = normalizeIssue(null);
  if (lastPlan) lastPlan.issue = issue;
  return issue;
}
function normalizeIssue(issue) {
  const now = new Date();
  const local = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const iso =
    now.getFullYear() +
    "-" +
    String(now.getMonth() + 1).padStart(2, "0") +
    "-" +
    String(now.getDate()).padStart(2, "0");
  const start = new Date(now.getFullYear(), 0, 0);
  const day = Math.floor((local - start) / 86400000);
  const out = Object.assign({}, issue || {});
  out.year = parseInt(out.year || now.getFullYear(), 10);
  out.number = parseInt(out.number || day, 10);
  out.date = String(out.date || iso);
  out.label = String(out.label || "Issue " + out.number + ", " + out.year);
  return out;
}
function pageModules(page) {
  if (page.content && page.content.modules) return page.content.modules;
  try {
    const prompt = JSON.parse(String(page.prompt || "{}"));
    return prompt.content && prompt.content.modules
      ? prompt.content.modules
      : "";
  } catch (_) {
    return "";
  }
}
function creativeKitForPage(kind) {
  const kit = (lastPlan && lastPlan.creativeKit) || {};
  const take = (arr) => (Array.isArray(arr) ? arr.slice(0, 5) : []);
  if (kind === "advert") return { adverts: take(kit.adverts) };
  if (kind === "back-page") return { backPage: take(kit.backPage) };
  if (kind === "filler")
    return { departments: take(kit.departments), sidebars: take(kit.sidebars) };
  return { sidebars: take(kit.sidebars) };
}
function visualStyleBrief(kind) {
  const style = (lastPlan && lastPlan.style) || {};
  const specific =
    kind === "cover"
      ? style.cover
      : kind === "feature"
        ? style.feature
        : kind === "advert"
          ? style.advert
          : kind === "filler"
            ? style.filler
            : kind === "back-page"
              ? style.back
              : style.content || style.short;
  return [
    "Self-contained visual system for this page:",
    style.core,
    style.content,
    specific,
    "Typography: " + (style.typography || ""),
    "Palette: " + (style.color || ""),
    "Print treatment: " + (style.print || ""),
    "Page furniture: repeat this exact system from the style JSON: same margins, column grid, running-header placement, footer rule, folio placement and image-text treatment.",
    style.avoid ? "Avoid: " + style.avoid : "",
  ]
    .filter(Boolean)
    .join(" ");
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
async function restoreWorkspaceState() {
  if (!workspace) {
    showStep(1, true);
    return;
  }
  try {
    const res = await fetch(
      "/api/tasks?workspace=" + encodeURIComponent(workspace),
    );
    if (!res.ok) {
      showStep(1, true);
      return;
    }
    const { tasks = [], state = {} } = await res.json();

    if (state.style) {
      try {
        const s = JSON.parse(state.style);
        if (s.prompt) $("style").value = s.prompt;
        if (s.referencePath) referencePath = s.referencePath;
        if (s.enhancedJson && $("style").value === s.prompt) {
          $("style").value = s.enhancedJson;
        }
      } catch (_) {}
    }

    if (state.articles) {
      try {
        articles = JSON.parse(state.articles);
        renderArticles();
      } catch (_) {}
    }

    if (state.plan) {
      try {
        applyBuildResult(JSON.parse(state.plan));
      } catch (_) {}
    } else {
      const doneBuild = tasks.find(
        (t) => t.kind === "build" && t.status === "done",
      );
      if (doneBuild) {
        try {
          applyBuildResult(JSON.parse(doneBuild.outputJson));
        } catch (_) {}
      }
    }

    tasks
      .filter((t) => t.kind === "render-page" && t.status === "done")
      .forEach((t) => {
        try {
          const inp = JSON.parse(t.inputJson || "{}");
          const out = JSON.parse(t.outputJson || "{}");
          if (inp.page && inp.page.number && out.image) {
            renderedImages[inp.page.number] = {
              image: out.image,
              publicUrl: out.publicUrl || "",
            };
          }
        } catch (_) {}
      });
    if (lastPlan) renderPlan(lastPlan);

    const runningBuild = tasks.find(
      (t) => t.kind === "build" && t.status === "running",
    );
    if (runningBuild) {
      const out = $("output");
      if (out)
        out.innerHTML = progressHTML("Reconnecting to build...", 10, "planProgress");
      startTaskPolling(workspace, runningBuild.id, "planProgress", (t) => {
        if (t.status === "done") {
          try {
            applyBuildResult(JSON.parse(t.outputJson));
            renderPlan(lastPlan);
          } catch (_) {}
        }
      });
    }

    const savedStep = parseInt(state.step || "0", 10);
    const runningRenders = tasks.filter(
      (t) => t.kind === "render-page" && t.status === "running",
    );
    const needsRenderResume =
      lastPlan &&
      savedStep === 4 &&
      lastPlan.pages.some((p) => !renderedImages[p.number]);

    if (needsRenderResume) {
      resumeRenderPhase(runningRenders);
      return;
    }

    if (savedStep >= 1 && savedStep <= 4) {
      showStep(savedStep, true);
    } else if (lastPlan) {
      showStep(3, true);
    } else {
      showStep(1, true);
    }
  } catch (_) {
    showStep(1, true);
  }
}

async function resumeRenderPhase(runningRenderTasks) {
  if (!lastPlan) return;
  isRendering = true;
  renderCallTotal = lastPlan.pages.length + 2;
  renderCallDone = 1 + Object.keys(renderedImages).length;
  showStep(4, true);
  $("renderStatus").innerHTML = progressHTML(
    "Resuming render (" +
      Object.keys(renderedImages).length +
      " of " +
      lastPlan.pages.length +
      " pages done)...",
    Math.round((renderCallDone / Math.max(1, renderCallTotal)) * 100),
    "renderProgress",
  );
  renderPlan(lastPlan);
  try {
    // Poll all currently running render-page tasks in parallel
    await Promise.all(
      runningRenderTasks.map((t) => {
        let pageNum;
        try {
          pageNum = JSON.parse(t.inputJson || "{}").page?.number;
        } catch (_) {}
        if (!pageNum) return Promise.resolve();
        setStatus(pageNum, "Rendering...");
        return new Promise((resolve) => {
          startTaskPolling(workspace, t.id, null, (done) => {
            if (done.status === "done") {
              try {
                const out = JSON.parse(done.outputJson || "{}");
                renderedImages[pageNum] = {
                  image: out.image,
                  publicUrl: out.publicUrl || "",
                };
                completeRenderCall("Rendered page " + pageNum);
                renderPlan(lastPlan);
              } catch (_) {}
            } else {
              setStatus(pageNum, done.error || "Failed");
            }
            resolve();
          });
        });
      }),
    );
    // Render any pages that are still missing (not started or failed)
    const remaining = lastPlan.pages.filter((p) => !renderedImages[p.number]);
    if (remaining.length > 0) {
      let idx = 0;
      async function worker() {
        while (idx < remaining.length) {
          const p = remaining[idx++];
          try {
            const img = await renderPage(p, "");
            renderedImages[p.number] = img;
            completeRenderCall("Rendered page " + p.number);
            renderPlan(lastPlan);
          } catch (_) {}
        }
      }
      await Promise.all([worker(), worker(), worker()]);
    }
    // Write PDF
    const ordered = lastPlan.pages
      .map((p) => renderedImages[p.number] && renderedImages[p.number].image)
      .filter(Boolean);
    if (ordered.length !== lastPlan.pages.length) {
      throw new Error(
        "Missing rendered pages: expected " +
          lastPlan.pages.length +
          ", got " +
          ordered.length,
      );
    }
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
    setProgress("renderProgress", 100, "Done. Download should start automatically.");
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
  } catch (e) {
    $("renderStatus").textContent = e.message || "Render failed";
  } finally {
    isRendering = false;
    renderPlan(lastPlan);
  }
}

function wireLoadWorkspace() {
  const btn = $("loadWorkspace");
  const inp = $("workspaceInput");
  if (!btn || !inp) return;
  async function doLoad() {
    const id = inp.value
      .trim()
      .toLowerCase()
      .replace(/[^a-z0-9-]/g, "");
    if (!id) return;
    workspace = id;
    articles = [];
    lastPlan = null;
    renderedImages = {};
    isRendering = false;
    updateWorkspaceLabel();
    await restoreWorkspaceState();
  }
  btn.onclick = doLoad;
  inp.onkeydown = (e) => {
    if (e.key === "Enter") doLoad();
  };
}

renderArticles();
initModelSelectors();
updateWorkspaceLabel();
requireApiKey();
wireLoadWorkspace();
restoreWorkspaceState();
