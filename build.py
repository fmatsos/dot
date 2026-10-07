#!/usr/bin/env python3
"""Build a static project site for GitHub Pages: pages of any structure, an optional
documentation rendered from the repository, a site map; everything checked.

    python3 build.py [REPO_DIR]     # REPO_DIR: a checkout of the project (for its docs), default ..

Inputs, next to this file:
  site.json          the project: name, url, repo, branch, lang, description, og image, docs
  src/layout.html    the frame of every page ({{placeholders}} below)
  src/*.css, *.js    inlined into every page, base.* first then alphabetical
  src/pages/**.html  one output page each, same path; optional first line
                     <!--page {"title": "…", "description": "…", "head": "…", "class": "…"} -->
  src/doc.html       the <main> of a docs page, when site.json has "docs"

Placeholders: layout {{lang}} {{title}} {{description}} {{url}} {{site}} {{name}} {{repo}}
{{og_image}} {{og_image_alt}} {{head}} {{css}} {{js}} {{main}} {{root}} {{html_class}};
in pages and doc.html also {{docs_list}} {{sitemap}} {{blank}} {{root}}; doc.html
{{doc_title}} {{doc_body}} {{doc_nav}} {{doc_pager}} {{docs_title}}.
A link to the page itself gets aria-current="page". The build fails on a link, anchor or
image that resolves to nothing, an unfilled placeholder, or GitHub markup it no longer
knows: fix the source, never silence the check.
Docs are rendered by GitHub's Markdown API (`gh api markdown`: gh logged in, or GH_TOKEN).
"""
import html
import json
import posixpath
import re
import shutil
import struct
import subprocess
import sys
from pathlib import Path

SITE = Path(__file__).resolve().parent
SRC = SITE / "src"
BLANK = "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw=="  # a sprite's src until it loads
CFG = json.loads((SITE / "site.json").read_text())


def fail(msg):
    sys.exit(f"build.py: {msg}")


def text(fragment):
    return re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", "", fragment))).strip()


def inline_md(s):
    return re.sub(r"`([^`]+)`", r"<code>\1</code>", html.escape(s, quote=False))


def fill(s, values, where, raw=None):
    """{{key}} -> value; a placeholder left is an error. `raw` values (page bodies, CSS, JS:
    content that may itself show "{{…}}") are inserted after that check."""
    raw = raw or {}
    for k, v in values.items():
        s = s.replace("{{" + k + "}}", v)
    for k in raw:
        s = s.replace("{{" + k + "}}", f"\0{k}\0")
    left = re.search(r"{{[a-z_]+}}", s)
    if left:
        fail(f"{where}: unfilled placeholder {left[0]}")
    for k, v in raw.items():
        s = s.replace(f"\0{k}\0", v)
    return s


# ---- docs: Markdown from the repository, rendered by GitHub ----

def render(md):
    return subprocess.run(["gh", "api", "markdown", "-f", "mode=markdown", "-F", f"text=@{md}"],
                          check=True, capture_output=True, text=True).stdout


def image_size(path):
    data = path.read_bytes()
    if data[:8] == b"\x89PNG\r\n\x1a\n":
        return struct.unpack(">II", data[16:24])
    if data[:6] in (b"GIF87a", b"GIF89a"):
        return struct.unpack("<HH", data[6:10])
    if data[:4] == b"RIFF" and data[8:12] == b"WEBP":
        chunk = data[12:16]
        if chunk == b"VP8X":
            return (int.from_bytes(data[24:27], "little") + 1,
                    int.from_bytes(data[27:30], "little") + 1)
        if chunk == b"VP8L" and data[20] == 0x2f:
            bits = data[21:25]
            width = 1 + bits[0] + ((bits[1] & 0x3f) << 8)
            height = 1 + (bits[1] >> 6) + (bits[2] << 2) + ((bits[3] & 0x0f) << 10)
            return width, height
        if chunk == b"VP8 " and data[23:26] == b"\x9d\x01\x2a":
            return (int.from_bytes(data[26:28], "little") & 0x3fff,
                    int.from_bytes(data[28:30], "little") & 0x3fff)
    fail(f"{path}: give docs images as PNG, GIF or WebP (their size is read to avoid layout shifts)")


