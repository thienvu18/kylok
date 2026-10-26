// SPDX-License-Identifier: LGPL-3.0-only
"use strict";
/** Resolve a fixed console element; user data is never used as a selector. */
const $ = id => document.getElementById(id);
const state = {token: sessionStorage.getItem("kylok.console") || "", config: null, devices: [], apps: [], operations: [], device: "", qr: null};

/** Report an actionable result without placing server-controlled text into HTML. */
function notice(message, error = false) { $("notice").textContent = message; $("notice").classList.toggle("error", error); }
/** Send explicit bearer authorization; no browser cookie can authorize a cross-site write. */
async function api(path, body, method) {
  const headers = {Authorization: `Bearer ${state.token}`};
  if (body && !(body instanceof FormData)) headers["Content-Type"] = "application/json";
  const response = await fetch(`/api/${path}`, {method: method || (body ? "POST" : "GET"), headers, body: body instanceof FormData ? body : body ? JSON.stringify(body) : undefined, cache: "no-store"});
  const value = await response.json().catch(() => ({error: response.status === 401 ? "Administrator token required" : "Request failed"}));
  if (!response.ok) throw new Error(value.error || `HTTP ${response.status}`);
  return value;
}
/** Switch the example's task panels while preserving selected device and enrollment state. */
function page(name) {
  document.querySelectorAll("[data-panel]").forEach(el => { el.hidden = el.dataset.panel !== name; });
  document.querySelectorAll("nav [data-page]").forEach(el => el.classList.toggle("active", el.dataset.page === name));
  $("page-title").textContent = {overview: "Overview", enrollment: "Enrollment", applications: "Applications", policies: "Policies & kiosk", activity: "Operations"}[name];
}
/** Construct a safe text node, including values read from device inventory. */
function element(tag, text, className) { const el = document.createElement(tag); if (text !== undefined) el.textContent = text; if (className) el.className = className; return el; }
/** Render bounded tables with real buttons instead of HTML string interpolation. */
function table(target, headings, rows) {
  const container = $(target); container.replaceChildren();
  if (!rows.length) { container.append(element("p", "Nothing here yet.", "empty")); return; }
  const t = element("table"), head = element("tr"); headings.forEach(h => head.append(element("th", h))); const thead = element("thead"); thead.append(head); t.append(thead);
  const body = element("tbody"); rows.forEach(cells => { const row = element("tr"); cells.forEach(value => { const td = element("td"); td.append(value instanceof Node ? value : document.createTextNode(String(value ?? "—"))); row.append(td); }); body.append(row); }); t.append(body); container.append(t);
}
/** Bind a deliberate operator action and surface asynchronous failures. */
function action(text, callback, danger = false) { const b = element("button", text, danger ? "danger" : "secondary"); b.type = "button"; b.addEventListener("click", () => run(callback)); return b; }
/** Map operation/compliance state into a readable state badge. */
function badge(value) { return element("span", value, `badge ${String(value).toLowerCase()}`); }
/** Keep IDs usable while allowing long immutable digests to fit the table. */
function identity(primary, secondary) { const el = element("div", primary); if (secondary) el.append(element("small", secondary)); return el; }
/** Surface asynchronous action failures as an explicit visible result. */
async function run(callback) { try { await callback(); } catch (error) { notice(error.message, true); } }
/** Read the scope once for each request so data never silently crosses scope boundaries. */
function scope() { return $("scope").value.trim(); }
/** Create a fresh business idempotency key for an intentional new assignment. */
function requestKey() { const bytes = new Uint8Array(16); crypto.getRandomValues(bytes); return `console-${Array.from(bytes, b => b.toString(16).padStart(2, "0")).join("")}`; } // getRandomValues also works on an explicitly chosen LAN HTTP origin.
/** Update selectors and inventory from the current scoped device page. */
function renderDevices() {
  $("device-count").textContent = state.devices.length;
  table("devices", ["Device", "State", "Desired / reported", "Last seen", ""], state.devices.map(d => [identity(d.inventory.model, d.id), badge(d.state), `${d.desired_revision} / ${d.reported_revision}`, d.last_seen_at.startsWith("0001") ? "Not synced" : new Date(d.last_seen_at).toLocaleString(), action(state.device === d.id ? "Selected" : "Inspect", async () => { state.device = d.id; renderDevices(); await refreshDevice(); })]));
  $("device-choice").replaceChildren(...state.devices.map(d => { const option = element("option", `${d.inventory.model} · ${d.id.slice(0, 8)}`); option.value = d.id; return option; }));
  $("device-choice").value = state.device;
  const selected = state.devices.find(d => d.id === state.device);
  $("selected-model").textContent = selected?.inventory.model || "None selected";
  $("inventory").hidden = !selected; $("inventory-json").textContent = selected ? JSON.stringify(selected, null, 2) : "";
  $("policy-device").textContent = selected ? `${selected.inventory.model} · ${selected.id}` : "Select a device on Overview or Applications.";
  $("operations-device").textContent = selected ? selected.inventory.model : "Select a device";
}
/** Present verified APK metadata and reject unsafe deletion through the SDK's reference checks. */
function renderApps() {
  const selected = $("application-choice").value;
  $("app-count").textContent = state.apps.length;
  table("applications", ["Package / version", "Digest", "SDK / size", ""], state.apps.map(a => [identity(`${a.package_name} · ${a.version_code}`, a.package_name === state.config.host_package ? "Agent self-update" : "Managed application"), identity(a.sha256.slice(0, 16), a.signing_certificates.join(", ")), `${a.min_sdk}–${a.max_sdk || "any"} · ${(a.size_bytes / 1048576).toFixed(1)} MiB`, action("Remove", async () => { if (!confirm(`Remove unreferenced catalog entry ${a.package_name}?`)) return; await api("remove", {scope: scope(), digest: a.sha256}); await refresh(); notice("Catalog entry removed. Shared artifact bytes are retained safely."); }, true)]));
  $("application-choice").replaceChildren(...state.apps.map(a => { const option = element("option", `${a.package_name} · ${a.version_code}${a.package_name === state.config.host_package ? " · agent update" : ""}`); option.value = a.sha256; return option; }));
  if (state.apps.some(a => a.sha256 === selected)) $("application-choice").value = selected;
}
/** Read current policy and operation evidence for the selected device, never infer success from assignment. */
async function refreshDevice(after = "") {
  if (!state.device) { table("operations", [], []); table("compliance", [], []); return; }
  const requestedScope = scope(), requestedDevice = state.device;
  const query = new URLSearchParams({scope: requestedScope, device: requestedDevice, after});
  const [operations, policy] = await Promise.all([api(`operations?${query}`), api(`policy?${query}`)]);
  // A slow response for a previous selection must not overwrite the current device's observations.
  if (scope() !== requestedScope || state.device !== requestedDevice) return;
  state.operations = after ? [...state.operations, ...operations] : operations;
  table("operations", ["Application", "State", "Download", "Result"], state.operations.map(o => { const progress = element("div"); const bar = element("progress"); bar.max = o.artifact.size_bytes; bar.value = o.downloaded_bytes; progress.append(bar, element("small", `${o.downloaded_bytes} / ${o.artifact.size_bytes} bytes`)); return [identity(o.artifact.package_name, `${o.id} · revision ${o.revision}`), badge(o.state), progress, identity(o.error_code || (o.state === "SUCCEEDED" ? "Observed installation" : "Awaiting device"), o.error_message)]; }));
  table("compliance", ["Rule", "State", "Reason"], (policy.compliance || []).map(p => [p.rule_id, badge(p.state), p.reason || "Observed"]));
  $("policy-details").textContent = JSON.stringify(policy, null, 2);
}
/** Refresh scoped catalog and registry data without changing server/device desired state. */
async function refresh() {
  const requestedScope = scope(), query = new URLSearchParams({scope: requestedScope});
  const [devices, apps] = await Promise.all([api(`devices?${query}`), api(`applications?${query}`)]);
  if (scope() !== requestedScope) return;
  [state.devices, state.apps] = [devices || [], apps || []];
  if (!state.devices.some(d => d.id === state.device)) state.device = state.devices[0]?.id || "";
  renderDevices(); renderApps(); await refreshDevice();
}
/** Restore an authorized tab and populate actual listener/interface choices. */
async function connect() {
  state.config = await api("config"); sessionStorage.setItem("kylok.console", state.token);
  $("login").hidden = true; $("workspace").hidden = false; $("logout").hidden = false;
  $("version").textContent = `SDK ${state.config.version} · experimental`;
  $("origin-choice").replaceChildren(...state.config.origins.map(o => { const option = element("option", `${o.name} · ${o.origin}`); option.value = o.origin; return option; }), element("option", "Custom IP / domain"));
  $("origin-choice").lastChild.value = "custom";
  $("bootstrap-origin").value = state.config.bootstrap_origin;
  $("separate-bootstrap").checked = state.config.bootstrap_origin !== state.config.api_origin;
  $("bootstrap-label").hidden = !$("separate-bootstrap").checked;
  $("assignment-key").value = requestKey(); $("policy-key").value = requestKey();
  await refresh(); notice("Connected. Management actions are scoped by the value above.");
}
/** Normalize a domain-only entry with the selected transport and API listener port. */
function selectedOrigins() {
  let origin = $("origin-choice").value;
  if (origin === "custom") {
    const value = $("custom-origin").value.trim(), base = new URL(state.config.api_origin);
    if (!value) throw new Error("Enter a custom IP or domain.");
    const absolute = value.includes("://"), u = new URL(absolute ? value : `${base.protocol}//${value}`);
    if (!["http:", "https:"].includes(u.protocol) || u.username || u.password || u.pathname !== "/" || u.search || u.hash) throw new Error("Use an HTTP/HTTPS origin without credentials, path, query or fragment.");
    // URL normalizes explicit default ports to empty; preserve :80/:443 instead of replacing them with the API port.
    if (!absolute && !/:\d+$/.test(value)) u.port = base.port;
    origin = u.origin;
  }
  return {origin, bootstrap: $("separate-bootstrap").checked ? $("bootstrap-origin").value.trim() : origin};
}
/** Replace only the QR's destinations while preserving its existing single-use token. */
async function redrawQR() {
  if (!state.qr) return;
  const value = await api("qr", {token: state.qr.token, ...selectedOrigins()}); state.qr = {...state.qr, ...value}; renderQR();
  notice("QR destination updated. The single-use token and expiry are unchanged.");
}
/** Display private enrollment material without persisting it to the server's public filesystem. */
function renderQR() { $("qr-empty").hidden = true; $("qr-result").hidden = false; $("qr-image").src = state.qr.image; $("qr-origin").textContent = state.qr.payload["android.app.extra.PROVISIONING_ADMIN_EXTRAS_BUNDLE"]["kylok.server"]; $("qr-expiry").textContent = `Expires ${new Date(state.qr.expires_at).toLocaleString()}`; }
/** Seed complete replacement policies using an existing verified catalog package. */
function preset(name) {
  const app = state.apps.find(a => a.sha256 === $("application-choice").value), pkg = app?.package_name;
  if ((name === "kiosk" || name === "permissions") && !pkg) throw new Error("Register and assign an APK first.");
  const values = {kiosk: {required_packages: [pkg], lock_task: {packages: [pkg], kiosk_package: pkg}}, restrictions: {camera_disabled: true, screen_capture_disabled: true, maximum_time_to_lock_millis: 60000, restrictions: [{name: "no_add_user", enabled: true}, {name: "no_modify_accounts", enabled: true}, {name: "no_uninstall_apps", enabled: true}, {name: "no_debugging_features", enabled: false}]}, permissions: {required_packages: [pkg], permissions: [{package_name: pkg, permission: "android.permission.CAMERA", state: "GRANTED"}]}, release: {}};
  $("policy-json").value = JSON.stringify(values[name], null, 2); $("policy-key").value = requestKey();
}

