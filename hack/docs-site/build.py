import argparse
import concurrent.futures
import datetime
import hashlib
import html
import json
import os
import pathlib
import posixpath
import re
import shutil
import string
import subprocess
import sys

HERE = pathlib.Path(__file__).resolve().parent
SHELL = HERE / "shell"

GO_HIDDEN = ("docs/rx/", "docs/tx/", "docs/targets/gnuradio.md", "docs/start/05-receive-on-a-radio.md")

ICONS = {
    "menu": '<line x1="4" x2="20" y1="12" y2="12"/><line x1="4" x2="20" y1="6" y2="6"/><line x1="4" x2="20" y1="18" y2="18"/>',
    "search": '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>',
    "sun": '<circle cx="12" cy="12" r="4"/><path d="M12 2v2"/><path d="M12 20v2"/><path d="m4.93 4.93 1.41 1.41"/><path d="m17.66 17.66 1.41 1.41"/><path d="M2 12h2"/><path d="M20 12h2"/><path d="m6.34 17.66-1.41 1.41"/><path d="m19.07 4.93-1.41 1.41"/>',
    "moon": '<path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z"/>',
    "monitor": '<rect width="20" height="14" x="2" y="3" rx="2"/><line x1="8" x2="16" y1="21" y2="21"/><line x1="12" x2="12" y1="17" y2="21"/>',
    "book": '<path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"/><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"/>',
    "down": '<path d="m6 9 6 6 6-6"/>',
    "left": '<path d="m15 18-6-6 6-6"/>',
    "right": '<path d="m9 18 6-6-6-6"/>',
    "radio": '<path d="M4.9 19.1C1 15.2 1 8.8 4.9 4.9"/><path d="M7.8 16.2c-2.3-2.3-2.3-6.1 0-8.5"/><circle cx="12" cy="12" r="2"/><path d="M16.2 7.8c2.3 2.3 2.3 6.1 0 8.5"/><path d="M19.1 4.9C23 8.8 23 15.1 19.1 19"/>',
    "msg": '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>',
    "tower": '<path d="M4.9 16.1C1 12.2 1 5.8 4.9 1.9"/><path d="M7.8 4.7a6.14 6.14 0 0 0-.8 7.5"/><circle cx="12" cy="9" r="2"/><path d="M16.9 4.7a6.14 6.14 0 0 1 .8 7.5"/><path d="M19.1 1.9a9.96 9.96 0 0 1 0 14.1"/><path d="M9.5 18h5"/><path d="m8 22 4-11 4 11"/>',
    "globe": '<circle cx="12" cy="12" r="10"/><path d="M12 2a14.5 14.5 0 0 0 0 20 14.5 14.5 0 0 0 0-20"/><path d="M2 12h20"/>',
    "x": '<path d="M18 6 6 18"/><path d="m6 6 12 12"/>',
    "package": '<path d="M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z"/><path d="M12 22V12"/><path d="m3.3 7 7.746 4.47a1 1 0 0 0 .508.13 1 1 0 0 0 .508-.13L20.7 7"/><path d="m7.5 4.27 9 5.15"/>',
    "server": '<rect width="20" height="8" x="2" y="2" rx="2"/><rect width="20" height="8" x="2" y="14" rx="2"/><line x1="6" x2="6.01" y1="6" y2="6"/><line x1="6" x2="6.01" y1="18" y2="18"/>',
    "plug": '<path d="M12 22v-5"/><path d="M9 8V2"/><path d="M15 8V2"/><path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z"/>',
    "code": '<polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/>',
}

APP_PATHS = {"/favicon.ico", "/lastheard", "/talkgroups", "/nets", "/peers"}

APP_LINKS = [("Last Heard", "/lastheard", "radio"), ("Talkgroups", "/talkgroups", "msg"), ("Nets", "/nets", "tower"), ("OpenBridge Peers", "/peers", "globe")]

GO_LEDE = "Decode and encode DMR bursts and PDUs as defined in ETSI TS 102 361 parts 1 to 4, run the calls, data, trunking and network protocols of DMR without I/O, and receive and transmit DMR on software radios. DMRHub uses it."