def convert(raw, docs, out_assets, rewrite):
    """GitHub's HTML -> plain, accessible HTML: headings with ids, table regions, no presentational markup."""
    h = re.sub(r'<div class="markdown-heading"><h([1-6])(?: align="[^"]*")? class="heading-element">(.*?)</h\1>'
               r'<a id="user-content-([^"]+)" class="anchor"[^>]*>.*?</a></div>', r'<h\1 id="\3">\2</h\1>', raw, flags=re.S)
    # lang="text" on code blocks isn't a language (RGAA 8.7); the rest is noise
    h = re.sub(r' (?:dir="auto"|class="notranslate"|role="table"|data-component="Octicon"|style="[^"]*"|lang="[^"]*")', "", h)
    h = h.replace("octicon mr-2", "octicon").replace('<p align="center">', '<p class="center">')
    out, last = [], "Table"   # a wide table scrolls on its own: a named, focusable region (RGAA 10.11)
    for part in re.split(r'(<h[2-6] id="[^"]+">.*?</h[2-6]>|</?markdown-accessiblity-table>|</?table>)', h):
        if part.startswith("<h"):
            last = text(part)
        elif part == "<markdown-accessiblity-table>":
            part = f'<div class="table" tabindex="0" role="region" aria-label="Table: {html.escape(last)}">'
        elif part == "</markdown-accessiblity-table>":
            part = "</div>"
        elif part == "<table>":   # bare table (no GitHub wrapper): same named, focusable region
            part = f'<div class="table" tabindex="0" role="region" aria-label="Table: {html.escape(last)}"><table>'
        elif part == "</table>":
            part = "</table></div>"
        out.append(part)
    h = "".join(out)
    h = re.sub(r'<a [^>]*href="(?![a-z]+:)[^"]*\.(?:png|gif|jpe?g|webp|svg)[^"]*"[^>]*>(<img [^>]*>)</a>', r"\1", h)  # GitHub links images to themselves
    h = re.sub(r'href="([^"]*)"', lambda m: f'href="{html.escape(rewrite(html.unescape(m[1])))}"', h)

    def img(m):
        src = m[1].split("?")[0]
        if re.match(r"[a-z]+:", src):
            alt = re.search(r' alt="([^"]*)"', m[0])
            return html.escape(alt[1]) if alt else ""
        f = (docs / src).resolve()
        if not f.is_file():
            fail(f"docs image not found: {src}")
        (out_assets / f.name).write_bytes(f.read_bytes())
        w, hh = image_size(f)
        tag = re.sub(r' (?:width|height)="[^"]*"', "", m[0]).replace(f'src="{m[1]}"', f'src="assets/{f.name}"')
        return tag.replace("<img ", f'<img width="{w}" height="{hh}" loading="lazy" decoding="async" ', 1)
    h = re.sub(r'<img src="([^"]+)"[^>]*>', img, h)
    for bad in ("user-content-", "align=", "markdown-heading", "markdown-accessiblity", r'href="(?![a-z]+:)[^"]*\.md[#"]'):
        if re.search(bad, h):
            fail(f"GitHub's markup changed: {bad!r} left in the output, update convert()")
    return h


