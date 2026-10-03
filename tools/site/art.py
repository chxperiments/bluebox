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
    return f"""<svg class="art fork-loop" viewBox="0 0 720 240" role="img" aria-labelledby="fork-art-t">
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
