"use client";

import { useEffect, useRef } from "react";

const COLS = 46;
const ROWS = 32;
const RAMP = " .,:;!^+";

// Renders a rotating, lit sphere as ASCII, split by a tilted dark band —
// a "lumen" of light sweeping across a surface of thought.
function frame(t: number): string {
  // Light direction orbits the sphere slowly.
  const la = t * 0.35;
  let lx = Math.cos(la), ly = -0.55 * Math.sin(la * 0.7), lz = Math.sin(la) * 0.6 + 0.3;
  const ll = Math.hypot(lx, ly, lz);
  lx /= ll; ly /= ll; lz /= ll;

  // Axis of the dark band, tilted ~45° and precessing.
  const ba = Math.PI / 4 + Math.sin(t * 0.25) * 0.25;
  const bx = Math.cos(ba), by = Math.sin(ba), bz = 0.25 * Math.cos(t * 0.4);

  const spin = t * 0.6;
  const cs = Math.cos(spin), sn = Math.sin(spin);

  let out = "";
  for (let y = 0; y < ROWS; y++) {
    for (let x = 0; x < COLS; x++) {
      const nx = (x - COLS / 2 + 0.5) / (COLS / 2);
      const ny = (y - ROWS / 2 + 0.5) / (ROWS / 2);
      const r2 = nx * nx + ny * ny;
      if (r2 > 1) { out += " "; continue; }
      const nz = Math.sqrt(1 - r2);

      // Dark channel through the middle of the sphere.
      const band = nx * bx + ny * by + nz * bz;
      if (Math.abs(band) < 0.16) { out += " "; continue; }

      let b = nx * lx + ny * ly + nz * lz;

      // Surface texture rotating with the sphere so it feels alive.
      const px = nx * cs + nz * sn;
      const tex = Math.sin(px * 9 + ny * 5 + t * 1.5) * 0.12;
      b = b * 0.85 + tex + 0.1;

      if (b <= 0.02) { out += " "; continue; }

      // Bright rim glints.
      if (r2 > 0.82 && b > 0.55) { out += (x + y) % 5 === 0 ? "+" : "^"; continue; }

      const i = Math.min(RAMP.length - 3, Math.max(1, Math.floor(b * (RAMP.length - 2))));
      out += RAMP[i];
    }
    out += "\n";
  }
  return out;
}

export default function AsciiOrb() {
  const ref = useRef<HTMLPreElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduce) { el.textContent = frame(1.2); return; }

    let raf = 0;
    let last = 0;
    const start = performance.now();
    const tick = (now: number) => {
      if (now - last > 33) { // ~30fps is plenty for ASCII
        el.textContent = frame((now - start) / 1000);
        last = now;
      }
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, []);

  return <pre ref={ref} aria-hidden="true">{frame(0)}</pre>;
}
