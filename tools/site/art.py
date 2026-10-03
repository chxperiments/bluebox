"""Line-art figures for the site: white strokes on the blue, drawn by CSS
(stroke-dashoffset on pathLength=1) and moved by SMIL, which site.js pauses
under prefers-reduced-motion."""

from math import cos, sin, radians


def iso_cube(cx, cy, s):
    """An isometric cube seen from above: hexagon outline plus the three
    visible inner edges, as one path."""
    k = 0.866 * s
    top, ur, lr = (cx, cy - s), (cx + k, cy - s / 2), (cx + k, cy + s / 2)
    bot, ll, ul = (cx, cy + s), (cx - k, cy + s / 2), (cx - k, cy - s / 2)
    c = (cx, cy)
    pts = lambda *p: " ".join(f"{x:.1f},{y:.1f}" for x, y in p)
    return (f"M{pts(top)} L{pts(ur, lr, bot, ll, ul)} Z "
            f"M{pts(ul)} L{pts(c, ur)} M{pts(c)} L{pts(bot)}")


def hero():
    cx, cy = 300, 300
    layers = [
        (250, "your machine", "dash"),
        (190, "VMM, confined", ""),
        (130, "KVM", ""),
        (70, "guest kernel", ""),
    ]
    paths, labels = [], []
    for i, (s, label, cls) in enumerate(layers):
        paths.append(f'<path class="s {cls}" pathLength="1" style="--i:{i}" d="{iso_cube(cx, cy, s)}"/>')
        # A leader from the cube's right edge to a label on the right.
        y = cy - s / 2 + 6
        x0 = cx + 0.866 * s
        labels.append(
            f'<g class="lbl" style="--i:{i}"><path class="s thin" d="M{x0:.0f},{y:.0f} H560"/>'
            f'<text x="568" y="{y + 4:.0f}">{label}</text></g>')
    # A command travels in from the top-left and lands in the guest.
    route = f"M40,60 C150,60 190,160 {cx},{cy}"
    return f"""<svg class="art draw" viewBox="0 0 720 600" role="img" aria-labelledby="hero-art-t">
  <title id="hero-art-t">Nested boxes: your machine, the confined VMM, KVM and the guest kernel. A command travels inward and runs in the guest.</title>
  <g class="float">
    {''.join(paths)}
    <path class="s thin" d="{route}" stroke-dasharray="2 6"/>
    <rect class="f" x="-5" y="-5" width="10" height="10" opacity="0">
      <animateMotion dur="3.6s" repeatCount="indefinite" path="{route}" keyPoints="0;1;1" keyTimes="0;0.55;1" calcMode="spline" keySplines="0.65 0 0.35 1;0 0 1 1"/>
      <animate attributeName="opacity" values="0;1;1;0;0" keyTimes="0;0.08;0.55;0.62;1" dur="3.6s" repeatCount="indefinite"/>
    </rect>
    <path class="f pulse" d="{iso_cube(cx, cy, 22).split(' M')[0]}"/>
  </g>
  {''.join(labels)}
</svg>"""


def hero_minimal():
    """The nested boxes alone, centred: the first thing on the site."""
    cx, cy = 300, 300
    paths = [
        f'<path class="s{" dash" if i == 0 else ""}" pathLength="1" style="--i:{i}" d="{iso_cube(cx, cy, s)}"/>'
        for i, s in enumerate((250, 190, 130, 70))
    ]
    route = f"M40,60 C150,60 190,160 {cx},{cy}"
    return f"""<svg class="art draw" viewBox="30 30 540 540" role="img" aria-labelledby="hero-art-t">
  <title id="hero-art-t">Nested boxes: your machine, the confined VMM, KVM and the guest kernel. A command travels inward and runs in the guest.</title>
  <g class="float">
    {''.join(paths)}
    <path class="s thin" d="{route}" stroke-dasharray="2 6"/>
    <rect class="f" x="-5" y="-5" width="10" height="10" opacity="0">
      <animateMotion dur="3.6s" repeatCount="indefinite" path="{route}" keyPoints="0;1;1" keyTimes="0;0.55;1" calcMode="spline" keySplines="0.65 0 0.35 1;0 0 1 1"/>
      <animate attributeName="opacity" values="0;1;1;0;0" keyTimes="0;0.08;0.55;0.62;1" dur="3.6s" repeatCount="indefinite"/>
    </rect>
    <path class="f pulse" d="{iso_cube(cx, cy, 22).split(' M')[0]}"/>
  </g>
</svg>"""