SCHEME = re.compile(r"^[a-z][a-z0-9+.-]*:")


def icon(name, extra=""):
    return f'<svg class="i" viewBox="0 0 24 24" aria-hidden="true"{extra}>{ICONS[name]}</svg>'


def esc(s):
    return html.escape(s, quote=True)


def strip_tags(s):
    return re.sub(r"\s+", " ", html.unescape(re.sub(r"<[^>]+>", "", s))).strip()


class Item:
    def __init__(self, title, src, section):
        self.title = title
        self.src = src
        self.section = section
        self.url = None


class Site:
    def __init__(self, key, name, root, docdir, prefix, github, hidden=()):
        self.key = key
        self.name = name
        self.root = root.resolve()
        self.docdir = docdir
        self.prefix = prefix
        self.github = github
        self.hidden = hidden
        self.sections = []
        self.pages = {}
        self.lastmod = {}

    def is_hidden(self, p):
        return any(p == h or (h.endswith("/") and p.startswith(h)) for h in self.hidden)

    def page_url(self, src):
        if src == "README.md":
            return self.prefix
        parts = src.removeprefix(self.docdir + "/").removesuffix(".md").lower().split("/")
        if parts[-1] in ("readme", "index"):
            parts.pop()
        else:
            parts[-1] = re.sub(r"^\d+-", "", parts[-1])
        return self.prefix + "".join(p + "/" for p in parts)

    def asset_url(self, src):
        return self.prefix + src.removeprefix(self.docdir + "/")

    def load_nav(self):
        navfile = self.root / self.docdir / "nav.md"
        if not navfile.exists():
            sys.exit(f"{navfile} not found")
        cur = None
        for line in navfile.read_text().splitlines():
            m = re.match(r"^(\s*)- (?:\[(.+?)\]\((.+?)\)|(.+?))\s*(?:<!--.*-->)?$", line)
            if not m:
                continue
            indent, title, target, plain = m.groups()
            if not indent and plain:
                cur = (plain.strip(), [])
                self.sections.append(cur)
                continue
            if not target or SCHEME.match(target):
                sys.exit(f"{navfile}: {line.strip()} is not a link to a page")
            src = posixpath.normpath(posixpath.join(self.docdir, target))
            if not indent:
                cur = (None, [])
                self.sections.append(cur)
            if self.is_hidden(src):
                continue
            if not (self.root / src).is_file():
                sys.exit(f"{navfile}: {title} links to {src}, which does not exist")
            it = Item(title, src, cur[0])
            it.url = self.page_url(src)
            cur[1].append(it)
            self.pages[src] = it
        self.sections = [s for s in self.sections if s[1]]
        for f in sorted((self.root / self.docdir).rglob("*.md")):
            src = f.relative_to(self.root).as_posix()
            if src in self.pages or src == f"{self.docdir}/nav.md" or src.startswith(f"{self.docdir}/site/") or self.is_hidden(src):
                continue
            it = Item(None, src, None)
            it.url = self.page_url(src)
            self.pages[src] = it
        urls = {}
        for it in self.pages.values():
            if it.url in urls:
                sys.exit(f"{it.src} and {urls[it.url]} both make {it.url}")
            urls[it.url] = it.src

    def flat(self):
        return [it for _, items in self.sections for it in items]

    def first_in(self, section):
        return next(items[0] for title, items in self.sections if title == section)

    def load_lastmod(self):
        try:
            top = subprocess.run(["git", "-C", str(self.root), "rev-parse", "--show-prefix"], check=True, capture_output=True, text=True).stdout.strip()
            out = subprocess.run(["git", "-C", str(self.root), "log", "--format=@%cI", "--name-only", "--", "."], check=True, capture_output=True, text=True).stdout
        except (OSError, subprocess.CalledProcessError):
            print(f"{self.root}: no git history, so the sitemap has no lastmod for {self.name}", file=sys.stderr)
            return
        when = None
        for line in out.splitlines():
            if line.startswith("@"):
                when = line[1:]
            elif line:
                self.lastmod.setdefault(line.removeprefix(top), when)


