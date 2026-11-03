export type DiffLine = { kind: "same" | "add" | "del"; text: string };

/**
 * Line diff (LCS). Mapfiles are a few hundred to a few thousand lines;
 * above ~4M cells it falls back to "everything changed".
 */
export function diffLines(a: string, b: string): DiffLine[] {
  const x = a.split("\n");
  const y = b.split("\n");
  const n = x.length;
  const m = y.length;
  if (n * m > 4_000_000) {
    return [...x.map((t) => ({ kind: "del" as const, text: t })), ...y.map((t) => ({ kind: "add" as const, text: t }))];
  }
  // lcs[i][j] = LCS length of x[i:], y[j:]
  const lcs: Uint32Array[] = Array.from({ length: n + 1 }, () => new Uint32Array(m + 1));
  for (let i = n - 1; i >= 0; i--)
    for (let j = m - 1; j >= 0; j--)
      lcs[i][j] = x[i] === y[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);

  const out: DiffLine[] = [];
  let i = 0;
  let j = 0;
  while (i < n && j < m) {
    if (x[i] === y[j]) {
      out.push({ kind: "same", text: x[i] });
      i++;
      j++;
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ kind: "del", text: x[i++] });
    } else {
      out.push({ kind: "add", text: y[j++] });
    }
  }
  while (i < n) out.push({ kind: "del", text: x[i++] });
  while (j < m) out.push({ kind: "add", text: y[j++] });
  return out;
}

/** Keeps `context` unchanged lines around changes; longer runs collapse to a marker. */
export function withContext(lines: DiffLine[], context = 3): (DiffLine | { kind: "skip"; count: number })[] {
  const keep = new Array(lines.length).fill(false);
  lines.forEach((l, i) => {
    if (l.kind !== "same") for (let k = Math.max(0, i - context); k <= Math.min(lines.length - 1, i + context); k++) keep[k] = true;
  });
  const out: (DiffLine | { kind: "skip"; count: number })[] = [];
  let skipped = 0;
  lines.forEach((l, i) => {
    if (keep[i]) {
      if (skipped) out.push({ kind: "skip", count: skipped });
      skipped = 0;
      out.push(l);
    } else skipped++;
  });
  if (skipped) out.push({ kind: "skip", count: skipped });
  return out;
}