def build_docs(repo_dir):
    """-> [(out path, title html, description, body, nav, pager)], the docs list, the output dir."""
    d = CFG["docs"]
    docs = (repo_dir / d["dir"]).resolve()
    index = d.get("index", "README.md")
    out = d.get("out", "docs")
    # By default, page order and summaries come from local .md links in the index.
    # Repositories whose README is not a docs index can provide explicit `pages` entries.
    toc, seen = [], set()
    if d.get("pages"):
        for item in d["pages"]:
            file = item["file"]
            name = Path(file).stem
            if name not in seen and (docs / file).is_file() and file != index:
                seen.add(name)
                toc.append((name, file, item.get("title", name), item.get("summary", "")))
    else:
        for m in re.finditer(r"\[(.+?)\]\(([\w./-]+)\.md\)(?:\s*[:—–-]\s*(.+))?", (docs / index).read_text()):
            name = m[2]
            file = f"{name}.md"
            if name not in seen and (docs / file).is_file() and file != index:
                seen.add(name)
                toc.append((name, file, m[1], (m[3] or "").strip()))
    stem = Path(index).stem
    blob = f"{CFG['repo']}/blob/{CFG.get('branch', 'main')}/{d['dir'].strip('/')}/"

    def rewrite(href):   # a link of a doc, as seen from its html page
        if re.match(r"[a-z]+:|#", href):
            return href
        m = re.fullmatch(r"([\w./-]+)\.md(#.*)?", href)
        if m and (m[1] == stem or (docs / f"{m[1]}.md").is_file()):
            return ("index.html" if m[1] == stem else f"{m[1]}.html") + (m[2] or "")
        target = posixpath.normpath(posixpath.join(d["dir"], href))
        return f"{CFG['repo']}/{'tree' if href.endswith('/') else 'blob'}/{CFG.get('branch', 'main')}/{target}"

    shutil.rmtree(SITE / out, ignore_errors=True)
    (SITE / out / "assets").mkdir(parents=True)
    pages = []
    names = [stem] + [n for n, _, _, _ in toc]
    for name in names:
        file = index if name == stem else next(f for n, f, _, _ in toc if n == name)
        body = convert(render(docs / file), docs, SITE / out / "assets", rewrite)
        m = re.match(r'\s*<h1 id="[^"]+">(.*?)</h1>\s*', body, re.S)
        title, body = (m[1], body[m.end():]) if m else (html.escape(name), body)
        if name == stem:   # the docs index is titled by the config; the README's own h1 becomes an h2
            title = html.escape(d.get("title", name))
            body = re.sub(r"<h1\b([^>]*)>(.*?)</h1>", r"<h2\1>\2</h2>", body, flags=re.S)
        first = re.search(r"<p>(.*?)</p>", body, re.S)
        summary = next((s for n, _, _, s in toc if n == name and s), "") or (text(first[1]) if first else text(title))
        path = f"{out}/index.html" if name == stem else f"{out}/{name}.html"
        pager = ""
        if name != stem:
            i = names.index(name) - 1
            prev = f'<a class="prev" rel="prev" href="{toc[i-1][0]}.html"><small>Previous</small> {inline_md(toc[i-1][2])}</a>' if i > 0 else ""
            nxt = f'<a class="next" rel="next" href="{toc[i+1][0]}.html"><small>Next</small> {inline_md(toc[i+1][2])}</a>' if i + 1 < len(toc) else ""
            pager = f'<nav class="pager" aria-label="Previous and next pages">{prev}{nxt}</nav>'
        nav = "\n".join(f'<li><a href="{n}.html">{inline_md(t)}</a></li>' for n, _, t, _ in toc)
        desc = summary if len(summary) < 160 else summary[:156].rsplit(" ", 1)[0] + "…"
        pages.append((path, title, desc, body, nav, pager))
    items = [(f"{out}/{n}.html", inline_md(t), html.escape(s)) for n, _, t, s in toc]
    return pages, items, out


# ---- pages, checks ----

def check(pages):
    """Every internal href, src and srcset must resolve; every #fragment must be an id of its page."""
    ids = {p: set(re.findall(r' id="([^"]+)"', h)) for p, h in pages.items()}
    for p, h in pages.items():
        base = posixpath.dirname(p)
        refs = re.findall(r'(?:href|src|data-src)="([^"]+)"', h)
        refs += [u.split()[0] for s in re.findall(r'(?:srcset|imagesrcset)="([^"]+)"', h) for u in s.split(",")]
        for ref in refs:
            ref = html.unescape(ref)
            if re.match(r"[a-z]+:", ref) or ref.startswith("//"):
                continue
            target, _, frag = ref.partition("#")
            t = posixpath.normpath(posixpath.join(base, target)) if target else p
            if target.endswith("/") or t == "." or (SITE / t).is_dir():
                t = posixpath.normpath(posixpath.join(t, "index.html"))
            if t not in pages and not (SITE / t).is_file():
                fail(f"{p}: {ref} resolves to nothing")
            if frag and t in pages and frag not in ids[t]:
                fail(f"{p}: no #{frag} in {t}")


def current(h, path):
    """aria-current="page" on the links to the page itself (menus, docs nav, site map)."""
    base = posixpath.dirname(path)

    def mark(m):
        href = html.unescape(m[2])
        if re.match(r"[a-z]+:|#", href) or "#" in href or "aria-current" in m[0]:
            return m[0]
        t = posixpath.normpath(posixpath.join(base, href))
        if href.endswith("/") or t == "." or (SITE / t).is_dir():
            t = posixpath.normpath(posixpath.join(t, "index.html"))
        return m[0].replace("<a ", '<a aria-current="page" ', 1) if t == path else m[0]
    return re.sub(r'<a ([^>]*?)href="([^"]+)"', mark, h)