def hero_labelled():
    """The centred hero box with its layers named, alternating left and
    right so the composition stays centred on the box."""
    cx, cy = 300, 300
    layers = [(250, "your machine", "l"), (190, "VMM, confined", "r"), (130, "KVM", "l"), (70, "guest kernel", "r")]
    paths, labels = [], []
    for i, (s_, name, side) in enumerate(layers):
        paths.append(f'<path class="s{" dash" if i == 0 else ""}" pathLength="1" style="--i:{i}" d="{iso_cube(cx, cy, s_)}"/>')
        y = cy - s_ / 2 + 4 + i * 22
        if side == "r":
            x0, x1 = cx + 0.866 * s_, 640
            labels.append(f'<g class="lbl" style="--i:{i}"><path class="s thin" d="M{x0:.0f},{y:.0f} H{x1}"/>'
                          f'<circle class="f" cx="{x0:.0f}" cy="{y:.0f}" r="3"/><text class="hl" x="{x1 + 12}" y="{y + 7:.0f}">{name}</text></g>')
        else:
            x0, x1 = cx - 0.866 * s_, -40
            labels.append(f'<g class="lbl" style="--i:{i}"><path class="s thin" d="M{x0:.0f},{y:.0f} H{x1}"/>'
                          f'<circle class="f" cx="{x0:.0f}" cy="{y:.0f}" r="3"/><text class="hl" x="{x1 - 12}" y="{y + 7:.0f}" text-anchor="end">{name}</text></g>')
    route = f"M40,60 C150,60 190,160 {cx},{cy}"
    return f"""<svg class="art draw" viewBox="-260 30 1120 540" role="img" aria-labelledby="hero-art-l">
  <title id="hero-art-l">Nested boxes: your machine, the confined VMM, KVM and the guest kernel. A command travels inward and runs in the guest.</title>
  <g class="float">
    {''.join(paths)}
    <path class="s thin" d="{route}" stroke-dasharray="2 6"/>
    <rect class="f" x="-5" y="-5" width="10" height="10" opacity="0">
      <animateMotion dur="3.6s" repeatCount="indefinite" path="{route}" keyPoints="0;1;1" keyTimes="0;0.55;1" calcMode="spline" keySplines="0.65 0 0.35 1;0 0 1 1"/>
      <animate attributeName="opacity" values="0;1;1;0;0" keyTimes="0;0.08;0.55;0.62;1" dur="3.6s" repeatCount="indefinite"/>
    </rect>
    <path class="f pulse" d="{iso_cube(cx, cy, 22).split(' M')[0]}"/>
  </g>
  {''.join(labels)}
</svg>"""


def architecture():
    cols = [("podman", 300), ("krun", 600), ("firecracker", 900)]
    rows = [
        ("launcher", 160, ["podman, crun", "crun, our spec", "crun jail"]),
        ("VMM", 236, ["libkrun", "libkrun", "Firecracker"]),
        ("confinement", 312, ["6 caps, seccomp", "6 caps, seccomp", "0 caps, seccomp"]),
        ("guest", 464, ["kernel 6.12", "kernel 6.12", "kernel 6.1"]),
    ]
    out = []
    out.append('<rect class="s" pathLength="1" style="--i:0" x="170" y="40" width="860" height="54" rx="4"/>')
    out.append('<text class="big lbl" x="194" y="74" style="--i:0">bluebox: CLI, SDKs, local API</text>')
    for r, (name, y, cells) in enumerate(rows, start=1):
        out.append(f'<text class="dim lbl" x="20" y="{y + 28}" style="--i:{r}">{name}</text>')
        for (col, x), label in zip(cols, cells):
            out.append(f'<rect class="s" pathLength="1" style="--i:{r}" x="{x - 130}" y="{y}" width="260" height="46" rx="4"/>')
            out.append(f'<text class="lbl" x="{x - 114}" y="{y + 28}" style="--i:{r}">{label}</text>')
    out.append('<rect class="s" pathLength="1" style="--i:4" x="170" y="388" width="860" height="46" rx="4"/>')
    out.append('<text class="lbl" x="194" y="416" style="--i:4">KVM: own kernel per sandbox</text>')
    out.append('<text class="dim lbl" x="20" y="416" style="--i:4">hypervisor</text>')
    for i, (col, x) in enumerate(cols):
        out.append(f'<text class="big lbl" x="{x - 130}" y="140" style="--i:1">{col}</text>')
        # The flow runs in the gap beside each column, never through text.
        gx = x + 142
        line = f"M{gx},94 V510"
        out.append(f'<path class="s dash" style="--i:{i}" d="{line}"/>')
        out.append(
            f'<circle class="f" r="4" opacity="0"><animateMotion dur="2.4s" begin="{i * 0.4}s" repeatCount="indefinite" path="{line}"/>'
            f'<animate attributeName="opacity" values="0;1;1;0" keyTimes="0;0.1;0.85;1" dur="2.4s" begin="{i * 0.4}s" repeatCount="indefinite"/></circle>')
    return f"""<svg class="art draw" viewBox="0 0 1060 530" role="img" aria-labelledby="arch-art-t">
  <title id="arch-art-t">A command flows from the bluebox interface down each backend, through its launcher, VMM and host confinement, across KVM, into the guest.</title>
  {''.join(out)}
</svg>"""