document.querySelectorAll("[data-page]").forEach(b => b.addEventListener("click", () => page(b.dataset.page)));
document.querySelectorAll("[data-preset]").forEach(b => b.addEventListener("click", () => run(() => preset(b.dataset.preset))));
$("login-form").addEventListener("submit", e => { e.preventDefault(); state.token = $("admin-token").value.trim(); run(connect); });
$("logout").addEventListener("click", () => { sessionStorage.removeItem("kylok.console"); location.reload(); });
$("refresh").addEventListener("click", () => run(refresh));
$("scope").addEventListener("change", () => run(async () => {
  state.device = ""; state.qr = null; $("qr-empty").hidden = false; $("qr-result").hidden = true; $("qr-image").removeAttribute("src");
  await refresh(); notice("Scope changed. Create a new QR for this scope.");
}));
$("device-choice").addEventListener("change", () => run(async () => { state.device = $("device-choice").value; renderDevices(); await refreshDevice(); }));
$("more-devices").addEventListener("click", () => run(async () => { const after = state.devices.at(-1)?.id || ""; const extra = await api(`devices?${new URLSearchParams({scope: scope(), after})}`); state.devices.push(...extra); renderDevices(); }));
$("more-apps").addEventListener("click", () => run(async () => { const after = state.apps.at(-1)?.sha256 || ""; const extra = await api(`applications?${new URLSearchParams({scope: scope(), after})}`); state.apps.push(...extra); renderApps(); }));
$("more-operations").addEventListener("click", () => run(() => refreshDevice(state.operations.at(-1)?.id || "")));
$("revoke").addEventListener("click", () => run(async () => { if (!state.device || !confirm("Revoke this device? Further sync, APK downloads and enrollment recovery will be rejected.")) return; await api("revoke", {scope: scope(), device: state.device}); await refresh(); notice("Device revoked. Revocation does not reset Android or release offline policies."); }));
$("enroll-form").addEventListener("submit", e => { e.preventDefault(); run(async () => { state.qr = await api("enroll", {scope: scope(), ttl_seconds: Number($("ttl").value), ...selectedOrigins()}); renderQR(); notice("Private enrollment QR created. Select another interface to redraw it."); }); });
for (const id of ["origin-choice", "custom-origin", "bootstrap-origin", "separate-bootstrap"]) $(id).addEventListener("change", () => run(async () => { $("bootstrap-label").hidden = !$("separate-bootstrap").checked; await redrawQR(); }));
$("download-qr").addEventListener("click", () => { const url = URL.createObjectURL(new Blob([JSON.stringify(state.qr.payload, null, 2)], {type: "application/json"})); const link = element("a"); link.href = url; link.download = "kylok-enrollment.json"; link.click(); URL.revokeObjectURL(url); });
$("upload-form").addEventListener("submit", e => { e.preventDefault(); run(async () => { const data = new FormData(); data.append("scope", scope()); data.append("apk", $("apk-file").files[0]); notice("Verifying APK signature and manifest…"); const artifact = await api("register", data); await refresh(); $("application-choice").value = artifact.sha256; notice(`Registered ${artifact.package_name} version ${artifact.version_code}.`); }); });
$("assign-form").addEventListener("submit", e => { e.preventDefault(); run(async () => { state.device = $("device-choice").value; await api("assign", {scope: scope(), device: state.device, digest: $("application-choice").value, request_key: $("assignment-key").value.trim(), timeout_seconds: Number($("timeout").value)}); $("assignment-key").value = requestKey(); await refresh(); page("activity"); notice("Desired app assigned. Wait for observed installation; a queued operation is not success."); }); });
$("policy-form").addEventListener("submit", e => { e.preventDefault(); run(async () => { if (!state.device) throw new Error("Select a device first."); await api("set-policy", {scope: scope(), device: state.device, request_key: $("policy-key").value.trim(), policy: JSON.parse($("policy-json").value)}); $("policy-key").value = requestKey(); await refresh(); notice("Replacement policy saved. Device readback determines compliance."); }); });
setInterval(() => { if (state.config && !document.hidden) run(refresh); }, 10000); // Poll observations only; desired changes require an explicit operator action.
if (state.token) run(connect);