def main():
    repo_dir = Path(sys.argv[1] if len(sys.argv) > 1 else SITE.parent).resolve()
    layout = (SRC / "layout.html").read_text()
    order = lambda ext: sorted(SRC.glob(f"*.{ext}"), key=lambda f: (not f.name.startswith("base."), f.name))
    css = "\n".join(f.read_text() for f in order("css"))
    js = "\n".join(f.read_text() for f in order("js"))
    root_of = lambda path: "../" * path.count("/") or "./"

    def shared(path):
        return {"root": root_of(path), "blank": BLANK, "name": html.escape(CFG["name"]), "repo": CFG["repo"]}

    docs, docs_items, docs_out = ([], [], None)
    if CFG.get("docs"):
        docs, docs_items, docs_out = build_docs(repo_dir)

    srcpages = {str(f.relative_to(SRC / "pages")): f.read_text() for f in sorted((SRC / "pages").rglob("*.html"))}
    metas = {}
    for path, s in srcpages.items():
        m = re.match(r"\s*<!--page\s+(\{.*?\})\s*-->\s*", s, re.S)
        metas[path] = (json.loads(m[1]) if m else {}), (s[m.end():] if m else s)
    titles = {p: m.get("title", CFG["name"]) for p, (m, _) in metas.items()}

    def sitemap(path):
        r = root_of(path)
        pages_li = "\n".join(f'<li><a href="{r}{p}">{html.escape(titles[p])}</a></li>' for p in metas)
        docs_li = "\n".join(f'<li><a href="{r}{p}">{t}</a></li>' for p, t, _ in docs_items)
        idx = f'<li><a href="{r}{docs_out}/">{html.escape(CFG["docs"].get("title", "Documentation"))}</a><ul>\n{docs_li}\n</ul></li>' if docs_out else ""
        return f"<ul>\n{pages_li}\n{idx}\n</ul>"

    def docs_list(path):
        r = root_of(path)
        return "\n".join(f'<li><a href="{r}{p}"><b>{t}</b><span>{s}</span></a></li>' for p, t, s in docs_items)

    out = {}
    for path, (meta, body) in metas.items():
        main_html = fill(body, {**shared(path), "sitemap": sitemap(path), "docs_list": docs_list(path)}, f"src/pages/{path}")
        out[path] = (meta.get("title", CFG["name"]), meta.get("description", CFG["description"]), main_html, meta.get("head", ""), meta.get("class", ""))
    tpl = (SRC / "doc.html").read_text() if docs else ""
    for path, title, desc, body, nav, pager in docs:
        main_html = fill(tpl, {**shared(path), "sitemap": sitemap(path), "docs_list": docs_list(path), "doc_title": title, "doc_nav": nav,
                               "doc_pager": pager, "docs_title": html.escape(CFG["docs"].get("title", "Documentation"))},
                         "src/doc.html", raw={"doc_body": body})
        head = text(title) if not path.endswith("/index.html") else CFG["docs"].get("title", text(title))
        out[path] = (f"{head} · {CFG['name']}", desc, main_html, "", "doc")

    built = {}
    for path, (title, desc, main_html, head, cls) in out.items():
        url = CFG["url"] + re.sub(r"(^|/)index\.html$", r"\1", path)
        page = fill(layout, {**shared(path), "lang": CFG.get("lang", "en"), "title": html.escape(title), "description": html.escape(desc),
                             "url": url, "site": CFG["url"], "og_image": CFG["url"] + CFG.get("og_image", "assets/og.jpg"),
                             "og_image_alt": html.escape(CFG.get("og_image_alt", CFG["name"])), "head": head, "html_class": cls},
                    "src/layout.html", raw={"css": css.replace("{{root}}", root_of(path)), "js": js, "main": main_html})
        page = current(page, path)
        (SITE / path).parent.mkdir(parents=True, exist_ok=True)
        (SITE / path).write_text(page)
        built[path] = page
    check(built)
    print(f"build.py: {len(built)} pages")


if __name__ == "__main__":
    main()