def fork():
    trunk = "M20,120 H190"
    b1 = "M190,120 C260,120 270,40 340,40 H430"
    b2 = "M190,120 H430"
    b3 = "M190,120 C260,120 270,200 340,200 H430"
    merge = "M430,120 C500,120 520,120 580,120 H700"
    return f"""<svg class="art fork-loop sans" viewBox="0 0 720 240" role="img" aria-labelledby="fork-art-t">
  <title id="fork-art-t">A sandbox forks into three trials. Two are discarded; one is applied back.</title>
  <path class="s" d="{trunk}"/>
  <circle class="f" cx="20" cy="120" r="5"/>
  <text x="20" y="100">agent</text>
  <text x="196" y="100" class="dim">fork</text>
  <g class="drop"><path class="s branch" pathLength="1" d="{b1}"/><text x="440" y="44">trial-a</text></g>
  <path class="s branch b2" pathLength="1" d="{b2}"/><text x="440" y="110" class="dim">trial-b</text>
  <g class="drop"><path class="s branch b3" pathLength="1" d="{b3}"/><text x="440" y="204">trial-c</text></g>
  <path class="s merge" pathLength="1" d="{merge}"/>
  <text x="560" y="100">apply</text>
  <text x="440" y="70" class="dim">discard</text>
  <text x="440" y="234" class="dim">discard</text>
  <circle class="f" cx="700" cy="120" r="5"/>
</svg>"""


def security():
    layers = [
        (40, "your machine"),
        (90, "VMM: few or no capabilities, seccomp, own namespaces"),
        (140, "KVM"),
        (190, "guest: root here, only here"),
    ]
    out = []
    for i, (inset, label) in enumerate(layers):
        w, h = 760 - 2 * inset, 420 - 2 * inset
        out.append(f'<rect class="s{" dash" if i == 0 else ""}" pathLength="1" style="--i:{i}" x="{inset}" y="{inset}" width="{w}" height="{h}" rx="4"/>')
        out.append(f'<text class="lbl{" dim" if i == 0 else ""}" style="--i:{i}" x="{inset + 14}" y="{inset + 24}">{label}</text>')
    checks = ["loopback", "host files", "symlinks", "mounts", "token", "fork bomb"]
    ticks = "".join(
        f'<g class="tick" style="--i:{i}"><path class="s" d="M{250 + i * 52},248 l6,7 l12,-14"/></g>' for i in range(len(checks)))
    return f"""<svg class="art draw" viewBox="0 0 760 420" role="img" aria-labelledby="sec-art-t" style="--scan:180px">
  <title id="sec-art-t">Four nested layers between a workload and your machine, with the escape suite's checks passing.</title>
  {''.join(out)}
  <path class="s scan" d="M200,232 H560"/>
  {ticks}
</svg>"""


