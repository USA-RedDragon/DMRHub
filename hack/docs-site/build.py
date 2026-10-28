import base64
import html
import mimetypes
import pathlib
import re
import sys

import markdown

DOC = pathlib.Path(sys.argv[1])
OUT = pathlib.Path(sys.argv[2])
TEMPLATE = pathlib.Path(__file__).with_name("template.html").read_text()

GROUPS = [
    ("Get started", [(None, [("getting-started.md", "Get started"), ("FAQ.md", "FAQ")])]),
    ("Guides", [
        ("Users", [("User-Guide.md", "User's guide"), ("guides/create-account.md", "Create an account"), ("guides/connect-hotspot.md", "Connect a hotspot"), ("guides/link-repeater.md", "Link a repeater"), ("guides/talkgroups.md", "Pick your talkgroups"), ("guides/parrot.md", "Test with the parrot"), ("guides/private-calls.md", "Make a private call"), ("guides/text-messages.md", "Send text messages"), ("guides/positions.md", "Share your position"), ("guides/nets.md", "Run or join a net"), ("guides/last-heard.md", "See who is on the air"), ("guides/voice-codec.md", "Use Codec2 radios"), ("guides/browser-radio.md", "Use the Browser Radio"), ("guides/account.md", "Manage your account")]),
        ("Admins", [("setup.md", "Set up a server"), ("admin/install.md", "Install DMRHub"), ("admin/overview.md", "Admin overview"), ("admin/users.md", "Users"), ("admin/talkgroups.md", "Talkgroups"), ("admin/repeaters.md", "Repeaters"), ("admin/nets.md", "Nets"), ("admin/openbridge.md", "OpenBridge peers"), ("admin/ipsc.md", "Motorola IPSC repeaters"), ("admin/modem.md", "MMDVM modem"), ("admin/transcoding.md", "Transcoding"), ("admin/private-calls.md", "Private call routing"), ("admin/routing.md", "Routing settings"), ("admin/ip-lists.md", "IP lists"), ("admin/messages.md", "Messages and retention"), ("admin/positions.md", "Positions and APRS-IS"), ("admin/plugins.md", "Plugins"), ("admin/replicas.md", "Replicas and scaling"), ("admin/backups.md", "Back up and restore"), ("admin/upgrade.md", "Upgrade DMRHub")]),
    ]),
    ("External software", [(None, [("external/index.md", "Overview"), ("external/pi-star.md", "Pi-Star"), ("external/mmdvmhost.md", "MMDVMHost"), ("external/dmrgateway.md", "DMRGateway"), ("external/droidstar.md", "DroidStar"), ("external/hblink3.md", "HBlink3"), ("external/hblink4.md", "HBlink4"), ("external/freedmr.md", "FreeDMR"), ("external/mmdvm-bridge.md", "MMDVM_Bridge"), ("external/analog-bridge.md", "Analog_Bridge"), ("external/ysf2dmr.md", "YSF2DMR"), ("external/ipsc2mmdvm.md", "ipsc2mmdvm"), ("external/ambeserver.md", "AMBE servers")])]),
    ("Reference", [(None, [("admin/configuration.md", "Configuration"), ("messages.md", "Messages and SMS"), ("positions.md", "Positions"), ("admin/transmission-kinds.md", "Transmission kinds"), ("admin/stress-results.md", "Stress results")])]),
    ("Developers", [(None, [("plugins.md", "Plugins"), ("oauth-apps.md", "OAuth apps"), ("webtransport.md", "MMDVM over WebTransport"), ("Building.md", "Building from source")])]),
]

missing = [d for _, subs in GROUPS for _, docs in subs for d, _ in docs if not (DOC / d).exists()]
if missing:
    sys.exit("nav names missing pages: " + ", ".join(missing))


def slug(name):
    return re.sub(r"[^a-z0-9]+", "-", name.lower().removesuffix(".md")).strip("-")


def data_uri(path):
    mime = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
    return f"data:{mime};base64," + base64.b64encode(path.read_bytes()).decode()


def render(name):
    here = (DOC / name).parent
    text = (DOC / name).read_text()
    page = slug(name)
    md = markdown.Markdown(extensions=["tables", "fenced_code", "toc", "sane_lists"], extension_configs={"toc": {"slugify": lambda v, s: page + "-" + slug(v)}})
    body = md.convert(text)

    def fix_link(m):
        target = html.unescape(m.group(1))
        if re.match(r"^[a-z]+:", target):
            return m.group(0)
        if target.startswith("#"):
            return f'href="#{page}-{target[1:]}"'
        file, _, frag = target.partition("#")
        dest = (here / file).resolve()
        if file.endswith(".md") and dest.exists() and DOC.resolve() in dest.parents:
            rel = dest.relative_to(DOC.resolve()).as_posix()
            return f'href="#{slug(rel)}-{frag}"' if frag else f'href="#{slug(rel)}"'
        repo = DOC.resolve().parent
        if dest.exists() and repo in dest.parents:
            return f'href="https://github.com/USA-RedDragon/DMRHub/blob/main/{dest.relative_to(repo).as_posix()}"'
        return m.group(0)

    def fix_img(m):
        src = html.unescape(m.group(1))
        if re.match(r"^[a-z]+:", src):
            return 'src="" data-dropped="external"'
        path = (DOC.parent / src.lstrip("/")).resolve() if src.startswith("/") else (here / src).resolve()
        return f'src="{data_uri(path)}"' if path.exists() else m.group(0)

    body = re.sub(r'href="([^"]*)"', fix_link, body)
    body = re.sub(r'src="([^"]*)"', fix_img, body)
    body = re.sub(r"<table>", '<div class="table"><table>', body).replace("</table>", "</table></div>")
    return f'<article id="{page}" class="doc">{body}</article>'


def items(docs):
    return "".join(f'<li><a href="#{slug(d)}">{html.escape(t)}</a></li>' for d, t in docs if (DOC / d).exists())


nav = []
zones = []
for group, subs in GROUPS:
    parts = "".join((f"<h5>{sub}</h5>" if sub else "") + f"<ul>{items(docs)}</ul>" for sub, docs in subs)
    nav.append(f"<h4>{group}</h4>{parts}")
    zones.append(f'<div class="zone"><h3>{group}</h3>{parts}</div>')
articles = [render(f.relative_to(DOC).as_posix()) for f in sorted(DOC.rglob("*.md"))]
shots = DOC / "Screenshots" / "lastheard.png"
page = TEMPLATE.replace("{{NAV}}", "".join(nav)).replace("{{ZONES}}", "".join(zones)).replace("{{DOCS}}", "\n".join(articles)).replace("{{SHOT}}", data_uri(shots) if shots.exists() else "")
OUT.write_text(page)
print(OUT, len(page))