class Builder:
    def __init__(self, args):
        self.out = pathlib.Path(args.out).resolve()
        self.base = args.base.rstrip("/")
        self.jobs = args.jobs
        self.hub = Site("hub", "DMRHub", pathlib.Path(args.hub), "doc", "/docs/", "https://github.com/USA-RedDragon/DMRHub")
        self.go = Site("dmrgo", "dmrgo", pathlib.Path(args.dmrgo), "docs", "/docs/dmrgo/", "https://github.com/USA-RedDragon/dmrgo", GO_HIDDEN) if args.dmrgo else None
        self.sites = [s for s in (self.hub, self.go) if s]
        self.template = string.Template((SHELL / "page.html").read_text())
        self.icons = {f"i_{k}": icon(k) for k in ICONS}
        self.icons |= {"i_mode_light": icon("sun", ' data-mode-icon="light" hidden'), "i_mode_dark": icon("moon", ' data-mode-icon="dark" hidden'), "i_mode_system": icon("monitor", ' data-mode-icon="system"')}
        self.copied = set()
        self.assets = {}

    def shell_assets(self):
        dest = self.out / "docs" / "_shell"
        dest.mkdir(parents=True)
        names = {}

        def put(name, data):
            stem, ext = name.rsplit(".", 1)
            hashed = f"{stem}.{hashlib.sha256(data).hexdigest()[:12]}.{ext}"
            (dest / hashed).write_bytes(data)
            names[name] = "/docs/_shell/" + hashed

        for f in sorted((SHELL / "fonts").iterdir()):
            put(f.name, f.read_bytes())
        css = re.sub(r"url\(fonts/([^)]+)\)", lambda m: f"url({names[m.group(1)]})", (SHELL / "shell.css").read_text())
        put("shell.css", css.encode())
        put("shell.js", (SHELL / "shell.js").read_bytes())
        put("search.js", ("window.docsIndex=" + json.dumps(self.search_index(), separators=(",", ":")) + ";\n").encode())
        self.assets = {"css": names["shell.css"], "js": names["shell.js"], "index": names["search.js"], "font": names["nunito-sans-latin-wght-normal.woff2"]}

    def search_index(self):
        idx = [["Overview", "DMRHub", "DMRHub docs", "/"]]
        for site in self.sites:
            for title, items in site.sections:
                idx += [[it.title, site.name, title or "Overview", it.url] for it in items]
        return idx

    def nav_html(self, site, cur):
        def link(it):
            on = ' class="on" aria-current="page"' if it.url == cur else ""
            return f'<li><a href="{esc(it.url)}"{on}>{esc(it.title)}</a></li>'

        out = []
        if site is self.hub:
            on = ' class="on" aria-current="page"' if cur == "/" else ""
            out.append(f'<li><a href="/"{on}>Overview</a></li>')
        for title, items in site.sections:
            lis = "".join(link(it) for it in items)
            if title and len(items) > 6:
                opened = " open" if any(it.url == cur for it in items) else ""
                out.append(f'<li class="sec"><details{opened}><summary class="label">{esc(title)}<span class="count">{len(items)}</span>{icon("down")}</summary><ul>{lis}</ul></details></li>')
            elif title:
                out.append(f'<li class="sec"><span class="label">{esc(title)}</span><ul>{lis}</ul></li>')
            else:
                out.append(lis)
        if site is self.hub and self.go:
            out.append(f'<li class="sec"><span class="label">DMRHub projects</span><ul><li><a href="{self.go.prefix}">dmrgo<span class="tag">Go library</span></a></li></ul></li>')
        head = ""
        if site is self.go:
            head = f'<div class="product"><a class="back" href="/">{icon("left")}DMRHub docs</a><div class="name"><b>dmrgo</b><span class="ver">v2</span></div><p>Go library for DMR. A DMRHub project.</p></div>'
        return head + "<ul>" + "".join(out) + "</ul>"

    def crumbs(self, site, it):
        c = [("DMRHub", "/")]
        if site is self.go:
            c.append(("dmrgo", site.prefix))
            if it.url == site.prefix:
                return c
        if it.section and it.section != it.title:
            c.append((it.section, site.first_in(it.section).url))
        c.append((it.title, it.url))
        return c

    def crumbs_html(self, crumbs):
        parts = []
        for i, (n, u) in enumerate(crumbs):
            if i:
                parts.append(f"<li>{icon('right')}</li>")
            parts.append(f'<li><a href="{esc(u)}">{esc(n)}</a></li>' if i < len(crumbs) - 1 else f'<li aria-current="page">{esc(n)}</li>')
        return f'<ol class="crumbs" aria-label="Breadcrumb">{"".join(parts)}</ol>'

    def pager(self, site, url):
        pages = [(it.title, it.url) for it in site.flat()]
        if site is self.hub:
            pages.insert(0, ("DMRHub docs", "/"))
        idx = next((i for i, p in enumerate(pages) if p[1] == url), None)
        if idx is None:
            return ""
        out = []
        if idx > 0:
            t, u = pages[idx - 1]
            out.append(f'<a class="prev" rel="prev" href="{esc(u)}"><span>Previous</span>{esc(t)}</a>')
        if idx + 1 < len(pages):
            t, u = pages[idx + 1]
            out.append(f'<a class="next" rel="next" href="{esc(u)}"><span>Next</span>{esc(t)}</a>')
        return f'<nav class="pager" aria-label="Previous and next page">{"".join(out)}</nav>'

    def resolve(self, here, target):
        p = posixpath.normpath(target[1:] if target.startswith("/") else posixpath.join(here, target))
        return None if p.startswith("..") else p

    def rewrite_links(self, site, src, body):
        here = posixpath.dirname(src)

        def repl(m):
            target, rest, inner = html.unescape(m.group(1)), m.group(2), m.group(3)
            if SCHEME.match(target) or target.startswith("#"):
                return m.group(0)
            file, _, frag = target.partition("#")
            frag = "#" + frag if frag else ""
            p = self.resolve(here, file) if file else src
            if p is None:
                return m.group(0)
            if site.is_hidden(p):
                return inner
            if p in site.pages:
                url = site.pages[p].url + frag
            elif (site.root / p).is_dir():
                url = f"{site.github}/tree/main/{p}{frag}"
            elif (site.root / p).is_file():
                url = f"{site.github}/blob/main/{p}{frag}"
            else:
                return m.group(0)
            return f'<a href="{esc(url)}"{rest}>{inner}</a>'

        return re.sub(r'<a href="([^"]*)"([^>]*)>(.*?)</a>', repl, body, flags=re.S)

    def rewrite_images(self, site, src, body):
        here = posixpath.dirname(src)

        def repl(m):
            target = html.unescape(m.group(1))
            if SCHEME.match(target):
                return m.group(0)
            p = self.resolve(here, target)
            if p is None or not (site.root / p).is_file():
                raise SystemExit(f"{site.root / src}: image {target} not found")
            url = site.asset_url(p)
            self.copied.add((site.root / p, self.out / url.lstrip("/")))
            return f'<img src="{esc(url)}"'

        return re.sub(r'<img src="([^"]*)"', repl, body)

    def post(self, body):
        body = re.sub(r'<img src="https?://[^"]*"[^>]*>', "", body)
        body = re.sub(r'<a href="[^"]*">\s*</a>', "", body)
        body = re.sub(r"<p>\s*</p>\n?", "", body)
        return body.replace("<table>", '<div class="table"><table>').replace("</table>", "</table></div>")

    def toc(self, body):
        heads = re.findall(r'<h([23]) id="([^"]+)">(.*?)</h\1>', body, re.S)
        if len(heads) < 2:
            return "", ""
        lis = "".join(f'<li class="l{lv}"><a href="#{i}">{esc(strip_tags(t))}</a></li>' for lv, i, t in heads)
        return f'<aside class="onpage" aria-label="On this page"><span class="label">On this page</span><ul>{lis}</ul></aside>', f'<details class="toc-inline"><summary>On this page</summary><ul>{lis}</ul></details>'

    def first_para(self, body, limit=158):
        m = re.search(r"<p>(.*?)</p>", body, re.S)
        text = strip_tags(m.group(1)) if m else ""
        if len(text) > limit:
            text = text[:limit].rsplit(" ", 1)[0].rstrip(",;:") + "."
        return text

    def pandoc(self, path):
        return subprocess.run(["pandoc", "-f", "gfm", "-t", "html5", "--wrap=none", str(path)], check=True, capture_output=True, text=True).stdout

    def write(self, url, title, desc, main, nav, crumbs, onpage=""):
        jsonld = {"@context": "https://schema.org", "@type": "BreadcrumbList", "itemListElement": [{"@type": "ListItem", "position": i + 1, "name": n, "item": self.base + u} for i, (n, u) in enumerate(crumbs)]}
        page = self.template.substitute(
            self.icons,
            title=esc(title),
            description=esc(desc),
            canonical=esc(self.base + (url or "/")),
            jsonld=json.dumps(jsonld).replace("</", "<\\/"),
            app_links="".join(f'<a class="nav-link" href="{u}">{t}</a>' for t, u, _ in APP_LINKS),
            app_menu="".join(f'<a class="m-link" href="{u}">{icon(i)}{t}</a>' for t, u, i in APP_LINKS),
            search=f'<div class="search" role="search">{icon("search")}<input type="search" placeholder="Search docs" aria-label="Search the DMRHub and dmrgo docs" autocomplete="off" spellcheck="false"><kbd>/</kbd><ul class="results" hidden></ul></div>',
            nav=nav,
            main=main,
            onpage=onpage,
            wide="" if onpage else " wide",
            year=datetime.date.today().year,
            **self.assets,
        )
        dest = self.out / "404.html" if url is None else self.out / url.lstrip("/") / "index.html"
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(page)

    def render(self, site, it):
        body = self.pandoc(site.root / it.src)
        body = self.post(self.rewrite_images(site, it.src, self.rewrite_links(site, it.src, body)))
        m = re.search(r"<h1[^>]*>(.*?)</h1>", body, re.S)
        if it.title is None:
            it.title = strip_tags(m.group(1)) if m else site.name
        nav = self.nav_html(site, it.url)
        crumbs = self.crumbs(site, it)
        edit = f'<p class="edit"><a href="{site.github}/blob/main/{it.src}">Edit this page on GitHub</a></p>'
        if site is self.go and it.url == site.prefix:
            body = re.sub(r"<h1[^>]*>.*?</h1>\s*", "", body, count=1, flags=re.S)
            p = re.search(r"<p>(.*?)</p>\s*", body, re.S)
            desc = self.first_para(body)
            lede = p.group(1)
            body = body[: p.start()] + body[p.end():]
            right, inline = self.toc(body)
            hero = f'<div class="hero"><p class="eyebrow">A DMRHub project</p><h1 class="mono">dmrgo</h1><p class="lede">{lede}</p><div class="ctas"><a class="btn primary" href="{site.first_in("Get started").url}">Get started</a><a class="btn outline" href="https://pkg.go.dev/github.com/USA-RedDragon/dmrgo/v2">Go API on pkg.go.dev</a><a class="btn outline" href="https://github.com/USA-RedDragon/dmrgo">GitHub</a></div></div>'
            main = f'<article class="doc">{self.crumbs_html(crumbs)}{hero}{inline}{body}{self.pager(site, it.url)}{edit}</article>'
            self.write(it.url, "dmrgo, the Go DMR library | DMRHub Docs", desc, main, nav, crumbs, right)
            return
        right, inline = self.toc(body)
        body = body.replace("</h1>", "</h1>" + inline, 1) if m else inline + body
        main = f'<article class="doc">{self.crumbs_html(crumbs)}{body}{self.pager(site, it.url)}{edit}</article>'
        self.write(it.url, f"{it.title} | {site.name} Docs", self.first_para(body), main, nav, crumbs, right)

    def home(self):
        card = ""
        if self.go:
            card = f'<div class="card project"><span class="mark">{icon("package")}</span><div><h3><a href="{self.go.prefix}"><b>dmrgo</b></a></h3><p>{GO_LEDE}</p><div class="links"><a href="{self.go.first_in("Get started").url}">Get started</a><a href="{self.go.first_in("Guides").url}">Guides</a></div></div></div>\n'
        main = string.Template((HERE / "home.html").read_text()).substitute(self.icons, dmrgo_card=card)
        lede = strip_tags(re.search(r'<p class="lede">(.*?)</p>', main).group(1))
        desc = lede[:158].rsplit(" ", 1)[0] + "." if len(lede) > 158 else lede
        self.write("/", "DMRHub: run your own DMR network from one binary", desc, main, self.nav_html(self.hub, "/"), [("DMRHub", "/")])

    def not_found(self):
        main = '<article class="doc"><h1>Page not found</h1><p>No page has this address.</p><p><a href="/">Go to the DMRHub docs</a></p></article>'
        self.write(None, "Page not found | DMRHub Docs", "No page has this address.", main, self.nav_html(self.hub, None), [("DMRHub", "/")])

    def sitemap(self):
        rows = []
        for site in self.sites:
            for it in sorted(site.pages.values(), key=lambda i: i.url):
                lm = site.lastmod.get(it.src)
                rows.append(f"<url><loc>{esc(self.base + it.url)}</loc>" + (f"<lastmod>{lm}</lastmod>" if lm else "") + "</url>")
        xml = '<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n' + "\n".join(rows) + "\n</urlset>\n"
        (self.out / "docs" / "sitemap.xml").write_text(xml)
        return len(rows)

    def check(self):
        ids, bad = {}, []

        def anchors(f):
            if f not in ids:
                ids[f] = set(re.findall(r'\sid="([^"]+)"', f.read_text()))
            return ids[f]

        for f in sorted(self.out.rglob("*.html")):
            for target in re.findall(r'(?:href|src)="([^"]*)"', f.read_text()):
                target = html.unescape(target)
                if SCHEME.match(target):
                    continue
                path, _, frag = target.partition("#")
                if path in APP_PATHS:
                    continue
                if not path:
                    dest = f
                elif not path.startswith("/") or path == "/docs/":
                    bad.append(f"{f.relative_to(self.out)}: {target}")
                    continue
                else:
                    dest = self.out / path.lstrip("/")
                    if path.endswith("/"):
                        dest = dest / "index.html"
                if not dest.is_file() or (frag and dest.suffix == ".html" and frag not in anchors(dest)):
                    bad.append(f"{f.relative_to(self.out)}: {target}")
        if bad:
            raise SystemExit("broken links:\n" + "\n".join(bad))

    def run(self):
        for site in self.sites:
            site.load_nav()
            site.load_lastmod()
        if self.out.exists():
            shutil.rmtree(self.out)
        self.out.mkdir(parents=True)
        self.shell_assets()
        work = [(site, it) for site in self.sites for it in site.pages.values()]
        with concurrent.futures.ThreadPoolExecutor(self.jobs) as pool:
            for f in [pool.submit(self.render, s, it) for s, it in work]:
                f.result()
        self.home()
        self.not_found()
        for a, b in sorted(self.copied):
            b.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(a, b)
        n = self.sitemap()
        self.check()
        print(f"{self.out}: {n} doc pages and the home page, {len(self.copied)} images")


def main():
    ap = argparse.ArgumentParser(description="Build the dmrhub.net docs site: the DMRHub docs at / and /docs/, and the dmrgo docs at /docs/dmrgo/.")
    ap.add_argument("--hub", default=str(HERE.parent.parent), help="DMRHub source tree (default: this repository)")
    ap.add_argument("--dmrgo", help="dmrgo source tree; without it the site has only the DMRHub docs")
    ap.add_argument("--out", default="artifacts/docs-site", help="output directory, replaced on each build")
    ap.add_argument("--base", default="https://dmrhub.net", help="absolute URL of the site, for canonical links and the sitemap")
    ap.add_argument("--jobs", type=int, default=os.cpu_count() or 4, help="pandoc processes at once")
    Builder(ap.parse_args()).run()


if __name__ == "__main__":
    main()