def workflow():
    """bluebox as a workflow: three ways in, one router, three engines, each
    booting its own microVM. Signals travel every connector."""
    out = []
    W = 1120

    def node(x, y, w, h, title, sub="", i=0, strong=False):
        cls = "s node-strong" if strong else "s"
        g = [f'<rect class="{cls}" pathLength="1" style="--i:{i}" x="{x}" y="{y}" width="{w}" height="{h}" rx="6"/>']
        ty = y + (h / 2 + 6 if not sub else h / 2 - 3)
        g.append(f'<text class="big lbl{" on-strong" if strong else ""}" style="--i:{i}" x="{x + 18}" y="{ty:.0f}">{title}</text>')
        if sub:
            g.append(f'<text class="dim lbl{" on-strong" if strong else ""}" style="--i:{i}" x="{x + 18}" y="{ty + 20:.0f}">{sub}</text>')
        return "".join(g)

    def flow(d, i, dur, begin):
        return (f'<path class="s thin" pathLength="1" style="--i:{i}" d="{d}"/>'
                f'<circle class="f" r="4" opacity="0"><animateMotion dur="{dur}s" begin="{begin}s" repeatCount="indefinite" path="{d}"/>'
                f'<animate attributeName="opacity" values="0;1;1;0" keyTimes="0;0.1;0.85;1" dur="{dur}s" begin="{begin}s" repeatCount="indefinite"/></circle>')

    # Inputs
    inputs = [("CLI", "bluebox run, exec", 70), ("SDKs", "Python, TS, Go, Rust", 230), ("MCP", "for AI agents", 390)]
    for k, (t, sub, y) in enumerate(inputs):
        out.append(node(20, y, 190, 64, t, sub, i=0))
        out.append(flow(f"M210,{y + 32} C280,{y + 32} 280,262 350,262", 1, 2.2, k * 0.5))
    # Router
    out.append(node(350, 210, 230, 104, "bluebox", "Bluefile: backend:", i=2, strong=True))
    # Engines
    engines = [("podman", "libkrun via podman", "1.28 s", 70), ("krun", "libkrun, direct", "0.76 s", 230), ("firecracker", "snapshot restore", "0.31 s", 390)]
    for k, (t, sub, ms, y) in enumerate(engines):
        out.append(flow(f"M580,262 C650,262 650,{y + 32} 720,{y + 32}", 3, 2.0, 1.1 + k * 0.45))
        out.append(node(720, y, 200, 64, t, sub, i=4))
        out.append(f'<g class="lbl" style="--i:5"><rect class="f" x="{928}" y="{y + 20}" width="62" height="24" rx="4"/>'
                   f'<text class="on-strong" x="{959}" y="{y + 37}" text-anchor="middle">{ms}</text></g>')
        # Each engine boots its own microVM: a small box with a kernel inside.
        cx, cy = 1062, y + 32
        out.append(flow(f"M990,{cy} H1032", 6, 1.2, 2.2 + k * 0.45))
        out.append(f'<path class="s" pathLength="1" style="--i:6" d="{iso_cube(cx, cy, 26)}"/>'
                   f'<path class="f pulse" d="{iso_cube(cx, cy, 8).split(" M")[0]}"/>')
    out.append('<text class="dim lbl" style="--i:6" x="1062" y="490" text-anchor="middle">own kernel</text>')
    return f"""<svg class="art draw workflow sans" viewBox="0 0 {W} 500" role="img" aria-labelledby="wf-t">
  <title id="wf-t">Calls from the CLI, the SDKs and MCP go to bluebox, which reads the Bluefile's backend and boots the sandbox with podman, krun or Firecracker, each a microVM with its own kernel.</title>
  {''.join(out)}
</svg>"""


# ---------------------------------------------------------------------------
# Street marks: sticker, crown, drips, scribble, arrow. Two pigments only.

def badge():
    """A round sticker, its ring of text turning slowly around a small box."""
    ring = "OWN KERNEL + FRESH EVERY RUN + NOTHING SHARED + "
    return f"""<svg class="badge" viewBox="0 0 200 200" aria-hidden="true">
  <defs><path id="badge-ring" d="M100,100 m-74,0 a74,74 0 1,1 148,0 a74,74 0 1,1 -148,0"/></defs>
  <circle class="badge-fill" cx="100" cy="100" r="97"/>
  <g class="spin"><text class="badge-text"><textPath href="#badge-ring">{ring}</textPath></text></g>
  <path class="badge-mark" d="{iso_cube(100, 100, 30)}"/>
</svg>"""


def crown():
    """A hand-drawn three-point crown, the street tag for a king."""
    return ('<path class="tag-stroke" pathLength="1" d="M8,62 L14,18 L34,44 L50,6 L66,44 L86,18 L92,62 Z"/>'
            '<circle class="tag-dot" cx="14" cy="13" r="5"/><circle class="tag-dot" cx="50" cy="1" r="5"/>'
            '<circle class="tag-dot" cx="86" cy="13" r="5"/>')


