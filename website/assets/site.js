// SPDX-License-Identifier: LGPL-3.0-only
"use strict";
const input = document.getElementById("search"), results = document.getElementById("search-results");
let index;
/** Search only the current language and keep matches as safe text links. */
async function search() {
  const query = input.value.trim().toLocaleLowerCase(document.documentElement.lang);
  results.replaceChildren();
  if (query.length < 2) return;
  index ||= await fetch("../search.json").then(response => response.json());
  const matches = index.filter(page => page.lang === document.documentElement.lang && `${page.title} ${page.text}`.toLocaleLowerCase(page.lang).includes(query)).slice(0, 8);
  for (const page of matches) { const link = document.createElement("a"); link.href = `../${page.url}`; link.textContent = page.title; results.append(link); }
  if (!matches.length) { const p = document.createElement("p"); p.textContent = document.documentElement.lang === "vi" ? "Không tìm thấy chủ đề." : "No matching topics."; results.append(p); }
}
input?.addEventListener("input", () => search().catch(() => { results.textContent = document.documentElement.lang === "vi" ? "Không thể tải chỉ mục tìm kiếm." : "Could not load the search index."; }));
