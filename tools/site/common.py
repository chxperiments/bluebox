import html

PAGES = [
    ("index.html", "Overview"),
    ("architecture.html", "Architecture"),
    ("security.html", "Security"),
    ("benchmarks.html", "Benchmarks"),
    ("docs.html", "Docs"),
]
GH = "https://github.com/chxperiments/bluebox"


def logo():
    """The bluebox wordmark, set in Archivo Expanded ExtraBold and stored as
    outlines (docs/assets/bluebox-wordmark.svg), so it needs no font load.
    Solid for the nav, hollow for the footer."""
    import re
    from pathlib import Path
    svg = (Path(__file__).resolve().parents[2] / "docs/assets/bluebox-wordmark.svg").read_text()
    vb = re.search(r'viewBox="([^"]+)"', svg).group(1)
    d = re.search(r' d="([^"]+)"', svg).group(1)
    x, y, w, h = (float(v) for v in vb.split())
    nav = f'<svg class="logo" viewBox="{vb}" aria-hidden="true"><path d="{d}"/></svg>'
    pad = 40
    foot = (f'<svg viewBox="{x - pad:.0f} {y - pad:.0f} {w + 2 * pad:.0f} {h + 2 * pad:.0f}">'
            f'<path d="{d}"/></svg>')
    return nav, foot


def page(filename, title, description, body):
    import art
    DRIPS = art.drips(23)
    LOGO, WORDMARK = logo()
    nav = "\n".join(
        f'        <a href="{f}"{" aria-current=\"page\"" if f == filename else ""}>{t}</a>'
        for f, t in PAGES if f != "index.html"
    )
    full_title = "bluebox" if filename == "index.html" else f"{title} | bluebox"
    return f"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{full_title}</title>
<meta name="description" content="{html.escape(description)}">
<meta property="og:title" content="{full_title}">
<meta property="og:description" content="{html.escape(description)}">
<meta name="theme-color" content="#1300f9">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Archivo:wdth,wght@125,700;125,800&family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap">
<link rel="stylesheet" href="assets/site.css">
</head>
<body>
<header class="nav">
  <div class="wrap">
    <a class="brand" href="index.html" aria-label="bluebox, home">{LOGO}</a>
    <nav class="nav-links" aria-label="Site">
{nav}
    </nav>
    <a class="nav-gh" href="{GH}">GitHub</a>
  </div>
</header>
<main id="top">
{body}
</main>
<footer class="footer invert wipe">
  <div class="drips">{DRIPS}</div>
  <div class="wrap">
    <div class="footer-top">
      <div>
        <p class="footer-lead">Run code you do not trust, in a box of its own.</p>
        <div class="install">
          <code><span class="p">$ </span>curl -fsSL https://chxperiments.github.io/bluebox/install.sh | sh</code>
          <button class="copy" type="button" data-copy="curl -fsSL https://chxperiments.github.io/bluebox/install.sh | sh">Copy</button>
        </div>
      </div>
      <nav aria-label="Project">
        <h4>Project</h4>
        <a href="architecture.html">Architecture</a>
        <a href="security.html">Security</a>
        <a href="benchmarks.html">Benchmarks</a>
        <a href="docs.html">Docs</a>
      </nav>
      <nav aria-label="Build with it">
        <h4>Build with it</h4>
        <a href="docs.html#sdk">Python SDK</a>
        <a href="docs.html#sdk">TypeScript SDK</a>
        <a href="docs.html#sdk">Go SDK</a>
        <a href="docs.html#sdk">Rust SDK</a>
        <a href="docs.html#mcp">MCP server</a>
        <a href="docs.html#api">Local API</a>
      </nav>
      <nav aria-label="Source">
        <h4>Source</h4>
        <a href="{GH}">GitHub</a>
        <a href="{GH}/blob/main/SECURITY.md">Threat model</a>
        <a href="{GH}/tree/main/bench">Benchmark code</a>
        <a href="{GH}/tree/main/examples">Examples</a>
      </nav>
    </div>
    <div class="wordmark" aria-hidden="true">{WORDMARK}</div>
    <div class="footer-base">
      <span>MIT licensed. Isolated, disposable microVM sandboxes.</span>
      <a href="#top">Back to top</a>
    </div>
  </div>
</footer>
<script src="assets/site.js" defer></script>
</body>
</html>
"""


def codebox(group, samples, label=None):
    """samples: list of (lang, code-html). One tab per language."""
    tabs = "".join(
        f'<button class="tab" role="tab" data-lang="{lang}" aria-selected="{"true" if i == 0 else "false"}" tabindex="{0 if i == 0 else -1}">{name}</button>'
        for i, (lang, name, _) in enumerate(samples)
    )
    # The first language shows without JavaScript; the script then applies
    # the reader's saved choice.
    panels = "".join(
        f'<pre class="code" data-lang-panel="{lang}"{"" if i == 0 else " hidden"}>{code}</pre>'
        for i, (lang, _, code) in enumerate(samples)
    )
    aria = f' aria-label="{label}"' if label else ""
    return f"""<div class="codebox" data-lang-group="{group}">
  <div class="codebox-bar" role="tablist"{aria}>{tabs}<button class="copy" type="button" data-copy-from>Copy</button></div>
  {panels}
</div>"""


def esc(s):
    return html.escape(s, quote=False)