def drips(seed=7):
    """Paint running down from the section above: a ragged edge with drops
    of different lengths, as one path."""
    import random
    rnd = random.Random(seed)
    x, pts = 0.0, ["M0,0"]
    while x < 1200:
        w = rnd.uniform(18, 46)
        if rnd.random() < 0.45:
            ln = rnd.uniform(14, 70)
            d = rnd.uniform(6, 11)
            mid = x + w / 2
            pts.append(f"L{mid - d:.1f},{rnd.uniform(6, 12):.1f} L{mid - d * 0.8:.1f},{ln:.1f} "
                       f"A{d * 0.8:.1f},{d * 0.8:.1f} 0 0 0 {mid + d * 0.8:.1f},{ln:.1f} L{mid + d:.1f},{rnd.uniform(6, 12):.1f}")
        x += w
        pts.append(f"L{min(x, 1200):.1f},{rnd.uniform(4, 13):.1f}")
    pts.append("L1200,0 Z")
    return (f'<svg class="drips-svg" viewBox="0 0 1200 90" preserveAspectRatio="none" aria-hidden="true">'
            f'<path d="{" ".join(pts)}"/></svg>')


def scribble(seed=3):
    """A quick marker underline: two loose passes."""
    import random
    rnd = random.Random(seed)
    a = f"M4,{12 + rnd.uniform(-2, 2):.1f} C60,{4 + rnd.uniform(-2, 2):.1f} 140,{16 + rnd.uniform(-2, 2):.1f} 236,{8 + rnd.uniform(-2, 2):.1f}"
    b = f"M18,{22 + rnd.uniform(-2, 2):.1f} C90,{14 + rnd.uniform(-2, 2):.1f} 160,{24 + rnd.uniform(-2, 2):.1f} 226,{18 + rnd.uniform(-2, 2):.1f}"
    return (f'<svg class="scribble on-view" viewBox="0 0 240 30" aria-hidden="true">'
            f'<path class="s mark" pathLength="1" style="--i:0" d="{a}"/><path class="s mark" pathLength="1" style="--i:2" d="{b}"/></svg>')


def arrow():
    """A hand-drawn arrow with a marker note, curving into the install line."""
    return ('<svg class="tryit on-view" viewBox="0 0 160 90" aria-hidden="true">'
            '<path class="s mark" pathLength="1" style="--i:0" d="M14,26 C40,30 70,40 96,60 C108,69 118,74 132,76"/>'
            '<path class="s mark" pathLength="1" style="--i:3" d="M118,62 L134,77 L114,86"/>'
            '<text class="marker" x="6" y="16" transform="rotate(-8 6 16)">try it</text></svg>')



def cube_drips(cx=300, cy=300, s=250, seed=5):
    """White paint running off the outer box's two lower edges."""
    import random
    rnd = random.Random(seed)
    k = 0.866 * s
    out = []
    for i, t in enumerate([0.12, 0.3, 0.55, 0.78, 0.9]):
        # along the lower-left edge (ll -> bottom), then the lower-right (bottom -> lr)
        if i % 2 == 0:
            x = cx - k + t * k
            y = cy + s / 2 + t * s / 2
        else:
            x = cx + t * k
            y = cy + s - t * s / 2
        ln = rnd.uniform(26, 90)
        out.append(f'<path class="s drip" pathLength="1" style="--i:{4 + i}" d="M{x:.1f},{y:.1f} V{y + ln:.1f}"/>'
                   f'<circle class="f drop" style="--i:{4 + i}" cx="{x:.1f}" cy="{y + ln + 4:.1f}" r="4.2"/>')
    return "".join(out)


def word_drips(seed=9):
    """Blue paint running off the wordmark's baseline."""
    import random
    rnd = random.Random(seed)
    out = []
    for i, x in enumerate([40, 112, 238, 395, 470, 560, 690, 790, 905, 950]):
        x += rnd.uniform(-8, 8)
        w = rnd.uniform(9, 15)
        ln = rnd.uniform(24, 78)
        out.append(f'<path class="wdrip" style="--n:{i}" d="M{x - w:.1f},206 C{x - w:.1f},214 {x - w * 0.55:.1f},218 '
                   f'{x - w * 0.55:.1f},{206 + ln:.1f} A{w * 0.55:.1f},{w * 0.55:.1f} 0 0 0 {x + w * 0.55:.1f},{206 + ln:.1f} '
                   f'C{x + w * 0.55:.1f},218 {x + w:.1f},214 {x + w:.1f},206 Z"/>')
    return "".join(out)
