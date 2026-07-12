(function () {
  "use strict";

  //  Config
  const API_BASE = (window.ARTECI_API_BASE || "http://localhost:3001").replace(
    /\/$/,
    "",
  );

  const STATUS_MESSAGES = [
    "Lecture du fichier…",
    "Analyse des colonnes de date…",
    "Normalisation en cours…",
    "Écriture des résultats…",
  ];

  //  State
  const state = {
    bucket: "arteci",
    file: "",
    columnsStatus: "idle", // idle | loading | error | success
    columnsError: "",
    columns: [],
    dateConfig: {}, // name → { enabled: bool, format: 'DMY' | 'MDY' }
    processStatus: "idle", // idle | loading | error | success
    processError: "",
    result: null,
    elapsedSeconds: 0,
    processDuration: 0,
    totalRows: 0,
    statusIndex: 0,
  };

  let _timer = null;
  let _statusTimer = null;
  let _startedAt = null;

  //  DOM helpers
  const $ = (id) => document.getElementById(id);

  function show(el) {
    el.style.display = el.dataset.displayType || "block";
  }
  function hide(el) {
    el.style.display = "none";
  }

  //  Render
  function render() {
    renderSteps();
    renderSourceCard();
    renderPanels();
  }

  function renderSteps() {
    let step = 1;
    if (state.columnsStatus === "success") step = 2;
    if (["loading", "error", "success"].includes(state.processStatus)) step = 3;

    for (let i = 1; i <= 3; i++) {
      const circle = $("step-circle-" + i);
      const label = $("step-label-" + i);
      const active = i === step;
      const done = i < step;

      circle.className = "step-circle" + (active || done ? " active" : "");
      label.className =
        "step-label" + (active ? " active" : done ? " done" : "");
    }

    for (let i = 1; i <= 2; i++) {
      $("connector-" + i).className =
        "step-connector" + (step > i ? " done" : "");
    }
  }

  function renderSourceCard() {
    const isProcessing = state.processStatus === "loading";
    $("source-card").classList.toggle("dimmed", isProcessing);

    const loadBtn = $("load-btn");
    const loading = state.columnsStatus === "loading";
    loadBtn.disabled = loading || isProcessing;
    loadBtn.textContent = loading ? "Chargement…" : "Charger les colonnes";

    // Columns error
    const errEl = $("columns-error");
    if (state.columnsStatus === "error") {
      errEl.style.display = "flex";
      $("columns-error-text").textContent = state.columnsError;
    } else {
      errEl.style.display = "none";
    }

    // Columns block
    const block = $("columns-block");
    if (state.columnsStatus === "success") {
      block.style.display = "block";
      $("columns-count-badge").textContent = state.columns.length;
      renderColumnsList();

      const n = Object.values(state.dateConfig).filter((c) => c.enabled).length;
      $("process-btn").disabled = isProcessing;
      $("date-col-count-text").textContent =
        n === 0
          ? "Aucune colonne de date sélectionnée — le fichier sera traité tel quel."
          : `${n} colonne${n > 1 ? "s" : ""} de date sélectionnée${n > 1 ? "s" : ""}.`;
    } else {
      block.style.display = "none";
    }
  }

  function renderColumnsList() {
    const list = $("columns-list");
    list.innerHTML = "";

    state.columns.forEach((name) => {
      const cfg = state.dateConfig[name] || { enabled: false, format: "DMY" };

      const row = document.createElement("div");
      row.className = "column-row" + (cfg.enabled ? " selected" : "");

      // Checkbox
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.className = "column-checkbox";
      cb.checked = cfg.enabled;
      cb.addEventListener("change", () => toggleColumn(name));

      // Column name
      const nameEl = document.createElement("div");
      nameEl.className = "column-name" + (cfg.enabled ? " selected" : "");
      nameEl.textContent = name;

      const sel = document.createElement("select");
      sel.className = "format-select";
      sel.innerHTML =
        '<option value="DMY">DMY (jour/mois/année)</option>' +
        '<option value="MDY">MDY (mois/jour/année)</option>';
      sel.value = cfg.format;
      sel.style.visibility = cfg.enabled ? "visible" : "hidden";
      sel.addEventListener("change", (e) => setFormat(name, e.target.value));

      row.appendChild(cb);
      row.appendChild(nameEl);
      row.appendChild(sel);

      list.appendChild(row);
    });
  }

  function renderPanels() {
    const showEmpty = state.columnsStatus === "idle";
    const showProcessing = state.processStatus === "loading";
    const showError = state.processStatus === "error";
    const showResults = state.processStatus === "success";

    $("empty-state").style.display = showEmpty ? "block" : "none";
    $("processing-state").style.display = showProcessing ? "flex" : "none";
    $("process-error").style.display = showError ? "block" : "none";
    $("results-card").style.display = showResults ? "block" : "none";

    if (showProcessing) {
      updateTimer();
      $("status-message").textContent = STATUS_MESSAGES[state.statusIndex];
    }

    if (showError) {
      $("process-error-text").textContent = state.processError;
    }

    if (showResults) {
      renderResults();
    }
  }

  function updateTimer() {
    const mm = Math.floor(state.elapsedSeconds / 60)
      .toString()
      .padStart(2, "0");
    const ss = (state.elapsedSeconds % 60).toString().padStart(2, "0");
    const el = $("elapsed-timer");
    if (el) el.textContent = `${mm}:${ss}`;
  }

  function renderResults() {
    const { columns, rows } = normalizeResult(state.result);
    const n = rows.length;

    const sec = (state.processDuration / 1000).toFixed(1);
    const total = state.totalRows
      ? `${state.totalRows.toLocaleString("fr-FR")} lignes dans le fichier · `
      : "";
    $("result-summary").textContent =
      `${total}${n} affichée${n > 1 ? "s" : ""} (max 100) · ${sec}s`;

    const dateColSet = new Set(
      Object.keys(state.dateConfig).filter((k) => state.dateConfig[k].enabled),
    );

    // Header
    const thead = $("results-thead");
    thead.innerHTML = "";
    const headRow = document.createElement("tr");
    columns.forEach((col) => {
      const th = document.createElement("th");
      th.textContent = col;
      if (dateColSet.has(col)) th.classList.add("col-date");
      headRow.appendChild(th);
    });
    thead.appendChild(headRow);

    // Body
    const tbody = $("results-tbody");
    tbody.innerHTML = "";
    rows.forEach((row) => {
      const tr = document.createElement("tr");
      row.forEach((cell, i) => {
        const td = document.createElement("td");
        td.textContent =
          cell === null || cell === undefined ? "" : String(cell);
        if (dateColSet.has(columns[i])) td.classList.add("col-date");
        tr.appendChild(td);
      });
      tbody.appendChild(tr);
    });
  }

  function normalizeResult(result) {
    if (!result) return { columns: [], rows: [] };

    if (Array.isArray(result)) {
      const columns = result.length ? Object.keys(result[0]) : [];
      const rows = result.map((r) => columns.map((c) => r[c]));
      return { columns, rows };
    }

    const columns =
      result.columns ||
      (result.rows && result.rows[0] && !Array.isArray(result.rows[0])
        ? Object.keys(result.rows[0])
        : []);
    let rows = result.rows || [];
    rows = rows.map((r) => (Array.isArray(r) ? r : columns.map((c) => r[c])));
    return { columns, rows };
  }

  // Actions
  function toggleColumn(name) {
    const cfg = state.dateConfig[name];
    state.dateConfig[name] = { ...cfg, enabled: !cfg.enabled };
    render();
  }

  function setFormat(name, format) {
    state.dateConfig[name] = { ...state.dateConfig[name], format };
    const n = Object.values(state.dateConfig).filter((c) => c.enabled).length;
    $("date-col-count-text").textContent =
      n === 0
        ? "Aucune colonne de date sélectionnée — le fichier sera traité tel quel."
        : `${n} colonne${n > 1 ? "s" : ""} de date sélectionnée${n > 1 ? "s" : ""}.`;
  }

  async function loadColumns() {
    if (!state.bucket || !state.file) return;

    Object.assign(state, {
      columnsStatus: "loading",
      columnsError: "",
      columns: [],
      dateConfig: {},
      processStatus: "idle",
      processError: "",
      result: null,
    });
    render();

    try {
      const url = `${API_BASE}/columns?bucket=${encodeURIComponent(state.bucket)}&file=${encodeURIComponent(state.file)}`;
      const res = await fetch(url);
      const text = await res.text();
      let data;
      try {
        data = JSON.parse(text);
      } catch (_) {
        data = null;
      }

      if (!res.ok) {
        const msg =
          (data && (data.detail || data.error || data.message)) ||
          text ||
          `Erreur HTTP ${res.status}`;
        Object.assign(state, {
          columnsStatus: "error",
          columnsError: typeof msg === "string" ? msg : JSON.stringify(msg),
        });
        render();
        return;
      }

      const cols = Array.isArray(data) ? data : (data && data.columns) || [];
      const dateConfig = {};
      cols.forEach((c) => {
        dateConfig[c] = { enabled: false, format: "DMY" };
      });

      Object.assign(state, {
        columnsStatus: "success",
        columns: cols,
        dateConfig,
      });
      render();
    } catch (err) {
      Object.assign(state, {
        columnsStatus: "error",
        columnsError: `Impossible de contacter l'API (${API_BASE}) : ${err.message}`,
      });
      render();
    }
  }

  async function processDate() {
    const dateColumns = Object.keys(state.dateConfig).filter(
      (k) => state.dateConfig[k].enabled,
    );
    const dateFormats = dateColumns.map((k) => state.dateConfig[k].format);

    clearInterval(_timer);
    clearInterval(_statusTimer);

    Object.assign(state, {
      processStatus: "loading",
      processError: "",
      result: null,
      elapsedSeconds: 0,
      statusIndex: 0,
    });
    _startedAt = Date.now();
    render();

    _timer = setInterval(() => {
      state.elapsedSeconds = Math.floor((Date.now() - _startedAt) / 1000);
      updateTimer();
    }, 1000);

    _statusTimer = setInterval(() => {
      state.statusIndex = (state.statusIndex + 1) % STATUS_MESSAGES.length;
      const el = $("status-message");
      if (el) el.textContent = STATUS_MESSAGES[state.statusIndex];
    }, 18000);

    try {
      const res = await fetch(`${API_BASE}/processDate`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          bucket: state.bucket,
          file: state.file,
          date_columns: dateColumns,
          date_formats: dateFormats,
        }),
      });
      const text = await res.text();
      let data;
      try {
        data = JSON.parse(text);
      } catch (_) {
        data = null;
      }

      const duration = Date.now() - _startedAt;
      clearInterval(_timer);
      clearInterval(_statusTimer);

      if (!res.ok) {
        const msg =
          (data && (data.detail || data.error || data.message)) ||
          text ||
          `Erreur HTTP ${res.status}`;
        Object.assign(state, {
          processStatus: "error",
          processError: typeof msg === "string" ? msg : JSON.stringify(msg),
        });
        render();
        return;
      }

      const totalRows = (data && data.total_rows) || 0;
      Object.assign(state, {
        processStatus: "success",
        result: data,
        processDuration: duration,
        totalRows,
      });
      render();
    } catch (err) {
      clearInterval(_timer);
      clearInterval(_statusTimer);
      Object.assign(state, {
        processStatus: "error",
        processError: `Impossible de contacter l'API (${API_BASE}) : ${err.message}`,
      });
      render();
    }
  }

  // Events
  $("bucket-input").addEventListener("input", (e) => {
    state.bucket = e.target.value;
  });
  $("file-input").addEventListener("input", (e) => {
    state.file = e.target.value;
  });

  $("load-btn").addEventListener("click", loadColumns);

  [$("bucket-input"), $("file-input")].forEach((el) => {
    el.addEventListener("keydown", (e) => {
      if (e.key === "Enter") loadColumns();
    });
  });

  document.addEventListener("click", (e) => {
    if (e.target && e.target.id === "process-btn") processDate();
  });

  render();
})();
